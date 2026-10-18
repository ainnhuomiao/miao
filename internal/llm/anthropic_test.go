package llm

import (
	"encoding/json"
	"testing"
)

func TestDetectProtocol(t *testing.T) {
	if got := DetectProtocol("", "https://api.anthropic.com"); got != "anthropic" {
		t.Fatalf("got %s", got)
	}
	if got := DetectProtocol("openai", "https://api.anthropic.com"); got != "openai" {
		t.Fatalf("explicit protocol should win: %s", got)
	}
	if got := DetectProtocol("", "https://api.deepseek.com"); got != "openai" {
		t.Fatalf("got %s", got)
	}
}

func TestAnthCacheControl(t *testing.T) {
	c := NewAnthropic("k", "https://api.anthropic.com", "m")
	req := c.buildRequest(ChatRequest{Messages: []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "hi"},
	}})
	if len(req.System) == 0 || req.System[0].CacheControl == nil || req.System[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("system cache: %+v", req.System)
	}
	last := req.Messages[len(req.Messages)-1].Content
	if len(last) == 0 || last[len(last)-1].CacheControl == nil {
		t.Fatalf("last block should have cache_control: %+v", last)
	}
}

func TestToAnthMessagesToolRoundtrip(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{
			ID: "t1", Type: "function", Function: FunctionCall{Name: "list_dir", Arguments: `{"path":"."}`},
		}}},
		{Role: RoleTool, ToolCallID: "t1", Name: "list_dir", Content: "a.go"},
	}
	out := toAnthMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if out[1].Role != "assistant" || out[1].Content[0].Type != "tool_use" {
		t.Fatalf("assistant = %+v", out[1])
	}
	if out[2].Role != "user" || out[2].Content[0].Type != "tool_result" {
		t.Fatalf("tool = %+v", out[2])
	}
	raw, _ := json.Marshal(out)
	if string(raw) == "" {
		t.Fatal("marshal empty")
	}
}
