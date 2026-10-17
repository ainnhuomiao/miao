package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxCommandOutput = 64 << 10 // 64 KiB
	commandTimeout   = 30 * time.Second
)

// 白名单内静默执行；名单外需 Approver 确认后才跑。
// 带路径的命令名始终拒绝（不允许绕过）。
var commandAllowlist = map[string]struct{}{
	"go": {}, "git": {}, "ls": {}, "cat": {}, "head": {}, "tail": {},
	"wc": {}, "rg": {}, "grep": {}, "find": {}, "pwd": {}, "echo": {},
	"date": {}, "make": {}, "uname": {}, "which": {}, "file": {}, "stat": {},
	"diff": {}, "sort": {}, "uniq": {}, "sed": {}, "awk": {}, "basename": {},
	"dirname": {}, "env": {}, "true": {}, "false": {}, "test": {},
}

func registerRunCommand(r *Registry) {
	r.Register(Tool{
		Def: toolDef("run_command",
			"在工作区根目录执行命令（无 shell）。白名单内直接执行；名单外需用户确认。参数用独立字段传入。",
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
				},
				"required": []string{"command"},
			}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
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
			if _, ok := commandAllowlist[name]; !ok {
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
					return Result{Content: "用户拒绝执行: " + display, IsError: true}
				}
			}

			return execCommand(ctx, r.Workspace(), name, in.Args)
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

func execCommand(ctx context.Context, dir, name string, args []string) Result {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := mergeOutputs(stdout.String(), stderr.String())
	out = truncateOutput(out, maxCommandOutput)

	if runCtx.Err() == context.DeadlineExceeded {
		return Result{
			Content: fmt.Sprintf("命令超时（>%s）\n%s", commandTimeout, out),
			IsError: true,
		}
	}
	if err != nil {
		if out == "" {
			out = err.Error()
		} else {
			out = out + "\n(" + err.Error() + ")"
		}
		return Result{Content: out, IsError: true}
	}
	if out == "" {
		out = "(无输出)"
	}
	return Result{Content: out}
}

func allowlistSummary() string {
	names := make([]string, 0, len(commandAllowlist))
	for n := range commandAllowlist {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func mergeOutputs(stdout, stderr string) string {
	stdout = strings.TrimRight(stdout, "\n")
	stderr = strings.TrimRight(stderr, "\n")
	switch {
	case stdout == "" && stderr == "":
		return ""
	case stderr == "":
		return stdout
	case stdout == "":
		return stderr
	default:
		return stdout + "\n--- stderr ---\n" + stderr
	}
}

func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n\n…(已截断)"
}
