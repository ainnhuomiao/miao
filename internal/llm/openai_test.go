package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamParsesSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("方法 = %s, 想要 POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("路径 = %s, 想要 /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		if req.Model != "test-model" || !req.Stream {
			t.Errorf("请求参数不正确: %+v", req)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := NewOpenAI("test-key", srv.URL, "test-model")

	var got strings.Builder
	err := client.Stream(context.Background(),
		[]Message{{Role: RoleUser, Content: "hi"}},
		func(d Delta) error {
			got.WriteString(d.Content)
			return nil
		})
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}
	if want := "你好"; got.String() != want {
		t.Errorf("拼出的回答 = %q, 想要 %q", got.String(), want)
	}
}

func TestStreamAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	client := NewOpenAI("bad-key", srv.URL, "test-model")
	err := client.Stream(context.Background(),
		[]Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T, 想要 *APIError: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("状态码 = %d, 想要 401", apiErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("错误信息应包含服务端原因: %v", err)
	}
}
