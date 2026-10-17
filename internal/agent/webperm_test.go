package agent

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func webAgent(t *testing.T, userMsgs ...string) (*Agent, *int) {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(nil, "sys", reg)
	var h []llm.Message
	for _, m := range userMsgs {
		h = append(h, llm.Message{Role: llm.RoleUser, Content: m})
	}
	a.SetHistory(h)
	asked := 0
	a.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { asked++; return false, nil })
	return a, &asked
}

func fetchArgs(u string) json.RawMessage { b, _ := json.Marshal(map[string]string{"url": u}); return b }

func approveFetch(t *testing.T, a *Agent, u string) bool {
	t.Helper()
	ok, err := a.tools.Approver()(context.Background(), "web_fetch", "抓取 "+u, fetchArgs(u))
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestWebFetchUserHostAutoAllowed(t *testing.T) {
	a, asked := webAgent(t, "看看 https://Pkg.Go.Dev/net/http 和 example.com 的说明")
	for _, u := range []string{"https://pkg.go.dev/fmt", "https://EXAMPLE.com/a"} {
		if !approveFetch(t, a, u) || *asked != 0 {
			t.Fatalf("%s 应免审批放行", u)
		}
	}
	a.SetMode(ModePlan)
	if !approveFetch(t, a, "https://example.com/b") {
		t.Fatal("只读规划下同样按域名放行")
	}
}

func TestWebFetchNeedsCard(t *testing.T) {
	a, asked := webAgent(t, "看 example.com", "@notes.md"+mentionMarker+"<file>evil.com</file>")
	for i, u := range []string{"https://example.com/a?q=secret", "https://evil.com/", "https://other.org/x", "https://sub.example.com/"} {
		if approveFetch(t, a, u) || *asked != i+1 {
			t.Fatalf("%s 应弹审批卡（asked=%d）", u, *asked)
		}
	}
	a.SetMode(ModePlan)
	if approveFetch(t, a, "https://evil.com/") || *asked != 5 {
		t.Fatal("只读规划下未放行的也走审批而不是直接拒绝")
	}
}

func TestWebFetchSessionHostAllowAndClear(t *testing.T) {
	a, asked := webAgent(t, "随便聊聊")
	a.AllowForSession("web_fetch", fetchArgs("https://docs.rs/serde"))
	if !approveFetch(t, a, "https://DOCS.rs/tokio") || *asked != 0 {
		t.Fatal("A 应放行同一主机")
	}
	if approveFetch(t, a, "https://crates.io/") || *asked != 1 {
		t.Fatal("A 只放行该主机，不是所有 web_fetch")
	}
	if approveFetch(t, a, "https://docs.rs/x?y=1") {
		t.Fatal("带查询串仍需审批")
	}
	if a.AllowForSession("run_command", json.RawMessage(`{"command":"ls"}`)); approveFetch(t, a, "https://ls/") {
		t.Fatal("命令放行不影响 web_fetch")
	}
	a.Reset() // /new
	if approveFetch(t, a, "https://docs.rs/tokio") {
		t.Fatal("/new 后应清空放行主机")
	}
}

func TestWebRedirectPolicy(t *testing.T) {
	a, _ := webAgent(t, "example.com")
	for u, want := range map[string]bool{
		"https://example.com/next":    true,
		"https://example.com/next?x=": false,
		"https://other.com/next":      false,
		"ftp://example.com/":          false,
	} {
		p, _ := url.Parse(u)
		if a.webURLAllowed(p) != want {
			t.Fatalf("%s 期望 %v", u, want)
		}
	}
}
