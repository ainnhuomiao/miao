package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// ThinkingCapabilities 区分服务端查询、文档适配和未知能力；auto 总是可用。
// Budget 表示本地 token 预算预设，不是模型原生的离散 effort 档位。
type ThinkingCapabilities struct {
	Levels  []string
	Known   bool
	Source  string
	Note    string
	Budget  bool
	mode    string
	offType string
}

func (c ThinkingCapabilities) Supports(level string) bool {
	return level == "auto" || !c.Known || slices.Contains(c.Levels, level)
}

func (c ThinkingCapabilities) Summary() string {
	if !c.Known {
		return "档位未知：尚未获得模型档位信息；可用 auto，其他值需手动输入（未验证）。"
	}
	label := "思考档位"
	if c.Budget {
		label = "本地预算预设"
	}
	n := 0
	for _, level := range c.Levels {
		if level != "auto" && level != "none" {
			n++
		}
	}
	return fmt.Sprintf("%s %d 档；可选：%s；来源：%s。%s", label, n, strings.Join(c.Levels, " / "), c.Source, c.Note)
}

type ThinkingCapabilityProvider interface {
	ThinkingCapabilities() ThinkingCapabilities
	QueryThinkingCapabilities(context.Context) (ThinkingCapabilities, error)
}

// ModelThinkingCapabilityProvider 读取指定模型的能力，不切换主代理模型。
type ModelThinkingCapabilityProvider interface {
	ThinkingCapabilitiesForModel(string) ThinkingCapabilities
	QueryThinkingCapabilitiesForModel(context.Context, string) (ThinkingCapabilities, error)
}

type thinkingQuerier interface {
	queryThinkingCapabilities(context.Context, string) (ThinkingCapabilities, error)
}

func unknownThinking() ThinkingCapabilities {
	return ThinkingCapabilities{Levels: []string{"auto"}, Source: "未知"}
}

func documentedThinking(c Client, model string) ThinkingCapabilities {
	model = strings.ToLower(model)
	var levels []string
	switch c.(type) {
	case *OpenAIClient:
		// 只匹配已核对的模型系列；未知型号不按通用枚举猜测。
		switch {
		case model == "deepseek-flash" || model == "deepseek-v4-pro" || model == "deepseek-chat" || model == "deepseek-reasoner":
			levels = []string{"none", "low", "high", "max"}
		case model == "gpt-5" || model == "gpt-5-mini" || model == "gpt-5-nano" || strings.HasPrefix(model, "gpt-5-2025-") || strings.HasPrefix(model, "gpt-5-mini-2025-") || strings.HasPrefix(model, "gpt-5-nano-2025-"):
			levels = []string{"minimal", "low", "medium", "high"}
		case model == "gpt-6-astra" || model == "gpt-6.1-sol":
			levels = []string{"low", "medium", "high", "xhigh", "max"}
		case model == "gpt-6-sol" || model == "gpt-6-luna":
			levels = []string{"none", "low", "medium", "high", "xhigh", "max"}
		}
	}
	if levels == nil {
		return unknownThinking()
	}
	return ThinkingCapabilities{Levels: append([]string{"auto"}, levels...), Known: true, Source: "文档适配", Note: "接口未提供档位元数据，按已知型号适配。"}
}

// ThinkingCapabilities 返回当前模型的缓存能力或文档适配，不产生网络请求。
func (s *Swappable) ThinkingCapabilities() ThinkingCapabilities {
	return s.ThinkingCapabilitiesForModel("")
}

func (s *Swappable) ThinkingCapabilitiesForModel(model string) ThinkingCapabilities {
	s.mu.RLock()
	c := s.c
	model = firstNonEmptyModel(model, clientModel(c))
	info, ok := s.thinkingCaps[model]
	s.mu.RUnlock()
	if !ok {
		info = documentedThinking(c, model)
	}
	info.Levels = slices.Clone(info.Levels)
	return info
}

func clientModel(c Client) string {
	if m, ok := c.(ModelSwitcher); ok {
		return m.Model()
	}
	return ""
}

// QueryThinkingCapabilities 只读取当前服务商的模型接口，不调用聊天接口做收费探测。
// generation 防止 /login 后旧请求覆盖新客户端的能力缓存。
func (s *Swappable) QueryThinkingCapabilities(ctx context.Context) (ThinkingCapabilities, error) {
	return s.QueryThinkingCapabilitiesForModel(ctx, "")
}

func (s *Swappable) QueryThinkingCapabilitiesForModel(ctx context.Context, model string) (ThinkingCapabilities, error) {
	s.mu.Lock()
	c, generation := s.c, s.generation
	model = firstNonEmptyModel(model, clientModel(c))
	cached, hasCached := s.thinkingCaps[model]
	if s.thinkingQueries == nil {
		s.thinkingQueries = make(map[string]uint64)
	}
	s.thinkingQueries[model]++
	query := s.thinkingQueries[model]
	s.mu.Unlock()
	info := documentedThinking(c, model)
	q, ok := c.(thinkingQuerier)
	if !ok {
		return info, nil
	}
	queried, err := q.queryThinkingCapabilities(ctx, model)
	if err == nil && queried.Known {
		info = queried
	} else if err != nil && hasCached && (cached.Source == "服务端查询" || cached.Source == "服务端缓存") {
		info = cached
		info.Source = "服务端缓存"
	}
	s.mu.Lock()
	if s.generation == generation && s.thinkingQueries[model] == query {
		if s.thinkingCaps == nil {
			s.thinkingCaps = make(map[string]ThinkingCapabilities)
		}
		stored := info
		stored.Levels = slices.Clone(info.Levels)
		s.thinkingCaps[model] = stored
	}
	s.mu.Unlock()
	return info, err
}

type capabilitySupport struct {
	Supported bool `json:"supported"`
}

type modelThinkingMetadata struct {
	ID           string `json:"id"`
	Capabilities *struct {
		Effort   map[string]json.RawMessage `json:"effort"`
		Thinking *struct {
			Supported bool                         `json:"supported"`
			Types     map[string]capabilitySupport `json:"types"`
		} `json:"thinking"`
	} `json:"capabilities"`
}

func (m modelThinkingMetadata) thinkingCapabilities() ThinkingCapabilities {
	if m.Capabilities == nil || m.Capabilities.Thinking == nil {
		return unknownThinking()
	}
	t := m.Capabilities.Thinking
	info := ThinkingCapabilities{Levels: []string{"auto"}, Known: true, Source: "服务端查询"}
	if t.Types["disabled"].Supported {
		info.Levels = append(info.Levels, "none")
		info.offType = "disabled"
	} else if t.Types["between_tools"].Supported {
		info.Levels = append(info.Levels, "none")
		info.offType = "between_tools"
	} else if strings.HasPrefix(m.ID, "claude-sonnet-5-5") {
		// 当前 Models API 没有 between_tools 字段，保留已知型号的最低模式。
		info.Levels = append(info.Levels, "none")
		info.offType = "between_tools"
		info.Note = "none 仅关闭回答前思考（该模式来自型号文档）。"
	}
	if !t.Supported {
		return info
	}
	if t.Types["adaptive"].Supported {
		info.mode = "adaptive"
		if len(m.Capabilities.Effort) == 0 {
			return unknownThinking()
		}
		for _, name := range []string{"low", "medium", "high", "xhigh", "max"} {
			var support capabilitySupport
			if json.Unmarshal(m.Capabilities.Effort[name], &support) == nil && support.Supported {
				info.Levels = append(info.Levels, name)
			}
		}
		return info
	}
	if t.Types["enabled"].Supported {
		info.mode, info.Budget = "enabled", true
		info.Levels = append(info.Levels, "low", "medium", "high", "xhigh", "max")
		info.Note = "模型支持连续 token 预算，菜单显示 miao 的预算预设。"
		return info
	}
	return unknownThinking()
}

func queryModelThinking(ctx context.Context, client *http.Client, endpoint, apiKey, modelID string, anthropic bool) (ThinkingCapabilities, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return unknownThinking(), err
	}
	if anthropic {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return unknownThinking(), fmt.Errorf("查询模型思考档位失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return unknownThinking(), parseAPIError(resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsBody+1))
	if err != nil || len(raw) > maxModelsBody {
		return unknownThinking(), fmt.Errorf("模型能力响应读取失败或超过 4MiB")
	}
	var model modelThinkingMetadata
	if err := json.Unmarshal(raw, &model); err != nil {
		return unknownThinking(), fmt.Errorf("解析模型思考档位失败: %w", err)
	}
	if model.ID == "" {
		var list struct {
			Data []modelThinkingMetadata `json:"data"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return unknownThinking(), err
		}
		for _, item := range list.Data {
			if item.ID == modelID {
				return item.thinkingCapabilities(), nil
			}
		}
	}
	return model.thinkingCapabilities(), nil
}

func (c *OpenAIClient) queryThinkingCapabilities(ctx context.Context, model string) (ThinkingCapabilities, error) {
	info, err := queryModelThinking(ctx, c.http, c.baseURL+"/models/"+url.PathEscape(model), c.apiKey, model, false)
	var apiErr *APIError
	if err == nil && !info.Known || errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 405) {
		return queryModelThinking(ctx, c.http, c.baseURL+"/models", c.apiKey, model, false)
	}
	return info, err
}

func (c *AnthropicClient) queryThinkingCapabilities(ctx context.Context, model string) (ThinkingCapabilities, error) {
	return queryModelThinking(ctx, c.http, c.modelsURL()+"/"+url.PathEscape(model), c.apiKey, model, true)
}
