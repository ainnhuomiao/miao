package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient 通过 OpenAI Chat Completions 协议访问模型。
// DeepSeek、Kimi、Ollama 等大多数服务都兼容这个协议。
type OpenAIClient struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// NewOpenAI 创建客户端。baseURL 需要包含 API 前缀，例如：
//
//	DeepSeek: https://api.deepseek.com
//	OpenAI:   https://api.openai.com/v1
//	Ollama:   http://localhost:11434/v1
func NewOpenAI(apiKey, baseURL, model string) *OpenAIClient {
	return &OpenAIClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		// 流式回答可能持续很久，给一个宽松的总超时，避免连接永久挂起。
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Stream 发起一次流式对话，每收到一段内容就调用 onDelta。
// 它实现了 Client 接口。
func (c *OpenAIClient) Stream(ctx context.Context, messages []Message, onDelta DeltaHandler) error {
	if onDelta == nil {
		onDelta = func(Delta) error { return nil }
	}

	payload, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("编码请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接模型服务失败: %w", err)
	}
	defer resp.Body.Close() // 函数返回时一定关闭响应体，防止连接泄漏

	if resp.StatusCode != http.StatusOK {
		return parseAPIError(resp)
	}

	// OpenAI 流式接口使用 SSE（Server-Sent Events）：
	// 每一行形如 "data: {...}"，最后一行是 "data: [DONE]"。
	scanner := bufio.NewScanner(resp.Body)
	// SSE 单行可能较长，放宽到 1 MiB，避免默认 64 KiB 上限导致截断报错。
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // 空行和 SSE 心跳/注释
		}
		if !strings.HasPrefix(line, "data:") {
			continue // event:、id: 等字段，这里用不到
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}

		var chunk streamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("解析流式响应失败: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("模型返回错误: %s", chunk.Error.Message)
		}

		for _, choice := range chunk.Choices {
			delta := Delta{
				Content:   choice.Delta.Content,
				Reasoning: choice.Delta.ReasoningContent,
			}
			if delta.Content == "" && delta.Reasoning == "" {
				continue
			}
			if err := onDelta(delta); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		// 用户按 Ctrl+C 取消时，优先返回 context.Canceled，
		// 这样上层可以用 errors.Is 识别"主动取消"。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("读取流式响应失败: %w", err)
	}

	// 有的服务不发送 [DONE]，而是直接关闭连接，这里视为正常结束。
	return nil
}

// APIError 表示模型服务返回了非 200 状态码。
// 定义成独立类型后，调用方可以用 errors.As 取出状态码做不同处理。
type APIError struct {
	StatusCode int
	Message    string
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
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// 以下是请求和流式响应的 JSON 结构体。
// 字段标签（`json:"..."`）告诉 encoding/json 如何映射键名。

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}
