package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OpenAIClient 通过 OpenAI Chat Completions 协议访问模型。
type OpenAIClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
	timeout time.Duration
	retries int
	debug   bool

	mu    sync.RWMutex
	model string
}

// NewOpenAI 创建客户端。
func NewOpenAI(apiKey, baseURL, model string) *OpenAIClient {
	return &OpenAIClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{}, // 超时用每次请求的 context，以便区分用户取消和超时
		timeout: defaultHTTPTimeout,
		retries: defaultMaxRetries,
	}
}

// SetTimeout 设置单次请求超时（含流式接收）；0 表示使用默认 10 分钟。
func (c *OpenAIClient) SetTimeout(d time.Duration) { c.timeout = d }

// SetRetries 设置 429/5xx/网络错误的额外重试次数。
func (c *OpenAIClient) SetRetries(n int) { c.retries = n }

// SetDebug 打开请求级 debug 日志（写到 slog）。
func (c *OpenAIClient) SetDebug(v bool) { c.debug = v }

func (c *OpenAIClient) requestTimeout() time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return defaultHTTPTimeout
}

// StreamChat 发起一次流式对话，聚合正文、思考与 tool_calls。
func (c *OpenAIClient) StreamChat(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error) {
	if onDelta == nil {
		onDelta = func(Delta) error { return nil }
	}
	ctr := &countingDelta{fn: onDelta}
	var last error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			d := backoffDelay(attempt-1, last)
			logRetry(attempt, d, last)
			if err := sleepCtx(ctx, d); err != nil {
				if errors.Is(err, context.Canceled) {
					return ChatResult{}, err
				}
				return ChatResult{}, last
			}
		}
		res, err := c.streamChatOnce(ctx, req, ctr.handle)
		if err == nil {
			return res, nil
		}
		last = err
		if ctr.n > 0 || !retryable(err) {
			return ChatResult{}, err
		}
	}
	return ChatResult{}, last
}

func (c *OpenAIClient) streamChatOnce(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error) {
	payload, err := json.Marshal(chatRequest{
		Model:         firstNonEmptyModel(req.Model, c.Model()),
		Messages:      req.Messages,
		Tools:         req.Tools,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	})
	if err != nil {
		return ChatResult{}, fmt.Errorf("编码请求失败: %w", err)
	}

	timeout := c.requestTimeout()
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResult{}, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	start := time.Now()
	resp, err := c.http.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return ChatResult{}, ctx.Err()
		}
		if reqCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return ChatResult{}, &TimeoutError{Timeout: timeout}
		}
		return ChatResult{}, fmt.Errorf("连接模型服务失败: %w", err)
	}
	defer resp.Body.Close()

	if c.debug {
		slog.Debug("llm response", "status", resp.StatusCode, "elapsed", time.Since(start).String(), "model", firstNonEmptyModel(req.Model, c.Model()))
	}

	if resp.StatusCode != http.StatusOK {
		return ChatResult{}, parseAPIError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	var (
		content   strings.Builder
		reasoning strings.Builder
		acc       = newToolCallAccumulator()
		finish    string
		usage     *Usage
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk streamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return ChatResult{}, fmt.Errorf("解析流式响应失败: %w", err)
		}
		if chunk.Error != nil {
			return ChatResult{}, fmt.Errorf("模型返回错误: %s", chunk.Error.Message)
		}

		// 开启 include_usage 后，最后一个 chunk 的 choices 为空、只带 usage。
		if chunk.Usage != nil {
			usage = chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			acc.Apply(choice.Delta.ToolCalls)

			delta := Delta{
				Content:   choice.Delta.Content,
				Reasoning: choice.Delta.ReasoningContent,
			}
			if delta.Content != "" {
				content.WriteString(delta.Content)
			}
			if delta.Reasoning != "" {
				reasoning.WriteString(delta.Reasoning)
			}
			if delta.Content == "" && delta.Reasoning == "" {
				continue
			}
			if err := onDelta(delta); err != nil {
				return ChatResult{}, err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			return ChatResult{}, ctx.Err()
		}
		if reqCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return ChatResult{}, &TimeoutError{Timeout: timeout}
		}
		return ChatResult{}, fmt.Errorf("读取流式响应失败: %w", err)
	}

	return ChatResult{
		Content:      content.String(),
		Reasoning:    reasoning.String(),
		ToolCalls:    acc.Result(),
		FinishReason: finish,
		Usage:        usage,
	}, nil
}

// toolCallAccumulator 按 index 拼接流式 tool_calls。
type toolCallAccumulator struct {
	byIndex map[int]*ToolCall
	order   []int
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIndex: make(map[int]*ToolCall)}
}

func (a *toolCallAccumulator) Apply(deltas []toolCallDelta) {
	for _, d := range deltas {
		tc, ok := a.byIndex[d.Index]
		if !ok {
			tc = &ToolCall{Type: "function"}
			a.byIndex[d.Index] = tc
			a.order = append(a.order, d.Index)
		}
		if d.ID != "" {
			tc.ID = d.ID
		}
		if d.Type != "" {
			tc.Type = d.Type
		}
		if d.Function.Name != "" {
			tc.Function.Name = d.Function.Name
		}
		if d.Function.Arguments != "" {
			tc.Function.Arguments += d.Function.Arguments
		}
	}
}

func (a *toolCallAccumulator) Result() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		out = append(out, *a.byIndex[idx])
	}
	return out
}

type APIError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
	err        error
}

func (e *APIError) Error() string {
	var hint string
	switch e.StatusCode {
	case http.StatusUnauthorized:
		hint = "，请检查 API Key"
	case http.StatusPaymentRequired:
		hint = "，账户余额不足"
	case http.StatusTooManyRequests:
		hint = "，请求过于频繁，请稍后重试"
	}
	return fmt.Sprintf("模型服务错误 (HTTP %d%s): %s", e.StatusCode, hint, e.Message)
}

func (e *APIError) Unwrap() error { return e.err }

func parseAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		msg = payload.Error.Message
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    msg,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Tools         []ToolDef      `json:"tools,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func firstNonEmptyModel(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
