package agent

import (
	"context"
	"errors"

	"agnet/internal/llm"
)

// ErrModelUnsupported 表示客户端不支持该操作。
var ErrModelUnsupported = errors.New("客户端不支持此操作")

// Model 返回客户端当前模型；客户端不支持切换时返回空串。
func (a *Agent) Model() string {
	if s, ok := a.client.(llm.ModelSwitcher); ok {
		return s.Model()
	}
	return ""
}

// SetModel 切换后续请求的模型。只应在没有进行中的请求时调用。
// 客户端不支持切换时为 noop（测试用假客户端）。
func (a *Agent) SetModel(name string) {
	if s, ok := a.client.(llm.ModelSwitcher); ok {
		s.SetModel(name)
	}
}

// ListModels 向服务端查询可用模型。
func (a *Agent) ListModels(ctx context.Context) ([]string, error) {
	if l, ok := a.client.(llm.ModelLister); ok {
		return l.ListModels(ctx)
	}
	return nil, ErrModelUnsupported
}
