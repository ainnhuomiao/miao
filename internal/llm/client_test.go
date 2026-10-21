package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// 用内存响应替换 HTTP 传输层：测试完整请求编码和响应解析，不联网也不消耗 API 额度。
type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 原始字符串用反引号包围，JSON 中的双引号无需转义。
const goodStream = `: heartbeat

data: {"choices":[{"index":0,"delta":{"content":"你"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"好"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

func TestChatEncodesRequestAndStreamsText(t *testing.T) {
	c, err := New("test-secret", "https://example.com/v1/", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://example.com/v1/chat/completions" {
			t.Fatalf("请求地址或方法错误：%s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer test-secret" || req.Header.Get("Content-Type") != "application/json" {
			t.Fatal("请求头不正确")
		}
		var body struct {
			Model    string    `json:"model"`
			Messages []Message `json:"messages"`
			Stream   bool      `json:"stream"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "test-model" || !body.Stream || len(body.Messages) != 1 || body.Messages[0].Content != "问题" {
			t.Fatalf("请求体不正确：%+v", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(goodStream))}, nil
	})
	var parts []string
	answer, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "问题"}}, func(part string) error {
		parts = append(parts, part)
		return nil
	})
	if err != nil || answer != "你好" || len(parts) != 2 {
		t.Fatalf("answer=%q parts=%v err=%v", answer, parts, err)
	}
}

// 表驱动测试：同一套断言检查多种损坏或未完成的响应。
func TestReadStreamRejectsIncompleteAnswers(t *testing.T) {
	cases := map[string]string{
		"断流":       strings.ReplaceAll(goodStream, "data: [DONE]", ""),
		"输出被截断":    strings.ReplaceAll(goodStream, `"stop"`, `"length"`),
		"缺少正常结束原因": strings.ReplaceAll(goodStream, `"stop"`, `null`),
		"坏JSON":    "data: {broken}\n\n",
		"服务端错误":    "data: {\"error\":{\"message\":\"private\"}}\n\n",
		"空回答":      "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
	}
	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			if answer, err := readStream(strings.NewReader(stream), nil); err == nil || answer != "" {
				t.Fatalf("不完整响应被接受：answer=%q err=%v", answer, err)
			}
		})
	}
}

func TestReadStreamPropagatesCallbackError(t *testing.T) {
	outputErr := errors.New("输出失败")
	_, err := readStream(strings.NewReader(goodStream), func(string) error { return outputErr })
	if !errors.Is(err, outputErr) {
		t.Fatalf("回调错误丢失：%v", err)
	}
}

func TestChatHidesServerErrorBodyAndPropagatesCancellation(t *testing.T) {
	c, err := New("test-secret", "https://example.com", "model")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("test-secret"))}, nil
	})
	if _, err := c.Chat(context.Background(), nil, nil); err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("服务端错误处理不正确：%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.http.Transport = transportFunc(func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() })
	if _, err := c.Chat(ctx, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消信号丢失：%v", err)
	}
}

func TestNewRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	for _, address := range []string{"", ":bad", "file:///tmp/model", "https://user:pass@example.com", "https://example.com?key=secret", "https://example.com#fragment"} {
		if _, err := New("key", address, "model"); err == nil {
			t.Fatalf("错误地址被接受：%q", address)
		}
	}
	if _, err := New("", "https://example.com", "model"); err == nil {
		t.Fatal("空密钥被接受")
	}
	c, err := New("key", "https://example.com", "model")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.http.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatal("客户端必须拒绝自动重定向")
	}
}
