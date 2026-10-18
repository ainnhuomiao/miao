package llm

import (
	"strings"
	"time"
)

// DetectProtocol 根据显式协议或 Base URL 选择 openai / anthropic。
func DetectProtocol(protocol, baseURL string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	if p != "" && p != "auto" {
		return p
	}
	if strings.Contains(strings.ToLower(baseURL), "anthropic") {
		return "anthropic"
	}
	return "openai"
}

// NewClient 按协议创建客户端。
func NewClient(protocol, apiKey, baseURL, model string) Client {
	switch DetectProtocol(protocol, baseURL) {
	case "anthropic":
		return NewAnthropic(apiKey, baseURL, model)
	default:
		return NewOpenAI(apiKey, baseURL, model)
	}
}

type configurable interface {
	SetTimeout(time.Duration)
	SetRetries(int)
	SetDebug(bool)
}

// ApplySettings 设置超时、重试和 debug；不支持的客户端忽略。
func ApplySettings(c Client, timeout time.Duration, retries int, debug bool) {
	s, ok := c.(configurable)
	if !ok {
		return
	}
	if timeout > 0 {
		s.SetTimeout(timeout)
	}
	if retries >= 0 {
		s.SetRetries(retries)
	}
	s.SetDebug(debug)
}
