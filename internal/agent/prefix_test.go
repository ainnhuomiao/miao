package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestPromptPrefixStableAcrossTurns(t *testing.T) {
	fake := &fakeClient{reply: "ok"}
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	ag := New(fake, "SYS", reg)
	if err := ag.Send(context.Background(), "one", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	if err := ag.Send(context.Background(), "two", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	if len(fake.gotReqs) != 2 {
		t.Fatalf("reqs = %d", len(fake.gotReqs))
	}
	r1, r2 := fake.gotReqs[0], fake.gotReqs[1]
	b1, _ := json.Marshal(r1.Tools)
	b2, _ := json.Marshal(r2.Tools)
	if !bytes.Equal(b1, b2) {
		t.Fatalf("工具列表第二轮变了\n1: %s\n2: %s", b1, b2)
	}
	if len(r2.Messages) <= len(r1.Messages) {
		t.Fatalf("第二轮消息应变长：%d vs %d", len(r2.Messages), len(r1.Messages))
	}
	p1, _ := json.Marshal(r1.Messages)
	p2, _ := json.Marshal(r2.Messages[:len(r1.Messages)])
	if !bytes.Equal(p1, p2) {
		t.Fatalf("前缀不稳定\n1: %s\n2: %s", p1, p2)
	}
}

func TestPlanModeDoesNotChangeSystemPrefix(t *testing.T) {
	fake := &fakeClient{reply: "ok"}
	ag := New(fake, "SYS", nil)
	if err := ag.Send(context.Background(), "one", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	ag.SetMode(ModePlan)
	if err := ag.Send(context.Background(), "two", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	r1, r2 := fake.gotReqs[0], fake.gotReqs[1]
	if r1.Messages[0].Role != r2.Messages[0].Role || r1.Messages[0].Content != r2.Messages[0].Content {
		t.Fatalf("系统提示词变了：%q -> %q", r1.Messages[0].Content, r2.Messages[0].Content)
	}
	found := false
	for _, m := range r2.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "只读规划") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("规划说明应追加在历史末尾：%+v", r2.Messages)
	}
}

func TestSetHistoryClearsPendingModeNote(t *testing.T) {
	a := New(nil, "SYS", nil)
	a.SetMode(ModePlan)
	if a.pendingModeNote == "" {
		t.Fatal("规划说明应挂起")
	}
	a.SetHistory([]llm.Message{{Role: llm.RoleUser, Content: "old"}})
	if a.pendingModeNote != "" {
		t.Fatalf("加载会话应丢掉挂起的模式说明：%q", a.pendingModeNote)
	}
}

func TestResetUnfreezesTools(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(tools.Tool{
		Def: llm.ToolDef{Type: "function", Function: llm.FunctionDef{Name: "alpha", Parameters: json.RawMessage(`{}`)}},
		Run: func(ctx context.Context, args json.RawMessage) tools.Result { return tools.Result{} },
	})
	fake := &fakeClient{reply: "ok"}
	ag := New(fake, "SYS", reg)
	if err := ag.Send(context.Background(), "one", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	reg.Register(tools.Tool{
		Def: llm.ToolDef{Type: "function", Function: llm.FunctionDef{Name: "beta", Parameters: json.RawMessage(`{}`)}},
		Run: func(ctx context.Context, args json.RawMessage) tools.Result { return tools.Result{} },
	})
	if names := toolNames(fake.gotReqs[0].Tools); contains(names, "beta") {
		t.Fatal("冻结后不应立刻暴露 beta")
	}
	ag.Reset()
	if err := ag.Send(context.Background(), "two", ignoreEvents); err != nil {
		t.Fatal(err)
	}
	if names := toolNames(fake.gotReqs[1].Tools); !contains(names, "alpha") || !contains(names, "beta") {
		t.Fatalf("新会话应重新冻结含迟到工具：%v", names)
	}
}

func TestPromptPrefixStableWithToolRounds(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("工具输出", 400)
	reg.Register(tools.Tool{
		Def: llm.ToolDef{Type: "function", Function: llm.FunctionDef{
			Name: "echo_blob", Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			return tools.Result{Content: long}
		},
	})
	scripts := make([]llm.ChatResult, 0, 6)
	for i := 0; i < 3; i++ {
		scripts = append(scripts, llm.ChatResult{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "c" + string(rune('1'+i)),
				Type:     "function",
				Function: llm.FunctionCall{Name: "echo_blob", Arguments: `{}`},
			}},
		}, llm.ChatResult{Content: "ok", FinishReason: "stop"})
	}
	c := &recordingClient{scripts: scripts}
	ag := New(c, "SYS", reg)
	for i := 0; i < 3; i++ {
		if err := ag.Send(context.Background(), "round", ignoreEvents); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.reqs) != 6 {
		t.Fatalf("应有 6 次请求，got %d", len(c.reqs))
	}
	for i := 1; i < len(c.reqs); i++ {
		prev, cur := c.reqs[i-1], c.reqs[i]
		b1, _ := json.Marshal(prev.Tools)
		b2, _ := json.Marshal(cur.Tools)
		if !bytes.Equal(b1, b2) {
			t.Fatalf("第 %d 次工具列表变了", i)
		}
		p1, _ := json.Marshal(prev.Messages)
		if len(cur.Messages) < len(prev.Messages) {
			t.Fatalf("第 %d 次消息变短了", i)
		}
		p2, _ := json.Marshal(cur.Messages[:len(prev.Messages)])
		if !bytes.Equal(p1, p2) {
			t.Fatalf("第 %d 次不是上一次的严格前缀扩展\nprev=%s\nhead=%s", i, p1, p2)
		}
	}
}

type recordingClient struct {
	scripts []llm.ChatResult
	calls   int
	reqs    []llm.ChatRequest
}

func (c *recordingClient) StreamChat(_ context.Context, req llm.ChatRequest, onDelta llm.DeltaHandler) (llm.ChatResult, error) {
	c.reqs = append(c.reqs, req)
	if c.calls >= len(c.scripts) {
		return llm.ChatResult{}, errors.New("unexpected extra StreamChat call")
	}
	step := c.scripts[c.calls]
	c.calls++
	if onDelta != nil && step.Content != "" {
		_ = onDelta(llm.Delta{Content: step.Content})
	}
	return step, nil
}

func toolNames(defs []llm.ToolDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Function.Name
	}
	return out
}

func ignoreEvents(Event) error { return nil }
