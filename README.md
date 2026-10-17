# GoAgent（agnet）

用 **纯 Go 标准库** 从零实现的终端 AI Agent，同时是我的 Go 语言学习项目。

当前版本已经可以：

- 和 DeepSeek（或任何 OpenAI 兼容接口）进行**多轮对话**
- **逐字流式输出**回答（自己手写 SSE 解析）
- 使用 `/help`、`/clear`、`/history`、`/exit` 等命令
- 生成回答时按 **Ctrl+C 只取消当前回答**，等待输入时按 Ctrl+C 退出
- 使用 `deepseek-reasoner` 时，用暗色显示思考过程

整个项目**没有任何第三方依赖**，HTTP、SSE、JSON、REPL 全部由标准库手写。

---

## 快速开始

### 1. 准备 API Key

到 <https://platform.deepseek.com/api_keys> 申请一个 Key。

### 2. 配置

```bash
cp .env.example .env
# 编辑 .env，把 AGENT_API_KEY 换成你的 Key
```

`.env` 已经在 `.gitignore` 里，不会被提交。

### 3. 运行

```bash
go run ./cmd/agent
```

也可以不用 `.env`，直接通过环境变量启动：

```bash
export AGENT_API_KEY=sk-xxxx
go run ./cmd/agent
```

启动后大致是这样：

```text
GoAgent · 用 Go 写的终端 AI 助手
模型: deepseek-chat
输入 /help 查看命令；等待输入时按 Ctrl+C 退出

你 › 用一句话解释 goroutine
agent › Goroutine 是 Go 运行时管理的轻量级线程……
```

### 换其他模型服务

只要兼容 OpenAI Chat Completions 协议，改 `.env` 即可：

| 服务 | `AGENT_BASE_URL` | `AGENT_MODEL` 示例 |
| --- | --- | --- |
| DeepSeek | `https://api.deepseek.com` | `deepseek-chat` / `deepseek-reasoner` |
| OpenAI | `https://api.openai.com/v1` | `gpt-4o-mini` |
| Ollama 本地 | `http://localhost:11434/v1` | `qwen2.5:7b` |
| 其他兼容服务 | 服务商提供的前缀 | 服务商提供的模型名 |

Ollama 不需要真正的 Key，`AGENT_API_KEY` 随便填一个非空值（例如 `ollama`）即可。

---

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `/help` | 显示帮助 |
| `/clear` | 清空对话历史，开始新会话 |
| `/history` | 查看当前对话历史 |
| `/exit` 或 `/quit` | 退出 |
| `Ctrl+C` | 生成中：取消本次回答；等待输入时：退出 |

开发命令：

```bash
make run     # go run ./cmd/agent
make build   # 构建到 bin/agent
make test    # 运行全部测试
make check   # go fmt + go vet + go test
```

---

## 项目结构

```text
agnet/
├── cmd/agent/main.go        # 程序入口：读配置、组装依赖、启动 REPL
├── internal/
│   ├── config/config.go     # 环境变量 + .env 读取
│   ├── llm/
│   │   ├── client.go        # Client 接口、消息类型
│   │   ├── openai.go        # OpenAI 兼容协议 + SSE 流式解析
│   │   └── openai_test.go   # 用 httptest 模拟服务端
│   ├── agent/
│   │   ├── agent.go         # Agent 核心：对话历史、调用模型、失败回滚
│   │   └── agent_test.go    # 用假 Client 测试，不联网
│   └── repl/repl.go         # 终端交互：输入、着色、命令、Ctrl+C
├── docs/ROADMAP.md          # 分阶段学习路线
├── .env.example
├── Makefile
└── go.mod
```

依赖方向是单向的：

```text
main ──> repl ──> agent ──> llm
          ▲          │         │
          └─ 流式输出 ┘◄─ SSE ──┘
```

`internal/` 是 Go 的特殊目录：只有同一个 module 内的代码可以 import 它，
可以用来放不打算对外暴露的实现。

### 为什么用接口？

`internal/llm/client.go` 里的 `Client` 是一个接口：

```go
type Client interface {
	Stream(ctx context.Context, messages []Message, onDelta DeltaHandler) error
}
```

`agent` 只依赖这个接口，不关心背后是 DeepSeek 还是 Ollama。
测试时就能注入一个假的 `fakeClient`，不用真的联网、也不花钱。
这是 Go 里最常用的解耦方式：“接受接口，返回结构体”。

---

## 一次请求的完整流程

1. `repl.Run` 打印 `你 ›`，用 `bufio.Reader` 读取一行输入。
2. 输入以 `/` 开头就当作命令处理；否则调用 `agent.Send`。
3. `agent.Send` 把用户消息追加到 `history`，拼上 system 提示词，
   调用 `llm.Client.Stream`。
4. `OpenAIClient.Stream` 用 `net/http` 发 POST 请求，`context` 负责取消，
   然后逐行读取 SSE：`data: {...}`。
5. 每个 JSON 分片里的 `delta.content` 通过回调一路传回 REPL，立即打印。
6. 流结束后，`agent` 把完整回答写入历史；如果中途失败或被取消，
   则回滚这条用户消息，避免留下半轮对话。
7. REPL 打印换行，回到第 1 步。

---

## 已经用到的 Go 知识点

| 文件 | 知识点 |
| --- | --- |
| `cmd/agent/main.go` | `package main`、`os.Exit`、依赖组装 |
| `internal/config` | 环境变量、文件读取、`bufio.Scanner`、`strings.Cut`、`defer` |
| `internal/llm/client.go` | 类型定义、常量、接口、函数类型 |
| `internal/llm/openai.go` | `net/http`、`encoding/json`、SSE、`context`、`%w` 错误包装、`defer` |
| `internal/agent/agent.go` | struct、构造函数、切片、`strings.Builder`、错误回滚 |
| `internal/repl` | goroutine、channel、`select`、`sync.Mutex`、`os/signal`、rune 截断 |
| `*_test.go` | `testing`、`httptest`、接口 mock、`errors.Is` / `errors.As` |

---

## 学习路线

详细的分阶段计划、每阶段要写的代码、练习和验收标准见 **[docs/ROADMAP.md](docs/ROADMAP.md)**。

当前进度：

- [x] **阶段 1 · 最小可用的流式 REPL**
- [ ] 阶段 2 · 配置、日志与健壮性
- [ ] 阶段 3 · 工具调用（让 agent 真正动手）
- [ ] 阶段 4 · 并发
- [ ] 阶段 5 · 测试与质量
- [ ] 阶段 6 · 会话持久化
- [ ] 阶段 7 · 进阶方向

### 建议的阅读顺序

1. `cmd/agent/main.go` —— 先看程序从哪里开始
2. `internal/config/config.go` —— 配置怎么来的
3. `internal/llm/client.go` —— 接口定义了哪些能力
4. `internal/llm/openai.go` —— 最核心的 HTTP + SSE
5. `internal/agent/agent.go` —— 对话历史怎么维护
6. `internal/repl/repl.go` —— 终端交互和信号处理
7. 两个 `*_test.go` —— 怎么不联网测试

### 几个可以立刻做的实验

- 修改 `agent.DefaultSystemPrompt`，让它扮演你喜欢的角色。
- 故意把 API Key 改错，观察 401 错误是怎么被包装成友好提示的。
- 在 `internal/llm/openai.go` 的流循环里加一行日志，看每个 SSE 分片长什么样。
- 把 `AGENT_MODEL` 改成 `deepseek-reasoner`，观察思考过程的显示。
- 运行 `go test ./...`，然后故意改坏一个测试，理解测试失败的输出。

---

## 环境要求

- Go 1.22 及以上（本项目在 Go 1.26.8 上开发验证）
- 一个可用的模型 API Key（DeepSeek 或任何 OpenAI 兼容服务）
