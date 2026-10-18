package agent

import (
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
)

type switchClient struct {
	llm.Client
	model string
}

func (c *switchClient) Model() string        { return c.model }
func (c *switchClient) SetModel(name string) { c.model = name }

func TestSystemPromptFollowsModel(t *testing.T) {
	c := &switchClient{model: "gpt-a"}
	a := New(c, "模型是 \""+ModelPlaceholder+"\"。", nil)

	first := a.requestMessages()[0].Content
	if !strings.Contains(first, `模型是 "gpt-a"`) {
		t.Fatalf("got %q", first)
	}
	a.SetModel("gpt-b")
	got := a.requestMessages()[0].Content
	if !strings.Contains(got, `模型是 "gpt-b"`) || strings.Contains(got, "gpt-a") {
		t.Fatalf("切换后应使用新模型名：%q", got)
	}
	if strings.Contains(got, "agents_md") {
		t.Fatalf("AGENTS.md 不应进系统提示词：%q", got)
	}
}

func TestSystemPromptNoPlaceholderOrUnknownModel(t *testing.T) {
	if got := New(&switchClient{model: "m"}, "固定", nil).systemPrompt(); got != "固定" {
		t.Fatalf("无占位符应原样：%q", got)
	}
	if got := New(nil, "模型 "+ModelPlaceholder, nil).systemPrompt(); got != "模型 未知" {
		t.Fatalf("不支持切换的客户端应显示未知：%q", got)
	}
}
