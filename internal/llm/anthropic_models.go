package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// AnthropicModel 是 GET /v1/models 里的一项。Line 是模型系列（如 "sonnet"），
// 服务端不返回时为空（也兼容 family 字段名）。
type AnthropicModel struct {
	ID        string
	CreatedAt string
	Line      string
}

// ListModels 调用 Anthropic 的 GET /v1/models，按发布时间从新到旧排列（时间相同按 id 倒序）。
func (c *AnthropicClient) ListModels(ctx context.Context) ([]string, error) {
	items, err := c.ListModelInfo(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(items))
	for i, d := range items {
		out[i] = d.ID
	}
	return out, nil
}

// NewestSonnet 从按新到旧排好的列表里选最新的 sonnet：只要有一项带系列字段，就只按系列字段选
// （系列为 sonnet 的里 created_at 最新的）；都没有系列字段时退回按 id 含 "sonnet" 匹配。
// 找不到返回空串。
func NewestSonnet(items []AnthropicModel) string {
	hasLine := false
	for _, m := range items {
		if m.Line != "" {
			hasLine = true
			break
		}
	}
	best := -1
	for i, m := range items {
		var ok bool
		if hasLine {
			ok = strings.EqualFold(m.Line, "sonnet")
		} else {
			ok = strings.Contains(strings.ToLower(m.ID), "sonnet")
		}
		if ok && (best < 0 || m.CreatedAt > items[best].CreatedAt) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return items[best].ID
}

// ListModelInfo 与 ListModels 相同，但保留发布时间和系列字段。
func (c *AnthropicClient) ListModelInfo(ctx context.Context) ([]AnthropicModel, error) {
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
			Line      string `json:"line"`
			Family    string `json:"family"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxModelsBody)).Decode(&body); err != nil {
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
	out := make([]AnthropicModel, len(items))
	for i, d := range items {
		line := d.Line
		if line == "" {
			line = d.Family
		}
		out[i] = AnthropicModel{ID: d.ID, CreatedAt: d.CreatedAt, Line: line}
	}
	return out, nil
}
