package agent

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

// mentionMarker 之后是 @ 引用附上的文件内容，不算用户亲手输入，不从中提取域名。
const mentionMarker = "\n\n以下是用户用 @ 引用的文件内容："

// reUserHost 匹配用户消息里的网址或裸域名（example.com、docs.go.dev:8080）。
var reUserHost = regexp.MustCompile(`(?i)(?:https?://)?((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,63})(?::\d+)?`)

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

// webURLAllowed 是 web_fetch 的免审批规则（也用于每一跳重定向）：
// 网址不带查询串，且主机名出现在用户本会话消息里，或已在审批卡上按 A 放行。
func (a *Agent) webURLAllowed(u *url.URL) bool {
	if u == nil || u.RawQuery != "" || u.ForceQuery || (u.Scheme != "http" && u.Scheme != "https") {
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
