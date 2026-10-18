package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
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
	res, err := a.Compact(context.Background(), 1, 0)
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
	last := fc.got.Messages[len(fc.got.Messages)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Content, "压缩") {
		t.Fatalf("摘要指令应在最后一条用户消息：%+v", last)
	}
	if fc.got.Messages[0].Role != llm.RoleSystem || fc.got.Messages[0].Content != "sys" {
		t.Fatalf("应复用原系统提示词：%+v", fc.got.Messages[0])
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
	if _, err := a.Compact(context.Background(), 2, 0); err == nil {
		t.Fatal("只有一轮时不应压缩")
	}
}

func TestSplitPointSkipsModeNotesAndSummary(t *testing.T) {
	h := []llm.Message{
		{Role: llm.RoleUser, Content: wrapReminder("工作区根目录是 \"/tmp\"。")},
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, Content: "a"},
		{Role: llm.RoleUser, Content: planModeNote},
		{Role: llm.RoleUser, Content: SummaryPrefix + "\n老摘要"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, Content: "b"},
	}
	if got := splitPoint(h, 1); got != 5 {
		t.Fatalf("keep 1 应切在真实用户输入，got %d", got)
	}
	if got := splitPoint(h, 2); got != 1 {
		t.Fatalf("keep 2 应切在第一轮，got %d", got)
	}
}

func TestCompactSkipsSummaryWhenBelowBudget(t *testing.T) {
	fc := &summaryClient{reply: "不应被调用"}
	a := New(fc, "sys", nil)
	a.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, Content: "答1"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, Content: "答2"},
	})
	res, err := a.Compact(context.Background(), 1, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summarized || fc.got.Messages != nil {
		t.Fatalf("低于阈值不应请求摘要：%+v req=%+v", res, fc.got)
	}
}

func TestCompactIgnoresToolCalls(t *testing.T) {
	fc := &summaryClient{reply: "- 摘要"}
	a := New(fc, "sys", nil)
	a.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, Content: "答1"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, Content: "答2"},
	})
	// summaryClient 不返回 tool_calls；覆盖「只取 Content」的路径即可。
	res, err := a.Compact(context.Background(), 1, 0)
	if err != nil || !res.Summarized {
		t.Fatalf("err=%v res=%+v", err, res)
	}
}

func TestCompactRequestIncludesTools(t *testing.T) {
	fc := &summaryClient{reply: "- 摘要"}
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	a := New(fc, "sys", reg)
	a.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, Content: "答1"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, Content: "答2"},
	})
	if _, err := a.Compact(context.Background(), 1, 0); err != nil {
		t.Fatal(err)
	}
	if len(fc.got.Tools) == 0 {
		t.Fatal("摘要请求必须带上工具定义，否则前缀对不齐")
	}
}

func TestCompactRestoresBaseline(t *testing.T) {
	fc := &summaryClient{reply: "- 摘要"}
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(fc, "sys", reg)
	a.SetProjectContext("用 gofmt", false, "")
	a.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: wrapReminder("工作区根目录是 \"old\"。")},
		{Role: llm.RoleUser, Content: "第一轮"},
		{Role: llm.RoleAssistant, Content: "答1"},
		{Role: llm.RoleUser, Content: "第二轮"},
		{Role: llm.RoleAssistant, Content: "答2"},
	})
	if _, err := a.Compact(context.Background(), 1, 0); err != nil {
		t.Fatal(err)
	}
	h := a.History()
	if len(h) < 3 || !isBaselineReminder(h[0]) || !strings.Contains(h[0].Content, a.Workspace()) || !strings.Contains(h[0].Content, "用 gofmt") {
		t.Fatalf("压缩后应把当前基线放回开头：%+v", h)
	}
	if !strings.HasPrefix(h[1].Content, SummaryPrefix) || h[3].Content != "第二轮" {
		t.Fatalf("基线之后应是摘要和最近一轮：%+v", h)
	}
}
