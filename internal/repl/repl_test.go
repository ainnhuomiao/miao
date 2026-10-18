package repl

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
)

type scriptClient struct {
	reply string
}

func (c *scriptClient) StreamChat(_ context.Context, _ llm.ChatRequest, on llm.DeltaHandler) (llm.ChatResult, error) {
	if on != nil {
		_ = on(llm.Delta{Content: c.reply})
	}
	return llm.ChatResult{Content: c.reply, FinishReason: "stop"}, nil
}

func newREPL(in string, reply string) (*REPL, *bytes.Buffer) {
	var out bytes.Buffer
	ag := agent.New(&scriptClient{reply: reply}, "sys", nil)
	r := &REPL{
		ag:    ag,
		model: "test-model",
		in:    bufio.NewReader(strings.NewReader(in)),
		out:   &out,
	}
	return r, &out
}

func TestREPLHelpClearHistoryExit(t *testing.T) {
	r, out := newREPL("/help\n/clear\n/history\n/nope\n/exit\n", "hi")
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"/help", "对话历史已清空", "还没有对话", "未知命令", "再见"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestREPLChat(t *testing.T) {
	r, out := newREPL("你好\n/exit\n", "世界")
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "世界") {
		t.Fatalf("got %q", out.String())
	}
	h := r.ag.History()
	if len(h) < 2 || h[0].Content != "你好" {
		t.Fatalf("history %+v", h)
	}
}

func TestSummarizeAndTruncate(t *testing.T) {
	if got := summarize("a\nb", 10); got != "a b" {
		t.Fatalf("%q", got)
	}
	if got := truncate("一二三四五", 3); got != "一二三…" {
		t.Fatalf("%q", got)
	}
}
