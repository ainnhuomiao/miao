package agent

import (
	"context"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
)

type captureClient struct {
	model string
	reqs  []llm.ChatRequest
}

func (c *captureClient) StreamChat(_ context.Context, req llm.ChatRequest, _ llm.DeltaHandler) (llm.ChatResult, error) {
	c.reqs = append(c.reqs, req)
	return llm.ChatResult{Content: "ok", FinishReason: "stop"}, nil
}
func (c *captureClient) Model() string     { return c.model }
func (c *captureClient) SetModel(n string) { c.model = n }

func TestSendAttachesImagesAndFiltersBlindModel(t *testing.T) {
	t.Setenv("MIAO_VISION", "")
	c := &captureClient{model: "gpt-4o"}
	a := New(c, "sys", nil)
	a.AttachImages([]llm.Image{{MIME: "image/png", Name: "a.png", Data: []byte("x")}})
	if err := a.Send(context.Background(), "看图", nil); err != nil {
		t.Fatal(err)
	}
	if len(c.reqs[0].Messages[1].Images) != 1 || len(a.History()[0].Images) != 1 {
		t.Fatal("图片应随用户消息发送")
	}
	if EstimateTokens(a.History()) < llm.ImageTokens {
		t.Fatal("估算应计入图片")
	}
	c.SetModel("deepseek-chat")
	if err := a.Send(context.Background(), "继续", nil); err != nil {
		t.Fatal(err)
	}
	if len(c.reqs[1].Messages[1].Images) != 0 || len(a.History()[0].Images) != 1 {
		t.Fatal("不支持图片的模型请求里应去掉图片，历史保留")
	}
	if a.Send(context.Background(), "再来", nil); len(c.reqs[2].Messages[5].Images) != 0 {
		t.Fatal("附图只用一次")
	}
}

func TestDisabledImagesFilterPendingAndRestoredHistory(t *testing.T) {
	t.Setenv("MIAO_VISION", "1")
	c := &captureClient{model: "gpt-4o"}
	a := New(c, "sys", nil)
	image := llm.Image{MIME: "image/png", Name: "private.png", Data: []byte("private")}
	a.SetHistory([]llm.Message{{Role: llm.RoleUser, Content: "old", Images: []llm.Image{image}}})
	a.AttachImages([]llm.Image{image})
	a.SetImagesEnabled(false)
	a.AttachImages([]llm.Image{image})
	if err := a.Send(context.Background(), "new", nil); err != nil {
		t.Fatal(err)
	}
	for _, msg := range c.reqs[0].Messages {
		if len(msg.Images) != 0 {
			t.Fatal("disabled images must never be sent, even with MIAO_VISION=1")
		}
	}
	if len(a.History()[0].Images) != 1 {
		t.Fatal("filtering must preserve original restored history")
	}
}
