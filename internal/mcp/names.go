package mcp

import (
	"fmt"
	"strings"
)

const maxToolName = 64

// sanitize 把名字里不符合 ^[a-zA-Z0-9_-]$ 的字符替换成 _。
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	return b.String()
}

// toolName 生成 服务器名__工具名，满足 OpenAI 函数名规则（≤64），与 taken 中已有名字去重。
func toolName(server, tool string, taken map[string]bool) string {
	s, t := sanitize(server), sanitize(tool)
	if len(s) > 24 {
		s = s[:24]
	}
	base := s + "__" + t
	if len(base) > maxToolName {
		base = base[:maxToolName]
	}
	name := base
	for i := 2; taken[name]; i++ {
		suf := fmt.Sprintf("_%d", i)
		cut := base
		if len(cut)+len(suf) > maxToolName {
			cut = cut[:maxToolName-len(suf)]
		}
		name = cut + suf
	}
	taken[name] = true
	return name
}
