package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// ModelSwitcher 是支持运行时切换模型的客户端。
type ModelSwitcher interface {
	Model() string
	SetModel(name string)
}

// ModelLister 是能列出服务端可用模型的客户端。
type ModelLister interface {
	ListModels(ctx context.Context) ([]string, error)
}

// Model 返回当前模型名。
func (c *OpenAIClient) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

// SetModel 切换后续请求使用的模型。
func (c *OpenAIClient) SetModel(name string) {
	c.mu.Lock()
	c.model = name
	c.mu.Unlock()
}

// ListModels 调用 OpenAI 兼容的 GET {baseURL}/models。
func (c *OpenAIClient) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取模型列表失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	var out []string
	for _, d := range body.Data {
		if d.ID != "" {
			out = append(out, d.ID)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	sort.Strings(out)
	return out, nil
}
