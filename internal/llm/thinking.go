package llm

import (
	"fmt"
	"strings"
)

// NormalizeThinking 校验强度；auto 表示省略参数，off/default 是便捷别名。
func NormalizeThinking(value string) (string, error) {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "", "default", "auto":
		return "auto", nil
	case "off", "none":
		return "none", nil
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return value, nil
	default:
		return "", fmt.Errorf("未知思考强度 %q，请用 /thinking 查询当前模型可用档位", value)
	}
}

// ThinkingController 控制后续请求，不影响已经发出的请求。
type ThinkingController interface {
	Thinking() string
	SetThinking(string) error
}

func (s *Swappable) Thinking() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.thinking == "" {
		return "auto"
	}
	return s.thinking
}

// SetThinking 的状态属于外层客户端，切换模型或 /login 后仍然保留。
func (s *Swappable) SetThinking(value string) error {
	level, err := NormalizeThinking(value)
	if err != nil {
		return err
	}
	if info := s.ThinkingCapabilities(); !info.Supports(level) {
		return fmt.Errorf("当前模型不支持思考强度 %s；可选：%s", level, strings.Join(info.Levels, " / "))
	}
	s.mu.Lock()
	s.thinking = level
	s.mu.Unlock()
	return nil
}

type thinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type outputConfig struct {
	Effort string `json:"effort"`
}

// configureAnthropicThinking 区分旧模型的预算模式和新模型的 adaptive 模式。
func configureAnthropicThinking(out *anthRequest, value string, explicitMax bool) error {
	return configureAnthropicThinkingMode(out, value, explicitMax, "", "")
}

func configureAnthropicThinkingMode(out *anthRequest, value string, explicitMax bool, mode, offType string) error {
	effort, err := NormalizeThinking(value)
	if err != nil || effort == "auto" {
		return err
	}
	model := strings.ToLower(out.Model)
	if effort == "none" {
		if offType != "" {
			out.Thinking = &thinkingConfig{Type: offType}
			return nil
		}
		if strings.Contains(model, "fable") || strings.Contains(model, "mythos") || strings.Contains(model, "opus-5-5") {
			return fmt.Errorf("模型 %s 不支持关闭思考，请使用 low 或 auto", out.Model)
		}
		out.Thinking = &thinkingConfig{Type: "disabled"}
		if strings.Contains(model, "sonnet-5-5") {
			out.Thinking.Type = "between_tools"
		}
		return nil
	}
	legacy := strings.Contains(model, "claude-3-7") || strings.Contains(model, "claude-3.7")
	for _, family := range []string{"opus", "sonnet", "haiku"} {
		prefix := "claude-" + family + "-4"
		if strings.HasPrefix(model, prefix) {
			rest := strings.TrimPrefix(model, prefix)
			legacy = rest == "" || strings.HasPrefix(rest, "-0") || strings.HasPrefix(rest, "-1") || strings.HasPrefix(rest, "-5") || strings.HasPrefix(rest, "-202")
		}
	}
	if mode != "" {
		legacy = mode == "enabled"
	}
	if legacy {
		budget := map[string]int{"minimal": 1024, "low": 1024, "medium": 4096, "high": 8192, "xhigh": 16384, "max": 24576}[effort]
		if !explicitMax {
			out.MaxTokens = max(out.MaxTokens, budget+4096)
		}
		// 留出正文空间；显式的小输出上限不能被静默扩大。
		if out.MaxTokens <= budget {
			budget = out.MaxTokens - 1
		}
		if budget < 1024 {
			return fmt.Errorf("扩展思考要求 max_tokens 大于 1024")
		}
		out.Thinking = &thinkingConfig{Type: "enabled", BudgetTokens: budget}
		return nil
	}
	if effort == "minimal" {
		effort = "low"
	}
	out.Thinking = &thinkingConfig{Type: "adaptive"}
	out.OutputConfig = &outputConfig{Effort: effort}
	return nil
}
