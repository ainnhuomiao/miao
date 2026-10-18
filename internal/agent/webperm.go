package agent

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

// mentionMarker 之后是 @ 引用附上的文件内容，不算用户亲手输入，不从中提取域名。
const mentionMarker = "\n\n以下是用户用 @ 引用的文件内容："

// reUserHost 只匹配用户消息里显式写出的 http(s):// 网址；裸词（install.sh、README.md）不算域名。
var reUserHost = regexp.MustCompile(`(?i)\bhttps?://((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,63})(?::\d+)?`)

func webHostKey(host string) string { return "web_host:" + strings.ToLower(host) }

// webFetchURLHost 取 web_fetch 参数里网址的主机名（小写）。
func webFetchURLHost(args json.RawMessage) string {
	var in struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(args, &in) != nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// userHosts 返回本会话用户亲手输入过的主机名（小写）。
func userHosts(history []llm.Message) map[string]bool {
	out := map[string]bool{}
	for _, m := range history {
		if m.Role != llm.RoleUser {
			continue
		}
		text, _, _ := strings.Cut(m.Content, mentionMarker)
		for _, g := range reUserHost.FindAllStringSubmatch(text, -1) {
			out[strings.ToLower(g[1])] = true
		}
	}
	return out
}

// maxAutoPathLen 是免审批网址路径的长度上限；更长的路径可能夹带外传数据，需审批。
const maxAutoPathLen = 100

// suspiciousPath 判断路径是否过长，或含疑似编码数据的高熵段（如 base64/hex 串）。
func suspiciousPath(u *url.URL) bool {
	p := u.EscapedPath()
	if len(p) > maxAutoPathLen {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if (len(seg) >= 24 && shannonEntropy(seg) >= 4.2) || (len(seg) >= 32 && isHex(seg)) {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	return strings.Trim(s, "0123456789abcdefABCDEF") == ""
}

func shannonEntropy(s string) float64 {
	var freq [256]int
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	var h float64
	n := float64(len(s))
	for _, c := range freq {
		if c > 0 {
			q := float64(c) / n
			h -= q * math.Log2(q)
		}
	}
	return h
}

// webURLAllowed 是 web_fetch 的免审批规则（也用于每一跳重定向）：
// 网址不带查询串、路径不过长且无高熵段，且主机名出现在用户本会话消息的 http(s):// 网址里，或已在审批卡上按 A 放行。
func (a *Agent) webURLAllowed(u *url.URL) bool {
	if u == nil || u.RawQuery != "" || u.ForceQuery || (u.Scheme != "http" && u.Scheme != "https") || suspiciousPath(u) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	a.perm.mu.Lock()
	ok := a.perm.session[webHostKey(host)]
	a.perm.mu.Unlock()
	return ok || userHosts(a.History())[host]
}

// approveWebFetch：满足规则直接放行；否则交给审批卡（显示完整网址）。
// 只读规划模式同样适用：web_fetch 只读，不直接拒绝，但未放行的仍要审批。
func (a *Agent) approveWebFetch(ctx context.Context, fn tools.Approver, display string, args json.RawMessage) (bool, error) {
	var in struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(args, &in)
	if u, err := url.Parse(strings.TrimSpace(in.URL)); err == nil && a.webURLAllowed(u) {
		return true, nil
	}
	if fn == nil {
		return false, nil
	}
	return fn(ctx, "web_fetch", display, args)
}
