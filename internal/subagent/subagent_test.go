package subagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agnet/internal/llm"
	"agnet/internal/tools"
)

func tempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "agnet", "config.json")
}

func TestConfigLoadSavePreservesKeysAndClamps(t *testing.T) {
	p := tempXDG(t)
	if c, err := LoadConfig(); err != nil || c != Defaults() || c.Enabled || c.Max != 3 {
		t.Fatalf("缺省配置应为默认值：%+v %v", c, err)
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"theme":"mocha","other":{"x":1}}`), 0o644)
	if err := SaveConfig(Config{Enabled: true, Max: 99, Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	var all map[string]any
	data, _ := os.ReadFile(p)
	_ = json.Unmarshal(data, &all)
	if all["theme"] != "mocha" || all["other"] == nil {
		t.Fatalf("其它键应保留：%s", data)
	}
	c, err := LoadConfig()
	if err != nil || !c.Enabled || c.Max != 8 || c.Model != "m2" {
		t.Fatalf("应夹到 8：%+v %v", c, err)
	}
	if (Config{Max: -5}).Normalize().Max != 1 {
		t.Fatal("负数应夹到 1")
	}
}

// fakeClient 按脚本回复：每次请求调用 step(n, req)。
type fakeClient struct {
	mu    sync.Mutex
	n     int
	step  func(n int, req llm.ChatRequest) (llm.ChatResult, error)
	model []string
}

func (f *fakeClient) StreamChat(ctx context.Context, req llm.ChatRequest, _ llm.DeltaHandler) (llm.ChatResult, error) {
	f.mu.Lock()
	n := f.n
	f.n++
	f.model = append(f.model, req.Model)
	f.mu.Unlock()
	return f.step(n, req)
}

func grepCall(id string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.FunctionCall{Name: "grep", Arguments: `{"pattern":"needle"}`}}
}

func newMgr(t *testing.T, c llm.Client, cfg Config) (*Manager, *tools.Registry) {
	t.Helper()
	tempXDG(t)
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "a.go"), []byte("package a\n// needle here\n"), 0o644)
	reg, err := tools.NewRegistry(ws)
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	return New(reg, c, func() string { return "main-model" }, cfg), reg
}

func hasDef(r *tools.Registry, name string) bool {
	for _, d := range r.Definitions() {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}

func TestExposureFollowsEnabled(t *testing.T) {
	m, reg := newMgr(t, &fakeClient{}, Defaults())
	if hasDef(reg, ToolName) {
		t.Fatal("关闭时不应暴露 task")
	}
	if err := m.SetConfig(Config{Enabled: true, Max: 2}); err != nil {
		t.Fatal(err)
	}
	if !hasDef(reg, ToolName) {
		t.Fatal("开启后应暴露 task")
	}
	if c, _ := LoadConfig(); !c.Enabled || c.Max != 2 {
		t.Fatalf("应已保存：%+v", c)
	}
	_ = m.SetConfig(Config{Enabled: false})
	if hasDef(reg, ToolName) || !strings.Contains(reg.Call(context.Background(), ToolName, `{}`).Content, "未知工具") {
		t.Fatal("关闭后应移除 task")
	}
}

func TestSubToolWhitelist(t *testing.T) {
	m, _ := newMgr(t, &fakeClient{}, Config{Enabled: true})
	sub := m.SubTools()
	for _, n := range AllowedTools {
		if !hasDef(sub, n) {
			t.Fatalf("缺少只读工具 %s", n)
		}
	}
	for _, n := range []string{"write_file", "edit_file", "run_command", "todo_write", ToolName} {
		if hasDef(sub, n) {
			t.Fatalf("子代理不应有 %s", n)
		}
	}
	if len(sub.Definitions()) != len(AllowedTools) {
		t.Fatalf("工具数不对：%d", len(sub.Definitions()))
	}
}

func TestTaskRunsGrepThenAnswers(t *testing.T) {
	fc := &fakeClient{step: func(n int, req llm.ChatRequest) (llm.ChatResult, error) {
		u := &llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
		if n == 0 {
			return llm.ChatResult{ToolCalls: []llm.ToolCall{grepCall("g1")}, Usage: u}, nil
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role != llm.RoleTool || !strings.Contains(last.Content, "a.go") {
			t.Errorf("第二轮应带 grep 结果：%+v", last)
		}
		if req.Messages[0].Content != SystemPrompt {
			t.Error("应使用子代理系统提示词")
		}
		return llm.ChatResult{Content: "结论：a.go:2", Usage: u}, nil
	}}
	m, reg := newMgr(t, fc, Config{Enabled: true, Model: ""})
	var acts []string
	var usage int
	var mu sync.Mutex
	ctx := tools.WithProgress(context.Background(), func(s string) { mu.Lock(); acts = append(acts, s); mu.Unlock() })
	ctx = WithUsageSink(ctx, func(u llm.Usage) { usage += u.TotalTokens })
	res := reg.Call(ctx, ToolName, `{"description":"找针","prompt":"找 needle"}`)
	if res.IsError || !strings.HasPrefix(res.Content, "结论：a.go:2") || !strings.Contains(res.Content, "↑20 ↓10") {
		t.Fatalf("结果不对：%+v", res)
	}
	if usage != 30 {
		t.Fatalf("用量应上报 30：%d", usage)
	}
	if !strings.Contains(strings.Join(acts, "|"), "搜索 needle") {
		t.Fatalf("应有进度：%v", acts)
	}
	if fc.model[0] != "main-model" {
		t.Fatalf("空 model 应跟随主模型：%v", fc.model)
	}
	_ = m.SetConfig(Config{Enabled: true, Model: "sub-model"})
	fc.n = 0
	reg.Call(context.Background(), ToolName, `{"description":"x","prompt":"y"}`)
	if fc.model[len(fc.model)-1] != "sub-model" {
		t.Fatalf("应使用配置的模型：%v", fc.model)
	}
}

func TestStepLimit(t *testing.T) {
	fc := &fakeClient{step: func(n int, req llm.ChatRequest) (llm.ChatResult, error) {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{grepCall("g")}}, nil
	}}
	m, reg := newMgr(t, fc, Config{Enabled: true})
	m.MaxSteps = 3
	res := reg.Call(context.Background(), ToolName, `{"description":"x","prompt":"y"}`)
	if !res.IsError || !strings.Contains(res.Content, "3 轮") || fc.n != 4 {
		t.Fatalf("应在 3 轮后停止：%+v n=%d", res, fc.n)
	}
}

func TestCancellationAndTimeout(t *testing.T) {
	fc := &fakeClient{}
	fc.step = func(n int, req llm.ChatRequest) (llm.ChatResult, error) {
		time.Sleep(20 * time.Millisecond)
		return llm.ChatResult{ToolCalls: []llm.ToolCall{grepCall("g")}}, nil
	}
	_, reg := newMgr(t, fc, Config{Enabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start := time.Now()
	res := reg.Call(ctx, ToolName, `{"description":"x","prompt":"y"}`)
	if !res.IsError || !strings.Contains(res.Content, "已取消") || time.Since(start) > 2*time.Second {
		t.Fatalf("应被取消：%+v", res)
	}

	m2, reg2 := newMgr(t, fc, Config{Enabled: true})
	m2.Timeout = 50 * time.Millisecond
	m2.MaxSteps = 1000
	res = reg2.Call(context.Background(), ToolName, `{"description":"x","prompt":"y"}`)
	if !res.IsError || !strings.Contains(res.Content, "超时") {
		t.Fatalf("应超时：%+v", res)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	var cur, peak int32
	fc := &fakeClient{step: func(n int, req llm.ChatRequest) (llm.ChatResult, error) {
		c := atomic.AddInt32(&cur, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if c <= p || atomic.CompareAndSwapInt32(&peak, p, c) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		return llm.ChatResult{Content: "ok"}, nil
	}}
	m, reg := newMgr(t, fc, Config{Enabled: true, Max: 2})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := reg.Call(context.Background(), ToolName, `{"description":"x","prompt":"y"}`); r.IsError {
				t.Error(r.Content)
			}
		}()
	}
	wg.Wait()
	if peak != 2 {
		t.Fatalf("并发峰值应为 2：%d", peak)
	}
	if m.Running() != 0 {
		t.Fatal("结束后运行数应为 0")
	}
}
