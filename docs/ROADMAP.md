# 学习路线图

目标：从 Go 基础语法出发，最终独立维护一个功能完整的终端 Agent。

原则：

- **每个阶段结束时，代码都必须是可运行的。** 先跑通，再重构。
- 每阶段只引入少量新知识点，并在真实功能里使用它们。
- 每个阶段都写测试，尤其是涉及协议和并发的地方。

环境变量前缀是 `MIAO_*`（连接配置仍兼容旧名 `AGENT_*`，其它变量兼容 `AGNET_*`）。

---

## 阶段 1 · 最小可用的流式 REPL ✅

状态：已完成。

交付物：

- `go run ./cmd/miao` 可以和模型连续对话
- 回答逐字流式打印，而不是等全部生成完才显示
- `/help`、`/clear`、`/history`、`/exit`
- 生成中 Ctrl+C 只取消本次回答；等待输入时 Ctrl+C 退出
- 没有 Key、Key 错误、限流时给出清晰提示
- 不联网的单元测试：假 Client + `httptest` 模拟 SSE

涉及的文件：

- `cmd/miao/main.go`
- `internal/config/config.go`
- `internal/llm/client.go`、`internal/llm/openai.go`
- `internal/agent/agent.go`
- `internal/repl/repl.go`

---

## 阶段 1.5 · 全屏 TUI ✅

状态：已完成。

交付物：全屏 Bubble Tea TUI、流式输出、思考折叠、非 TTY 回退 REPL、主题、侧栏。

---

## 阶段 2 · 配置、日志与健壮性 ✅

状态：已完成。

- 命令行：`-plain`、`-model`、`-base-url`、`-system`、`-debug`、`-list-models`、`-timeout`、`-protocol`、`-c`/`-resume`
- `log/slog`：`-debug` 时输出请求耗时和状态码
- 每次请求 `context.WithTimeout`，错误区分用户取消和超时
- 429 / 5xx / 网络错误指数退避重试，尊重 `Retry-After`
- `APIError` 实现 `Unwrap`，带 `RetryAfter`

---

## 阶段 3 · 工具调用 ✅

状态：已完成。

- `read_file`、`list_dir`、`grep`、`glob`、`write_file`、`edit_file`、`run_command`、`web_fetch`、`web_search`、`todo_write`、`task`
- 工作区路径锁定 + 符号链接检查；`.env` 等敏感文件不可读
- `run_command` 白名单只覆盖只读用法；越界路径、`--pre`、`uniq` 写文件、`git --no-index` 都要审批
- `go test` / `go build` 会编译并运行仓库代码（有意允许，见 README）
- 工具循环上限可配置（默认 32），超限保留历史并提示继续
- 可选 `MIAO_SANDBOX=1` + bubblewrap

---

## 阶段 4 · 并发 ✅

状态：已完成。

- 只读工具并行；写文件和命令串行
- Agent 历史用 mutex 保护（TUI 后台 goroutine 写、UI 线程读）
- 取消传播、`go test -race`

---

## 阶段 5 · 测试与质量 ✅

状态：已完成。

- `httptest` 覆盖流式协议、重试、超时
- `golangci-lint` + GitHub Actions CI
- `make check`：gofmt、vet、test、race、lint

---

## 阶段 6 · 会话持久化 ✅

状态：已完成。

- 会话存到 `$XDG_DATA_HOME/miao/sessions/<id>.json`
- 侧栏切换、`/fork`、`/load`、`-c`/`--resume` 恢复最近一次
- 原子写入；侧栏列表只解析元数据
- 会话落盘保存原文（目录 0700）；`/export` 时脱敏 API Key 等
- `/export` 导出 Markdown

---

## 阶段 7 · 进阶 ✅（持续迭代）

已落地：

| 方向 | 状态 |
| --- | --- |
| MCP 客户端 | ✅ stdio，兼容 Claude Desktop / Cursor 的 `mcp.json` |
| 子 agent | ✅ `task` 工具，只读，可配置并发/模型/联网 |
| TUI | ✅ Markdown、主题、会话侧栏、审批卡、diff |
| 多模型 | ✅ `/model`、OpenAI 兼容 + Anthropic Messages（含 prompt cache 标记）、主模型失败回退 |
| 成本 | ✅ 按模型单价估算，`/cost` 按会话累计 |
| 规划执行 | ✅ 只读规划 + `/apply` 切到编辑并执行 |
| 仓库地图 | ✅ `repo_map`（go/ast） |
| 发布 | ✅ GitHub Actions + goreleaser + nix flake check |
| Headless | ✅ `-output-format json\|stream-json`、`-max-turns`、`-allowed-tools` |
| 诊断 | ✅ `miao doctor`；写 Go/TS 后附加 vet/tsc |

还可以继续做：Landlock 沙箱、OpenAI Responses API、MCP HTTP/SSE、可写子代理、ACP、RAG、Homebrew。

---

## 推荐资料

- [A Tour of Go](https://go.dev/tour/)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go by Example](https://gobyexample.com/)
- [Go 标准库文档](https://pkg.go.dev/std)
- 《Go 语言圣经》（The Go Programming Language）
- [Go Concurrency Patterns](https://go.dev/blog/pipelines)
