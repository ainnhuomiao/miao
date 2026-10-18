package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxCommandOutput  = 16 << 10 // 头尾合计 16KiB
	commandOutputKeep = 8 << 10  // 超出时各留 8KiB
)

// 白名单内静默执行；名单外需 Approver 确认后才跑。
// 带路径的命令名始终拒绝（不允许绕过）。
var commandAllowlist = map[string]struct{}{
	"go": {}, "git": {}, "ls": {}, "cat": {}, "head": {}, "tail": {},
	"wc": {}, "rg": {}, "grep": {}, "find": {}, "pwd": {}, "echo": {},
	"date": {}, "uname": {}, "which": {}, "file": {}, "stat": {},
	"diff": {}, "sort": {}, "uniq": {}, "basename": {},
	"dirname": {}, "true": {}, "false": {}, "test": {},
}

func registerRunCommand(r *Registry) {
	r.Register(Tool{
		Def: toolDef("run_command",
			"在工作区执行命令（无 shell、无管道）。看文本文件用 read_file，不要 cat/head。白名单只读用法可直接跑；写文件、任意程序、越界路径需确认。结果最后一行是 [exit code: N]，失败先查原因再继续。输出过长时保留开头和末尾。长时间命令设 background=true，再用 command_output 查看。",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "可执行文件名（如 go、git、ls），不能含路径分隔符",
					},
					"args": map[string]any{
						"type":        "array",
						"description": "命令行参数列表",
						"items":       map[string]any{"type": "string"},
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"description": "超时秒数，默认 120，最多 600",
					},
					"background": map[string]any{
						"type":        "boolean",
						"description": "true 时后台运行，之后用 command_output 查看",
					},
				},
				"required": []string{"command"},
			}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Timeout int      `json:"timeout_seconds"`
				BG      bool     `json:"background"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			name := strings.TrimSpace(in.Command)
			if name == "" {
				return Result{Content: "command 不能为空", IsError: true}
			}
			if err := validateBareCommandName(name); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}

			display := formatCommandDisplay(name, in.Args)
			if CommandDenied(name, in.Args) {
				auditCommand(r.Workspace(), display, "deny")
				return Result{Content: "命令被拒绝列表拦截: " + display, IsError: true}
			}
			if needsApprovalIn(r.Workspace(), name, in.Args) {
				if name == "git" && UntrustedGitExecConfig(r.Workspace()) {
					display += "（未信任仓库的 .git/config 含有 filter、gpg、core.sshCommand 或 textconv，可能执行程序）"
				}
				approver := r.Approver()
				if approver == nil {
					return Result{
						Content: fmt.Sprintf("命令不在白名单内且未配置审批: %s（允许: %s）", name, allowlistSummary()),
						IsError: true,
					}
				}
				allow, err := approver(ctx, "run_command", display, args)
				if err != nil {
					return Result{Content: "审批中断: " + err.Error(), IsError: true}
				}
				if !allow {
					auditCommand(r.Workspace(), display, "reject")
					return Result{Content: "用户拒绝执行: " + display, IsError: true}
				}
				auditCommand(r.Workspace(), display, "allow")
			}

			if in.BG {
				return r.startBackground(name, in.Args, display)
			}
			return execCommandStream(ctx, r.Workspace(), name, in.Args, commandTimeoutFor(in.Timeout))
		},
	})
}

func validateBareCommandName(name string) error {
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("不允许带路径的命令名: %q", name)
	}
	return nil
}

func formatCommandDisplay(name string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, name)
	for _, a := range args {
		if strings.ContainsAny(a, " \t\n\"'") {
			parts = append(parts, fmt.Sprintf("%q", a))
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

func allowlistSummary() string {
	names := make([]string, 0, len(commandAllowlist))
	for n := range commandAllowlist {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func truncateOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	keep := commandOutputKeep
	if max < keep*2 {
		keep = max / 2
	}
	if keep < 1 {
		return s[:max] + "\n\n…(已截断：仅保留开头)"
	}
	if len(s) <= keep*2 {
		return s
	}
	omitted := len(s) - keep*2
	return s[:keep] + fmt.Sprintf("\n\n…(已截断：保留开头和末尾各 %d 字节，省略中间 %d 字节)\n\n", keep, omitted) + s[len(s)-keep:]
}
