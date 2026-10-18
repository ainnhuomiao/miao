package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// modelsURL 返回 GET /v1/models 的地址（与 messagesURL 一样兼容带不带 /v1 的 Base URL）。
func (c *AnthropicClient) modelsURL() string {
	b := c.baseURL
	if strings.HasSuffix(b, "/v1") {
		return b + "/models"
	}
	return b + "/v1/models"
}

// ListModels 调用 Anthropic 的 GET /v1/models，按发布时间从新到旧排列（时间相同按 id 倒序）。
func (c *AnthropicClient) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.modelsURL()+"?limit=1000", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
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
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	items := body.Data[:0]
	for _, d := range body.Data {
		if d.ID != "" {
			items = append(items, d)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	// created_at 是 RFC 3339，同一格式下字符串比较即时间比较
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].ID > items[j].ID
	})
	out := make([]string, len(items))
	for i, d := range items {
		out[i] = d.ID
	}
	return out, nil
}
