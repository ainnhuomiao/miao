package session

import (
	"regexp"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
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

// RedactForExport 在导出时脱敏；MIAO_SESSION_REDACT=0 时原样返回。
func RedactForExport(s string) string {
	if !redactEnabled() {
		return s
	}
	return RedactSecrets(s)
}

func redactEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(config.Getenv("SESSION_REDACT")))
	return v != "0" && v != "false" && v != "off"
}
