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
	"date": {}, "uname": {}, "which": {}, "file": {}, "stat": {},
	"diff": {}, "sort": {}, "uniq": {}, "basename": {},
	"dirname": {}, "true": {}, "false": {}, "test": {},
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
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"description": "超时秒数，默认 120，最多 600",
					},
					"background": map[string]any{
						"type":        "boolean",
						"description": "为 true 时放到后台运行（适合开发服务器、go run 等不会自己结束的命令），之后用 command_output 查看输出",
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
			if needsApproval(name, in.Args) {
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

// 这些命令名在白名单里，但只有部分子命令/参数是只读安全的。
var (
	gitReadOnly = map[string]bool{"status": true, "log": true, "diff": true, "show": true,
		"rev-parse": true, "ls-files": true, "blame": true, "shortlog": true, "describe": true}
	goSafe = map[string]bool{"build": true, "test": true, "vet": true, "version": true,
		"list": true, "doc": true, "env": true}
	findDanger = map[string]bool{"-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
		"-delete": true, "-fprint": true, "-fprint0": true, "-fprintf": true, "-fls": true}
)

// needsApproval 判断命令是否要走审批：白名单外，或白名单内但带写入/任意执行能力的用法。
func needsApproval(name string, args []string) bool {
	if _, ok := commandAllowlist[name]; !ok {
		return true
	}
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	for _, a := range args {
		if dangerousFlag(name, a) {
			return true
		}
	}
	switch name {
	case "git":
		return !gitReadOnly[first]
	case "go":
		if first == "env" && len(args) > 1 && strings.HasPrefix(args[1], "-w") {
			return true
		}
		return !goSafe[first]
	case "find":
		for _, a := range args {
			if findDanger[a] {
				return true
			}
		}
	case "sort":
		for _, a := range args {
			if a == "-o" || strings.HasPrefix(a, "--output") {
				return true
			}
		}
	}
	return false
}

// dangerousFlag 识别能写文件或执行任意程序的参数（含 -flag=value 形式）。
func dangerousFlag(name, a string) bool {
	key, _, _ := strings.Cut(a, "=")
	switch name {
	case "go":
		switch strings.TrimLeft(key, "-") {
		case "exec", "toolexec", "o", "overlay", "modfile", "coverprofile",
			"cpuprofile", "memprofile", "blockprofile", "mutexprofile", "trace", "outputdir":
			return strings.HasPrefix(a, "-")
		}
	case "git":
		k := strings.TrimLeft(key, "-")
		return strings.HasPrefix(a, "-") && (k == "output" || k == "ext-diff" || k == "textconv" ||
			strings.HasPrefix(k, "exec") || k == "c" || k == "config-env")
	}
	return false
}
