// Package llm 定义与"大模型"对话所需的最小协议，
// 并提供一个兼容 OpenAI Chat Completions 的实现。
//
// 这个包刻意不依赖任何第三方库：HTTP、SSE、JSON 都由标准库完成。
package llm

import (
	"context"
	"encoding/json"
)

// Role 是对话消息的角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 携带工具名和 JSON 参数字符串。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message 是一条对话消息。
// 助手消息可带 ToolCalls；工具结果消息用 ToolCallID 关联。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Images 是用户消息附带的图片；非空时 content 序列化为多模态 parts，见 image.go。
	Images []Image `json:"-"`
}

// ToolDef 描述可供模型调用的工具（OpenAI tools 字段）。
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef 是工具的函数定义。
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Delta 是流式响应中的一个增量片段。
// Reasoning 对应推理模型的思考过程；ToolCalls 在流式拼接过程中也可能增量出现，
// 但完整结果以 ChatResult.ToolCalls 为准。
type Delta struct {
	Content   string
	Reasoning string
}

// DeltaHandler 处理一个增量片段。返回错误会中止本次流式请求。
type DeltaHandler func(Delta) error

// ChatRequest 是一次对话请求。
type ChatRequest struct {
	Messages []Message
	Tools    []ToolDef
	Model    string // 非空时覆盖客户端当前模型（子代理用，不改变主模型）
}

// ChatResult 是一次流式请求聚合后的助手回合。
type ChatResult struct {
	Content      string
	Reasoning    string
	ToolCalls    []ToolCall
	FinishReason string
	// Usage 是服务端在流末尾返回的用量；服务端不支持时为 nil。
	Usage *Usage
}

// Usage 是一次请求的 token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// PromptCacheHitTokens 是 DeepSeek 扩展字段：命中上下文缓存的输入 token 数。
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
}

// Client 是模型客户端的能力抽象。
type Client interface {
	StreamChat(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error)
}
