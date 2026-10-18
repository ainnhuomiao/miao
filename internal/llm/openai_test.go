package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamChat_ContentAndReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth: %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}
		writeSSE(w, flusher, `{"choices":[{"delta":{"reasoning_content":"想一下"}}]}`)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"你好"}}]}`)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"，世界"},"finish_reason":"stop"}]}`)
		writeSSE(w, flusher, "[DONE]")
	}))
	defer srv.Close()

	client := NewOpenAI("test-key", srv.URL, "test-model")
	var deltas []Delta
	result, err := client.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(d Delta) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if result.Content != "你好，世界" {
		t.Fatalf("content = %q", result.Content)
	}
	if result.Reasoning != "想一下" {
		t.Fatalf("reasoning = %q", result.Reasoning)
	}
	if result.FinishReason != "stop" {
		t.Fatalf("finish = %q", result.FinishReason)
	}
	if len(deltas) != 3 {
		t.Fatalf("delta count = %d", len(deltas))
	}
}

func TestStreamChat_ToolCallsByIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
			t.Fatalf("tools = %+v", req.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}`)
		writeSSE(w, flusher, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`)
		writeSSE(w, flusher, "[DONE]")
	}))
	defer srv.Close()

	client := NewOpenAI("test-key", srv.URL, "test-model")
	result, err := client.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "read"}},
		Tools: []ToolDef{{
			Type: "function",
			Function: FunctionDef{
				Name:        "read_file",
				Description: "read",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if result.FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" {
		t.Fatalf("tool call = %+v", tc)
	}
	if tc.Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("args = %q", tc.Function.Arguments)
	}
}

func TestStreamChat_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	client := NewOpenAI("bad", srv.URL, "m")
	_, err := client.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "x"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("message = %v", err)
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, data string) {
	_, _ = w.Write([]byte("data: " + data + "\n\n"))
	f.Flush()
}

func TestStreamChatUsageChunk(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(w, flusher, `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":64}}`)
		writeSSE(w, flusher, `[DONE]`)
	}))
	defer srv.Close()

	res, err := NewOpenAI("k", srv.URL, "m").StreamChat(context.Background(), ChatRequest{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := gotBody["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Fatalf("请求未带 stream_options.include_usage: %v", gotBody["stream_options"])
	}
	if res.Content != "hi" {
		t.Fatalf("content = %q", res.Content)
	}
	want := Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, PromptCacheHitTokens: 64}
	if res.Usage == nil || *res.Usage != want {
		t.Fatalf("usage = %+v, want %+v", res.Usage, want)
	}
}

func TestPassReasoningContent(t *testing.T) {
	if PassReasoningContent("deepseek-reasoner") {
		t.Fatal("早期 reasoner 不应回传 thinking")
	}
	if !PassReasoningContent("deepseek-chat") || !PassReasoningContent("deepseek-flash") {
		t.Fatal("其它 DeepSeek 模型应回传")
	}
}

func TestStreamChatOmitsReasoningForReasoner(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(w, flusher, `[DONE]`)
	}))
	defer srv.Close()
	req := ChatRequest{Messages: []Message{{Role: RoleAssistant, Content: "hi", ReasoningContent: "think"}}}
	if _, err := NewOpenAI("k", srv.URL, "deepseek-reasoner").StreamChat(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "reasoning_content") {
		t.Fatalf("reasoner 请求不应带 reasoning_content: %s", body)
	}
	if _, err := NewOpenAI("k", srv.URL, "deepseek-chat").StreamChat(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "reasoning_content") {
		t.Fatalf("chat 请求应回传 reasoning_content: %s", body)
	}
}

func TestStreamChatCachedTokensDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(w, flusher, `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":40}}}`)
		writeSSE(w, flusher, `[DONE]`)
	}))
	defer srv.Close()

	res, err := NewOpenAI("k", srv.URL, "m").StreamChat(context.Background(), ChatRequest{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage == nil || res.Usage.PromptCacheHitTokens != 40 {
		t.Fatalf("cached_tokens 未解析: %+v", res.Usage)
	}
}

func TestStreamChatNoUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(w, flusher, `[DONE]`)
	}))
	defer srv.Close()

	res, err := NewOpenAI("k", srv.URL, "m").StreamChat(context.Background(), ChatRequest{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage != nil {
		t.Fatalf("usage = %+v, want nil", res.Usage)
	}
}

func TestStreamChat_RetryThenOK(t *testing.T) {
	old := backoffUnit
	backoffUnit = time.Millisecond
	t.Cleanup(func() { backoffUnit = old })

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"busy"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		writeSSE(w, flusher, "[DONE]")
	}))
	defer srv.Close()

	c := NewOpenAI("k", srv.URL, "m")
	c.SetRetries(3)
	res, err := c.StreamChat(context.Background(), ChatRequest{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "ok" {
		t.Fatalf("content = %q", res.Content)
	}
	if n.Load() != 3 {
		t.Fatalf("calls = %d", n.Load())
	}
}

func TestStreamChat_NoRetryAfterPartialOutput(t *testing.T) {
	old := backoffUnit
	backoffUnit = time.Millisecond
	t.Cleanup(func() { backoffUnit = old })

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"delta":{"content":"hello"}}]}`)
		flusher.Flush()
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				if tc, ok := conn.(*net.TCPConn); ok {
					_ = tc.SetLinger(0)
				}
				_ = conn.Close()
			}
			return
		}
	}))
	defer srv.Close()

	c := NewOpenAI("k", srv.URL, "m")
	c.SetRetries(3)
	var got strings.Builder
	_, err := c.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(d Delta) error {
		got.WriteString(d.Content)
		return nil
	})
	if n.Load() != 1 {
		t.Fatalf("已有输出后不应重试，calls=%d err=%v", n.Load(), err)
	}
	if strings.Count(got.String(), "hello") != 1 {
		t.Fatalf("界面增量被重复推送：%q err=%v", got.String(), err)
	}
}

func TestStreamChat_NoRetryOn4xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad"}}`))
	}))
	defer srv.Close()
	c := NewOpenAI("k", srv.URL, "m")
	c.SetRetries(3)
	_, err := c.StreamChat(context.Background(), ChatRequest{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("should not retry 400, calls = %d", n.Load())
	}
}

func TestAPIErrorUnwrapAndRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow"}}`))
	}))
	defer srv.Close()
	c := NewOpenAI("k", srv.URL, "m")
	c.SetRetries(0)
	_, err := c.StreamChat(context.Background(), ChatRequest{}, nil)
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("want APIError, got %T %v", err, err)
	}
	if api.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter = %s", api.RetryAfter)
	}
	if errors.Unwrap(api) != nil && api.Unwrap() != api.err {
		t.Fatal("Unwrap should be implemented")
	}
}

func TestStreamChat_TimeoutVsCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := NewOpenAI("k", srv.URL, "m")
	c.SetRetries(0)
	c.SetTimeout(50 * time.Millisecond)
	t0 := time.Now()
	_, err := c.StreamChat(context.Background(), ChatRequest{}, nil)
	if time.Since(t0) > time.Second {
		t.Fatalf("timeout path too slow: %s err=%v", time.Since(t0), err)
	}
	var to *TimeoutError
	if !errors.As(err, &to) {
		t.Fatalf("want TimeoutError, got %T %v", err, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.SetTimeout(time.Minute)
	_, err = c.StreamChat(ctx, ChatRequest{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}
