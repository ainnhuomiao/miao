package llm

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Swappable 包一层客户端，/login 换 key 或服务商时原地替换，持有它的主代理和子代理不用重建。
type Swappable struct {
	mu sync.RWMutex
	c  Client
}

// NewSwappable 用初始客户端创建。
func NewSwappable(c Client) *Swappable { return &Swappable{c: c} }

// Swap 换成新的客户端，之后的请求都走它；进行中的请求不受影响。
func (s *Swappable) Swap(c Client) {
	s.mu.Lock()
	s.c = c
	s.mu.Unlock()
}

func (s *Swappable) inner() Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.c
}

// StreamChat 实现 Client。
func (s *Swappable) StreamChat(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error) {
	return s.inner().StreamChat(ctx, req, onDelta)
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
