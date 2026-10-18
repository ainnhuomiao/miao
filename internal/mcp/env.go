package mcp

import (
	"strings"

	"github.com/ainnhuomiao/miao/internal/childenv"
)

// safeEnvKeys 是允许从 miao 继承给 MCP 子进程的环境变量（精确匹配）。
var safeEnvKeys = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LANGUAGE": true, "TERM": true, "TMPDIR": true, "TZ": true,
	"NIX_PATH": true, "NIX_PROFILES": true, "NIX_SSL_CERT_FILE": true, "SSL_CERT_FILE": true,
	"LOCALE_ARCHIVE": true,
}

func inheritable(key string) bool {
	if childenv.Sensitive(key) {
		return false
	}
	return safeEnvKeys[key] || strings.HasPrefix(key, "LC_") || strings.HasPrefix(key, "XDG_")
}

// buildEnv 从父进程环境挑出最小安全子集，再叠加该服务器显式配置的 env。
// 绝不整体透传 miao 的环境（里面有从 .env 读入的 API Key）。
func buildEnv(parent []string, extra map[string]string) []string {
	var out []string
	for _, kv := range parent {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !inheritable(k) {
			continue
		}
		if _, override := extra[k]; override {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
