package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
)

type summaryClient struct {
	reply string
	got   llm.ChatRequest
}

func (f *summaryClient) StreamChat(ctx context.Context, req llm.ChatRequest, on llm.DeltaHandler) (llm.ChatResult, error) {
	f.got = req
	return llm.ChatResult{Content: f.reply}, nil
}

func TestCompactKeepsToolPairsAndRecentTurns(t *testing.T) {
	fc := &summaryClient{reply: "- 用户在改 miao"}
	a := New(fc, "sys", nil)
	a.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: "结果"},
		{Role: llm.RoleAssistant, Content: "答1"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2"}}},
		{Role: llm.RoleTool, ToolCallID: "2", Content: "结果2"},
		{Role: llm.RoleAssistant, Content: "答2"},
	})
	res, err := a.Compact(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	h := a.History()
	if !strings.HasPrefix(h[0].Content, SummaryPrefix) || h[2].Content != "第二轮" || len(h) != 6 {
		t.Fatalf("压缩后历史不对：%+v", h)
	}
	if res.Dropped != 4 || !strings.Contains(fc.got.Messages[1].Content, "第一轮") {
		t.Fatalf("应把第一轮交给模型总结：%+v", res)
	}
}

func TestCompactTooShort(t *testing.T) {
	a := New(&summaryClient{reply: "x"}, "", nil)
	a.SetHistory([]llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "yo"}})
	if _, err := a.Compact(context.Background(), 2); err == nil {
		t.Fatal("只有一轮时不应压缩")
	}
}
