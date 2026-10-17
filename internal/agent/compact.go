package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// SummaryPrefix 标记由压缩生成的摘要消息，会话恢复和 UI 可据此识别。
const SummaryPrefix = "[早前对话摘要]"

const compactPrompt = `请把下面这段用户与助手（含工具调用结果）的早前对话压缩成一份简洁的中文摘要，供后续对话继续使用。
要求：
- 保留用户的目标、已做出的决定、约束和偏好；
- 保留涉及的文件路径、函数名、命令、关键数值和报错原文；
- 说明已经完成了什么、还有什么没做完；
- 不要寒暄，不要编造，用条目列出，尽量不超过 400 字。`

// CompactResult 是一次压缩的前后估算。
type CompactResult struct {
	Before, After int // 估算 token
	Dropped       int // 被摘要替换掉的消息数
}

// Compact 把除最近 keepTurns 轮以外的历史交给模型总结成一条摘要。
// 只在用户消息处切分，保证 assistant 的 tool_calls 与对应 tool 结果不会被拆开。
func (a *Agent) Compact(ctx context.Context, keepTurns int) (CompactResult, error) {
	if keepTurns < 1 {
		keepTurns = 1
	}
	cut := splitPoint(a.history, keepTurns)
	if cut <= 0 {
		return CompactResult{}, fmt.Errorf("对话太短，暂时不需要压缩")
	}
	before := EstimateTokens(a.requestMessages())

	var sb strings.Builder
	for _, m := range a.history[:cut] {
		sb.WriteString(renderForSummary(m))
		sb.WriteString("\n")
	}
	req := llm.ChatRequest{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: compactPrompt},
		{Role: llm.RoleUser, Content: sb.String()},
	}}
	res, err := a.client.StreamChat(ctx, req, func(llm.Delta) error { return ctx.Err() })
	if err != nil {
		return CompactResult{}, err
	}
	summary := strings.TrimSpace(res.Content)
	if summary == "" {
		return CompactResult{}, fmt.Errorf("模型没有返回摘要")
	}

	kept := a.history[cut:]
	next := make([]llm.Message, 0, len(kept)+2)
	next = append(next,
		llm.Message{Role: llm.RoleUser, Content: SummaryPrefix + "\n" + summary},
		llm.Message{Role: llm.RoleAssistant, Content: "好的，我已了解之前的对话内容，我们继续。"},
	)
	next = append(next, kept...)
	a.history = next
	return CompactResult{Before: before, After: EstimateTokens(a.requestMessages()), Dropped: cut}, nil
}

// splitPoint 返回保留最近 keepTurns 轮时的切分下标（该处是一条用户消息）；不足以压缩时返回 0。
func splitPoint(h []llm.Message, keepTurns int) int {
	seen := 0
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == llm.RoleUser {
			seen++
			if seen == keepTurns {
				return i
			}
		}
	}
	return 0
}

func renderForSummary(m llm.Message) string {
	const maxTool = 2000 // 工具结果只取开头，避免摘要请求本身爆窗口
	switch m.Role {
	case llm.RoleTool:
		c := m.Content
		if utf8.RuneCountInString(c) > maxTool {
			c = string([]rune(c)[:maxTool]) + "…（已截断）"
		}
		return "【工具 " + m.Name + " 结果】" + c
	case llm.RoleAssistant:
		s := "【助手】" + m.Content
		for _, tc := range m.ToolCalls {
			s += "\n【助手调用工具】" + tc.Function.Name + " " + tc.Function.Arguments
		}
		return s
	default:
		return "【用户】" + m.Content
	}
}

// EstimateTokens 粗略估算消息的 token 数：中文约 1 字 1 token，其余约 4 字节 1 token。
func EstimateTokens(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += estimateText(m.Content) + 4 + llm.ImageTokens*len(m.Images)
		for _, tc := range m.ToolCalls {
			n += estimateText(tc.Function.Name+tc.Function.Arguments) + 4
		}
	}
	return n
}

func estimateText(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return other + (ascii+3)/4
}
