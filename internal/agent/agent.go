// Package agent 实现 Agent 的核心：维护对话历史，调用模型。
package agent

import (
	"context"
	"fmt"
	"strings"

	"agnet/internal/llm"
)

// DefaultSystemPrompt 是每次请求都会放在最前面的系统提示词。
// 它会塑造 agent 的角色、语言和风格，可以自由修改。
const DefaultSystemPrompt = `你是一个运行在用户终端里的个人 AI 助手，名字叫 GoAgent。
回答默认使用简体中文，除非用户要求其他语言。
保持回答简洁、准确、可操作；涉及代码时给出可以直接运行的示例。
不确定的事情要直说，不要编造。`

// Agent 持有对话历史，并负责调用模型。
//
// 注意：Agent 目前不是并发安全的。当前 REPL 是单 goroutine 调用，
// 阶段 4 会讨论如何支持并发（加锁或改成单 goroutine 所有权模型）。
type Agent struct {
	client  llm.Client
	system  string
	history []llm.Message
}

// New 创建 Agent。system 为空表示不使用系统提示词。
func New(client llm.Client, system string) *Agent {
	return &Agent{client: client, system: system}
}

// Send 发送一条用户消息，并把模型的流式输出通过 onDelta 交给调用方。
//
// 只有完整收到回答后，这一轮问答才会写入历史；
// 如果请求失败或被取消，则回滚用户消息，避免留下"只有提问没有回答"的半轮对话。
func (a *Agent) Send(ctx context.Context, input string, onDelta llm.DeltaHandler) error {
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("消息不能为空")
	}
	if onDelta == nil {
		onDelta = func(llm.Delta) error { return nil }
	}

	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: input})

	// 每次请求都重新拼装：system 消息 + 全部历史。
	// 历史里不保存 system 消息，避免重复。
	messages := make([]llm.Message, 0, len(a.history)+1)
	if a.system != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: a.system})
	}
	messages = append(messages, a.history...)

	var reply strings.Builder // 高效累积字符串，避免 + 反复分配内存
	err := a.client.Stream(ctx, messages, func(d llm.Delta) error {
		reply.WriteString(d.Content)
		return onDelta(d)
	})
	if err != nil {
		a.history = a.history[:len(a.history)-1] // 回滚本条用户消息
		return err
	}

	// 只保存最终回答，不保存推理模型的 reasoning 内容：
	// DeepSeek 等服务的多轮对话不应把 reasoning_content 回传。
	a.history = append(a.history, llm.Message{
		Role:    llm.RoleAssistant,
		Content: reply.String(),
	})
	return nil
}

// Reset 清空对话历史，开启一个新会话。
func (a *Agent) Reset() {
	a.history = a.history[:0]
}

// History 返回历史记录的副本。
// 返回副本可以防止调用方无意中修改 Agent 的内部状态。
func (a *Agent) History() []llm.Message {
	out := make([]llm.Message, len(a.history))
	copy(out, a.history)
	return out
}
