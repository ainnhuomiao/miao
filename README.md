# GoAgent（agnet）

用 Go 写的终端 AI Agent，同时也是我的 Go 语言学习项目。

核心的模型调用、SSE 流式解析、对话历史和 Agent 逻辑全部只用标准库手写；
界面提供两种形态：

- **默认：全屏 TUI**（基于 Bubble Tea 生态），带对话区、输入框、滚动和状态栏
- **`-plain`：逐行 REPL**（纯标准库），适合管道、脚本和阅读最小实现

## 界面

```text
 GoAgent · deepseek-chat                                             ● 就绪
 ────────────────────────────────────────────────────────────────────────────
  · 已就绪。输入消息开始对话，输入 /help 查看命令。
  ▌ 你
  用一句话解释 goroutine

  ▌ GoAgent
  思考 128 字 · Ctrl+R 展开
  Goroutine 是 Go 运行时调度的轻量级执行单元，比操作系统线程便宜得多……

  ▌ 你
  再举一个并发的例子
  ▌ GoAgent
  下面是一个用 channel 汇总结果的例子……

╭────────────────────────────────────────────────────────────────────────────╮
│ › 输入消息，Enter 发送…                                                    │
╰────────────────────────────────────────────────────────────────────────────╯
 Enter 发送 · Ctrl+J 换行 · Ctrl+R 思考 · PgUp/PgDn 滚动 · Ctrl+C 退出
```

## 功能

- 多轮对话，回答**逐字流式输出**（手写 SSE 解析）
- **工具调用（一期）**：`read_file` / `list_dir`，工作区路径锁定；主区工具卡 + `Tab` 右侧抽屉
- 左侧会话侧栏接口已预留（`[` / `]` 一期 noop）
- 全屏 TUI：对话记录自动换行、鼠标滚轮 / `PgUp` / `PgDn` 滚动
- 推理模型（如 `deepseek-reasoner`）的思考过程可折叠，`Ctrl+R` 展开
- 生成中按 `Esc` **只取消当前回答**；空闲时 `Ctrl+C` 退出
- 斜杠命令：`/help`、`/clear`、`/reasoning`、`/exit`
- 没有终端（管道、重定向、CI）时自动退回逐行 REPL
- 不联网的单元测试：假 Client + `httptest` 模拟 SSE + TUI 状态机测试

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

> 如果下载依赖很慢，可以使用 Go 模块镜像：
>
> ```bash
> GOPROXY=https://goproxy.cn,direct go mod download
> ```

### 纯文本模式

```bash
go run ./cmd/agent -plain
```

或者在管道 / 重定向场景下自动启用：

```bash
printf '用一句话解释 goroutine\n' | go run ./cmd/agent
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

## 使用

### TUI 快捷键

| 按键 | 作用 |
| --- | --- |
| `Enter` | 发送消息 |
| `Ctrl+J` | 输入换行 |
| `Ctrl+R` | 展开 / 收起思考过程 |
| `PgUp` / `PgDn` | 向上 / 向下滚动对话 |
| 鼠标滚轮 | 滚动对话 |
| `Esc` | 取消正在生成的回答 |
| `Ctrl+C` | 生成中：取消；空闲时：退出 |

> 启用鼠标后，部分终端需要用 `Shift` + 拖拽来选中复制文本。

### 斜杠命令

| 命令 | 作用 |
| --- | --- |
| `/help` | 显示帮助 |
| `/clear` | 清空对话历史，开始新会话 |
| `/reasoning` | 展开或收起思考过程 |
| `/exit` 或 `/quit` | 退出 |

### 开发命令

```bash
make run     # go run ./cmd/agent
make build   # 构建到 bin/agent
make test    # 运行全部测试
make check   # go fmt + go vet + go test
```

## 项目结构

```text
agnet/
├── cmd/agent/main.go        # 入口：flag、TTY 检测、组装依赖
├── internal/
│   ├── config/config.go     # 环境变量 + .env 读取
│   ├── llm/
│   │   ├── client.go        # Client 接口、消息类型
│   │   ├── openai.go        # OpenAI 兼容协议 + SSE 流式解析
│   │   └── openai_test.go   # httptest 模拟服务端
│   ├── agent/
│   │   ├── agent.go         # 对话历史、调用模型、失败回滚
│   │   └── agent_test.go    # 假 Client 测试
│   ├── tui/
│   │   ├── tui.go           # 启动 Bubble Tea
│   │   ├── model.go         # 状态机：消息、流式事件、命令
│   │   ├── view.go          # 布局、样式、CJK 换行
│   │   └── model_test.go    # 状态机和布局测试
│   └── repl/repl.go         # 纯标准库的逐行 REPL
├── docs/ROADMAP.md          # 分阶段学习路线
├── .env.example
├── Makefile
└── go.mod
```

依赖方向：

```text
              ┌──────────────┐
              │ cmd/agent    │
              └──┬────────┬──┘
                 │        │
        ┌────────▼──┐  ┌──▼─────────┐
        │ internal/ │  │ internal/  │
        │    tui    │  │    repl    │
        └────────┬──┘  └──┬─────────┘
                 └────┬───┘
                      ▼
               internal/agent ──> internal/llm ──> HTTP / SSE
```

`internal/` 是 Go 的特殊目录：只有同一个 module 内的代码可以 import 它，
适合放不打算对外暴露的实现。

### 为什么用接口？

`internal/llm/client.go` 里的 `Client` 是一个接口：

```go
type Client interface {
	Stream(ctx context.Context, messages []Message, onDelta DeltaHandler) error
}
```

`agent` 只依赖这个接口，不关心背后是 DeepSeek 还是 Ollama。
测试时就能注入假的 `fakeClient`，不用真的联网、也不花钱。
这是 Go 里最常用的解耦方式：“接受接口，返回结构体”。

### 为什么 TUI 用 Bubble Tea？

自己从零实现一个全屏终端编辑器（光标、换行、滚动、粘贴、宽字符）工作量很大，
而且容易在边角情况上出错。Bubble Tea 提供了成熟的 Elm 架构和现成组件：

- `viewport`：可滚动的对话区
- `textarea`：支持多行编辑的输入框
- `spinner`：生成中的动画
- `lipgloss`：自适应终端主题的样式

聊天核心仍然只依赖标准库，TUI 只是一个可替换的“外壳”。

## 一次请求的完整流程

1. TUI 在 `Enter` 后调用 `agent.Send`，并把输入框清空。
2. `agent` 把用户消息追加到 `history`，拼上 system 提示词，
   调用 `llm.Client.Stream`。
3. `OpenAIClient.Stream` 用 `net/http` 发 POST 请求，`context` 负责取消，
   逐行读取 SSE：`data: {...}`。
4. 每个分片的 `delta.content` 通过回调写入 channel；TUI 的 `Update`
   收到 `streamEventMsg` 后追加到消息并重绘。
5. 流结束后，`agent` 把完整回答写入历史；失败或取消则回滚用户消息。
6. TUI 收到 done 事件，停止 spinner，恢复输入状态。

`-plain` 模式下第 1、4 步换成 `repl` 的同步读取和直接打印，
核心的 2、3、5 步完全复用。

## 已经用到的 Go 知识点

| 文件 | 知识点 |
| --- | --- |
| `cmd/agent/main.go` | `flag`、TTY 检测、依赖组装、`os.Exit` |
| `internal/config` | 环境变量、文件读取、`bufio.Scanner`、`strings.Cut`、`defer` |
| `internal/llm/client.go` | 类型定义、常量、接口、函数类型 |
| `internal/llm/openai.go` | `net/http`、`encoding/json`、SSE、`context`、`%w` 错误包装 |
| `internal/agent/agent.go` | struct、构造函数、切片、`strings.Builder`、错误回滚 |
| `internal/tui` | Bubble Tea Elm 架构、消息循环、goroutine + channel、ANSI 样式、Unicode 换行 |
| `internal/repl` | goroutine、`select`、`sync.Mutex`、`os/signal`、rune 截断 |
| `*_test.go` | `testing`、`httptest`、接口 mock、`errors.Is` / `errors.As`、布局断言 |

## 依赖说明

核心包（`llm`、`agent`、`config`、`repl`）只用标准库。
TUI 引入了 Charm 生态的依赖：

- `github.com/charmbracelet/bubbletea` —— 事件循环和 Elm 架构
- `github.com/charmbracelet/bubbles` —— viewport / textarea / spinner
- `github.com/charmbracelet/lipgloss` —— 终端样式和布局
- `github.com/mattn/go-runewidth`、`github.com/rivo/uniseg` —— 宽字符和 Unicode 换行

## 学习路线

详细的分阶段计划、每阶段要写的代码、练习和验收标准见 **[docs/ROADMAP.md](docs/ROADMAP.md)**。

当前进度：

- [x] **阶段 1 · 最小可用的流式 REPL**
- [x] **阶段 1.5 · 全屏 TUI**（从阶段 7 提前）
- [ ] 阶段 2 · 配置、日志与健壮性（`-plain` 和 TTY 检测已完成）
- [ ] 阶段 3 · 工具调用（让 agent 真正动手）
- [ ] 阶段 4 · 并发
- [ ] 阶段 5 · 测试与质量
- [ ] 阶段 6 · 会话持久化
- [ ] 阶段 7 · 进阶方向

### 建议的阅读顺序

1. `cmd/agent/main.go` —— 程序从哪里开始
2. `internal/config/config.go` —— 配置怎么来的
3. `internal/llm/client.go` —— 接口定义了哪些能力
4. `internal/llm/openai.go` —— 最核心的 HTTP + SSE
5. `internal/agent/agent.go` —— 对话历史怎么维护
6. `internal/tui/model.go` —— 状态机如何驱动流式 UI
7. `internal/tui/view.go` —— 布局、样式和中文换行
8. `internal/repl/repl.go` —— 对照阅读最小实现
9. 各包的 `*_test.go` —— 怎么不联网测试

### 几个可以立刻做的实验

- 修改 `agent.DefaultSystemPrompt`，让它扮演你喜欢的角色。
- 把 `AGENT_MODEL` 改成 `deepseek-reasoner`，按 `Ctrl+R` 观察思考过程。
- 修改 `internal/tui/view.go` 里的 `accentColor`，换一套配色。
- 故意把 API Key 改错，观察 401 错误在 TUI 里如何显示。
- 把终端缩到很窄，看看布局如何降级。
- 运行 `go test ./...`，然后故意改坏一个测试，理解测试失败的输出。

## 环境要求

- Go 1.24.2 及以上（本项目在 Go 1.26.8 上开发验证）
- 一个可用的模型 API Key（DeepSeek 或任何 OpenAI 兼容服务）
- 现代终端（支持真彩色；TUI 需要真正的 TTY，否则自动使用 REPL）
