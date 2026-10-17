package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"miao/internal/llm"
	"miao/internal/subagent"
	"miao/internal/tools"
)

// toggleClient 记录每次请求的工具名，第一轮后开启子代理。
type toggleClient struct {
	n     int
	names [][]string
	after func()
}

func (c *toggleClient) StreamChat(ctx context.Context, req llm.ChatRequest, _ llm.DeltaHandler) (llm.ChatResult, error) {
	var ns []string
	for _, d := range req.Tools {
		ns = append(ns, d.Function.Name)
	}
	c.names = append(c.names, ns)
	c.n++
	if c.n == 1 {
		c.after()
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "1", Type: "function", Function: llm.FunctionCall{Name: "list_dir", Arguments: `{}`}}}}, nil
	}
	return llm.ChatResult{Content: "done"}, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestToolListReflectsSubagentSettingPerRequest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "x"), nil, 0o644)
	reg, _ := tools.NewRegistry(ws)
	tools.RegisterBuiltins(reg)
	c := &toggleClient{}
	mgr := subagent.New(reg, c, nil, subagent.Defaults())
	c.after = func() { _ = mgr.SetConfig(subagent.Config{Enabled: true}) }
	a := New(c, "sys", reg)
	if err := a.Send(context.Background(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if contains(c.names[0], "task") || !contains(c.names[1], "task") {
		t.Fatalf("工具列表应随设置变化：%v", c.names)
	}
	if !isReadOnlyTool("task") {
		t.Fatal("task 应可与只读工具并发批处理")
	}
}
