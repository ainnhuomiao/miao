package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

type fakeClient struct {
	reply   string
	err     error
	gotReqs []llm.ChatRequest
}

func (f *fakeClient) StreamChat(ctx context.Context, req llm.ChatRequest, onDelta llm.DeltaHandler) (llm.ChatResult, error) {
	f.gotReqs = append(f.gotReqs, req)
	if f.err != nil {
		return llm.ChatResult{}, f.err
	}
	var content strings.Builder
	for _, r := range f.reply {
		s := string(r)
		content.WriteString(s)
		if onDelta != nil {
			if err := onDelta(llm.Delta{Content: s}); err != nil {
				return llm.ChatResult{}, err
			}
		}
	}
	return llm.ChatResult{Content: content.String(), FinishReason: "stop"}, nil
}

type scriptClient struct {
	calls   int
	scripts []llm.ChatResult
}

func (c *scriptClient) StreamChat(ctx context.Context, req llm.ChatRequest, onDelta llm.DeltaHandler) (llm.ChatResult, error) {
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

func TestSendRecordsHistory(t *testing.T) {
	fake := &fakeClient{reply: "你好"}
	ag := New(fake, "你是测试助手", nil)

	var got strings.Builder
	err := ag.Send(context.Background(), "hi", func(e Event) error {
		if e.Kind == EventDelta {
			got.WriteString(e.Delta.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Send 返回错误: %v", err)
	}
	if got.String() != "你好" {
		t.Errorf("流式输出 = %q", got.String())
	}
	if len(fake.gotReqs) != 1 || len(fake.gotReqs[0].Messages) != 2 {
		t.Fatalf("请求 = %+v", fake.gotReqs)
	}
	history := ag.History()
	if len(history) != 2 || history[1].Content != "你好" {
		t.Fatalf("history = %+v", history)
	}
}

func TestSendRollsBackOnError(t *testing.T) {
	fake := &fakeClient{err: errors.New("boom")}
	ag := New(fake, "", nil)
	if err := ag.Send(context.Background(), "hi", nil); err == nil {
		t.Fatal("期望返回错误")
	}
	if n := len(ag.History()); n != 0 {
		t.Errorf("失败后历史长度 = %d", n)
	}
}

func TestAgent_ToolRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)

	fake := &scriptClient{scripts: []llm.ChatResult{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "c1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"note.txt"}`,
				},
			}},
		},
		{
			Content:      "文件里写着 secret",
			FinishReason: "stop",
		},
	}}

	a := New(fake, "sys", reg)
	var events []Event
	err = a.Send(context.Background(), "读 note.txt", func(e Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fake.calls != 2 {
		t.Fatalf("client calls = %d", fake.calls)
	}
	var sawTool bool
	for _, m := range a.History() {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "secret") {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatal("missing tool result")
	}
	var sawStart, sawEnd bool
	for _, e := range events {
		if e.Kind == EventToolStart && e.ToolName == "read_file" {
			sawStart = true
		}
		if e.Kind == EventToolEnd && strings.Contains(e.ToolResult, "secret") {
			sawEnd = true
		}
	}
	if !sawStart || !sawEnd {
		t.Fatalf("events = %+v", events)
	}
}

func TestAgent_RejectLengthTruncatedTools(t *testing.T) {
	reg, _ := tools.NewRegistry(t.TempDir())
	tools.RegisterBuiltins(reg)
	fake := &scriptClient{scripts: []llm.ChatResult{{
		FinishReason: "length",
		ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.FunctionCall{Name: "list_dir", Arguments: `{}`},
		}},
	}}}
	a := New(fake, "", reg)
	err := a.Send(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "长度截断") {
		t.Fatalf("err = %v", err)
	}
	if h := a.History(); len(h) != 1 || !isBaselineReminder(h[0]) {
		t.Fatalf("失败后应只保留基线 reminder，got %+v", h)
	}
}

func TestAgent_MaxRounds(t *testing.T) {
	reg, _ := tools.NewRegistry(t.TempDir())
	tools.RegisterBuiltins(reg)
	const limit = 4
	scripts := make([]llm.ChatResult, 0, limit)
	for i := 0; i < limit; i++ {
		scripts = append(scripts, llm.ChatResult{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "c",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "list_dir",
					Arguments: `{"path":"."}`,
				},
			}},
		})
	}
	fake := &scriptClient{scripts: scripts}
	a := New(fake, "", reg)
	a.SetMaxToolRounds(limit)
	err := a.Send(context.Background(), "loop", nil)
	if err == nil || !errors.Is(err, ErrMaxToolRounds) {
		t.Fatalf("err = %v", err)
	}
	if n := len(a.History()); n == 0 {
		t.Fatal("超限时应保留历史，而不是 rollback")
	}
}

type failOnceClient struct {
	captureClient
	calls int
}

func (c *failOnceClient) StreamChat(ctx context.Context, req llm.ChatRequest, onDelta llm.DeltaHandler) (llm.ChatResult, error) {
	c.calls++
	if c.calls == 1 {
		return llm.ChatResult{}, errors.New("primary down")
	}
	return c.captureClient.StreamChat(ctx, req, onDelta)
}

func TestFallbackRestoresPrimary(t *testing.T) {
	c := &failOnceClient{captureClient: captureClient{model: "primary"}}
	a := New(c, "SYS", nil)
	a.SetFallbackModel("backup")
	var notice string
	err := a.Send(context.Background(), "hi", func(e Event) error {
		if e.Kind == EventNotice {
			notice = e.ToolResult
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Model() != "primary" {
		t.Fatalf("应切回主模型，got %q", c.Model())
	}
	if !strings.Contains(notice, "备用模型 backup") {
		t.Fatalf("应提示备用模型：%q", notice)
	}
}
