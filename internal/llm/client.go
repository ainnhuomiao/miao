// Package llm 定义与"大模型"对话所需的最小协议，
// 并提供一个兼容 OpenAI Chat Completions 的实现。
//
// 这个包刻意不依赖任何第三方库：HTTP、SSE、JSON 都由标准库完成。
package llm

import "context"

// Role 是对话消息的角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message 是一条对话消息。
// 当前阶段只需要 role 和 content；阶段 3 会加入 tool_calls。
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Delta 是流式响应中的一个增量片段。
// Reasoning 对应 deepseek-reasoner 等推理模型的思考过程，
// 普通模型通常为空。
type Delta struct {
	Content   string
	Reasoning string
}

// DeltaHandler 处理一个增量片段。返回错误会中止本次流式请求。
type DeltaHandler func(Delta) error

// Client 是模型客户端的能力抽象（Go 的接口）。
//
// agent 只依赖这个接口，因此：
//   - 测试时可以注入一个假实现，不需要真的联网；
//   - 以后可以换成 Anthropic、Ollama 等不同的实现。
type Client interface {
	Stream(ctx context.Context, messages []Message, onDelta DeltaHandler) error
}
