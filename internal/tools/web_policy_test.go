package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebFetchRedirectRecheck(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("other")) }))
	defer other.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/to-other":
			http.Redirect(w, req, other.URL+"/x", http.StatusFound)
		case "/to-query":
			http.Redirect(w, req, "/ok?leak=1", http.StatusFound)
		case "/to-same":
			http.Redirect(w, req, "/ok", http.StatusFound)
		default:
			w.Write([]byte("fine"))
		}
	}))
	defer srv.Close()
	webFetchAllowPrivate = true
	defer func() { webFetchAllowPrivate = false }()
	r := newFetchReg(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	// 测试服务都在 127.0.0.1，用 host:port 区分“主机”。
	r.SetWebPolicy(func(u *url.URL) bool { return u.Host == host && u.RawQuery == "" })
	r.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { return true, nil }) // 原地址经审批卡批准
	call := func(p string) Result { return r.Call(context.Background(), "web_fetch", `{"url":"`+srv.URL+p+`"}`) }

	if res := call("/to-same"); res.IsError || !strings.Contains(res.Content, "fine") {
		t.Fatalf("同主机无查询串的跳转应放行：%+v", res)
	}
	for _, p := range []string{"/to-other", "/to-query"} {
		res := call(p)
		if !res.IsError || !strings.Contains(res.Content, "重定向到未放行的地址") || !strings.Contains(res.Content, "需重新审批") {
			t.Fatalf("%s 应拦截跳转：%+v", p, res)
		}
	}
}

func TestWebFetchDeniedByApprover(t *testing.T) {
	r := newFetchReg(t)
	r.SetWebPolicy(func(*url.URL) bool { return false })
	r.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { return false, nil })
	res := r.Call(context.Background(), "web_fetch", `{"url":"https://example.com/?a=1"}`)
	if !res.IsError || !strings.Contains(res.Content, "用户拒绝抓取") {
		t.Fatalf("%+v", res)
	}
}
