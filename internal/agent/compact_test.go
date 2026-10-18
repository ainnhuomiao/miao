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

func TestTrimOldToolResults(t *testing.T) {
	a := New(&summaryClient{reply: "x"}, "", nil)
	long := strings.Repeat("工具输出", 200)
	a.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: long},
		{Role: llm.RoleAssistant, Content: "答1"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, Content: "答2"},
	})
	n := a.TrimOldToolResults(1, 80)
	if n != 1 {
		t.Fatalf("应折叠 1 条，got %d", n)
	}
	got := a.History()[2].Content
	if !strings.Contains(got, "已折叠") || strings.Contains(got, long) {
		t.Fatalf("应截断早期工具输出：%q", got)
	}
	if a.History()[4].Content != "第二轮" {
		t.Fatal("最近一轮不应动")
	}
}

func TestCompactTooShort(t *testing.T) {
	a := New(&summaryClient{reply: "x"}, "", nil)
	a.SetHistory([]llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "yo"}})
	if _, err := a.Compact(context.Background(), 2); err == nil {
		t.Fatal("只有一轮时不应压缩")
	}
}
