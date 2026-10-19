package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Swappable 包一层客户端，/login 换 key 或服务商时原地替换，持有它的主代理和子代理不用重建。
type Swappable struct {
	mu              sync.RWMutex
	c               Client
	thinking        string
	maxOutputTokens int
	thinkingCaps    map[string]ThinkingCapabilities
	thinkingQueries map[string]uint64
	generation      uint64
}

// NewSwappable 用初始客户端创建。
func NewSwappable(c Client) *Swappable { return &Swappable{c: c} }

// Swap 换成新的客户端，之后的请求都走它；进行中的请求不受影响。
func (s *Swappable) Swap(c Client) {
	s.mu.Lock()
	s.c = c
	s.thinkingCaps = nil
	s.thinkingQueries = nil
	s.generation++
	if info := documentedThinking(c, clientModel(c)); !info.Supports(s.thinking) {
		s.thinking = "auto"
	}
	s.mu.Unlock()
}

func (s *Swappable) inner() Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.c
}

// StreamChat 实现 Client。
func (s *Swappable) StreamChat(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error) {
	s.mu.RLock()
	c, thinking, maxOutputTokens := s.c, s.thinking, s.maxOutputTokens
	model := firstNonEmptyModel(req.Model, clientModel(c))
	info, known := s.thinkingCaps[model]
	s.mu.RUnlock()
	if req.MaxTokens == 0 {
		req.MaxTokens = maxOutputTokens
	}
	if !known {
		info = documentedThinking(c, model)
	}
	if req.Thinking == "" {
		req.Thinking = thinking
		if !info.Supports(req.Thinking) {
			req.Thinking = "auto"
		}
	}
	level, err := NormalizeThinking(req.Thinking)
	if err != nil {
		return ChatResult{}, err
	}
	if !info.Supports(level) {
		return ChatResult{}, fmt.Errorf("模型 %s 不支持思考强度 %s；可选：%s", model, level, strings.Join(info.Levels, " / "))
	}
	req.Thinking = level
	req.thinkingMode, req.thinkingOffType = info.mode, info.offType
	return c.StreamChat(ctx, req, onDelta)
}

// SetMaxOutputTokens 设置后续请求的默认输出上限；0 沿用协议默认值。
// 状态保存在外层，因此 /login、摘要和子代理请求都会沿用此设置。
func (s *Swappable) SetMaxOutputTokens(n int) {
	s.mu.Lock()
	s.maxOutputTokens = max(0, n)
	s.mu.Unlock()
}

// Model 实现 ModelSwitcher。
func (s *Swappable) Model() string {
	if m, ok := s.inner().(ModelSwitcher); ok {
		return m.Model()
	}
	return ""
}

// SetModel 实现 ModelSwitcher。
func (s *Swappable) SetModel(name string) {
	if m, ok := s.inner().(ModelSwitcher); ok {
		m.SetModel(name)
		info := s.ThinkingCapabilities()
		s.mu.Lock()
		if !info.Supports(s.thinking) {
			s.thinking = "auto"
		}
		s.mu.Unlock()
	}
}

// ListModels 实现 ModelLister；内层不支持时返回错误。
func (s *Swappable) ListModels(ctx context.Context) ([]string, error) {
	if l, ok := s.inner().(ModelLister); ok {
		return l.ListModels(ctx)
	}
	return nil, errors.New("当前协议不支持列出模型")
}

// SetTimeout 转给内层。
func (s *Swappable) SetTimeout(d time.Duration) {
	if c, ok := s.inner().(configurable); ok {
		c.SetTimeout(d)
	}
}

// SetRetries 转给内层。
func (s *Swappable) SetRetries(n int) {
	if c, ok := s.inner().(configurable); ok {
		c.SetRetries(n)
	}
}

// SetDebug 转给内层。
func (s *Swappable) SetDebug(v bool) {
	if c, ok := s.inner().(configurable); ok {
		c.SetDebug(v)
	}
}
