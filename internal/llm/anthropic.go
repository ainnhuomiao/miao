package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const anthropicVersion = "2023-06-01"

// AnthropicClient 通过 Anthropic Messages API 访问模型（含 prompt cache 标记）。
type AnthropicClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
	timeout time.Duration
	retries int
	debug   bool

	mu    sync.RWMutex
	model string
}

// NewAnthropic 创建 Anthropic 客户端。
func NewAnthropic(apiKey, baseURL, model string) *AnthropicClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{},
		timeout: defaultHTTPTimeout,
		retries: defaultMaxRetries,
	}
}

func (c *AnthropicClient) SetTimeout(d time.Duration) { c.timeout = d }
func (c *AnthropicClient) SetRetries(n int)           { c.retries = n }
func (c *AnthropicClient) SetDebug(v bool)            { c.debug = v }

func (c *AnthropicClient) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

func (c *AnthropicClient) SetModel(name string) {
	c.mu.Lock()
	c.model = name
	c.mu.Unlock()
}

func (c *AnthropicClient) requestTimeout() time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return defaultHTTPTimeout
}

func (c *AnthropicClient) messagesURL() string {
	b := c.baseURL
	if strings.HasSuffix(b, "/v1") {
		return b + "/messages"
	}
	return b + "/v1/messages"
}

// StreamChat 实现 Client。
func (c *AnthropicClient) StreamChat(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error) {
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
		res, err := c.streamOnce(ctx, req, ctr.handle)
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

func (c *AnthropicClient) streamOnce(ctx context.Context, req ChatRequest, onDelta DeltaHandler) (ChatResult, error) {
	payload, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return ChatResult{}, fmt.Errorf("编码请求失败: %w", err)
	}
	timeout := c.requestTimeout()
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.messagesURL(), bytes.NewReader(payload))
	if err != nil {
		return ChatResult{}, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

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
	defer func() { _ = resp.Body.Close() }()
	if c.debug {
		slog.Debug("llm response", "protocol", "anthropic", "status", resp.StatusCode, "elapsed", time.Since(start).String())
	}
	if resp.StatusCode != http.StatusOK {
		return ChatResult{}, parseAPIError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var (
		content   strings.Builder
		reasoning strings.Builder
		tools     []ToolCall
		cur       *ToolCall
		finish    string
		usage     *Usage
	)
	flushTool := func() {
		if cur != nil {
			tools = append(tools, *cur)
			cur = nil
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev anthStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				flushTool()
				cur = &ToolCall{ID: ev.ContentBlock.ID, Type: "function", Function: FunctionCall{Name: ev.ContentBlock.Name}}
				if err := onDelta(Delta{ToolCall: true}); err != nil {
					return ChatResult{}, err
				}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					content.WriteString(ev.Delta.Text)
					if err := onDelta(Delta{Content: ev.Delta.Text}); err != nil {
						return ChatResult{}, err
					}
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					reasoning.WriteString(ev.Delta.Thinking)
					if err := onDelta(Delta{Reasoning: ev.Delta.Thinking}); err != nil {
						return ChatResult{}, err
					}
				}
			case "input_json_delta":
				if cur != nil {
					cur.Function.Arguments += ev.Delta.PartialJSON
				}
			}
		case "content_block_stop":
			flushTool()
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				finish = ev.Delta.StopReason
				if finish == "tool_use" {
					finish = "tool_calls"
				}
			}
			if ev.Usage != nil {
				if usage == nil {
					usage = &Usage{}
				}
				usage.CompletionTokens = ev.Usage.OutputTokens
				usage.PromptTokens = ev.Usage.InputTokens + ev.Usage.CacheReadInputTokens + ev.Usage.CacheCreationInputTokens
				usage.PromptCacheHitTokens = ev.Usage.CacheReadInputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		case "error":
			if ev.Error != nil {
				return ChatResult{}, fmt.Errorf("模型返回错误: %s", ev.Error.Message)
			}
		}
	}
	flushTool()
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
		ToolCalls:    tools,
		FinishReason: finish,
		Usage:        usage,
	}, nil
}

func (c *AnthropicClient) buildRequest(req ChatRequest) anthRequest {
	sys, msgs := splitSystem(req.Messages)
	out := anthRequest{
		Model:     firstNonEmptyModel(req.Model, c.Model()),
		MaxTokens: 8192,
		Stream:    true,
		Messages:  toAnthMessages(msgs),
	}
	if sys != "" {
		out.System = []anthSystemBlock{{
			Type:         "text",
			Text:         sys,
			CacheControl: &anthCacheControl{Type: "ephemeral"},
		}}
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, anthTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	out.Messages = markLastCache(out.Messages)
	return out
}

func markLastCache(msgs []anthMessage) []anthMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if n := len(msgs[i].Content); n > 0 {
			msgs[i].Content[n-1].CacheControl = &anthCacheControl{Type: "ephemeral"}
			break
		}
	}
	return msgs
}

func splitSystem(msgs []Message) (string, []Message) {
	var sys strings.Builder
	var rest []Message
	for _, m := range msgs {
		if m.Role == RoleSystem {
			if sys.Len() > 0 {
				sys.WriteByte('\n')
			}
			sys.WriteString(m.Content)
			continue
		}
		rest = append(rest, m)
	}
	return sys.String(), rest
}

func toAnthMessages(msgs []Message) []anthMessage {
	var out []anthMessage
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		switch m.Role {
		case RoleUser:
			out = append(out, anthMessage{Role: "user", Content: userContent(m)})
		case RoleAssistant:
			out = append(out, anthMessage{Role: "assistant", Content: assistantContent(m)})
		case RoleTool:
			var blocks []anthContent
			for ; i < len(msgs) && msgs[i].Role == RoleTool; i++ {
				blocks = append(blocks, anthContent{
					Type:      "tool_result",
					ToolUseID: msgs[i].ToolCallID,
					Content:   msgs[i].Content,
				})
			}
			i--
			out = append(out, anthMessage{Role: "user", Content: blocks})
		}
	}
	return out
}

func userContent(m Message) []anthContent {
	var blocks []anthContent
	if m.Content != "" {
		blocks = append(blocks, anthContent{Type: "text", Text: m.Content})
	}
	for _, img := range m.Images {
		if len(img.Data) == 0 {
			continue
		}
		blocks = append(blocks, anthContent{Type: "image", Source: &anthImageSource{
			Type: "base64", MediaType: img.MIME, Data: img.DataURL()[strings.Index(img.DataURL(), ",")+1:],
		}})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthContent{Type: "text", Text: "(空)"})
	}
	return blocks
}

func assistantContent(m Message) []anthContent {
	var blocks []anthContent
	if m.Content != "" {
		blocks = append(blocks, anthContent{Type: "text", Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		var input json.RawMessage = json.RawMessage(tc.Function.Arguments)
		if !json.Valid(input) {
			input = json.RawMessage(`{}`)
		}
		blocks = append(blocks, anthContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthContent{Type: "text", Text: ""})
	}
	return blocks
}

type anthRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	Stream    bool              `json:"stream"`
	System    []anthSystemBlock `json:"system,omitempty"`
	Messages  []anthMessage     `json:"messages"`
	Tools     []anthTool        `json:"tools,omitempty"`
}

type anthSystemBlock struct {
	Type         string            `json:"type"`
	Text         string            `json:"text"`
	CacheControl *anthCacheControl `json:"cache_control,omitempty"`
}

type anthCacheControl struct {
	Type string `json:"type"`
}

type anthMessage struct {
	Role    string        `json:"role"`
	Content []anthContent `json:"content"`
}

type anthContent struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	ToolUseID    string            `json:"tool_use_id,omitempty"`
	Content      string            `json:"content,omitempty"`
	Source       *anthImageSource  `json:"source,omitempty"`
	CacheControl *anthCacheControl `json:"cache_control,omitempty"`
}

type anthImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthStreamEvent struct {
	Type         string `json:"type"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}
