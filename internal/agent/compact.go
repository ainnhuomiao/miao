package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// SummaryPrefix 标记由压缩生成的摘要消息，会话恢复和 UI 可据此识别。
const SummaryPrefix = "[早前对话摘要]"

const compactTrimMaxRunes = 1500

func compactInstruction(keepTurns int) string {
	return fmt.Sprintf(`请把上面这段对话中、最近 %d 轮用户提问之前的部分压缩成一份简洁的中文摘要，供后续对话继续使用。
要求：
- 只输出摘要正文，不要调用任何工具，不要复述最近 %d 轮的原文；
- 保留用户的目标、已做出的决定、约束和偏好；
- 保留涉及的文件路径、函数名、命令、关键数值和报错原文；
- 说明已经完成了什么、还有什么没做完；
- 不要寒暄，不要编造，用条目列出，尽量不超过 400 字。`, keepTurns, keepTurns)
}

// CompactResult 是一次压缩的前后估算。
type CompactResult struct {
	Before, After int  // 估算 token
	Dropped       int  // 被摘要替换掉的消息数
	Trimmed       int  // 就地折叠的早期工具输出条数
	Summarized    bool // 是否请求了模型摘要
}

// Compact 把除最近 keepTurns 轮以外的历史压缩。
// 先折叠早期超长工具输出；若 skipBelow>0 且折叠后估算已低于该值，则不再请求模型。
// 摘要请求复用当前对话前缀（同一段 system + 历史 + 工具定义），指令放在最后一条用户消息。
func (a *Agent) Compact(ctx context.Context, keepTurns, skipBelow int) (CompactResult, error) {
	if keepTurns < 1 {
		keepTurns = 1
	}
	before := EstimateTokens(a.requestMessages())
	trimmed := a.TrimOldToolResults(keepTurns, compactTrimMaxRunes)
	afterTrim := EstimateTokens(a.requestMessages())
	if skipBelow > 0 && afterTrim > 0 && afterTrim <= skipBelow {
		return CompactResult{Before: before, After: afterTrim, Trimmed: trimmed}, nil
	}

	a.mu.Lock()
	cut := splitPoint(a.history, keepTurns)
	a.mu.Unlock()
	if cut <= 0 {
		if trimmed > 0 {
			return CompactResult{Before: before, After: afterTrim, Trimmed: trimmed}, nil
		}
		return CompactResult{}, fmt.Errorf("对话太短，暂时不需要压缩")
	}

	var defs []llm.ToolDef
	if a.tools != nil {
		defs = a.tools.Definitions()
	}
	msgs := append(a.requestMessages(), llm.Message{Role: llm.RoleUser, Content: compactInstruction(keepTurns)})
	res, err := a.client.StreamChat(ctx, llm.ChatRequest{Messages: msgs, Tools: defs}, func(llm.Delta) error {
		return ctx.Err()
	})
	if err != nil {
		return CompactResult{}, err
	}
	summary := strings.TrimSpace(res.Content)
	if summary == "" {
		return CompactResult{}, fmt.Errorf("模型没有返回摘要")
	}

	a.mu.Lock()
	kept := dropLeadingBaseline(a.history[cut:])
	baseline := a.baselineReminderLocked()
	next := make([]llm.Message, 0, len(kept)+3)
	if baseline != "" {
		next = append(next, llm.Message{Role: llm.RoleUser, Content: baseline})
	}
	next = append(next,
		llm.Message{Role: llm.RoleUser, Content: SummaryPrefix + "\n" + summary},
		llm.Message{Role: llm.RoleAssistant, Content: "好的，我已了解之前的对话内容，我们继续。"},
	)
	next = append(next, kept...)
	a.history = next
	a.mu.Unlock()
	return CompactResult{
		Before:     before,
		After:      EstimateTokens(a.requestMessages()),
		Dropped:    cut,
		Trimmed:    trimmed,
		Summarized: true,
	}, nil
}

// splitPoint 返回保留最近 keepTurns 轮真实用户输入时的切分下标；不足以压缩时返回 0。
// 模式说明和摘要消息不计入轮次。
func splitPoint(h []llm.Message, keepTurns int) int {
	seen := 0
	for i := len(h) - 1; i >= 0; i-- {
		if !isUserTurn(h[i]) {
			continue
		}
		seen++
		if seen == keepTurns {
			return i
		}
	}
	return 0
}

func isUserTurn(m llm.Message) bool {
	if m.Role != llm.RoleUser {
		return false
	}
	c := strings.TrimSpace(m.Content)
	if strings.HasPrefix(c, SummaryPrefix) {
		return false
	}
	if isReminder(m) {
		return false
	}
	return true
}

func dropLeadingBaseline(h []llm.Message) []llm.Message {
	if len(h) > 0 && isBaselineReminder(h[0]) {
		return h[1:]
	}
	return h
}

// TrimOldToolResults 把早于最近 keepTurns 轮的超长工具结果裁成摘要，返回折叠条数。
func (a *Agent) TrimOldToolResults(keepTurns, maxRunes int) int {
	if keepTurns < 1 {
		keepTurns = 1
	}
	if maxRunes < 80 {
		maxRunes = 80
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cut := splitPoint(a.history, keepTurns)
	if cut <= 0 {
		return 0
	}
	n := 0
	for i := 0; i < cut; i++ {
		if a.history[i].Role != llm.RoleTool {
			continue
		}
		r := []rune(a.history[i].Content)
		if len(r) <= maxRunes {
			continue
		}
		a.history[i].Content = string(r[:maxRunes]) + fmt.Sprintf("\n…（已折叠，原 %d 字）", len(r))
		n++
	}
	return n
}

// EstimateContext 估算当前发给模型的上下文 token 数。
func (a *Agent) EstimateContext() int {
	return EstimateTokens(a.requestMessages())
}

// EstimateTokens 粗略估算消息的 token 数：中文约 1 字 1 token，其余约 4 字节 1 token。
func EstimateTokens(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += estimateText(m.Content) + estimateText(m.ReasoningContent) + 4 + llm.ImageTokens*len(m.Images)
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
