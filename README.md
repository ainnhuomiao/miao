# miao

终端里的 AI 编程助手，用 Go 编写。以轻量、简约、可配置为核心：常用能力内置，工具、联网、子代理和扩展由你按需调整。支持 OpenAI Chat Completions 兼容服务，以及 Anthropic Messages API。

```text
 /\_/\ 
( ･ω･ )   miao
 > ^ <    输入 / 看命令 · ? 看快捷键
```

## 功能

- 全屏 TUI（Bubble Tea），回答流式输出；`-plain` 逐行 REPL；`-p` 非交互模式，方便脚本和管道
- 内置工具：`read_file`、`list_dir`、`grep`、`glob`、`repo_map`、`write_file`、`edit_file`、`run_command`、`web_fetch`、`web_search`、`todo_write`、`task`；路径锁定在工作区内，不跟随越界符号链接，不读取 `.env`
- 只读工具并行执行；文件编辑审批卡显示 diff，`B` 允许本轮其余写入
- 任务清单（todo），`Ctrl+T` 查看
- 只读子代理（`task` 工具），可配置并发、模型和是否允许联网
- MCP 客户端（stdio），兼容 Claude Desktop / Cursor 的 `mcp.json` 格式
- Hooks：工具调用前后、回合结束时执行 shell 命令
- 自定义斜杠命令：一个 Markdown 文件就是一条命令
- 会话持久化：侧栏切换、`/fork` 分叉、`Esc Esc` 回到之前的消息重新编辑（rewind）
- `/undo` 撤销文件修改，`/diff` 查看改动，`/commit` 让模型写提交信息
- 图片输入：`Ctrl+V` 粘贴剪贴板图片，或用 `@` 引用图片文件；`@` 也能引用普通文件
- `/model` 运行时切换模型，`/thinking` 调整模型思考强度
- `/compact` 压缩上下文（默认用量到 80% 时自动触发，可调整阈值或关闭；先折叠早期工具输出，摘要请求复用当前对话前缀）；`/export` 导出 Markdown 到数据目录（会脱敏）
- `/apply` 确认只读规划方案并切到编辑模式执行；`/cost` 查看估算花费；`/init` 生成 `AGENTS.md`
- 首次启动配置向导、`miao init`、`/login` 换 key；Key 存在 `~/.miao/credentials`
- `!<命令>` 在工作区直通执行 shell；`miao doctor` 检查 API、bwrap、剪贴板和终端
- `-c` / `--resume` 启动时恢复最近一次会话
- 自动加载工作区里的 `AGENTS.md` 作为项目记忆
- 三种权限模式，`Shift+Tab` 切换
- 多套主题，`/theme` 切换并持久化
- 用户级 JSON 管理核心运行参数；工具允许/禁用列表、MCP 和 hooks 开关；`miao config` 查看生效的核心配置
- 会话保存、项目记忆、自定义命令、`!` shell、图片、编辑后诊断、撤销快照和提示音均可独立关闭
- `/settings` 用菜单保存功能开关；`miao config set/unset/check` 修改或校验配置，无需 Key

## 安装

用 Nix（详见 [docs/nix.md](docs/nix.md)）：

```bash
nix run github:ainnhuomiao/miao   # 直接运行
nix run .                        # 在克隆下来的仓库里运行
```

用 Go（1.26 及以上）：

```bash
go install github.com/ainnhuomiao/miao/cmd/miao@latest
```

或者从源码构建：

```bash
git clone https://github.com/ainnhuomiao/miao.git
cd miao
go build ./cmd/miao     # 或 make build，产物在 bin/miao
```

## 首次配置

第一次在终端里运行 `miao`，没找到 API Key 时会打开配置向导：选服务商（DeepSeek / Anthropic / OpenAI 兼容）→ 粘贴 key（只显示 `•`）→ 自动测试连接，连上就进入正常界面。也可以随时运行：

```bash
miao init     # 同样的流程，逐行提示
```

在 TUI 里输入 `/login` 可以随时换 key 或服务商。Key 保存在 `~/.miao/credentials`（权限 0600，原子写入，拒绝符号链接），不会写进工作区 `.env`，也不会出现在界面、会话记录、日志或 `/export` 里。

非交互场景（`-p`、管道、没有 TTY）不会打开向导，没有 key 时直接报错；请先运行 `miao init`，或设置环境变量 `MIAO_API_KEY`。

API Key 的查找顺序：环境变量 `MIAO_API_KEY` → `~/.miao/credentials` → 当前目录 `.env`（旧用法，仍兼容）。工作区 `.env` 是白名单：**只接受 `MIAO_API_KEY`**，而且只读进 miao 自己的配置，不会写入进程环境（子进程、工具命令都看不到）。其它键——`MIAO_HOME`、`MIAO_WORKSPACE`、`MIAO_ALLOW_COMMANDS`、`MIAO_BASE_URL`、`LD_PRELOAD`、`HTTPS_PROXY`、`GIT_CONFIG_*` 等——一律忽略，启动时用一行灰字列出被忽略的键名（不显示值）。连接地址可在用户级 `config.json`、真实环境变量、credentials 或命令行中设置；`MIAO_HOME`、`MIAO_WORKSPACE` 仍只从真实环境变量读取，避免不可信仓库借 `.env` 改家目录、绕过 `/trust-project` 或把 API Key 转到别处。

| 变量 | 作用 | 默认值 |
| --- | --- | --- |
| `MIAO_API_KEY` | API Key（优先于 `~/.miao/credentials`） | 无 |
| `MIAO_BASE_URL` | OpenAI 兼容接口地址 | `https://api.deepseek.com` |
| `MIAO_MODEL` | 模型名 | `deepseek-chat` |
| `MIAO_WORKSPACE` | 工具可访问的工作区根目录 | 当前目录 |
| `MIAO_HOME` | 用户级目录 | `~/.miao` |

```bash
# 不想用向导时，直接用环境变量：
export MIAO_API_KEY=sk-xxxx
export MIAO_BASE_URL=https://api.example.com/v1
export MIAO_MODEL=<模型名>
miao
```

其它可选变量：

| 变量 | 作用 |
| --- | --- |
| `MIAO_MODELS` | `/model` 菜单里的候选模型，逗号分隔（不设则向服务端拉取列表） |
| `MIAO_THINKING` | 思考强度，默认 `auto`（使用服务端默认）；也可用 `-thinking` 覆盖 |
| `MIAO_CONTEXT_WINDOW` | 模型上下文窗口大小（token） |
| `MIAO_VISION` | `1` / `0` 强制开启 / 关闭图片输入 |
| `MIAO_ALLOW_COMMANDS` | 免审批的命令前缀白名单，见下文 |
| `MIAO_DENY_COMMANDS` | 硬拒绝的命令前缀，如 `curl,rm -rf`（命中后不询问、不执行） |
| `MIAO_FALLBACK_MODEL` | 主模型在尚未产生任何输出时失败，改用这个备用模型 |
| `MIAO_NERD_FONT` | `1` 时使用 Nerd Font 图标 |
| `MIAO_PROTOCOL` | `openai` / `anthropic` / `auto`（Base URL 含 anthropic 时自动切） |
| `MIAO_TIMEOUT` | 单次 LLM 请求超时秒数，默认 600 |
| `MIAO_MAX_TOOL_ROUNDS` | 一轮对话最多工具循环次数，默认 32 |
| `MIAO_MODE` | 启动权限模式，`ask` / `edits` / `plan`，默认 `ask` |
| `MIAO_PARALLEL_TOOLS` | 同批只读工具并发上限，默认 8，1 表示串行 |
| `MIAO_MAX_RETRIES` | 请求失败后的额外重试次数，默认 3，设为 0 关闭重试 |
| `MIAO_MAX_OUTPUT_TOKENS` | 单次请求的默认输出 token 上限，0 沿用协议默认值 |
| `MIAO_SANDBOX` | `1` 时用 bubblewrap 把命令关在「根只读、工作区可写」的命名空间里（需安装 `bwrap`；`go`/`git` 不套） |
| `MIAO_PRICE_IN` / `MIAO_PRICE_OUT` | 每百万 token 的美元单价，底栏显示累计花费 |
| `MIAO_PRICE_CACHE` | 缓存命中相对输入价的倍率（默认 DeepSeek/Claude 0.1，其它 0.25） |
| `MIAO_SESSION_REDACT` | `0` 时 `/export` 也不脱敏（会话落盘始终保存原文，目录权限 0700） |

兼容旧名：连接配置的 `AGENT_API_KEY` / `AGENT_BASE_URL` / `AGENT_MODEL` / `AGENT_WORKSPACE` 仍然有效（还会回退到 `DEEPSEEK_API_KEY`、`DEEPSEEK_BASE_URL`、`DEEPSEEK_MODEL`）；其它变量的旧名 `AGNET_*`（如 `AGNET_MODELS`）也仍会读取。新旧都设置时 `MIAO_*` 优先。

所有用户级文件都在 `~/.miao/` 下（设置 `MIAO_HOME` 可以换位置，目录权限 0700）：

| 路径 | 内容 |
| --- | --- |
| `~/.miao/config.json` | 模型、请求限制、工具开关、主题、子代理、hooks 设置 |
| `~/.miao/mcp.json`、`~/.miao/commands/` | MCP 配置、自定义命令 |
| `~/.miao/data/` | 会话、导出、审计日志 |
| `~/.miao/state/` | 运行状态（MCP 日志等） |

用户级配置文件 `~/.miao/config.json` 管理核心行为，API Key 不放在这里。例如保留内置编程工具，关闭联网和外部扩展：

```json
{
  "model": "deepseek-chat",
  "thinking": "auto",
  "max_retries": 3,
  "max_output_tokens": 4096,
  "disabled_tools": ["web_fetch", "web_search"],
  "mcp_enabled": false,
  "hooks_enabled": false,
  "subagents": { "enabled": false, "max": 3, "model": "", "thinking": "", "allow_web": false }
}
```

运行参数优先级：命令行 → 真实环境变量 → 项目配置（遵守信任限制）→ 用户配置 → 默认值。`allowed_tools` 不设置或为 `null` 时不限制，设为 `[]` 时禁用全部工具；`disabled_tools` 优先于允许列表，也约束子代理。设置 `plain: true` 默认使用逐行 REPL。完整字段说明和用法见 [配置指南](docs/configuration.md)，可从 [完整示例](docs/config.example.json) 开始调整。

工具之外的内置功能也能按需调整，例如 `sessions_enabled: false` 关闭会话自动保存，`project_memory_enabled: false` 关闭 AGENTS.md 自动加载，`shell_enabled: false` 关闭 `!` 命令。`custom_commands_enabled`、`images_enabled`、`diagnostics_enabled`、`snapshots_enabled`、`bell_enabled` 分别控制自定义命令、图片、诊断、撤销快照和提示音；默认均开启。关闭诊断或快照不会绕过文件审批和工作区保护。

```bash
miao config                           # 无需 Key，不联网，不启动 MCP 或 hooks
miao -retries 0 -mcp=false config      # 查看命令行覆盖后的核心配置
miao -disabled-tools web_fetch,web_search
miao -allowed-tools= -plain            # 纯对话，不向模型提供工具
miao -sessions=false -project-memory=false -shell=false
miao config set sessions_enabled false
miao config set disabled_tools '["web_fetch","web_search"]'
miao config unset model               # 移除用户覆盖，恢复其它来源或默认值
miao config check                     # 校验配置，不运行扩展或连接 API
```

TUI 输入 `/settings` 后用 ↑/↓ 选择、Enter 切换，也可输入 `/settings images off`。菜单显示保存的启动设置，重启后生效；环境变量和命令行仍优先。设置、主题和子代理共用安全保存逻辑，保留其它字段，文件权限 0600，损坏的 JSON 不会被覆盖。`miao config keys` 列出可修改的核心字段，`miao config path` 显示文件位置。

命令行参数（覆盖环境变量）：`-model`、`-base-url`、`-thinking`、`-system`、`-debug`、`-list-models`、`-protocol`、`-timeout`、`-retries`、`-max-output-tokens`、`-auto-compact`、`-compact-keep-turns`、`-mcp`、`-hooks`、`-c`/`-resume`、`-plain`、`-p`、`-mode`、`-version`、`-output-format`、`-max-turns`、`-allowed-tools`、`-disabled-tools`。子命令：`miao init`（配置 key）、`miao doctor`、`miao config`。

项目级配置写在 `<工作区>/.miao/config.json`（不要放 API Key）。**未信任的仓库只能收紧权限**（降低工具轮次、打开沙箱）；`allow_commands` / `model` / `protocol` 要等你在 TUI 里输入 `/trust-project` 按文件哈希记住之后才生效。JSON 写错会在启动时提示，而不会静默忽略。

```json
{
  "allow_commands": ["go test", "git status"],
  "max_tool_rounds": 48,
  "sandbox": true,
  "protocol": "openai"
}
```

在 `$HOME`（或 `MIAO_HOME`）下启动时，`~/.miao` 是全局目录，不会被当成项目目录，也不读取那里的 `.env`。

从旧版本升级：首次启动时会把旧的 XDG 目录 `~/.config/miao`、`~/.local/share/miao`、`~/.local/state/miao`（没有时用更早的 `~/.config/agnet` 等）**复制**到 `~/.miao`、`~/.miao/data`、`~/.miao/state`。只复制、逐个文件校验，不删除旧目录，不跟随符号链接，`~/.miao` 里已有的文件不会被覆盖（冲突会提示）。成功后会提示一次，确认无误可手动删除旧目录；有文件复制失败时会在启动时警告，下次启动重试。旧会话里记录的旧目录绝对路径仍能正常读取。项目里的 `.agnet/` 不会被移动，没有 `.miao/` 时仍会读取它。

## 思考强度

TUI 输入 `/thinking` 后按 Enter，会自动查询当前模型的思考能力，再用 ↑/↓ 选择、Enter 确认；`/thinking refresh` 强制重新查询。也可以直接输入 `/thinking high`。底部状态栏在模型名旁显示当前设置（如 `思考 high`）。`-plain` 模式下 `/thinking` 同样查询并显示档位。

菜单按当前模型生成，并标明来源：

- **服务端查询**：读取模型能力元数据，只列出接口声明支持的档位；关闭思考与强度档位分开计数。
- **文档适配**：接口不提供档位时，已知型号使用文档适配。例如 DeepSeek 的有效思考强度为 `low / high / max`，加上 `none` 关闭选项。
- **档位未知**：未获得元数据且没有已知适配时，只显示 `auto` 和当前手动设置，不把通用枚举当作支持列表。仍可手动输入 `/thinking high`，界面会标明“尚未验证”。
- **预算预设**：旧版 Claude 的连续 token 预算显示为 miao 的本地预设，不当作模型原生的离散档位。

`auto`（别名 `default`）表示不额外发送思考参数，使用服务端默认。`none`（别名 `off`）请求关闭思考，仅在模型支持时可选。`auto` 和 `none` 不计入思考强度档数。能力查询只向当前配置的服务商发送模型信息 GET 请求，不用聊天请求逐档探测。

设置用于本次运行的后续请求（包括子代理和摘要请求）。切换模型或 `/login` 后重新识别能力；已知不兼容的设置恢复为 `auto`。重启后从启动参数读取，正在生成时不能手动切换。`/reasoning` 和 `Ctrl+R` 仍只控制内容显示。

```bash
export MIAO_THINKING=high          # 真实环境变量，工作区 .env 不接受此配置
miao -thinking low                # 命令行优先
miao -p "分析这个项目" -thinking high
```

协议说明：[Claude 模型能力接口](https://platform.claude.com/docs/en/api/models)提供 `capabilities.effort` 和 thinking 模式；[标准 OpenAI 模型接口](https://developers.openai.com/api/reference/resources/models/methods/retrieve)没有档位字段，兼容服务若扩展了能力元数据也可读取。OpenAI 协议发送 `reasoning_effort`；DeepSeek 的参数说明见[官方文档](https://api-docs.deepseek.com/guides/thinking_mode/)。

## 快捷键

| 按键 | 作用 |
| --- | --- |
| `Enter` | 发送 |
| `Ctrl+J` | 换行 |
| `Ctrl+V` | 粘贴剪贴板里的图片 |
| `Ctrl+B` | 会话侧栏 |
| `Ctrl+T` | 任务清单与子代理结果 |
| `Ctrl+R` | 展开或收起思考过程 |
| `Shift+Tab` | 切换权限模式 |
| `Esc` | 关闭帮助/菜单 · 取消审批 · 取消生成 |
| `Esc Esc` | 回到之前的某条消息重新编辑 |
| `Ctrl+P/N` | 回退列表里上下移动（同 ↑↓） |
| `Ctrl+D` | 会话列表里删除会话（按两次确认） |
| `PgUp/PgDn` | 滚动对话 |
| `?` | 快捷键速查（输入框为空时） |
| `Ctrl+C` | 取消生成，空闲时退出 |

## 斜杠命令

| 命令 | 作用 |
| --- | --- |
| `/help` | 打开帮助页，↑↓ / PgUp/PgDn 滚动，Esc 关闭 |
| `/settings` | 功能开关菜单，Enter 切换并保存；重启后生效 |
| `/new` | 新建会话 |
| `/sessions` | 切换会话 |
| `/clear` | 清空对话并换新会话 |
| `/undo` | 撤销最近一次文件修改 |
| `/compact` | 压缩上下文：把早期对话总结成摘要（默认用量到 80% 时自动触发，可配置） |
| `/login` | 更换 API Key 或服务商（保存到 `~/.miao/credentials`） |
| `/export` | 把当前会话导出为 Markdown（默认 `~/.miao/data/exports`，会脱敏） |
| `/diff` | 查看本次会话改过的文件和 diff |
| `/commit` | 让模型写提交信息，确认后提交本会话改过的文件 |
| `/fork` | 把当前对话复制成新会话（原会话保留） |
| `/model` | 切换模型 |
| `/theme` | 切换主题 |
| `/subagents` | 分层设置子代理模型、思考强度、并发与联网 |
| `/mcp` | 查看 MCP 服务器状态与工具 |
| `/hooks` | 查看已配置的 hook |
| `/thinking` | 选择模型思考强度，或 `/thinking high` 直接设置 |
| `/reasoning` | 思考过程：展开/收起 |
| `/exit` | 退出 |

## 权限模式

`Shift+Tab` 在三种模式间循环：

- **每次询问**（默认）：文件写入和命令都要审批
- **自动同意编辑**：`write_file` / `edit_file` 自动同意，命令仍询问
- **只读规划**：拒绝一切写入和需审批的命令，只读取、搜索、给方案

审批卡上：`Y` 允许 · `B` 本轮其余文件写入都允许 · `A` 本会话总是允许 · `N` 拒绝 · `Esc` 取消。`A` 对文件编辑整类放行，对命令只放行完全相同的命令行。

`run_command` 白名单只覆盖只读用法。下面这些会弹审批，不会静默执行：`rg --pre`、`uniq` 写文件、`cat /etc/passwd` 这类工作区外路径、`git diff --no-index`。`grep -r`（含 `--recurs` 这类长选项缩写）、`diff -r` / `--recursive`、直接比较两个目录，以及 `rg --hidden` / `-u` / `--no-ignore` 这类会扫到 `.env` 的用法，还有 `sort` 白名单外的参数（`-o`、`-T`、`--compress-program` 等）也要审批。`go test` / `go build` / `go vet` 会编译并运行仓库里的代码，**需要审批**；想免审批就写进 `MIAO_ALLOW_COMMANDS`。设 `MIAO_SANDBOX=1`（需 `bwrap`）可以从系统层禁止往工作区外写。

`MIAO_ALLOW_COMMANDS` 是命令白名单，逗号分隔，按词边界前缀匹配：

```bash
export MIAO_ALLOW_COMMANDS="go test,git status,make"
export MIAO_DENY_COMMANDS="curl,rm -rf"   # 命中则直接拒绝，不询问
```

审批过的命令会追加到 `~/.miao/data/audit.log`。

miao 启动的子进程（`run_command`、后台任务、hooks、自动诊断）拿不到名字里带 `KEY` / `TOKEN` / `SECRET` / `PASSWORD` 或以 `MIAO_` 开头的环境变量，所以 `GITHUB_TOKEN` 这类也不会传下去，`gh` 等工具通过 miao 运行时可能需要重新登录。miao 调用 git 时会关掉 hooks、fsmonitor、外部 diff、textconv、提交签名校验（`log.showSignature`）和系统级 gitconfig，**通过 miao 提交不会运行 `pre-commit` 等 git hook**，需要的话请自己在终端里提交或手动跑检查。未信任的仓库如果在本地 git 配置（含 `include`）里写了 `filter.*`、`gpg.*`、`core.sshCommand` 或 `*.textconv`，`git status` 这类只读命令也要审批；`git blame` 会带上 `--no-textconv`。确认仓库可信后输入 `/trust-project` 不再做这项检查。

编辑 `.go` 文件后的自动 `go vet` 只在已信任的仓库运行（同样以 `.miao/config.json` 加 `/trust-project` 为准，没有项目配置可以先写一个 `{}`）；未信任时跳过，并显示一行提示。

`web_fetch` 的免审批规则：网址是 http/https、不带查询串，且主机名出现在你本会话亲手输入的消息里（`@` 引用的文件内容不算），或已在审批卡上按 `A` 放行；每一跳重定向都重新检查。其它情况弹审批卡并显示完整网址，只读规划模式下也一样。

用户配置的 `mode`（或 `MIAO_MODE`、`-mode`）指定启动权限，适用于 TUI、REPL 和非交互模式。默认 `ask`；非交互模式下需要审批的操作一律拒绝。`parallel_tools`（或 `MIAO_PARALLEL_TOOLS`、`-parallel-tools`）控制只读工具并发，默认 8，设为 1 全部串行；写入和命令始终串行。项目配置不能改写这两项用户设置。

## Hooks

用户级写在 `~/.miao/config.json` 的 `"hooks"` 字段，直接生效；项目级写在 `<工作区>/.miao/hooks.json`，第一次运行前需要确认，文件内容变了会重新确认。

```json
{
  "hooks": {
    "pre_tool":  [{ "match": "edit_file|write_file", "command": "echo \"$MIAO_TOOL\"", "timeout": 60 }],
    "post_tool": [{ "match": "edit_file|write_file", "command": "gofmt -l ." }],
    "stop":      [{ "command": "go test ./..." }]
  }
}
```

- `match` 是工具名正则，留空匹配全部（`stop` 忽略）；`timeout` 单位秒
- 命令用 `sh -c` 在工作区执行，环境变量 `MIAO_EVENT`、`MIAO_TOOL`、`MIAO_TOOL_ARGS` 描述触发点，stdin 是同样内容的 JSON
- 退出码非 0：`pre_tool` 阻止这次工具调用；`post_tool` 把输出附在工具结果后；`stop` 把输出交给模型继续处理

## MCP

配置文件 `~/.miao/mcp.json`，格式兼容 Claude Desktop / Cursor：

```json
{
  "mcpServers": {
    "<服务器名>": {
      "command": "<可执行文件>",
      "args": ["<参数>"],
      "env": { "<变量名>": "<值>" },
      "disabled": false,
      "connect_timeout_seconds": 10,
      "call_timeout_seconds": 60
    }
  }
}
```

用 `/mcp` 查看服务器状态和工具。连接和工具调用超时可按服务器设置，单位秒；省略或为 0 时分别使用默认的 10 秒和 60 秒。

## 子代理

子代理是只读的（`task` 工具），默认关闭。默认不限制工具调用轮数，单个任务超时 5 分钟；可以设置任务超时和工具轮次上限。用 `/subagents` 设置，或写在 `~/.miao/config.json`：

```json
{
  "subagents": { "enabled": true, "max": 3, "model": "<模型名>", "thinking": "high", "allow_web": false, "timeout_seconds": 120, "max_tool_rounds": 16 }
}
```

- `max`：并发上限，1–8，默认 3
- `model`：留空跟随主模型
- `thinking`：留空跟随主代理思考强度；`auto` 使用子代理模型的服务端默认，也可独立设置 `low` / `high` 等受支持档位。
- `allow_web`：为 `true` 时子代理额外获得 `web_fetch`（默认关闭，防止数据外传）
- `timeout_seconds`：单个任务超时秒数，0 或省略使用默认的 5 分钟
- `max_tool_rounds`：工具调用批次上限，0 或省略不限制；上限后仍可返回最终回答

`/subagents` 一级菜单只显示开关、模型、思考强度、并发上限和联网设置。选中“模型”或“思考强度”后按 Enter / → 进入二级列表；模型列表支持输入筛选和 Tab 补全，思考档位按子代理所选模型查询。选择后立即保存并返回上级，Esc / ← 也可返回。可分别选择“跟随主模型”和“跟随主代理”，配置只影响新启动的子代理任务。查询档位或独立设置子代理不会修改主代理的模型与思考强度。

## 自定义命令

把 Markdown 文件放进 `~/.miao/commands/`（用户级）或 `<工作区>/.miao/commands/`（项目级，同名覆盖用户级）。文件名就是命令名，正文是提示词模板，`$ARGS` 替换为命令后面的参数（模板里没有 `$ARGS` 时参数接在末尾）。

`.miao/commands/review.md`：

```markdown
---
description: 审查指定文件
---
请审查 $ARGS，指出 bug 和可以简化的地方。
```

使用：`/review internal/agent/agent.go`

## 开发

```bash
make run     # go run ./cmd/miao
make build   # 构建到 bin/miao
make test    # go test ./...
make check   # 格式检查、编译、vet、测试、竞态测试、golangci-lint
```

GitHub Actions 会跑测试、竞态、golangci-lint 和 `nix flake check`。打 `v*` 标签会用 goreleaser 发布各平台二进制。

## 许可证

MIT，见 [LICENSE](LICENSE)
