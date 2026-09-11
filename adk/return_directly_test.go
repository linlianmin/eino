/*
 * Copyright 2025 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package adk

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	mockModel "github.com/cloudwego/eino/internal/mock/components/model"
	"github.com/cloudwego/eino/schema"
)

// cacheLookupTool simulates a lookup tool that decides at runtime whether its result is
// already the final answer: when the arguments contain "hit", it marks the current call
// as return-directly so the ReAct loop terminates without another model call.
type cacheLookupTool struct {
	name string
}

func (c *cacheLookupTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: c.name, Desc: "looks up a cache"}, nil
}

func (c *cacheLookupTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	if strings.Contains(argumentsInJSON, "hit") {
		if err := SetReturnDirectly(ctx); err != nil {
			return "", err
		}
		return "cached answer", nil
	}
	return "cache miss", nil
}

// streamCacheLookupTool is the streaming counterpart of cacheLookupTool.
type streamCacheLookupTool struct {
	name string
}

func (s *streamCacheLookupTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: s.name, Desc: "streams a cache lookup"}, nil
}

func (s *streamCacheLookupTool) StreamableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (*schema.StreamReader[string], error) {
	if strings.Contains(argumentsInJSON, "hit") {
		if err := SetReturnDirectly(ctx); err != nil {
			return nil, err
		}
	}
	sr, sw := schema.Pipe[string](2)
	go func() {
		defer sw.Close()
		_ = sw.Send("cached ", nil)
		_ = sw.Send("answer", nil)
	}()
	return sr, nil
}

// returnDirectlyOnMarkerMiddleware marks a tool call as return-directly from a middleware,
// based on the tool's output, without the tool itself being aware of the mechanism.
type returnDirectlyOnMarkerMiddleware struct {
	*BaseChatModelAgentMiddleware
	marker string
}

func (m *returnDirectlyOnMarkerMiddleware) WrapInvokableToolCall(_ context.Context, endpoint InvokableToolCallEndpoint, _ *ToolContext) (InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return "", err
		}
		if strings.Contains(out, m.marker) {
			if err := SetReturnDirectly(ctx); err != nil {
				return "", err
			}
		}
		return out, nil
	}, nil
}

func rdToolCallMsg(content, callID, toolName, args string) Message {
	return schema.AssistantMessage(content, []schema.ToolCall{
		{ID: callID, Function: schema.FunctionCall{Name: toolName, Arguments: args}},
	})
}

func rdDrainEvents(iter *AsyncIterator[*AgentEvent]) []*AgentEvent {
	var events []*AgentEvent
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		events = append(events, ev)
	}
	return events
}

// concatEventMessage returns the fully materialized message of an event, concatenating the
// stream when the event carries one.
func concatEventMessage(t *testing.T, ev *AgentEvent) Message {
	require.NotNil(t, ev)
	require.Nil(t, ev.Err)
	require.NotNil(t, ev.Output)
	require.NotNil(t, ev.Output.MessageOutput)
	if !ev.Output.MessageOutput.IsStreaming {
		return ev.Output.MessageOutput.Message
	}
	var chunks []Message
	for {
		chunk, err := ev.Output.MessageOutput.MessageStream.Recv()
		if err != nil {
			break
		}
		chunks = append(chunks, chunk)
	}
	msg, err := schema.ConcatMessages(chunks)
	require.NoError(t, err)
	return msg
}

func TestSetReturnDirectly_DecidesPerToolCall(t *testing.T) {
	ctx := context.Background()

	newAgent := func(t *testing.T, cm model.ToolCallingChatModel) Agent {
		agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
			Name:        "TestAgent",
			Description: "agent whose lookup tool may terminate the loop at runtime",
			Model:       cm,
			ToolsConfig: ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{
					Tools: []tool.BaseTool{&cacheLookupTool{name: "lookup"}},
				},
			},
		})
		require.NoError(t, err)
		return agent
	}

	// The model asks for a lookup that misses, then one that hits. The hit must end the
	// loop, so the model must not be called a third time.
	t.Run("Invoke", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()
		var calls int32
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ []Message, _ ...model.Option) (Message, error) {
				switch atomic.AddInt32(&calls, 1) {
				case 1:
					return rdToolCallMsg("first", "call-1", "lookup", `{"key":"miss"}`), nil
				case 2:
					return rdToolCallMsg("second", "call-2", "lookup", `{"key":"hit"}`), nil
				default:
					return schema.AssistantMessage("should not be reached", nil), nil
				}
			}).Times(2)

		events := rdDrainEvents(NewRunner(ctx, RunnerConfig{Agent: newAgent(t, cm)}).Query(ctx, "q"))
		require.Len(t, events, 4) // assistant, tool(miss), assistant, tool(hit)
		for _, ev := range events {
			require.Nil(t, ev.Err)
		}

		last := events[len(events)-1].Output.MessageOutput.Message
		assert.Equal(t, schema.Tool, last.Role)
		assert.Equal(t, "call-2", last.ToolCallID)
		assert.Equal(t, "cached answer", last.Content)
		assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	})

	t.Run("Stream", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()
		var calls int32
		cm.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ []Message, _ ...model.Option) (MessageStream, error) {
				sr, sw := schema.Pipe[Message](1)
				defer sw.Close()
				switch atomic.AddInt32(&calls, 1) {
				case 1:
					sw.Send(rdToolCallMsg("first", "call-1", "lookup", `{"key":"miss"}`), nil)
				case 2:
					sw.Send(rdToolCallMsg("second", "call-2", "lookup", `{"key":"hit"}`), nil)
				default:
					sw.Send(schema.AssistantMessage("should not be reached", nil), nil)
				}
				return sr, nil
			}).Times(2)

		events := rdDrainEvents(NewRunner(ctx, RunnerConfig{Agent: newAgent(t, cm), EnableStreaming: true}).Query(ctx, "q"))
		require.Len(t, events, 4)

		last := concatEventMessage(t, events[len(events)-1])
		assert.Equal(t, schema.Tool, last.Role)
		assert.Equal(t, "call-2", last.ToolCallID)
		assert.Equal(t, "cached answer", last.Content)
		assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	})

	// When the tool never marks itself, the loop continues to the model's final answer.
	t.Run("NotMarkedContinuesLoop", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(rdToolCallMsg("first", "call-1", "lookup", `{"key":"miss"}`), nil).Times(1)
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(schema.AssistantMessage("final answer", nil), nil).Times(1)

		events := rdDrainEvents(NewRunner(ctx, RunnerConfig{Agent: newAgent(t, cm)}).Query(ctx, "q"))
		require.Len(t, events, 3)
		last := events[len(events)-1].Output.MessageOutput.Message
		assert.Equal(t, schema.Assistant, last.Role)
		assert.Equal(t, "final answer", last.Content)
	})
}

func TestSetReturnDirectly_StreamableTool(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	cm := mockModel.NewMockToolCallingChatModel(ctrl)
	cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()
	cm.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ []Message, _ ...model.Option) (MessageStream, error) {
			sr, sw := schema.Pipe[Message](1)
			defer sw.Close()
			sw.Send(rdToolCallMsg("lookup", "call-1", "stream_lookup", `{"key":"hit"}`), nil)
			return sr, nil
		}).Times(1)

	agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "TestAgent",
		Description: "agent with a streaming lookup tool",
		Model:       cm,
		ToolsConfig: ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{&streamCacheLookupTool{name: "stream_lookup"}},
			},
		},
	})
	require.NoError(t, err)

	events := rdDrainEvents(NewRunner(ctx, RunnerConfig{Agent: agent, EnableStreaming: true}).Query(ctx, "q"))
	require.Len(t, events, 2)

	last := concatEventMessage(t, events[len(events)-1])
	assert.Equal(t, schema.Tool, last.Role)
	assert.Equal(t, "call-1", last.ToolCallID)
	assert.Equal(t, "cached answer", last.Content)
}

// With parallel tool calls, only the marked call terminates the loop, its result is the
// agent's final output, and its event is emitted last, matching ToolsConfig.ReturnDirectly.
func TestSetReturnDirectly_ParallelToolCalls(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	cm := mockModel.NewMockToolCallingChatModel(ctrl)
	cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()
	cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(schema.AssistantMessage("two lookups", []schema.ToolCall{
			{ID: "call-hit", Function: schema.FunctionCall{Name: "lookup", Arguments: `{"key":"hit"}`}},
			{ID: "call-miss", Function: schema.FunctionCall{Name: "lookup", Arguments: `{"key":"miss"}`}},
		}), nil).Times(1)

	agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "TestAgent",
		Description: "agent with parallel lookups",
		Model:       cm,
		ToolsConfig: ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{&cacheLookupTool{name: "lookup"}},
			},
		},
	})
	require.NoError(t, err)

	events := rdDrainEvents(NewRunner(ctx, RunnerConfig{Agent: agent}).Query(ctx, "q"))
	require.Len(t, events, 3)
	for _, ev := range events {
		require.Nil(t, ev.Err)
	}

	miss := events[1].Output.MessageOutput.Message
	assert.Equal(t, "call-miss", miss.ToolCallID)
	assert.Equal(t, "cache miss", miss.Content)

	hit := events[2].Output.MessageOutput.Message
	assert.Equal(t, "call-hit", hit.ToolCallID)
	assert.Equal(t, "cached answer", hit.Content)
}

// SetReturnDirectly can be invoked from a ChatModelAgentMiddleware that inspects the tool
// output, so tools do not need to know about the ADK to benefit from it.
func TestSetReturnDirectly_FromMiddleware(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	cm := mockModel.NewMockToolCallingChatModel(ctrl)
	cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()
	cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(rdToolCallMsg("call", "call-1", "plain", `{}`), nil).Times(1)

	agent, err := NewChatModelAgent(ctx, &ChatModelAgentConfig{
		Name:        "TestAgent",
		Description: "agent whose middleware decides to return directly",
		Model:       cm,
		ToolsConfig: ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{&myTool{name: "plain", desc: "always succeeds"}},
			},
		},
		Handlers: []ChatModelAgentMiddleware{
			&returnDirectlyOnMarkerMiddleware{
				BaseChatModelAgentMiddleware: &BaseChatModelAgentMiddleware{},
				marker:                       "success",
			},
		},
	})
	require.NoError(t, err)

	events := rdDrainEvents(NewRunner(ctx, RunnerConfig{Agent: agent}).Query(ctx, "q"))
	require.Len(t, events, 2)
	for _, ev := range events {
		require.Nil(t, ev.Err)
	}
	last := events[1].Output.MessageOutput.Message
	assert.Equal(t, schema.Tool, last.Role)
	assert.Equal(t, "call-1", last.ToolCallID)
	assert.Equal(t, "success", last.Content)
}

func TestSetReturnDirectly_Agentic(t *testing.T) {
	ctx := context.Background()

	mdl := &sequentialAgenticModel{
		responses: []*schema.AgenticMessage{
			agenticToolCallMsg("lookup", "call-1", `{"key":"miss"}`),
			agenticToolCallMsg("lookup", "call-2", `{"key":"hit"}`),
			agenticMsg("should not be reached"),
		},
	}

	agent, err := NewTypedChatModelAgent(ctx, &TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        t.Name(),
		Description: "agentic agent with a runtime return-directly tool",
		Model:       mdl,
		ToolsConfig: ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{&cacheLookupTool{name: "lookup"}},
			},
		},
	})
	require.NoError(t, err)

	for _, streaming := range []bool{false, true} {
		name := "Invoke"
		if streaming {
			name = "Stream"
		}
		t.Run(name, func(t *testing.T) {
			atomic.StoreInt32(&mdl.callCount, 0)
			runner := NewTypedRunner(TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: streaming})
			events := drainAgenticEvents(runner.Query(ctx, "q"))
			require.NoError(t, firstAgenticEventError(events))
			assert.Equal(t, int32(2), atomic.LoadInt32(&mdl.callCount))

			last := lastAgenticEvent(events)
			require.NotNil(t, last)
			require.NotNil(t, last.Output)
			require.NotNil(t, last.Output.MessageOutput)

			var msg *schema.AgenticMessage
			if last.Output.MessageOutput.IsStreaming {
				for {
					chunk, recvErr := last.Output.MessageOutput.MessageStream.Recv()
					if recvErr != nil {
						break
					}
					msg = chunk
				}
			} else {
				msg = last.Output.MessageOutput.Message
			}
			require.NotNil(t, msg)
			_, callID := extractToolIdentifiers(msg)
			assert.Equal(t, "call-2", callID)
		})
	}
}

func TestSetReturnDirectly_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("OutsideToolCall", func(t *testing.T) {
		err := SetReturnDirectly(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool call ID not found")
	})

	// A bare compose.ToolsNode provides a tool call ID but no ChatModelAgent state.
	t.Run("OutsideChatModelAgent", func(t *testing.T) {
		tn, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{&cacheLookupTool{name: "lookup"}},
		})
		require.NoError(t, err)

		_, err = tn.Invoke(ctx, rdToolCallMsg("call", "call-1", "lookup", `{"key":"hit"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ChatModelAgent state not found")
	})
}
