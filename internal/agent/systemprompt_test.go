package agent

import (
	"strings"
	"testing"

	"agnet/internal/llm"
)

type switchClient struct {
	llm.Client
	model string
}

func (c *switchClient) Model() string        { return c.model }
func (c *switchClient) SetModel(name string) { c.model = name }

func TestSystemPromptFollowsModel(t *testing.T) {
	c := &switchClient{model: "gpt-a"}
	sys := WithAgentsMD("模型是 \""+ModelPlaceholder+"\"。", "项目规则", false)
	a := New(c, sys, nil)

	first := a.requestMessages()[0].Content
	if !strings.Contains(first, `模型是 "gpt-a"`) {
		t.Fatalf("got %q", first)
	}
	a.SetModel("gpt-b")
	got := a.requestMessages()[0].Content
	if !strings.Contains(got, `模型是 "gpt-b"`) || strings.Contains(got, "gpt-a") {
		t.Fatalf("切换后应使用新模型名：%q", got)
	}
	if !strings.HasSuffix(got, "<agents_md>\n项目规则\n</agents_md>") {
		t.Fatalf("AGENTS.md 应仍在末尾：%q", got)
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
