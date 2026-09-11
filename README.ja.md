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

[English](README.md) | [中文](README.zh_CN.md) | 日本語

# 概要

**Eino['aino]** は Go 言語向けの LLM アプリケーション開発フレームワークです。LangChain や Google ADK などのオープンソースフレームワークを参考にしつつ、Go の慣習に沿って設計されています。

Eino が提供するもの：
- **[コンポーネント](https://github.com/cloudwego/eino-ext)**：`ChatModel`、`Tool`、`Retriever`、`ChatTemplate` などの再利用可能な構成要素。OpenAI、Ollama などの公式実装を用意
- **Agent Development Kit（ADK）**：ツール呼び出し、マルチエージェント連携、コンテキスト管理、Human-in-the-Loop 向けの中断/再開、そしてすぐに使えるエージェントパターンで AI エージェントを構築
- **オーケストレーション**：コンポーネントをグラフやワークフローとして組み合わせ、単独で実行することも、エージェントのツールとして公開することも可能
- **[サンプル](https://github.com/cloudwego/eino-examples)**：よく使われるパターンや実際のユースケースの動作するコード

![](.github/static/img/eino/eino_concept.jpeg)

# クイックスタート

## ChatModelAgent

ChatModel を設定し、必要に応じてツールを追加すれば、動作するエージェントができあがります：

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

ツールを追加してエージェントに機能を与えます：

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

エージェントは ReAct ループを内部で処理し、いつツールを呼び出し、いつ応答するかを自ら判断します。

→ [ChatModelAgent のサンプル](https://github.com/cloudwego/eino-examples/tree/main/adk/intro) · [ドキュメント](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_implementation/chat_model/)

## DeepAgent

複雑なタスクには DeepAgent を使います。問題をステップに分解し、サブエージェントに委譲し、進捗を追跡します：

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

DeepAgent は、複数の専門エージェントの連携、シェルコマンドの実行、Python コードの実行、Web 検索を行うように設定できます。

→ [DeepAgent のサンプル](https://github.com/cloudwego/eino-examples/tree/main/adk/multiagent/deep) · [ドキュメント](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_implementation/deepagents/)

## オーケストレーション

実行フローを厳密に制御したい場合は、`compose` を使ってグラフやワークフローを構築します：

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

構築したグラフはエージェントのツールとして公開でき、決定的なワークフローと自律的な振る舞いを橋渡しします：

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

これにより、厳密に制御されたドメイン固有のパイプラインを構築し、それをいつ使うかはエージェントに任せることができます。

→ [GraphTool のサンプル](https://github.com/cloudwego/eino-examples/tree/main/adk/common/tool/graphtool) · [compose のドキュメント](https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/)

# 主な特徴

## コンポーネントエコシステム

Eino はコンポーネントの抽象（ChatModel、Tool、Retriever、Embedding など）を定義し、OpenAI、Claude、Gemini、Ark、Ollama、Elasticsearch などの公式実装を提供します。

→ [eino-ext](https://github.com/cloudwego/eino-ext)

## ストリーム処理

Eino はオーケストレーション全体でストリーミングを自動的に処理します。ノード間でデータが流れる際のストリームの連結、ボックス化、マージ、コピーを担います。コンポーネントは自身にとって意味のあるストリーミングパラダイムだけを実装すればよく、残りはフレームワークが処理します。

→ [ドキュメント](https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/stream_programming_essentials/)

## コールバックアスペクト

コンポーネント、グラフ、エージェントを横断して、固定されたポイント（OnStart、OnEnd、OnError、OnStartWithStreamInput、OnEndWithStreamOutput）にロギング、トレーシング、メトリクスを注入できます。

→ [ドキュメント](https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/callback_manual/)

## 中断/再開

あらゆるエージェントやツールは、人間の入力を待つために実行を一時停止し、チェックポイントから再開できます。状態の永続化とルーティングはフレームワークが処理します。

→ [ドキュメント](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_hitl/) · [サンプル](https://github.com/cloudwego/eino-examples/tree/main/adk/human-in-the-loop)

# フレームワーク構成

![](.github/static/img/eino/eino_framework.jpeg)

Eino フレームワークは以下で構成されています：

- Eino（本リポジトリ）：型定義、ストリーミング機構、コンポーネント抽象、オーケストレーション、エージェント実装、アスペクト機構

- [EinoExt](https://github.com/cloudwego/eino-ext)：コンポーネント実装、コールバックハンドラ、使用例、評価器、プロンプト最適化器

- [Eino Devops](https://github.com/cloudwego/eino-ext/tree/main/devops)：可視化された開発とデバッグ

- [EinoExamples](https://github.com/cloudwego/eino-examples)：サンプルアプリケーションとベストプラクティス

## ドキュメント

- [Eino ユーザーマニュアル](https://www.cloudwego.io/docs/eino/)
- [Eino: クイックスタート](https://www.cloudwego.io/docs/eino/quick_start/)

## 依存関係
- Go 1.18 以上

## コードスタイル

本リポジトリでは `golangci-lint` を使用しています。ローカルでのチェック：

```bash
golangci-lint run ./...
```

適用されるルール：
- エクスポートされる関数、インターフェース、パッケージなどには GoDoc コメントを付ける
- コードは `gofmt -s` でフォーマットする
- import の順序は `goimports` に従う（std -> third party -> local）

## セキュリティ

本プロジェクトで潜在的なセキュリティ上の問題を発見した場合、または発見したと思われる場合は、[セキュリティセンター](https://security.bytedance.com/src)または[脆弱性報告メール](mailto:sec@bytedance.com?subject=Feedback%20On%20Eino)を通じて ByteDance Security にご連絡ください。

公開の GitHub Issue は作成**しないで**ください。

## お問い合わせ
- メンバーシップ：[COMMUNITY MEMBERSHIP](https://github.com/cloudwego/community/blob/main/COMMUNITY_MEMBERSHIP.md)
- Issues：[Issues](https://github.com/cloudwego/eino/issues)
- Lark：[Feishu](https://www.feishu.cn/en/) で下の QR コードをスキャンして CloudWeGo/eino ユーザーグループに参加

&ensp;&ensp;&ensp; <img src=".github/static/img/eino/lark_group_zh.png" alt="LarkGroup" width="200"/>

## ライセンス

本プロジェクトは [Apache-2.0 ライセンス](LICENSE-APACHE)の下で公開されています。
