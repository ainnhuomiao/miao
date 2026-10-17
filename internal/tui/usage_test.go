package tui

import (
	"testing"

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
	if got := m.usageLabel(); got != "" {
		t.Fatalf("无用量时应为空，got %q", got)
	}
	m.usage = &llm.Usage{PromptTokens: 1000, CompletionTokens: 234, TotalTokens: 1234, PromptCacheHitTokens: 800}
	if got, want := m.usageLabel(), "1.2k tok · 缓存 80%"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
