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

// 一期白名单：只允许这些可执行文件名（不允许路径、不允许 shell）。
// 人工确认 / 更细策略留给后续。
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
			"在工作区根目录执行白名单内的命令（无 shell）。参数用独立字段传入，不要拼 shell 字符串。",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "可执行文件名，必须在白名单内（如 go、git、ls），不能含路径",
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
			if err := validateCommandName(name); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}

			runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
			defer cancel()

			cmd := exec.CommandContext(runCtx, name, in.Args...)
			cmd.Dir = r.Workspace()
			// 不继承全部敏感环境也可，这里先继承 PATH 等，便于找到 go/git。
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
		},
	})
}

func validateCommandName(name string) error {
	if name != filepath.Base(name) || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("只允许白名单内的命令名，不允许路径: %q", name)
	}
	if _, ok := commandAllowlist[name]; !ok {
		return fmt.Errorf("命令不在白名单内: %s（允许: %s）", name, allowlistSummary())
	}
	return nil
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
