package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func openAICreds(url string) config.Credentials {
	p, _ := ProviderByID("openai")
	return Credentials(p, " sk-test ", url+"/")
}

func TestCredentialsBuiltinURL(t *testing.T) {
	p, _ := ProviderByID("anthropic")
	c := Credentials(p, "k", "https://ignored")
	if c.BaseURL != "https://api.anthropic.com" || c.Protocol != "anthropic" {
		t.Fatalf("内置服务商不用用户地址：%+v", c)
	}
	c = openAICreds("https://x.example/v1")
	if c.BaseURL != "https://x.example/v1" || c.APIKey != "sk-test" {
		t.Fatalf("%+v", c)
	}
}

func TestDefaultTesterListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-b"},{"id":"m-a"}]}`))
	}))
	defer srv.Close()
	c, err := DefaultTester(context.Background(), openAICreds(srv.URL))
	if err != nil || c.Model != "m-a" {
		t.Fatalf("应列出模型并补全：%+v %v", c, err)
	}
}

func TestDefaultTesterAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	_, err := DefaultTester(context.Background(), openAICreds(srv.URL))
	te := Classify(err)
	if te == nil || te.Kind != KindAuth || !strings.Contains(te.Error(), "key 错了") {
		t.Fatalf("401 应判为 key 错了：%v", err)
	}
	if strings.Contains(te.Error(), "sk-test") {
		t.Fatal("错误信息里不能有 key")
	}
}

func TestDefaultTesterFallbackCompletion(t *testing.T) {
	var chat bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(404)
			return
		}
		chat = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"h\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := openAICreds(srv.URL)
	c.Model = "m"
	if _, err := DefaultTester(context.Background(), c); err != nil || !chat {
		t.Fatalf("不支持列模型时应退回 1 token 补全：chat=%v err=%v", chat, err)
	}
}

func TestDefaultTesterNetwork(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, err = DefaultTester(context.Background(), openAICreds("http://"+addr))
	if te := Classify(err); te == nil || te.Kind != KindNetwork || !strings.Contains(te.Error(), "网络不通") {
		t.Fatalf("连不上应判为网络不通：%v", err)
	}
}

func TestClassify(t *testing.T) {
	if Classify(&llm.APIError{StatusCode: 403}).Kind != KindAuth {
		t.Fatal("403")
	}
	if Classify(&llm.APIError{StatusCode: 500}).Kind != KindOther {
		t.Fatal("500")
	}
	if Classify(context.DeadlineExceeded).Kind != KindNetwork {
		t.Fatal("超时")
	}
	if Classify(errors.New("x")).Kind != KindOther {
		t.Fatal("其它")
	}
}

func TestValidateBaseURL(t *testing.T) {
	for _, ok := range []string{"https://a.example/v1", "http://localhost:8080"} {
		if ValidateBaseURL(ok) != nil {
			t.Fatal(ok)
		}
	}
	for _, bad := range []string{"", "a.example", "ftp://x"} {
		if ValidateBaseURL(bad) == nil {
			t.Fatal(bad)
		}
	}
}

func anthropicCreds(url string) config.Credentials {
	p, _ := ProviderByID("anthropic")
	c := Credentials(p, "sk-ant", "")
	c.BaseURL = url
	return c
}

func TestAnthropicPicksNewestSonnet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "sk-ant" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"claude-sonnet-4-5-20250929","created_at":"2025-09-29T00:00:00Z"},
			{"id":"claude-opus-9","created_at":"2026-09-01T00:00:00Z"},
			{"id":"claude-sonnet-5-6-20260801","created_at":"2026-08-01T00:00:00Z"},
			{"id":"claude-haiku-4-5","created_at":"2025-10-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	c, err := DefaultTester(context.Background(), anthropicCreds(srv.URL))
	if err != nil || c.Model != "claude-sonnet-5-6-20260801" {
		t.Fatalf("应选最新的 sonnet：%q %v", c.Model, err)
	}
}

func TestAnthropicNoSonnetPicksNewest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-old","created_at":"2024-01-01T00:00:00Z"},{"id":"claude-new","created_at":"2026-01-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	c, err := DefaultTester(context.Background(), anthropicCreds(srv.URL))
	if err != nil || c.Model != "claude-new" {
		t.Fatalf("没有 sonnet 时选最新的：%q %v", c.Model, err)
	}
}

func TestAnthropicListUnsupportedFallsBackToDefault(t *testing.T) {
	var chatModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(404)
			return
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		chatModel = body.Model
		if body.MaxTokens != 1 {
			t.Errorf("max_tokens=%d", body.MaxTokens)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()
	c, err := DefaultTester(context.Background(), anthropicCreds(srv.URL))
	if err != nil || c.Model != "claude-sonnet-5-5" || chatModel != "claude-sonnet-5-5" {
		t.Fatalf("列模型不支持时用内置默认：%q chat=%q %v", c.Model, chatModel, err)
	}
}

func TestDeepSeekKeepsDefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-reasoner"},{"id":"deepseek-chat"}]}`))
	}))
	defer srv.Close()
	p, _ := ProviderByID("deepseek")
	c := Credentials(p, "k", "")
	c.BaseURL = srv.URL
	got, err := DefaultTester(context.Background(), c)
	if err != nil || got.Model != "deepseek-chat" {
		t.Fatalf("DeepSeek 保持原逻辑：%q %v", got.Model, err)
	}
}

func TestAnthropicPicksSonnetByLine(t *testing.T) {
	// 带 line 字段时只按 line 选：id 含 sonnet 但 line 不是 sonnet 的不算，
	// id 不含 sonnet 但 line 是 sonnet 的算；sonnet 中按 created_at 取最新（与返回顺序无关）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"claude-sonnet-lookalike","created_at":"2026-10-01T00:00:00Z","line":"opus"},
			{"id":"claude-s-5-4","created_at":"2026-03-01T00:00:00Z","line":"sonnet"},
			{"id":"claude-s-5-7","created_at":"2026-09-01T00:00:00Z","line":"sonnet"},
			{"id":"claude-haiku-5","created_at":"2026-09-15T00:00:00Z","line":"haiku"}]}`))
	}))
	defer srv.Close()
	c, err := DefaultTester(context.Background(), anthropicCreds(srv.URL))
	if err != nil || c.Model != "claude-s-5-7" {
		t.Fatalf("应按 line 选最新的 sonnet：%q %v", c.Model, err)
	}
}

func TestAnthropicNoLineFallsBackToIDMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"claude-opus-9","created_at":"2026-09-01T00:00:00Z"},
			{"id":"claude-sonnet-5-1","created_at":"2026-01-01T00:00:00Z"},
			{"id":"claude-sonnet-5-6","created_at":"2026-08-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	c, err := DefaultTester(context.Background(), anthropicCreds(srv.URL))
	if err != nil || c.Model != "claude-sonnet-5-6" {
		t.Fatalf("没有 line 时按 id 匹配 sonnet：%q %v", c.Model, err)
	}
}
