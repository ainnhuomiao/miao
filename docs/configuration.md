# 配置指南

miao 把常用的编程能力直接内置在 Go 程序里。你可以在配置中选择使用哪些工具、是否联网、是否启用子代理，以及模型如何工作。MCP、hooks 和自定义命令用于额外扩展。

配置文件位于 `~/.miao/config.json`；设置真实环境变量 `MIAO_HOME` 可以改变其目录。文件不存在时使用默认值。可以参考 [完整示例](config.example.json)，只保留需要修改的字段。已存在配置时，合并需要的字段以保留主题、hooks 和子代理设置。

## 修改配置

无需 API Key 即可管理配置：

```bash
miao config keys                          # 可修改的核心字段及类型
miao config path                          # 配置文件位置
miao config set model local-model         # 字符串可直接输入
miao config set parallel_tools 1
miao config set sessions_enabled false
miao config set allowed_tools '[]'         # 禁用模型工具
miao config set disabled_tools '["web_fetch","web_search"]'
miao config unset model                   # 移除用户覆盖
miao config check                         # 校验，不联网、不执行扩展
```

`set` 接受 `config keys` 列出的核心字段；布尔值、数字和数组使用 JSON 语法，字符串也可直接输入普通文本。字符串中的空格按 shell 的规则加引号。`unset` 删除字段，允许环境变量、credentials 或默认值接管。API Key 仍使用 `miao init` 管理。主题、子代理和 hooks 的配置节会保留，修改它们分别使用对应的交互命令或编辑 JSON。

`config check` 检查核心配置、项目配置、子代理、MCP 和 hooks 的格式及约束，不测试服务端能力或连接。无效值会返回非零退出码。配置结构损坏时，`config path` 仍可显示位置以便手动修复；单个已知字段的非法值可尝试通过 `set` 改正或 `unset` 删除。

TUI 输入 `/settings` 打开功能开关菜单，支持 ↑/↓、Tab 筛选补全、Enter 切换、Esc 关闭。菜单使用 `mcp`、`hooks`、`sessions`、`memory`、`commands`、`shell`、`images`、`diagnostics`、`snapshots`、`bell`、`plain` 这些简称。也可直接输入 `/settings images off` 或 `/settings sessions on`。

菜单显示的是已保存的启动设置，修改后重启生效，本次运行继续沿用当前设置。环境变量和命令行覆盖仍然优先。配置修改使用私有原子写入和进程间锁，保留其它字段与精确数值；无效输入、损坏的 JSON 和符号链接目标不会被写入。

## 参数与优先级

核心运行参数按以下顺序生效：命令行 → 真实环境变量 → 项目配置 → 用户配置 → 默认值。项目配置始终受信任规则约束：未信任的仓库只能降低工具轮次、缩短请求超时或打开沙箱；模型、协议和命令放行规则需要 `/trust-project`。用户配置中的新运行参数不写入进程环境。

API Key 单独保存：真实环境变量 → `~/.miao/credentials` → 工作区 `.env` 中的 `MIAO_API_KEY`。Key 来自 credentials 时，其中的地址、模型和协议作为连接默认值，用户配置、真实环境变量和命令行可覆盖。Key 来自环境变量时，不套用 credentials 的连接信息。

| JSON 字段 | 默认值 | 作用 | 环境变量 / 命令行 |
| --- | --- | --- | --- |
| `base_url` | `https://api.deepseek.com` | API 地址 | `MIAO_BASE_URL` / `-base-url` |
| `model` | `deepseek-chat` | 主模型 | `MIAO_MODEL` / `-model` |
| `protocol` | 自动检测 | `auto`、`openai`、`anthropic` | `MIAO_PROTOCOL` / `-protocol` |
| `fallback_model` | 空 | 主模型在尚无输出时失败后的备用模型 | `MIAO_FALLBACK_MODEL` |
| `thinking` | `auto` | 思考设置，受模型能力限制 | `MIAO_THINKING` / `-thinking` |
| `system` | 空 | 追加会话指令，与项目说明一起进入会话基线 | `-system` |
| `mode` | `ask` | TUI、REPL、非交互模式的启动权限：`ask` / `edits` / `plan` | `MIAO_MODE` / `-mode` |
| `parallel_tools` | `8` | 同批只读工具最大并发数；1 表示串行，必须大于 0 | `MIAO_PARALLEL_TOOLS` / `-parallel-tools` |
| `timeout_seconds` | `600` | 单次请求超时秒数，必须大于 0 | `MIAO_TIMEOUT` / `-timeout` |
| `max_retries` | `3` | 失败后的额外重试次数，0 关闭重试 | `MIAO_MAX_RETRIES` / `-retries` |
| `max_tool_rounds` | `32` | 一轮对话的工具循环上限，必须大于 0 | `MIAO_MAX_TOOL_ROUNDS` / `-max-turns`（非交互） |
| `max_output_tokens` | `0` | 默认输出上限，0 沿用协议默认值 | `MIAO_MAX_OUTPUT_TOKENS` / `-max-output-tokens` |
| `auto_compact_percent` | `80` | TUI 自动压缩阈值，1..100；0 关闭 | `-auto-compact` |
| `compact_keep_turns` | `2` | 手动或自动压缩时保留原文的最近轮数，必须大于 0 | `-compact-keep-turns` |
| `allowed_tools` | `null` | 工具允许列表，空数组禁用全部工具 | `-allowed-tools`，逗号分隔 |
| `disabled_tools` | 空列表 | 工具拒绝列表，优先于允许列表 | `-disabled-tools`，逗号分隔 |
| `mcp_enabled` | `true` | 是否读取 MCP 配置并启动服务器 | `-mcp=false` / `-mcp=true` |
| `hooks_enabled` | `true` | 是否加载并运行用户和项目 hooks | `-hooks=false` / `-hooks=true` |
| `sessions_enabled` | `true` | TUI 会话自动保存、恢复和分叉 | `MIAO_SESSIONS_ENABLED` / `-sessions` |
| `project_memory_enabled` | `true` | 启动时加载 AGENTS.md，以及 `/init` | `MIAO_PROJECT_MEMORY_ENABLED` / `-project-memory` |
| `custom_commands_enabled` | `true` | TUI 自定义斜杠命令的发现和展开 | `MIAO_CUSTOM_COMMANDS_ENABLED` / `-custom-commands` |
| `shell_enabled` | `true` | TUI 中直接输入 `!` 执行 shell | `MIAO_SHELL_ENABLED` / `-shell` |
| `images_enabled` | `true` | 图片引用、剪贴板图片与请求中的图片 | `MIAO_IMAGES_ENABLED` / `-images` |
| `diagnostics_enabled` | `true` | 编辑后自动检查 Go/TS 文件 | `MIAO_DIAGNOSTICS_ENABLED` / `-diagnostics` |
| `snapshots_enabled` | `true` | 文件修改的撤销快照 | `MIAO_SNAPSHOTS_ENABLED` / `-snapshots` |
| `bell_enabled` | `true` | 审批或完成时的提示音 | `MIAO_BELL_ENABLED` / `-bell` |
| `plain` | `false` | 是否默认使用逐行 REPL | `-plain` / `-plain=false` |

额外重试仅用于可重试错误，已有输出后不重试，以免出现重复回答。默认输出上限适用于主对话、子代理和摘要请求，`/login` 后保留；请求自身显式指定的输出长度优先。Anthropic 协议必须发送输出长度，未配置时沿用原有的 8192 token 默认值，旧模型启用思考时可能为思考预算增加空间；显式配置的输出上限不会因此扩大。

`mode` 和 `parallel_tools` 是用户级设置，项目配置不能覆盖它们。`ask` 在交互模式弹出审批，非交互模式则拒绝需要审批的操作；`edits` 仅自动允许文件编辑；`plan` 拒绝文件写入和需要审批的命令。修改只读工具并发不会让写文件、命令或联网审批并行；子代理还同时受 `subagents.max` 限制。

JSON 类型错误、非法的数值范围和协议会在启动时明确报错。参数在启动时加载；修改文件后重新启动。`/model`、`/thinking` 的交互修改用于本次运行，`/theme` 和 `/subagents` 沿用各自的持久化设置方式。`false`、`0` 和空数组都是有效的显式配置。

布尔命令行开关使用 `-sessions=false` 这样的形式，环境变量接受 `true` / `false` / `1` / `0`。`MIAO_DIAGNOSE=0` 仍兼容，新环境变量和命令行可覆盖它。`images_enabled` 控制图片功能是否可用，`MIAO_VISION` 单独决定如何识别模型的图片能力；关闭图片功能时，即使 `MIAO_VISION=1` 也不会发送图片。

关闭会话保存后，对话和撤销快照只留在本次进程的内存里；`/new` 会清空它们。关闭 `snapshots_enabled` 则连内存撤销快照也不保存，`/undo` 和文件回退随之不可用。`/export` 是用户主动导出，在会话自动保存关闭时仍可使用。会话开关不删除已有文件，也不改变独立的工具审批审计日志。

`project_memory_enabled` 控制自动项目记忆，已恢复会话原有的内容仍属于其历史。`shell_enabled` 控制直接输入的 `!` 命令，模型的命令执行工具使用 `allowed_tools` / `disabled_tools` 单独控制。关闭自定义命令后不会读取命令模板目录；关闭诊断后不执行检查，也不显示检查被跳过的提示。关闭的会话、项目记忆和撤销命令会从斜杠菜单中隐藏，手动输入时说明关闭原因。

## 选择内置能力

只关闭联网，其他工具照常可用：

```json
{
  "disabled_tools": ["web_fetch", "web_search"],
  "mcp_enabled": false
}
```

只提供本地读取与搜索：

```json
{
  "allowed_tools": ["read_file", "list_dir", "grep", "glob", "repo_map"],
  "mcp_enabled": false,
  "hooks_enabled": false
}
```

最简纯对话：

```json
{
  "plain": true,
  "allowed_tools": [],
  "auto_compact_percent": 0,
  "mcp_enabled": false,
  "hooks_enabled": false,
  "sessions_enabled": false,
  "project_memory_enabled": false,
  "custom_commands_enabled": false,
  "shell_enabled": false,
  "images_enabled": false,
  "diagnostics_enabled": false,
  "snapshots_enabled": false,
  "bell_enabled": false
}
```

允许列表不设置或为 `null` 时不限制工具名；`[]` 表示不提供任何工具。拒绝列表会同时阻止工具定义暴露给模型和实际调用，也适用于启动后注册的 MCP 工具。子代理只从主代理已启用的工具中选择自己的只读工具，`subagents.allow_web` 不能重新打开已禁用的 `web_fetch`。工具开关控制模型调用；直接输入的 `!命令` 等用户操作仍由对应的交互命令处理。

| 能力 | 内置工具名 |
| --- | --- |
| 文件读取与结构 | `read_file`、`list_dir`、`repo_map` |
| 搜索 | `grep`、`glob` |
| 文件修改 | `write_file`、`edit_file` |
| 运行命令和查看后台输出 | `run_command`、`command_output` |
| 联网 | `web_fetch`、`web_search` |
| 任务清单 | `todo_write` |
| 子代理 | `task`，同时需要 `subagents.enabled: true` |

子代理、主题、hooks、自定义斜杠命令、图片输入、命令审批与沙箱的其他配置见 [README](../README.md)。不需要的外部扩展可保持关闭，内置工具可独立使用。

## 子代理与 MCP 的执行限制

子代理设置保存在同一份 `config.json` 的 `subagents` 对象中：

```json
{
  "subagents": {
    "enabled": true,
    "max": 3,
    "timeout_seconds": 120,
    "max_tool_rounds": 16
  }
}
```

`timeout_seconds` 为正数时设置单个任务的超时；省略或为 0 时保持 5 分钟默认值。`max_tool_rounds` 为 0 时不限制工具轮次，正数限制可执行的工具批次；达到上限后仍可返回最终回答，但不会再执行新的工具批次。父任务取消或更短的超时始终优先。通过 `/subagents` 修改模型等设置时会保留这些字段，新任务使用修改后的设置。

每个 MCP 服务器可在 `~/.miao/mcp.json` 中单独设置超时：

```json
{
  "mcpServers": {
    "local": {
      "command": "your-mcp-server",
      "connect_timeout_seconds": 20,
      "call_timeout_seconds": 90
    }
  }
}
```

省略或为 0 时，连接与握手超时默认 10 秒，单次工具调用默认 60 秒。超时均以秒为单位，不接受负数或溢出的值。调整超时保留 MCP 的工具审批和父任务取消规则。

## 检查生效配置

```bash
miao config
miao -model local-model -retries 0 -mcp=false config
miao -disabled-tools web_fetch,web_search config
```

参数放在 `config` 子命令之前。该命令输出 JSON 核心启动配置，不要求 API Key，不发送 API 请求，也不启动 MCP 或 hooks。输出包含用户配置路径、工作区、项目是否可信、Key 是否已配置及其来源；Key 本身、连接 URL 中的用户信息和查询参数会脱敏。主题、子代理和 hooks 的详细内容仍通过各自的交互命令查看。
