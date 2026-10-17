package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"agnet/internal/agent"
	"agnet/internal/llm"
)

func TestFormatTokens(t *testing.T) {
	cases := map[int]string{0: "0", 980: "980", 1000: "1k", 1234: "1.2k", 15678: "15k", 1_300_000: "1.3M"}
	for n, want := range cases {
		if got := formatTokens(n); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestUsageLabel(t *testing.T) {
	m := &Model{}
	if got := m.usageLabel(true); got != "" {
		t.Fatalf("无用量时应为空，got %q", got)
	}
	m.usage = &llm.Usage{PromptTokens: 1234, CompletionTokens: 340, PromptCacheHitTokens: 1049}
	if got, want := ansi.Strip(m.usageLabel(true)), " ↑1.2k ↓340 · 缓存 85% "; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := ansi.Strip(m.usageLabel(false)); strings.Contains(got, "缓存") {
		t.Fatalf("showCache=false 不应显示缓存: %q", got)
	}
	m.usage.PromptCacheHitTokens = 0
	if got := ansi.Strip(m.usageLabel(true)); strings.Contains(got, "缓存") {
		t.Fatalf("无缓存数据时不应显示缓存: %q", got)
	}
}

func TestUsageAccumulatesWithinTurn(t *testing.T) {
	m := &Model{usageFresh: true, usage: &llm.Usage{PromptTokens: 999}}
	m.handleStreamEvent(streamEventMsg{kind: agent.EventUsage, usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 10, PromptCacheHitTokens: 50}})
	m.handleStreamEvent(streamEventMsg{kind: agent.EventUsage, usage: &llm.Usage{PromptTokens: 200, CompletionTokens: 20, PromptCacheHitTokens: 150}})
	want := llm.Usage{PromptTokens: 300, CompletionTokens: 30, PromptCacheHitTokens: 200}
	if *m.usage != want {
		t.Fatalf("usage = %+v, want %+v", *m.usage, want)
	}
}

func TestHintViewFitsWidth(t *testing.T) {
	ag := agent.New(nil, "", nil)
	for _, w := range []int{30, 60, 100, 160} {
		m := &Model{ag: ag, modelName: "deepseek-flash", width: w,
			usage: &llm.Usage{PromptTokens: 7700, CompletionTokens: 939, PromptCacheHitTokens: 2700}}
		got := m.hintView()
		if gw := ansi.StringWidth(got); gw > w {
			t.Errorf("width %d: 底栏宽 %d 超出", w, gw)
		}
		if w >= 100 && ansi.StringWidth(got) != w {
			t.Errorf("width %d: 宽屏时底栏应铺满整行，实际 %d", w, ansi.StringWidth(got))
		}
		if w >= 100 && !strings.Contains(ansi.Strip(got), "⏎ 发送") {
			t.Errorf("width %d: 宽屏应显示快捷键: %q", w, ansi.Strip(got))
		}
	}
}
