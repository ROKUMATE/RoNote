package chat

import "fmt"

// Price is a model's rates, in micro-dollars per million tokens.
type Price struct {
	Input, Output, CacheWrite, CacheRead int64
}

// prices are Anthropic's list prices. Claude Haiku 5.5's hold for prompts
// of up to 100,000 tokens, which the input cap keeps every call under;
// cache writes are 5-minute ones.
var prices = map[string]Price{
	"claude-haiku-5-5": {Input: 100_000, Output: 500_000, CacheWrite: 125_000, CacheRead: 10_000},
}

// PriceOf returns model's price. A model without one can't be used: its
// answers' cost would go unrecorded.
func PriceOf(model string) (Price, error) {
	p, ok := prices[model]
	if !ok {
		return Price{}, fmt.Errorf("chat: no price for model %q; add it to internal/chat/price.go", model)
	}
	return p, nil
}

// Cost is what u cost, in micro-dollars, rounded up.
func (p Price) Cost(u Usage) int64 {
	// Tokens times micro-dollars per million tokens: millionths of a
	// micro-dollar.
	scaled := int64(u.InputTokens)*p.Input + int64(u.OutputTokens)*p.Output +
		int64(u.CacheCreationInputTokens)*p.CacheWrite + int64(u.CacheReadInputTokens)*p.CacheRead
	return (scaled + 999_999) / 1_000_000
}
