package agent

import (
	"context"

	"github.com/ainnhuomiao/miao/internal/llm"
)

func (a *Agent) ThinkingCapabilities() llm.ThinkingCapabilities {
	if c, ok := a.client.(llm.ThinkingCapabilityProvider); ok {
		return c.ThinkingCapabilities()
	}
	return llm.ThinkingCapabilities{Levels: []string{"auto"}, Source: "未知"}
}

func (a *Agent) QueryThinkingCapabilities(ctx context.Context) (llm.ThinkingCapabilities, error) {
	if c, ok := a.client.(llm.ThinkingCapabilityProvider); ok {
		return c.QueryThinkingCapabilities(ctx)
	}
	return a.ThinkingCapabilities(), nil
}

func (a *Agent) ThinkingCapabilitiesForModel(model string) llm.ThinkingCapabilities {
	if c, ok := a.client.(llm.ModelThinkingCapabilityProvider); ok {
		return c.ThinkingCapabilitiesForModel(model)
	}
	return llm.ThinkingCapabilities{Levels: []string{"auto"}, Source: "未知"}
}

func (a *Agent) QueryThinkingCapabilitiesForModel(ctx context.Context, model string) (llm.ThinkingCapabilities, error) {
	if c, ok := a.client.(llm.ModelThinkingCapabilityProvider); ok {
		return c.QueryThinkingCapabilitiesForModel(ctx, model)
	}
	return a.ThinkingCapabilitiesForModel(model), nil
}

func (a *Agent) Thinking() string {
	if c, ok := a.client.(llm.ThinkingController); ok {
		return c.Thinking()
	}
	return "auto"
}

func (a *Agent) SetThinking(value string) error {
	if c, ok := a.client.(llm.ThinkingController); ok {
		return c.SetThinking(value)
	}
	return ErrModelUnsupported
}

// 已编辑的前缀或切换后的模型不能复用绑定旧前缀的 Claude 思考签名。
// 保留普通正文和工具调用，由客户端重新构建这些内容块。调用时必须持有 mu。
func (a *Agent) clearAnthropicContentLocked() {
	for i := range a.history {
		a.history[i].AnthropicContent = nil
	}
}
