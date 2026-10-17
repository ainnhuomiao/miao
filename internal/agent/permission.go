package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"agnet/internal/tools"
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
const AllowCommandsEnv = "AGNET_ALLOW_COMMANDS"

const planModeNote = "\n\n当前处于「只读规划」模式：只能读取和搜索文件、给出方案，不要尝试写文件或执行有副作用的命令（这类调用会被直接拒绝）。等用户切回其他模式后再动手修改。"

type permissions struct {
	mu        sync.Mutex
	mode      PermissionMode
	session   map[string]bool // 本会话总是允许
	allowCmds []string
}

func isEditTool(name string) bool { return name == "write_file" || name == "edit_file" }

// Mode 返回当前权限模式。
func (a *Agent) Mode() PermissionMode {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	return a.perm.mode
}

// SetMode 设置权限模式。
func (a *Agent) SetMode(m PermissionMode) {
	a.perm.mu.Lock()
	a.perm.mode = m
	a.perm.mu.Unlock()
}

// CycleMode 切到下一个模式并返回它。
func (a *Agent) CycleMode() PermissionMode {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	a.perm.mode = (a.perm.mode + 1) % 3
	return a.perm.mode
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
	a.perm.mu.Unlock()
}

// SetAllowedCommands 设置命令白名单（前缀匹配，按词边界）。
func (a *Agent) SetAllowedCommands(prefixes []string) {
	var clean []string
	for _, p := range prefixes {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			clean = append(clean, p)
		}
	}
	a.perm.mu.Lock()
	a.perm.allowCmds = clean
	a.perm.mu.Unlock()
}

// AllowedCommandsFromEnv 读取 AGNET_ALLOW_COMMANDS。
func AllowedCommandsFromEnv() []string {
	v := os.Getenv(AllowCommandsEnv)
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
	return toolName
}

func matchPrefix(line, prefix string) bool {
	return line == prefix || strings.HasPrefix(line, prefix+" ")
}

// decide 返回 (decided, allow)：decided=false 表示要交给用户确认。
func (p *permissions) decide(toolName string, args json.RawMessage) (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == ModePlan {
		return true, false
	}
	if p.session[sessionKey(toolName, args)] {
		return true, true
	}
	if p.mode == ModeAcceptEdits && isEditTool(toolName) {
		return true, true
	}
	if toolName == "run_command" {
		name, cargs, ok := parseCommand(args)
		raw := strings.Join(append([]string{name}, cargs...), " ")
		// 危险参数（-exec、-toolexec、-o…）和 shell 元字符、换行一律交给用户确认，白名单不放行。
		if !ok || tools.HasDangerousFlag(name, cargs) || strings.ContainsAny(raw, ";&|`$<>()\n\r") {
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
	return func(ctx context.Context, toolName, display string, args json.RawMessage) (bool, error) {
		if decided, allow := a.perm.decide(toolName, args); decided {
			return allow, nil
		}
		if fn == nil {
			return false, nil
		}
		return fn(ctx, toolName, display, args)
	}
}
