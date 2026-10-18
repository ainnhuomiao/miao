package llm

import "testing"

func TestEstimateCostUSD(t *testing.T) {
	u := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	got, ok := EstimateCostUSD("deepseek-chat", u, 0, 0)
	if !ok || got <= 0 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := EstimateCostUSD("unknown-model-xyz", u, 0, 0); ok {
		t.Fatal("unknown model should not estimate")
	}
	got, ok = EstimateCostUSD("unknown-model-xyz", u, 1, 2)
	if !ok || got != 3 {
		t.Fatalf("override: %v %v", got, ok)
	}
}
