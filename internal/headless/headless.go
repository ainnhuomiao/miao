// Package headless 实现 `miao -p "..."` 非交互模式：跑完一轮直接输出结果，方便写进脚本或 CI。
//
// 默认模型回复写到 stdout，工具调用过程写到 stderr，所以 `miao -p ... > out.md` 只会拿到回答正文。
// `-output-format json` / `stream-json` 时 stdout 是 JSON。
package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
)

// MaxStdinBytes 是从管道读入的附加内容上限。
const MaxStdinBytes = 256 << 10

// Options 控制非交互输出。
type Options struct {
	Verbose bool
	Format  string // text / json / stream-json
}

// ParseMode 把 -mode 参数转成权限模式。
func ParseMode(s string) (agent.PermissionMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "ask", "default":
		return agent.ModeAsk, nil
	case "edits", "accept-edits", "acceptedits":
		return agent.ModeAcceptEdits, nil
	case "plan", "readonly", "read-only":
		return agent.ModePlan, nil
	}
	return agent.ModeAsk, fmt.Errorf("未知的 -mode %q（可选 ask / edits / plan）", s)
}

// ParseFormat 把 -output-format 转成内部格式名。
func ParseFormat(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	case "stream-json", "stream_json", "jsonl":
		return "stream-json", nil
	}
	return "", fmt.Errorf("未知的 -output-format %q（可选 text / json / stream-json）", s)
}

// BuildPrompt 把 -p 的内容和管道输入拼成一条用户消息。stdin 为 nil 表示没有管道输入。
func BuildPrompt(prompt string, stdin io.Reader) (string, error) {
	prompt = strings.TrimSpace(prompt)
	var piped string
	if stdin != nil {
		b, err := io.ReadAll(io.LimitReader(stdin, MaxStdinBytes+1))
		if err != nil {
			return "", fmt.Errorf("读取标准输入失败: %w", err)
		}
		if len(b) > MaxStdinBytes {
			b = append(b[:MaxStdinBytes], []byte("\n…（标准输入超过 256KiB，已截断）")...)
		}
		piped = strings.TrimSpace(string(b))
	}
	switch {
	case prompt == "" || prompt == "-":
		if piped == "" {
			return "", errors.New("-p 需要一段提示词，或者通过管道传入内容")
		}
		return piped, nil
	case piped == "":
		return prompt, nil
	default:
		return prompt + "\n\n<stdin>\n" + piped + "\n</stdin>", nil
	}
}

// Run 发送一轮对话并把结果写出。需要审批的工具一律拒绝（非交互模式没人按 Y），
// 能否自动放行取决于权限模式和 MIAO_ALLOW_COMMANDS。
func Run(ctx context.Context, ag *agent.Agent, prompt string, stdout, stderr io.Writer, opt Options) error {
	ag.SetApprover(func(_ context.Context, toolName, display string, _ json.RawMessage) (bool, error) {
		fmt.Fprintf(stderr, "✗ %s 需要审批，非交互模式下已拒绝：%s\n  （可用 -mode edits 自动同意编辑，或在 MIAO_ALLOW_COMMANDS 里放行命令）\n",
			toolName, firstLine(display))
		return false, nil
	})

	format := opt.Format
	if format == "" {
		format = "text"
	}
	var text strings.Builder
	var usage llm.Usage
	wroteContent := false
	endsWithNewline := true
	emit := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(b))
		return err
	}

	err := ag.Send(ctx, prompt, func(e agent.Event) error {
		switch e.Kind {
		case agent.EventDelta:
			if c := e.Delta.Content; c != "" {
				wroteContent = true
				endsWithNewline = strings.HasSuffix(c, "\n")
				text.WriteString(c)
				switch format {
				case "text":
					fmt.Fprint(stdout, c)
				case "stream-json":
					return emit(map[string]any{"type": "delta", "text": c})
				}
			}
		case agent.EventToolStart:
			if opt.Verbose && format == "text" {
				fmt.Fprintf(stderr, "⚙ %s %s\n", e.ToolName, clip(e.ToolArgs, 120))
			}
			if format == "stream-json" {
				return emit(map[string]any{"type": "tool_start", "tool": e.ToolName, "args": e.ToolArgs})
			}
		case agent.EventToolEnd:
			if format == "text" && (opt.Verbose || e.ToolError) {
				status := "✓"
				if e.ToolError {
					status = "✗"
				}
				fmt.Fprintf(stderr, "  %s %s · %s · %s\n", status, e.ToolName, e.Duration.Round(time.Millisecond), clip(firstLine(e.ToolResult), 120))
			}
			if format == "stream-json" {
				return emit(map[string]any{"type": "tool_end", "tool": e.ToolName, "error": e.ToolError, "result": firstLine(e.ToolResult)})
			}
		case agent.EventUsage:
			if e.Usage != nil {
				usage.PromptTokens += e.Usage.PromptTokens
				usage.CompletionTokens += e.Usage.CompletionTokens
				usage.TotalTokens += e.Usage.TotalTokens
				usage.PromptCacheHitTokens += e.Usage.PromptCacheHitTokens
			}
		}
		return nil
	})
	if format == "text" && wroteContent && !endsWithNewline {
		fmt.Fprintln(stdout)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			err = errors.New("已取消")
		}
		if format == "json" {
			_ = emit(map[string]any{"type": "result", "error": err.Error(), "text": text.String()})
		}
		if format == "stream-json" {
			_ = emit(map[string]any{"type": "error", "error": err.Error()})
		}
		return err
	}
	if !wroteContent {
		err = errors.New("模型没有返回文本内容")
		if format == "json" {
			_ = emit(map[string]any{"type": "result", "error": err.Error()})
		}
		return err
	}
	if format == "json" {
		out := map[string]any{"type": "result", "text": text.String()}
		if usage.PromptTokens+usage.CompletionTokens > 0 {
			out["usage"] = usage
		}
		return emit(out)
	}
	if format == "stream-json" {
		out := map[string]any{"type": "result", "text": text.String()}
		if usage.PromptTokens+usage.CompletionTokens > 0 {
			out["usage"] = usage
		}
		return emit(out)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
