package session

import (
	"regexp"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
)

var (
	kvSecretRe = regexp.MustCompile(`(?i)((?:api[_-]?key|secret|token|password|authorization|bearer)["']?\s*[=:]\s*["']?)([^\s"'\\]{8,})`)
	skRe       = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,})`)
	// GitHub 令牌：ghp_/gho_/ghu_/ghs_/ghr_ 与细粒度 github_pat_。
	ghTokenRe = regexp.MustCompile(`\b(gh[pousr]_)[A-Za-z0-9]{20,}`)
	ghPATRe   = regexp.MustCompile(`\b(github_pat_)[A-Za-z0-9_]{20,}`)
	// AWS 访问密钥 ID，以及 aws_secret_access_key 这类赋值。
	awsKeyIDRe  = regexp.MustCompile(`\b((?:AKIA|ASIA)[A-Z0-9]{16})\b`)
	awsSecretRe = regexp.MustCompile(`(?i)(aws_?secret_?access_?key["']?\s*[=:]\s*["']?)([A-Za-z0-9/+=]{40})`)
	// PEM 私钥整块。
	pemRe = regexp.MustCompile(`(?s)-----BEGIN ([A-Z0-9 ]*)PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

// RedactSecrets 把常见密钥形态替换成占位符。
func RedactSecrets(s string) string {
	s = pemRe.ReplaceAllString(s, "-----BEGIN ${1}PRIVATE KEY-----\n***\n-----END ${1}PRIVATE KEY-----")
	s = awsSecretRe.ReplaceAllString(s, "${1}***")
	s = awsKeyIDRe.ReplaceAllString(s, "AKIA***")
	s = ghPATRe.ReplaceAllString(s, "${1}***")
	s = ghTokenRe.ReplaceAllString(s, "${1}***")
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
