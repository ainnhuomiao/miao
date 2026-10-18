package llm

import (
	"fmt"
	"strings"
)

// ModelPrice 是每百万 token 的美元单价（输入 / 输出）。
type ModelPrice struct {
	In, Out float64
}

// 常见模型的公开标价，仅用于估算；可用 MIAO_PRICE_IN / MIAO_PRICE_OUT 覆盖。
var modelPrices = []struct {
	key   string
	price ModelPrice
}{
	{"claude-opus", ModelPrice{In: 15, Out: 75}},
	{"claude-sonnet", ModelPrice{In: 3, Out: 15}},
	{"claude-haiku", ModelPrice{In: 0.8, Out: 4}},
	{"gpt-4.1", ModelPrice{In: 2, Out: 8}},
	{"gpt-4o", ModelPrice{In: 2.5, Out: 10}},
	{"o4-mini", ModelPrice{In: 1.1, Out: 4.4}},
	{"deepseek-reasoner", ModelPrice{In: 0.55, Out: 2.19}},
	{"deepseek", ModelPrice{In: 0.27, Out: 1.1}},
	{"qwen", ModelPrice{In: 0.3, Out: 1.2}},
	{"kimi", ModelPrice{In: 0.2, Out: 2}},
	{"glm", ModelPrice{In: 0.1, Out: 0.1}},
}

// LookupPrice 按模型名子串匹配单价；没有则 ok=false。
func LookupPrice(model string) (ModelPrice, bool) {
	name := strings.ToLower(model)
	for _, p := range modelPrices {
		if strings.Contains(name, p.key) {
			return p.price, true
		}
	}
	return ModelPrice{}, false
}

// CacheMultiplier 是缓存命中相对输入价的倍率。override>0 时用它；
// 否则 DeepSeek / Claude 按 0.1，其余按 0.25。
func CacheMultiplier(model string, override float64) float64 {
	if override > 0 {
		return override
	}
	n := strings.ToLower(model)
	if strings.Contains(n, "deepseek") || strings.Contains(n, "claude") || strings.Contains(n, "anthropic") {
		return 0.1
	}
	return 0.25
}

// EstimateCostUSD 按用量估算美元花费。cacheMul<=0 时按模型选默认缓存倍率。
func EstimateCostUSD(model string, u Usage, overrideIn, overrideOut, cacheMul float64) (float64, bool) {
	in, out := overrideIn, overrideOut
	if in <= 0 || out <= 0 {
		p, ok := LookupPrice(model)
		if !ok && in <= 0 && out <= 0 {
			return 0, false
		}
		if in <= 0 {
			in = p.In
		}
		if out <= 0 {
			out = p.Out
		}
	}
	uncached := u.PromptTokens - u.PromptCacheHitTokens
	if uncached < 0 {
		uncached = 0
	}
	mul := CacheMultiplier(model, cacheMul)
	cost := (float64(uncached)*in + float64(u.PromptCacheHitTokens)*in*mul + float64(u.CompletionTokens)*out) / 1_000_000
	return cost, true
}

// FormatCost 把美元花费格式化成 $0.012。
func FormatCost(usd float64) string {
	if usd < 0.001 {
		return fmt.Sprintf("$%.4f", usd)
	}
	if usd < 0.1 {
		return fmt.Sprintf("$%.3f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}
