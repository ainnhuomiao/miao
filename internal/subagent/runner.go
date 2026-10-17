package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agnet/internal/llm"
	"agnet/internal/tools"
)

// AllowedTools 是子代理可用的只读工具白名单。
var AllowedTools = []string{"read_file", "list_dir", "grep", "glob", "web_fetch"}

// SystemPrompt 是子代理的系统提示词。
const SystemPrompt = `你是 agnet 的只读研究子代理，由主代理派来完成一项调研任务。
你有全新的上下文，看不到主对话；只能用只读工具（read_file、list_dir、grep、glob、web_fetch）查看工作区和网页，不能修改文件或执行命令。
先高效地搜索和阅读，再给出简洁的结论：列出关键发现，并尽量附上 文件路径:行号 引用。
最终回答只写结论本身，不要复述过程。`

// Runner 驱动一个子代理的独立循环。
type Runner struct {
	Client   llm.Client
	Model    string
	Tools    *tools.Registry
	MaxSteps int
	// OnActivity 报告当前动作（如「搜索 foo」）；可为 nil。
	OnActivity func(string)
	// OnUsage 在每次模型请求后报告本次用量；可为 nil。
	OnUsage func(llm.Usage)
}

// Outcome 是子代理的运行结果。
type Outcome struct {
	Answer string
	Steps  int // 工具轮数
	Usage  llm.Usage
}

// Run 以全新上下文执行 prompt，返回最终回答。
func (r *Runner) Run(ctx context.Context, prompt string) (Outcome, error) {
	var out Outcome
	activity := func(s string) {
		if r.OnActivity != nil {
			r.OnActivity(s)
		}
	}
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: SystemPrompt},
		{Role: llm.RoleUser, Content: prompt},
	}
	defs := r.Tools.Definitions()
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		activity("思考中")
		res, err := r.Client.StreamChat(ctx, llm.ChatRequest{Model: r.Model, Messages: msgs, Tools: defs}, nil)
		if err != nil {
			return out, err
		}
		if res.Usage != nil {
			out.Usage.PromptTokens += res.Usage.PromptTokens
			out.Usage.CompletionTokens += res.Usage.CompletionTokens
			out.Usage.TotalTokens += res.Usage.TotalTokens
			out.Usage.PromptCacheHitTokens += res.Usage.PromptCacheHitTokens
			if r.OnUsage != nil {
				r.OnUsage(*res.Usage)
			}
		}
		if len(res.ToolCalls) == 0 {
			out.Answer = res.Content
			return out, nil
		}
		if out.Steps >= r.MaxSteps {
			return out, fmt.Errorf("子代理超过 %d 轮工具调用上限", r.MaxSteps)
		}
		out.Steps++
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: res.Content, ToolCalls: res.ToolCalls})
		for _, tc := range res.ToolCalls {
			activity(describeCall(tc.Function.Name, tc.Function.Arguments))
			result := r.Tools.Call(ctx, tc.Function.Name, tc.Function.Arguments)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, Content: result.Content, ToolCallID: tc.ID, Name: tc.Function.Name})
		}
	}
}

// describeCall 把工具调用转成简短的中文动作描述。
func describeCall(name, args string) string {
	var in map[string]any
	_ = json.Unmarshal([]byte(args), &in)
	str := func(k string) string {
		s, _ := in[k].(string)
		return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	}
	switch name {
	case "read_file":
		return "读取 " + str("path")
	case "list_dir":
		return "列目录 " + str("path")
	case "grep":
		return "搜索 " + str("pattern")
	case "glob":
		return "匹配 " + str("pattern")
	case "web_fetch":
		return "抓取 " + str("url")
	}
	return "调用 " + name
}
