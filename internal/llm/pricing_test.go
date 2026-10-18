package llm

import "testing"

func TestEstimateCostUSD(t *testing.T) {
	u := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	got, ok := EstimateCostUSD("deepseek-chat", u, 0, 0, 0)
	if !ok || got <= 0 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := EstimateCostUSD("unknown-model-xyz", u, 0, 0, 0); ok {
		t.Fatal("unknown model should not estimate")
	}
	got, ok = EstimateCostUSD("unknown-model-xyz", u, 1, 2, 0)
	if !ok || got != 3 {
		t.Fatalf("override: %v %v", got, ok)
	}
}

func TestCacheMultiplier(t *testing.T) {
	if CacheMultiplier("deepseek-chat", 0) != 0.1 {
		t.Fatal("deepseek")
	}
	if CacheMultiplier("claude-sonnet-4", 0) != 0.1 {
		t.Fatal("claude")
	}
	if CacheMultiplier("gpt-4o", 0) != 0.25 {
		t.Fatal("openai default")
	}
	if CacheMultiplier("gpt-4o", 0.5) != 0.5 {
		t.Fatal("override")
	}
	u := Usage{PromptTokens: 1_000_000, PromptCacheHitTokens: 500_000, CompletionTokens: 0}
	got, ok := EstimateCostUSD("deepseek-chat", u, 1, 1, 0)
	if !ok {
		t.Fatal("ok")
	}
	// uncached 0.5 + cached 0.5*0.1 = 0.55
	if got != 0.55 {
		t.Fatalf("cache cost = %v", got)
	}
}
