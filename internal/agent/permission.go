package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/tools"
)

// PermissionMode 是审批策略，Shift+Tab 在三种模式间循环切换。
type PermissionMode int

const (
	ModeAsk         PermissionMode = iota // 每次询问
	ModeAcceptEdits                       // 自动同意文件编辑，命令仍询问
	ModePlan                              // 只读规划：拒绝一切写入和需审批的命令
)

func (m PermissionMode) String() string {
	switch m {
	case ModeAcceptEdits:
		return "自动同意编辑"
	case ModePlan:
		return "只读规划"
	default:
		return "每次询问"
	}
}

// AllowCommandsEnv 是命令白名单环境变量，逗号分隔，按前缀匹配，如 "go test,git status,make"。
const AllowCommandsEnv = config.EnvPrefix + "ALLOW_COMMANDS"

const planModeNote = "<system-reminder>\n" +
	"已进入只读规划模式，直到用户用 /apply 批准或切换模式。用户说“实现某某”时，意思是规划如何实现，而不是动手。\n" +
	"先用只读的读取、搜索、检查把方案落到仓库的真实代码上；不要写文件、改配置、运行会改写已跟踪文件的格式化或生成命令，也不要提交。\n" +
	"为了保持请求缓存稳定，各模式下工具列表不变；写入类工具仍会列出，但在本模式下不要使用。\n" +
	"能查到的事实自己查，只有需要用户做决定的问题才问。\n" +
	"方案要完整到可以直接实施：目标与验收标准、按模块列出改动、涉及的接口和数据变化、边界情况、测试，以及明确的假设。\n" +
	"写完方案就停下，等用户 /apply；不要再问“要我开始吗”。\n" +
	"</system-reminder>"

type permissions struct {
	mu         sync.Mutex
	mode       PermissionMode
	session    map[string]bool // 本会话总是允许
	allowCmds  []string
	roundEdits bool // 本轮其余文件编辑都允许（审批卡按 B）
}

func isEditTool(name string) bool { return name == "write_file" || name == "edit_file" }

// Mode 返回当前权限模式。
func (a *Agent) Mode() PermissionMode {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	return a.perm.mode
}

// SetMode 设置权限模式。说明只记在 pendingModeNote，下次 Send 开头写入历史。
func (a *Agent) SetMode(m PermissionMode) {
	a.perm.mu.Lock()
	old := a.perm.mode
	a.perm.mode = m
	a.perm.mu.Unlock()
	if old != m {
		a.noteMode(m)
	}
}

// CycleMode 切到下一个模式并返回它。
func (a *Agent) CycleMode() PermissionMode {
	a.perm.mu.Lock()
	a.perm.mode = (a.perm.mode + 1) % 3
	m := a.perm.mode
	a.perm.mu.Unlock()
	a.noteMode(m)
	return m
}

func (a *Agent) noteMode(m PermissionMode) {
	note := planModeNote
	if m != ModePlan {
		note = wrapReminder("已切换到「" + m.String() + "」模式。")
	}
	a.mu.Lock()
	a.pendingModeNote = note
	a.mu.Unlock()
}

// AllowForSession 记下「本会话总是允许」：文件编辑整类放行，命令按完全相同的命令行放行。
func (a *Agent) AllowForSession(toolName string, args json.RawMessage) {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	if a.perm.session == nil {
		a.perm.session = map[string]bool{}
	}
	a.perm.session[sessionKey(toolName, args)] = true
}

// ClearSessionAllows 清掉本会话的放行记录（新建或切换会话时调用）。
func (a *Agent) ClearSessionAllows() {
	a.perm.mu.Lock()
	a.perm.session = nil
	a.perm.roundEdits = false
	a.perm.mu.Unlock()
}

// AllowEditsThisRound 允许本轮剩余的 write_file / edit_file（审批卡按 B）。
func (a *Agent) AllowEditsThisRound() {
	a.perm.mu.Lock()
	a.perm.roundEdits = true
	a.perm.mu.Unlock()
}

func (a *Agent) clearRoundAllows() {
	a.perm.mu.Lock()
	a.perm.roundEdits = false
	a.perm.mu.Unlock()
}

// SetAllowedCommands 设置命令白名单（前缀匹配，按词边界）。
func (a *Agent) SetAllowedCommands(prefixes []string) {
	seen := make(map[string]bool, len(prefixes))
	var clean []string
	for _, p := range prefixes {
		p = strings.Join(strings.Fields(p), " ")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		clean = append(clean, p)
	}
	a.perm.mu.Lock()
	a.perm.allowCmds = clean
	a.perm.mu.Unlock()
}

// AllowedCommands 返回当前命令白名单副本。
func (a *Agent) AllowedCommands() []string {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	return append([]string(nil), a.perm.allowCmds...)
}

// AllowedCommandsFromEnv 读取 MIAO_ALLOW_COMMANDS（旧名 AGNET_ALLOW_COMMANDS 作回退）。
func AllowedCommandsFromEnv() []string {
	v := config.Getenv(strings.TrimPrefix(AllowCommandsEnv, config.EnvPrefix))
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func parseCommand(args json.RawMessage) (string, []string, bool) {
	var in struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal(args, &in) != nil {
		return "", nil, false
	}
	return in.Command, in.Args, true
}

func commandLine(args json.RawMessage) string {
	var in struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal(args, &in) != nil {
		return ""
	}
	return strings.Join(strings.Fields(strings.Join(append([]string{in.Command}, in.Args...), " ")), " ")
}

func sessionKey(toolName string, args json.RawMessage) string {
	if isEditTool(toolName) {
		return "edit"
	}
	if toolName == "run_command" {
		return "run_command:" + commandLine(args)
	}
	if toolName == "web_fetch" {
		return webHostKey(webFetchURLHost(args))
	}
	if toolName == "web_search" {
		return webSearchKey(args)
	}
	return toolName
}

func matchPrefix(line, prefix string) bool {
	return line == prefix || strings.HasPrefix(line, prefix+" ")
}

// neverAutoAllow 是能执行任意代码的程序：哪怕写进白名单也每次都要用户确认。
var neverAutoAllow = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true,
	"env": true, "xargs": true, "sudo": true, "doas": true, "nohup": true, "timeout": true, "nice": true,
	"python": true, "python3": true, "perl": true, "ruby": true, "node": true, "deno": true, "bun": true,
	"lua": true, "php": true, "nix-shell": true, "eval": true, "exec": true, "busybox": true,
}

// goNeverAutoAllow 是会编译并运行任意程序的 go 子命令。
var goNeverAutoAllow = map[string]bool{"run": true, "generate": true, "tool": true}

// decide 返回 (decided, allow)：decided=false 表示要交给用户确认。
func (p *permissions) decide(toolName string, args json.RawMessage, workspace string) (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == ModePlan {
		return true, false
	}
	if p.session[sessionKey(toolName, args)] {
		return true, true
	}
	if isEditTool(toolName) && (p.mode == ModeAcceptEdits || p.roundEdits) {
		return true, true
	}
	if toolName == "run_command" {
		name, cargs, ok := parseCommand(args)
		if ok && tools.CommandDenied(name, cargs) {
			return true, false
		}
		raw := strings.Join(append([]string{name}, cargs...), " ")
		// 危险参数、越界路径、shell 元字符一律交给用户确认，ALLOW_COMMANDS 不放行。
		if !ok || neverAutoAllow[filepath.Base(name)] || (filepath.Base(name) == "go" && len(cargs) > 0 && goNeverAutoAllow[cargs[0]]) || tools.HasDangerousFlag(name, cargs) || tools.CommandEscapesWorkspace(workspace, name, cargs) || strings.ContainsAny(raw, ";&|`$<>()\n\r") {
			return false, false
		}
		line := commandLine(args)
		for _, pre := range p.allowCmds {
			if line != "" && matchPrefix(line, pre) {
				return true, true
			}
		}
	}
	return false, false
}

// wrapApprover 在用户审批前先按权限模式、会话放行和白名单自动决定。
func (a *Agent) wrapApprover(fn tools.Approver) tools.Approver {
	if a.tools != nil {
		a.tools.SetWebPolicy(a.webURLAllowed)
	}
	return func(ctx context.Context, toolName, display string, args json.RawMessage) (bool, error) {
		if toolName == "web_fetch" {
			return a.approveWebFetch(ctx, fn, display, args)
		}
		if toolName == "web_search" {
			return a.approveWebSearch(ctx, fn, display, args)
		}
		if decided, allow := a.perm.decide(toolName, args, a.Workspace()); decided {
			return allow, nil
		}
		if fn == nil {
			return false, nil
		}
		return fn(ctx, toolName, display, args)
	}
}

// approveWebSearch：查询串会发往外部搜索引擎，和 web_fetch 一样需审批（卡片显示完整查询串）。
// 只读规划模式下也不直接拒绝；按 A 只放行完全相同的查询串，其他查询仍要审批；无审批回调（-p）时拒绝。
func (a *Agent) approveWebSearch(ctx context.Context, fn tools.Approver, display string, args json.RawMessage) (bool, error) {
	a.perm.mu.Lock()
	ok := a.perm.session[sessionKey("web_search", args)]
	a.perm.mu.Unlock()
	if ok {
		return true, nil
	}
	if fn == nil {
		return false, nil
	}
	return fn(ctx, "web_search", display, args)
}

// webSearchKey 是 web_search 的会话放行键：完全相同的查询串（不做任何规范化），
// 防止按一次 A 后被提示注入用任意查询外传数据。参数解析失败时退回原始参数字节。
func webSearchKey(args json.RawMessage) string {
	var in struct {
		Query *string `json:"query"`
	}
	if json.Unmarshal(args, &in) != nil || in.Query == nil {
		return "web_search_raw:" + string(args)
	}
	return "web_search_q:" + strconv.Quote(*in.Query)
}
