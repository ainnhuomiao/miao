package session

import (
	"regexp"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
)

var (
	kvSecretRe = regexp.MustCompile(`(?i)((?:api[_-]?key|secret|token|password|authorization|bearer)["']?\s*[=:]\s*["']?)([^\s"'\\]{8,})`)
	skRe       = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,})`)
)

// RedactSecrets 把常见密钥形态替换成占位符。
func RedactSecrets(s string) string {
	s = kvSecretRe.ReplaceAllString(s, "${1}***")
	s = skRe.ReplaceAllString(s, "sk-***")
	return s
}

func redactEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(config.Getenv("SESSION_REDACT")))
	return v != "0" && v != "false" && v != "off"
}

func redactMessages(msgs []llm.Message) []llm.Message {
	if !redactEnabled() {
		return msgs
	}
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].Content = RedactSecrets(out[i].Content)
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		tcs := append([]llm.ToolCall(nil), out[i].ToolCalls...)
		for j := range tcs {
			tcs[j].Function.Arguments = RedactSecrets(tcs[j].Function.Arguments)
		}
		out[i].ToolCalls = tcs
	}
	return out
}
