# 学习路线图

目标：从 Go 基础语法出发，最终独立维护一个功能完整的终端 Agent。

原则：

- **每个阶段结束时，代码都必须是可运行的。** 先跑通，再重构。
- 每阶段只引入少量新知识点，并在真实功能里使用它们。
- 每个阶段都写测试，尤其是涉及协议和并发的地方。

---

## 阶段 1 · 最小可用的流式 REPL ✅

状态：已完成。

交付物：

- `go run ./cmd/agent` 可以和模型连续对话
- 回答逐字流式打印，而不是等全部生成完才显示
- `/help`、`/clear`、`/history`、`/exit`
- 生成中 Ctrl+C 只取消本次回答；等待输入时 Ctrl+C 退出
- 没有 Key、Key 错误、限流时给出清晰提示
- 不联网的单元测试：假 Client + `httptest` 模拟 SSE

涉及的文件：

- `cmd/agent/main.go`
- `internal/config/config.go`
- `internal/llm/client.go`、`internal/llm/openai.go`
- `internal/agent/agent.go`
- `internal/repl/repl.go`

Go 知识点：

- module、package、`internal/` 的可见性
- struct、方法、构造函数
- 接口与依赖注入
- error、`fmt.Errorf` 的 `%w`、`errors.Is` / `errors.As`
- `context` 取消
- `net/http`、`encoding/json`
- `bufio.Scanner` / `bufio.Reader`
- `strings.Builder`、字符串与 rune
- goroutine、channel、`select`、`sync.Mutex`、`os/signal`
- `testing`、`httptest`、接口 mock

练习：

1. 修改 `agent.DefaultSystemPrompt`，让它扮演一个角色（比如 Go 导师）。
2. 给 `/history` 加上"只显示最近 N 条"的参数（提示：`strconv.Atoi`）。
3. 把 `truncate` 改成按终端显示宽度截断（提示：`unicode/utf8`，中文占 2 列）。
4. 故意把 `AGENT_BASE_URL` 指向一个不存在的地址，观察错误链路。
5. 用 `go test -race ./...` 跑一遍测试，理解它在检查什么。

---

## 阶段 1.5 · 全屏 TUI ✅

状态：已完成（从阶段 7 提前，因为界面体验直接影响日常使用）。

交付物：

- 默认启动全屏 TUI，使用备用屏幕，退出后恢复原终端内容
- 对话区自动换行、跟随滚动，支持鼠标滚轮和 `PgUp` / `PgDn`
- 多行输入：`Enter` 发送，`Ctrl+J` 换行
- 状态栏显示“就绪 / 思考中 / 生成中”，带 spinner
- 思考过程默认折叠为“思考 N 字”，`Ctrl+R` 展开
- `Esc` 取消当前生成，空闲时 `Ctrl+C` 退出
- 非 TTY（管道、CI）自动回退 REPL，也可以 `-plain` 强制启用
- 窗口缩放、小终端降级、中文自动换行
- 状态机测试：布局不溢出、流式写入、取消、命令、折叠

涉及文件：

- `cmd/agent/main.go`
- `internal/tui/tui.go`、`model.go`、`view.go`
- `internal/tui/model_test.go`

Go / 工程知识点：

- Bubble Tea 的 Elm 架构：`Init` / `Update` / `View`
- 消息驱动 UI：`tea.Msg`、`tea.Cmd`、`tea.Batch`
- 网络 goroutine + channel 与 UI 解耦，避免阻塞渲染
- `viewport`、`textarea`、`spinner` 组件的组合
- lipgloss 布局、边框和自适应主题色
- Unicode UAX #14 换行、宽字符（中文占 2 列）
- TTY 检测与优雅降级

练习：

1. 修改 `view.go` 里的 `accentColor`，换一套配色，分别看深色/浅色终端。
2. 给状态栏加“本轮耗时”或“已接收 N 字”（提示：`time.Since`、`utf8.RuneCountInString`）。
3. 增加 `/theme` 命令，在两组配色间切换（提示：把包级样式改成 `Model` 字段）。
4. 接入 `glamour`，把完成后的回答渲染成 Markdown（流式阶段先保持纯文本）。
5. 增加“回到最新消息”快捷键（提示：`viewport.GotoBottom()`）。
6. 用 `go test ./internal/tui -run TestViewFitsTerminal -v` 观察不同窗口尺寸。

---

## 阶段 2 · 配置、日志与健壮性

目标：从"能跑的 demo"变成"像样的 CLI 工具"。

要做的事：

1. **命令行参数**：`-plain` 和 TTY 自动回退已经完成；继续用 `flag`
   支持 `-model`、`-base-url`、`-system`、`-debug`，环境变量作为默认值。例如：
   ```bash
   go run ./cmd/agent -model deepseek-reasoner -debug
   ```
2. **日志**：引入 `log/slog`。默认只输出错误；`-debug` 时输出请求耗时、
   状态码、分片数量。日志写到 stderr，不要污染对话输出。
3. **超时**：给每次请求加 `context.WithTimeout`（比如 2 分钟）。
   注意区分"用户 Ctrl+C 取消"和"超时"，错误提示要不同。
4. **重试**：遇到网络错误、429、5xx 时做指数退避重试（1s、2s、4s……），
   最多 3 次；4xx（除 429）不重试。重试期间也要响应 context 取消。
5. **扩展错误类型**：给 `APIError` 加 `RetryAfter`，实现 `Unwrap`，
   用 `errors.As` 在 REPL 里给出针对性建议。
6. 读懂 `repl.watchSignals`：为什么需要 mutex？如果把 `cancel` 改成
   普通字段会发生什么？

Go 知识点：`flag`、`log/slog`、`time`、`context.WithTimeout`、
自定义错误类型、重试循环、`errors.Join`（可选）。

验收：

- 断网、错误 Key、429 时程序不崩溃，提示清晰
- 重试有单元测试：`httptest` 先返回 500，再返回 200，断言最终成功
- `go vet ./...` 和 `go test -race ./...` 通过

进阶挑战：

- 用 `signal.NotifyContext` 简化信号处理。
- 增加 `-list-models`：调用 `GET /models` 列出可用模型。

---

## 阶段 3 · 工具调用：让 agent 动手

目标：接入 OpenAI function calling，让 agent 能读文件、列目录、执行命令。

### 3.1 扩展协议

- `llm.Message` 增加：
  ```go
  ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
  ToolCallID string     `json:"tool_call_id,omitempty"`
  ```
- 增加 `type Tool struct { Type string; Function FunctionDef }` 和
  `FunctionDef{Name, Description, Parameters json.RawMessage}`。
- 请求体增加 `Tools []Tool`（JSON 标签 `tools,omitempty`）。
- 解析流式 `delta.tool_calls`：**这是本阶段最容易踩坑的地方**。
  - 工具调用的 `arguments` 是逐段返回的字符串，需要按 `index` 累积拼接。
  - `id` 和 `name` 通常只在第一段出现，后续片段可能为空。
  - 一轮可能返回多个工具调用。
- 聚合完成后，在 history 里追加一条 `role=assistant` + `tool_calls` 的消息。

### 3.2 实现工具包

新建 `internal/tools`：

```go
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

type Registry struct {
	tools map[string]Tool
}
```

第一批工具：

- `read_file`：读取文本文件（限制大小，比如 256 KiB）
- `list_dir`：列出目录内容
- `write_file`：写入文件（可选，先只允许 workspace 内）
- `run_command`：执行命令（**最后再做**，先只允许白名单，后续加人工确认）

安全要点：

- 所有路径必须限制在 workspace 根目录内：
  `filepath.Clean` + `filepath.Rel` 检查，不能只用字符串前缀。
- 不跟随符号链接，或明确检查 `EvalSymlinks`。
- 工具返回的错误也应作为 `role=tool` 消息回传给模型，让它自己纠正。

### 3.3 Agent 循环

```text
发送消息
  └─> 模型返回 tool_calls?
        ├─ 否：输出最终回答，结束
        └─ 是：依次执行工具 -> 追加 role=tool 消息 -> 再次发送
```

- 设置最大迭代次数（例如 8），防止模型无限调用工具。
- 每轮工具执行结果实时打印（哪个工具、参数、耗时、结果摘要）。
- 历史里保留工具调用记录，模型才能理解上下文。

Go 知识点：嵌套 struct、`json.RawMessage`、递归 JSON、接口注册表、
`filepath`、`os` 文件操作、错误传播。

验收：

- 对 agent 说："看看 README 里写了什么，然后用三句话总结"，它能自动调用 `read_file`。
- 对 agent 说："stat 一下 main.go 有多少行"，它能调用 `run_command`（或专门的工具）。
- `go test ./...` 覆盖工具循环：假 Client 第一轮返回 tool_call，第二轮返回文本。

---

## 阶段 4 · 并发

目标：让 agent 更快，同时理解 Go 的并发模型。

要做的事：

1. **并行执行多个工具**：同一轮返回多个 tool_call 时，用 goroutine 并发执行，
   用 `sync.WaitGroup` 或 `errgroup` 收集结果。注意保持结果顺序与调用顺序一致。
2. **spinner / 状态提示**：模型思考时显示转动的进度指示器：
   ```go
   ticker := time.NewTicker(100 * time.Millisecond)
   defer ticker.Stop()
   ```
   用一个 goroutine 刷新，回答开始时停止。注意避免输出交错。
3. **取消传播**：一个工具失败或用户取消时，取消同轮其他工具
   （`context.WithCancel`，共享 ctx）。
4. **数据竞争**：给 `agent.history` 加锁，或改成"只有主 goroutine 能修改"。
   用 `go test -race ./...` 验证。
5. **限流**：最多同时执行 N 个工具（带缓冲 channel 作信号量）。

Go 知识点：goroutine、channel、`select`、`sync.WaitGroup`、`sync.Mutex`、
`context` 取消树、`errgroup`（引入第一个第三方依赖 `golang.org/x/sync`）、
`go test -race`。

验收：

- 三个各耗时 1 秒的工具并行执行，总耗时约 1 秒。
- 回答生成中按 Ctrl+C，所有工具 goroutine 都能及时退出（无泄漏）。
- `-race` 下测试全部通过。

进阶挑战：

- 实现"边生成边执行"：模型还在输出时，已完成的工具调用先开始执行。
- 用 `runtime.NumGoroutine()` 写一个测试，断言没有 goroutine 泄漏。

---

## 阶段 5 · 测试与质量

目标：让重构不再害怕。

要做的事：

- 给 `llm` 客户端补测试：多个 chunk、`[DONE]`、非 200、坏 JSON、
  连接中断、`reasoning_content`、工具调用分片聚合。
- 给 agent 写表驱动测试：普通回答、工具调用、工具报错、达到最大迭代次数。
- 用 `httptest` 模拟慢响应，测试 context 取消。
- 学习 `t.Run`、`t.Helper`、`t.Cleanup`、`t.TempDir`、`t.Setenv`。
- 基准测试：`go test -bench=. -benchmem`，比较 `strings.Builder`
  和 `+=` 拼接的性能差异。
- 可选：接入 `golangci-lint`，修复所有告警。

验收：

- 核心包测试覆盖率 > 70%：`go test -cover ./...`
- 所有测试不依赖网络和真实 API Key。

---

## 阶段 6 · 会话持久化

目标：关掉终端后还能继续之前的对话。

要做的事：

1. 会话存到 `~/.local/share/goagent/sessions/<name>.jsonl`，
   每行一条消息（JSONL 便于追加）。
2. 命令：
   - `/save <name>` 保存当前会话
   - `/load <name>` 加载会话
   - `/sessions` 列出所有会话
   - `/delete <name>` 删除
3. 启动时自动恢复最近一次会话（可配置）。
4. 写入要安全：先写临时文件，再 `os.Rename` 原子替换，
   防止程序中途退出留下半个文件。
5. 保存时不要重复存 system 消息；加载时校验角色顺序。

Go 知识点：`os.UserHomeDir`、`os.OpenFile` 权限位、`bufio.Writer`、
`encoding/json` 的 `Encoder` / `Decoder`、`filepath`、`io/fs`、
`time.Format`、原子重命名。

验收：

- 退出后重新启动，`/load` 能恢复完整多轮上下文。
- 并发保存同一会话不会产生损坏文件（加文件锁或用唯一临时文件名）。

---

## 阶段 7 · 进阶方向（选一个深入）

| 方向 | 内容 | 主要知识点 |
| --- | --- | --- |
| MCP 客户端 | 用 JSON-RPC over stdio 接入社区 MCP 工具 | 子进程、`os/exec`、JSON-RPC、协议设计 |
| 子 agent | 父 agent 派发任务给子 agent，汇总结果 | 接口、并发、上下文隔离 |
| TUI | 基础版已完成（见阶段 1.5）；可继续做 Markdown 渲染、主题系统、会话侧栏 | Elm 架构、消息循环、终端控制 |
| RAG | 文档切分、embedding、向量检索 | 文件遍历、相似度计算、数据持久化 |
| 多模型路由 | 按任务选择模型，统计 token 与成本 | 配置、策略模式、统计 |
| 发布 | GitHub Actions + goreleaser 交叉编译 | CI、版本注入、跨平台构建 |

---

## 推荐资料

- [A Tour of Go](https://go.dev/tour/) —— 官方交互式入门
- [Effective Go](https://go.dev/doc/effective_go) —— 写法与惯用法
- [Go by Example](https://gobyexample.com/) —— 按主题的小例子
- [Go 标准库文档](https://pkg.go.dev/std) —— 遇到包先查这里
- 《Go 语言圣经》（The Go Programming Language）
- [Go Concurrency Patterns](https://go.dev/blog/pipelines) —— 并发模式
