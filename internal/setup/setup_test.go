package setup

import (
	"context"
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
