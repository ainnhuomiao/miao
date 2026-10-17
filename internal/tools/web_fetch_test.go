package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTMLToMarkdown(t *testing.T) {
	src := `<html><head><title>文档</title><style>x{}</style><script>alert(1)</script></head>
<body><nav>导航</nav><main>
<h2>安装  步骤</h2>
<p>运行 <code>go get</code> 然后看 <a href="https://go.dev">官网</a>，<strong>重要</strong>。</p>
<ul><li>一</li><li>二<ol><li>甲</li><li>乙</li></ol></li></ul>
<pre><code class="language-go">func main() {
	fmt.Println("hi")
}</code></pre>
<blockquote><p>引用</p></blockquote>
</main><footer>页脚</footer></body></html>`
	got := htmlToMarkdown(src)
	for _, want := range []string{
		"# 文档", "## 安装 步骤", "运行 `go get` 然后看 [官网](https://go.dev)，**重要**。",
		"- 一", "- 二", "  1. 甲", "  2. 乙",
		"```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```", "> 引用",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q\n---\n%s", want, got)
		}
	}
	for _, bad := range []string{"alert", "导航", "页脚", "x{}"} {
		if strings.Contains(got, bad) {
			t.Errorf("不该出现 %q", bad)
		}
	}
}

func newFetchReg(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	return r
}

func TestWebFetchTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<html><body><h1>你好</h1><p>正文</p></body></html>"))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"a":1}`))
		case "/big":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(strings.Repeat("火", webFetchMaxOutput)))
		case "/bin":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0x89, 'P', 'N', 'G'})
		case "/redirect":
			http.Redirect(w, req, "/page", http.StatusFound)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	webFetchAllowPrivate = true
	defer func() { webFetchAllowPrivate = false }()
	r := newFetchReg(t)
	call := func(u string) Result {
		return r.Call(context.Background(), "web_fetch", `{"url":"`+u+`"}`)
	}

	if res := call(srv.URL + "/page"); res.IsError || !strings.Contains(res.Content, "# 你好") || !strings.Contains(res.Content, "正文") {
		t.Fatalf("html: %+v", res)
	}
	if res := call(srv.URL + "/json"); res.IsError || !strings.Contains(res.Content, `{"a":1}`) {
		t.Fatalf("json: %+v", res)
	}
	if res := call(srv.URL + "/redirect"); res.IsError || !strings.Contains(res.Content, "URL: "+srv.URL+"/page") {
		t.Fatalf("重定向后应显示最终网址: %+v", res)
	}
	res := call(srv.URL + "/big")
	if res.IsError || !strings.Contains(res.Content, "已截断") || len(res.Content) > webFetchMaxOutput+300 {
		t.Fatalf("截断: err=%v len=%d", res.IsError, len(res.Content))
	}
	errCases := map[string]string{
		srv.URL + "/missing": "HTTP 404",
		srv.URL + "/bin":     "不支持的内容类型",
		"ftp://x.com/a":      "只支持 http/https",
		"not a url":          "只支持 http/https",
	}
	for u, want := range errCases {
		if res := call(u); !res.IsError || !strings.Contains(res.Content, want) {
			t.Errorf("%s: 期望含 %q，得到 %+v", u, want, res)
		}
	}
}

func TestWebFetchTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-block:
		case <-req.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	webFetchAllowPrivate = true
	defer func() { webFetchAllowPrivate = false }()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := webFetch(ctx, srv.URL); err == nil {
		t.Fatal("超时应报错")
	}
}

func TestWebFetchBlocksPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer srv.Close()
	for _, u := range []string{srv.URL, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)} {
		if _, err := webFetch(context.Background(), u); err == nil || !strings.Contains(err.Error(), "不允许访问内网地址") {
			t.Errorf("%s 应被拒绝，得到 %v", u, err)
		}
	}
	for ip, want := range map[string]bool{"10.1.2.3": true, "172.20.0.1": true, "172.32.0.1": false, "192.168.1.1": true, "169.254.169.254": true, "::1": true, "100.64.0.1": true, "8.8.8.8": false, "2606:4700::1111": false} {
		if got := isPrivateIP(net.ParseIP(ip)); got != want {
			t.Errorf("isPrivateIP(%s)=%v want %v", ip, got, want)
		}
	}
}
