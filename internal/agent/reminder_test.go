package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestBaselineInjectedOnFirstSend(t *testing.T) {
	fake := &fakeClient{reply: "ok"}
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(fake, DefaultSystemPrompt, reg)
	a.SetProjectContext("用 gofmt。", false, "额外说明")
	if err := a.Send(context.Background(), "hi", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	h := a.History()
	if len(h) < 3 || !isBaselineReminder(h[0]) {
		t.Fatalf("第一条应是基线 reminder：%+v", h)
	}
	if !strings.Contains(h[0].Content, a.Workspace()) || !strings.Contains(h[0].Content, "用 gofmt。") || !strings.Contains(h[0].Content, "额外说明") {
		t.Fatalf("基线内容不完整：%q", h[0].Content)
	}
	if h[1].Content != "hi" {
		t.Fatalf("用户消息应在基线之后：%q", h[1].Content)
	}
	sys := fake.gotReqs[0].Messages[0].Content
	if strings.Contains(sys, a.Workspace()) || strings.Contains(sys, "用 gofmt") || strings.Contains(sys, "额外说明") {
		t.Fatalf("系统提示词不应含工作区或 AGENTS.md：%q", sys)
	}
	if !strings.Contains(sys, "你是 miao") {
		t.Fatalf("系统提示词不对：%q", sys)
	}
	if err := a.Send(context.Background(), "again", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range a.History() {
		if isBaselineReminder(m) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("基线只能有一条，got %d", n)
	}
}

func TestRepeatToolReminder(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(tools.Tool{
		Def: llm.ToolDef{Type: "function", Function: llm.FunctionDef{
			Name: "echo_blob", Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			return tools.Result{Content: "same"}
		},
	})
	scripts := make([]llm.ChatResult, 0, 4)
	for i := 0; i < 3; i++ {
		scripts = append(scripts, llm.ChatResult{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "c1",
				Type:     "function",
				Function: llm.FunctionCall{Name: "echo_blob", Arguments: `{"x":1}`},
			}},
		})
	}
	scripts = append(scripts, llm.ChatResult{Content: "done", FinishReason: "stop"})
	c := &recordingClient{scripts: scripts}
	a := New(c, "SYS", reg)
	if err := a.Send(context.Background(), "loop", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	var notes int
	for _, m := range a.History() {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "你已连续 3 次用相同参数调用 echo_blob") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("第三次应追加重复提醒一次，got %d 条；history=%+v", notes, a.History())
	}
}
