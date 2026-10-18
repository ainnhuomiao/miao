// Package childenv 负责给 miao 启动的子进程过滤环境变量，防止 API Key 等密钥泄露给子进程。
package childenv

import (
	"os"
	"strings"
)

// Sensitive 报告变量名是否像密钥，或属于 miao 自身（含旧前缀 AGNET_）配置。
func Sensitive(key string) bool {
	k := strings.ToUpper(key)
	for _, s := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	for _, p := range []string{"MIAO_", "AGNET_", "OPENAI_", "DEEPSEEK_"} {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// Strip 返回去掉敏感变量后的环境副本，其余变量原样保留。
func Strip(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || Sensitive(k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Environ 是去掉敏感变量后的 os.Environ()，供 run_command、hooks 等子进程使用。
func Environ() []string { return Strip(os.Environ()) }
