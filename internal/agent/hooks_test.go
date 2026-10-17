package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miao/internal/hooks"
	"miao/internal/llm"
	"miao/internal/tools"
)

func hookAgent(t *testing.T, userHooks string, scripts []llm.ChatResult) (*Agent, *scriptClient, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	os.MkdirAll(filepath.Join(cfg, "miao"), 0o755)
	os.WriteFile(filepath.Join(cfg, "miao", "config.json"), []byte(`{"hooks":`+userHooks+`}`), 0o644)
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("hello"), 0o644)
	reg, err := tools.NewRegistry(ws)
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	c := &scriptClient{scripts: scripts}
	a := New(c, "", reg)
	r, err := hooks.Load(ws, a.HookApprover())
	if err != nil {
		t.Fatal(err)
	}
	a.SetHooks(r)
	return a, c, ws
}

func readCall(id string) llm.ChatResult {
	return llm.ChatResult{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{
		ID: id, Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`},
	}}}
}

func toolResult(a *Agent, id string) string {
	for _, m := range a.History() {
		if m.Role == llm.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	return ""
}

func TestPreToolHookBlocks(t *testing.T) {
	a, _, _ := hookAgent(t, `{"pre_tool":[{"match":"read_file","command":"echo 禁止读取; exit 1"}]}`,
		[]llm.ChatResult{readCall("c1"), {Content: "好的", FinishReason: "stop"}})
	if err := a.Send(context.Background(), "读一下", nil); err != nil {
		t.Fatal(err)
	}
	got := toolResult(a, "c1")
	if !strings.Contains(got, "阻止") || !strings.Contains(got, "禁止读取") || strings.Contains(got, "hello") {
		t.Fatalf("工具不应执行：%q", got)
	}
}

func TestPostToolHookAppendsOutput(t *testing.T) {
	a, _, _ := hookAgent(t, `{"post_tool":[{"command":"echo lint 失败; exit 1"}]}`,
		[]llm.ChatResult{readCall("c1"), {Content: "好的", FinishReason: "stop"}})
	if err := a.Send(context.Background(), "读一下", nil); err != nil {
		t.Fatal(err)
	}
	if got := toolResult(a, "c1"); !strings.Contains(got, "hello") || !strings.Contains(got, "lint 失败") {
		t.Fatalf("got %q", got)
	}
}

func TestStopHookFeedsBackAndIsBounded(t *testing.T) {
	a, c, _ := hookAgent(t, `{"stop":[{"command":"echo 测试没过; exit 1"}]}`, []llm.ChatResult{
		{Content: "做完了", FinishReason: "stop"},
		{Content: "修好了", FinishReason: "stop"},
		{Content: "真的修好了", FinishReason: "stop"},
	})
	var hookEvents int
	err := a.Send(context.Background(), "干活", func(e Event) error {
		if e.ToolName == "hook" && e.Kind == EventToolEnd {
			hookEvents++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.calls != 1+maxStopHookRetries || hookEvents != maxStopHookRetries {
		t.Fatalf("calls=%d events=%d", c.calls, hookEvents)
	}
	var fed int
	for _, m := range a.History() {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "测试没过") {
			fed++
		}
	}
	if fed != maxStopHookRetries {
		t.Fatalf("hook 输出应交给模型 %d 次，got %d", maxStopHookRetries, fed)
	}
}

func TestProjectHookTrustUsesRawApprover(t *testing.T) {
	a, _, ws := hookAgent(t, `{}`, []llm.ChatResult{{Content: "ok", FinishReason: "stop"}})
	os.MkdirAll(filepath.Join(ws, ".miao"), 0o755)
	os.WriteFile(filepath.Join(ws, ".miao", "hooks.json"), []byte(`{"stop":[{"command":"touch ran"}]}`), 0o644)
	r, _ := hooks.Load(ws, a.HookApprover())
	a.SetHooks(r)
	a.SetMode(ModeAcceptEdits) // 自动同意编辑的模式也不能跳过 hook 确认
	var asked string
	a.SetApprover(func(_ context.Context, name, display string, _ json.RawMessage) (bool, error) {
		asked = name
		return false, nil
	})
	if err := a.Send(context.Background(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if asked != "hook" {
		t.Fatalf("应弹审批卡确认项目 hook，got %q", asked)
	}
	if _, err := os.Stat(filepath.Join(ws, "ran")); err == nil {
		t.Fatal("拒绝后不应执行")
	}
}
