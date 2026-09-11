# Eino

![coverage](https://raw.githubusercontent.com/cloudwego/eino/badges/.badges/main/coverage.svg)
[![Release](https://img.shields.io/github/v/release/cloudwego/eino)](https://github.com/cloudwego/eino/releases)
[![WebSite](https://img.shields.io/website?up_message=cloudwego&url=https%3A%2F%2Fwww.cloudwego.io%2F)](https://www.cloudwego.io/)
[![License](https://img.shields.io/github/license/cloudwego/eino)](https://github.com/cloudwego/eino/blob/main/LICENSE-APACHE)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloudwego/eino)](https://goreportcard.com/report/github.com/cloudwego/eino)
[![OpenIssue](https://img.shields.io/github/issues/cloudwego/eino)](https://github.com/cloudwego/eino/issues)
[![ClosedIssue](https://img.shields.io/github/issues-closed/cloudwego/eino)](https://github.com/cloudwego/eino/issues?q=is%3Aissue+is%3Aclosed)
![Stars](https://img.shields.io/github/stars/cloudwego/eino)
![Forks](https://img.shields.io/github/forks/cloudwego/eino)

[English](README.md) | [简体中文](README.zh_CN.md) | 繁體中文 | [日本語](README.ja.md)

# 簡介

**Eino['aino]** 是一個 Go 語言的 LLM 應用開發框架，借鑑了 LangChain、Google ADK 等開源專案，並依照 Go 的慣例設計。

Eino 提供：
- **[元件](https://github.com/cloudwego/eino-ext)**：`ChatModel`、`Tool`、`Retriever`、`ChatTemplate` 等可重用模組，官方實作涵蓋 OpenAI、Ollama 等
- **智慧代理開發套件（ADK）**：支援工具呼叫、多代理協同、上下文管理、中斷/恢復等人機互動，以及開箱即用的代理模式
- **編排**：把元件組裝成圖或工作流程，既能獨立執行，也能作為工具供代理呼叫
- **[範例](https://github.com/cloudwego/eino-examples)**：常見模式與實際場景的可執行程式碼

![](.github/static/img/eino/eino_concept.jpeg)

# 快速上手

## ChatModelAgent

設定好 ChatModel，加上工具（可選），就能跑起來：

```Go
chatModel, _ := openai.NewChatModel(ctx, &openai.ChatModelConfig{
    Model:  "gpt-4o",
    APIKey: os.Getenv("OPENAI_API_KEY"),
})

agent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Model: chatModel,
})

runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
iter := runner.Query(ctx, "Hello, who are you?")
for {
    event, ok := iter.Next()
    if !ok {
        break
    }
    fmt.Println(event.Message.Content)
}
```

加上工具，讓代理具備更多能力：

```Go
agent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Model: chatModel,
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{weatherTool, calculatorTool},
        },
    },
})
```

代理內部會自動處理 ReAct 迴圈，自行判斷何時呼叫工具、何時回覆。

→ [ChatModelAgent 範例](https://github.com/cloudwego/eino-examples/tree/main/adk/intro) · [文件](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/agent_implementation/chat_model/)

## DeepAgent

複雜任務用 DeepAgent，它會把問題拆成步驟，分派給子代理，並追蹤進度：

```Go
deepAgent, _ := deep.New(ctx, &deep.Config{
    ChatModel: chatModel,
    SubAgents: []adk.Agent{researchAgent, codeAgent},
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{shellTool, pythonTool, webSearchTool},
        },
    },
})

runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: deepAgent})
iter := runner.Query(ctx, "Analyze the sales data in report.csv and generate a summary chart")
```

DeepAgent 可以設定成：協調多個專業代理、執行 shell 命令、執行 Python、搜尋網路。

→ [DeepAgent 範例](https://github.com/cloudwego/eino-examples/tree/main/adk/multiagent/deep) · [文件](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/agent_implementation/deepagents/)

## 編排

需要精確控制執行流程時，用 `compose` 建構圖或工作流程：

```Go
graph := compose.NewGraph[*Input, *Output]()
graph.AddLambdaNode("validate", validateFn)
graph.AddChatModelNode("generate", chatModel)
graph.AddLambdaNode("format", formatFn)

graph.AddEdge(compose.START, "validate")
graph.AddEdge("validate", "generate")
graph.AddEdge("generate", "format")
graph.AddEdge("format", compose.END)

runnable, _ := graph.Compile(ctx)
result, _ := runnable.Invoke(ctx, input)
```

編排出來的流程可以包裝成工具供代理使用，把確定性流程與自主決策結合起來：

```Go
tool, _ := graphtool.NewInvokableGraphTool(graph, "data_pipeline", "Process and validate data")

agent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Model: chatModel,
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{tool},
        },
    },
})
```

這樣你可以寫出精確可控的業務流程，再讓代理決定何時呼叫。

→ [GraphTool 範例](https://github.com/cloudwego/eino-examples/tree/main/adk/common/tool/graphtool) · [編排文件](https://www.cloudwego.io/zh/docs/eino/core_modules/chain_and_graph_orchestration/)

# 主要特性

## 元件生態

Eino 定義了元件抽象（ChatModel、Tool、Retriever、Embedding 等），官方實作涵蓋 OpenAI、Claude、Gemini、Ark、Ollama、Elasticsearch 等。

→ [eino-ext](https://github.com/cloudwego/eino-ext)

## 串流處理

Eino 在編排中自動處理串流：拼接、裝箱、合併、複製。元件只需實作有業務意義的串流範式，框架處理其餘部分。

→ [文件](https://www.cloudwego.io/zh/docs/eino/core_modules/chain_and_graph_orchestration/stream_programming_essentials/)

## 回呼切面

在固定切點（OnStart、OnEnd、OnError、OnStartWithStreamInput、OnEndWithStreamOutput）注入日誌、追蹤、指標，適用於元件、圖、代理。

→ [文件](https://www.cloudwego.io/zh/docs/eino/core_modules/chain_and_graph_orchestration/callback_manual/)

## 中斷/恢復

任何代理或工具都能暫停等待人工輸入，並從檢查點恢復。框架負責狀態持久化與路由。

→ [文件](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/agent_hitl/) · [範例](https://github.com/cloudwego/eino-examples/tree/main/adk/human-in-the-loop)

# 框架結構

![](.github/static/img/eino/eino_framework.jpeg)

Eino 框架包含：

- Eino（本儲存庫）：型別定義、串流處理機制、元件抽象、編排、代理實作、切面機制

- [EinoExt](https://github.com/cloudwego/eino-ext)：元件實作、回呼處理器、使用範例、評估器、提示詞優化器

- [Eino Devops](https://github.com/cloudwego/eino-ext/tree/main/devops)：視覺化開發與除錯

- [EinoExamples](https://github.com/cloudwego/eino-examples)：範例應用與最佳實踐

## 文件

- [Eino 使用者手冊](https://www.cloudwego.io/zh/docs/eino/)
- [Eino: 快速開始](https://www.cloudwego.io/zh/docs/eino/quick_start/)

## 相依性
- Go 1.18 及以上

## 程式碼規範

本儲存庫使用 `golangci-lint`，本機檢查：

```bash
golangci-lint run ./...
```

規則：
- 匯出的函式、介面、package 等需要 GoDoc 註解
- 程式碼格式符合 `gofmt -s`
- import 順序符合 `goimports`（std -> third party -> local）

## 安全性

如果你在本專案中發現潛在的安全問題，或認為可能發現了安全問題，請透過我們的[安全中心](https://security.bytedance.com/src)
或[漏洞回報信箱](mailto:sec@bytedance.com?subject=關於Eino的反饋)通知字節跳動安全團隊。


請**不要**建立公開的 GitHub Issue。

## 聯絡我們
- 成為 member：[COMMUNITY MEMBERSHIP](https://github.com/cloudwego/community/blob/main/COMMUNITY_MEMBERSHIP.md)
- Issues：[Issues](https://github.com/cloudwego/eino/issues)
- 飛書：掃碼加入 CloudWeGo/eino 使用者群

&ensp;&ensp;&ensp; <img src=".github/static/img/eino/lark_group_zh.png" alt="LarkGroup" width="200"/>

## 開源授權

本專案基於 [Apache-2.0 授權](LICENSE-APACHE) 開源。
