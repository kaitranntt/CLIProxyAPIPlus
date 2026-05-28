package helps

import (
	"math"
	"testing"
)

// usdAt computes the dollar cost of a usage breakdown at the given prices.
func usdAt(p kiroModelPrice, in, cw, cr, out int64) float64 {
	return float64(in)*p.Input +
		float64(cw)*p.CacheWrite +
		float64(cr)*p.CacheRead +
		float64(out)*p.Output
}

// approxEqualPct returns true if a and b agree within tolPct (e.g. 0.01 = 1%).
func approxEqualPct(a, b, tolPct float64) bool {
	if b == 0 {
		return math.Abs(a) < 1e-9
	}
	return math.Abs(a-b)/math.Abs(b) <= tolPct
}

func TestKiroCreditPricing_TypicalOpus47(t *testing.T) {
	// User-provided sample: credits=0.1847, ctxPct=3.99% (0.0399), output=52.
	const credits = 0.1847
	const ctxPct = 0.0399
	const output int64 = 52
	const model = "claude-opus-4.7"

	params := DefaultCreditPricingParams()
	got := ComputeUsageFromCredits(model, credits, ctxPct, output, params)

	prices := lookupKiroPrice(model)
	target := credits * kiroUSDPerCredit
	actual := usdAt(prices, got.InputTokens, got.CacheCreationTokens, got.CacheReadTokens, got.OutputTokens)
	if !approxEqualPct(actual, target, 0.01) {
		t.Fatalf("cost mismatch: target=%.6f USD, actual=%.6f USD, detail=%+v",
			target, actual, got)
	}
	if got.OutputTokens != output {
		t.Fatalf("output tokens should be unchanged, got %d", got.OutputTokens)
	}
	// Cache fields should dominate the input side under default 80/5/15.
	if got.CacheReadTokens <= got.InputTokens {
		t.Errorf("expected cache_read >> input under default ratios, got input=%d cache_read=%d",
			got.InputTokens, got.CacheReadTokens)
	}
	if got.TotalTokens != got.InputTokens+got.CacheCreationTokens+got.CacheReadTokens+got.OutputTokens {
		t.Errorf("total tokens not equal to sum of parts: %+v", got)
	}
}

func TestKiroCreditPricing_RatiosApproximatelyHeld(t *testing.T) {
	got := ComputeUsageFromCredits("claude-opus-4.7", 0.1847, 0.0399, 52, DefaultCreditPricingParams())
	totalCtx := got.InputTokens + got.CacheCreationTokens + got.CacheReadTokens
	if totalCtx == 0 {
		t.Fatalf("no context-side tokens emitted: %+v", got)
	}
	rIn := float64(got.InputTokens) / float64(totalCtx)
	rCw := float64(got.CacheCreationTokens) / float64(totalCtx)
	rCr := float64(got.CacheReadTokens) / float64(totalCtx)
	// Allow ±2% tolerance against 0.15/0.05/0.80.
	if math.Abs(rIn-0.15) > 0.02 || math.Abs(rCw-0.05) > 0.02 || math.Abs(rCr-0.80) > 0.02 {
		t.Errorf("ratios drifted: in=%.3f cw=%.3f cr=%.3f", rIn, rCw, rCr)
	}
}

func TestKiroCreditPricing_NoCredits(t *testing.T) {
	got := ComputeUsageFromCredits("claude-opus-4.7", 0, 0.0399, 52, DefaultCreditPricingParams())
	if got.InputTokens != 0 || got.CacheCreationTokens != 0 || got.CacheReadTokens != 0 {
		t.Errorf("no credits should yield zero input-side tokens, got %+v", got)
	}
	if got.OutputTokens != 52 {
		t.Errorf("output tokens should be preserved, got %d", got.OutputTokens)
	}
}

func TestKiroCreditPricing_NoContext(t *testing.T) {
	// With no context signal we must still account for the credits — fall
	// back to all-input.
	got := ComputeUsageFromCredits("claude-sonnet-4.5", 0.1, 0, 10, DefaultCreditPricingParams())
	if got.CacheReadTokens != 0 || got.CacheCreationTokens != 0 {
		t.Errorf("no context should not produce cache fields, got %+v", got)
	}
	if got.InputTokens <= 0 {
		t.Errorf("expected non-zero input tokens, got %+v", got)
	}
	prices := lookupKiroPrice("claude-sonnet-4.5")
	target := 0.1 * kiroUSDPerCredit
	actual := usdAt(prices, got.InputTokens, 0, 0, got.OutputTokens)
	if !approxEqualPct(actual, target, 0.01) {
		t.Errorf("cost mismatch in no-context case: target=%.6f actual=%.6f", target, actual)
	}
}

func TestKiroCreditPricing_UnknownModel(t *testing.T) {
	// Unknown model should fall back to opus-4.7 prices silently (warns once).
	a := ComputeUsageFromCredits("totally-not-a-model", 0.1847, 0.04, 52, DefaultCreditPricingParams())
	b := ComputeUsageFromCredits(kiroPriceFallbackModel, 0.1847, 0.04, 52, DefaultCreditPricingParams())
	if a != b {
		t.Errorf("unknown model fallback should match %s exactly: a=%+v b=%+v",
			kiroPriceFallbackModel, a, b)
	}
}

func TestKiroCreditPricing_OutputDominated(t *testing.T) {
	// Tiny credit amount with huge output → output cost exceeds budget,
	// input fields should be zero (clamped) and we don't go negative.
	got := ComputeUsageFromCredits("claude-opus-4.7", 0.0001, 0.5, 100000, DefaultCreditPricingParams())
	if got.InputTokens < 0 || got.CacheCreationTokens < 0 || got.CacheReadTokens < 0 {
		t.Errorf("token counts must be non-negative: %+v", got)
	}
	if got.OutputTokens != 100000 {
		t.Errorf("output should be preserved, got %d", got.OutputTokens)
	}
}

func TestKiroCreditPricing_ToolOnlyOutput(t *testing.T) {
	// Tool-only responses surface as a tiny placeholder output; the algorithm
	// should still produce sensible non-negative input-side numbers.
	got := ComputeUsageFromCredits("claude-sonnet-4.5", 0.05, 0.02, 1, DefaultCreditPricingParams())
	if got.InputTokens < 0 || got.CacheCreationTokens < 0 || got.CacheReadTokens < 0 {
		t.Errorf("token counts must be non-negative: %+v", got)
	}
	if got.OutputTokens != 1 {
		t.Errorf("placeholder output should be preserved, got %d", got.OutputTokens)
	}
	// Cost should still match target.
	prices := lookupKiroPrice("claude-sonnet-4.5")
	target := 0.05 * kiroUSDPerCredit
	actual := usdAt(prices, got.InputTokens, got.CacheCreationTokens, got.CacheReadTokens, got.OutputTokens)
	if !approxEqualPct(actual, target, 0.02) {
		t.Errorf("cost mismatch tool-only: target=%.6f actual=%.6f detail=%+v",
			target, actual, got)
	}
}

func TestKiroCreditPricing_PercentNormalization(t *testing.T) {
	// ctxPct expressed as whole percent (3.99) should yield the same result
	// as the fractional form (0.0399).
	a := ComputeUsageFromCredits("claude-opus-4.7", 0.1847, 0.0399, 52, DefaultCreditPricingParams())
	b := ComputeUsageFromCredits("claude-opus-4.7", 0.1847, 3.99, 52, DefaultCreditPricingParams())
	if a != b {
		t.Errorf("percent normalization mismatch: a=%+v b=%+v", a, b)
	}
}
