// Package llm 用 Go 标准库调用 Chat Completions 兼容接口。
// HTTP、JSON 和流式响应在这里处理，终端界面不需要知道网络细节。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Message 是一条对话消息。结构体标签告诉 encoding/json 使用什么字段名。
// 例如 Message{Role: "user", Content: "你好"} 会变成对应的 JSON 对象。
type Message struct {
	Role    string `json:"role"`    // system：角色说明；user：用户；assistant：模型。
	Content string `json:"content"` // 这一条消息的文字。
}

// Client 只规定“客户端能做什么”，不规定具体实现。
// 实现了下面方法的类型就自动满足接口，不需要写 implements。
// onText 是函数参数：有新文字时调用它；返回错误会中止读取。
type Client interface {
	Chat(ctx context.Context, messages []Message, onText func(string) error) (string, error)
}

// HTTPClient 是 Client 的网络实现。它不保存对话历史。
type HTTPClient struct {
	endpoint string       // 完整请求地址，例如 https://api.example.com/v1/chat/completions。
	apiKey   string       // 只放在 Authorization 请求头里，不输出到终端。
	model    string       // 服务商提供的模型名称。
	http     *http.Client // 复用 HTTP 客户端，避免每次请求都新建连接池。
}

// New 创建客户端，并检查启动参数。baseURL 包含服务要求的版本路径。
// 第一版不读取 .env、不保存密钥，也不自动重试请求。
func New(apiKey, baseURL, model string) (*HTTPClient, error) {
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("API Key 和模型名不能为空")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("接口地址必须是 http/https 地址，且不能包含账号、查询参数或片段")
	}

	return &HTTPClient{
		endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:   apiKey,
		model:    model,
		http: &http.Client{
			Timeout: 60 * time.Second, // 包括连接和读取完整回答，避免无限等待。
			// 不跟随重定向，避免把带密钥的请求发往意外地址。
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Chat 把 Go 数据编码成 JSON，通过 HTTP POST 发给模型，再读取流式回答。
// messages 必须按 system、user、assistant 的实际对话顺序排列。
func (c *HTTPClient) Chat(ctx context.Context, messages []Message, onText func(string) error) (string, error) {
	// 匿名结构体只在这里使用，不需要为了一个请求再创建一个公共类型。
	request := struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}{Model: c.model, Messages: messages, Stream: true}
	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("编码请求: %w", err)
	}

	// WithContext 让调用方取消操作时，HTTP 请求也能结束。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求模型: %w", err)
	}
	// defer 在函数离开时执行，保证成功或失败都会关闭响应体。
	// 此处忽略 Close 的错误，因为响应读取的结果才决定本轮是否成功。
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// 不直接打印服务端响应体，避免它回显密钥或整段对话。
		return "", fmt.Errorf("模型服务返回 HTTP %d，请检查地址、模型和 API Key", resp.StatusCode)
	}
	return readStream(resp.Body, onText)
}

// readStream 读取 SSE（Server-Sent Events）中以 data: 开头的数据行。
// 本版支持 Chat Completions 常见的“每个 data 行一个 JSON”格式。
// 服务端分多次发送 delta.content，我们一边显示，一边拼出完整回答。
func readStream(r io.Reader, onText func(string) error) (string, error) {
	scanner := bufio.NewScanner(r)
	// Scanner 默认只允许较短的行；这里把单行上限设为 1MiB。
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var answer strings.Builder // Builder 用来逐段拼接字符串。
	var finish string          // stop 表示模型正常完成；length 等值表示未完整完成。

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // 空行、事件说明和心跳不属于回答文字。
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			// 结束标记和正常结束原因都要存在，防止把断流当作成功。
			if finish != "stop" {
				return "", fmt.Errorf("模型未正常完成回答，本轮未加入历史")
			}
			if strings.TrimSpace(answer.String()) == "" {
				return "", fmt.Errorf("模型没有返回文字")
			}
			return answer.String(), nil
		}

		// 只声明我们关心的 JSON 字段；encoding/json 会忽略其他字段。
		var chunk struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"` // RawMessage 暂存原始 JSON，只判断是否有错误。
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("解析流式响应: %w", err)
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return "", fmt.Errorf("模型服务在回答过程中返回错误")
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue // 本版只处理第一份回答，不请求多个候选。
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			text := choice.Delta.Content
			answer.WriteString(text)
			if text != "" && onText != nil {
				if err := onText(text); err != nil {
					return "", err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("读取流式响应: %w", err)
	}
	return "", fmt.Errorf("连接在收到 [DONE] 前结束，本轮未加入历史")
}
