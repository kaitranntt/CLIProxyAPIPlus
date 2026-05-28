// Package helps contains helper utilities for runtime executors.
//
// kiro_credit_pricing.go reverse-engineers a plausible token-level usage
// breakdown (input / cache_creation / cache_read / output) from the credit
// figure Kiro reports via meteringEvent, so the response presented to the
// client looks like a normal Anthropic Claude usage object.
//
// Conversion rule:
//
//	target_usd = credits * $0.02 / discount      (discount = 0.1 → 1折)
//
// The reconstructed usage cost, evaluated at Anthropic's published per-MTok
// prices, is forced to equal target_usd modulo rounding.
package helps

import (
	"math"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// kiroCreditPerUSD is the Kiro pricing rule: $20 = 1000 credits.
// Inverted: 1 credit = $0.02.
const (
	kiroUSDPerCredit       = 0.02
	kiroMaxContextTokens   = 200000
	kiroPriceFallbackModel = "claude-opus-4.7"
)

// kiroModelPrice holds Anthropic per-million-token prices for one model.
// All values are USD per token (already divided by 1e6).
type kiroModelPrice struct {
	Input      float64
	CacheWrite float64 // 5m cache write
	CacheRead  float64
	Output     float64
}

// per converts a $/MTok value to $/token.
func per(mtok float64) float64 { return mtok / 1_000_000.0 }

// kiroPriceTable is keyed by the normalized backend model id produced by
// KiroExecutor.mapModelToKiro (e.g. "claude-opus-4.7", "claude-sonnet-4.5").
// Source: claude.com/pricing screenshot supplied by user (Opus 4.5/4.6/4.7
// rows are taken verbatim from that screenshot — Anthropic has not published
// these rows on the public API page, so this represents Kiro-internal pricing).
var kiroPriceTable = map[string]kiroModelPrice{
	"claude-opus-4.7": {Input: per(5), CacheWrite: per(6.25), CacheRead: per(0.50), Output: per(25)},
	"claude-opus-4.6": {Input: per(5), CacheWrite: per(6.25), CacheRead: per(0.50), Output: per(25)},
	"claude-opus-4.5": {Input: per(5), CacheWrite: per(6.25), CacheRead: per(0.50), Output: per(25)},
	"claude-opus-4.1": {Input: per(15), CacheWrite: per(18.75), CacheRead: per(1.50), Output: per(75)},
	"claude-opus-4":   {Input: per(15), CacheWrite: per(18.75), CacheRead: per(1.50), Output: per(75)},

	"claude-sonnet-4.6": {Input: per(3), CacheWrite: per(3.75), CacheRead: per(0.30), Output: per(15)},
	"claude-sonnet-4.5": {Input: per(3), CacheWrite: per(3.75), CacheRead: per(0.30), Output: per(15)},
	"claude-sonnet-4":   {Input: per(3), CacheWrite: per(3.75), CacheRead: per(0.30), Output: per(15)},

	"claude-haiku-4.5": {Input: per(1), CacheWrite: per(1.25), CacheRead: per(0.10), Output: per(5)},
	"claude-haiku-3.5": {Input: per(0.80), CacheWrite: per(1), CacheRead: per(0.08), Output: per(4)},
}

var (
	kiroPriceWarnedMu sync.Mutex
	kiroPriceWarned   = map[string]struct{}{}
)

// lookupKiroPrice returns the price row for model, falling back to
// kiroPriceFallbackModel when the model is unknown. Each unknown model is
// warn-logged once per process.
func lookupKiroPrice(model string) kiroModelPrice {
	key := strings.ToLower(strings.TrimSpace(model))
	if price, ok := kiroPriceTable[key]; ok {
		return price
	}
	kiroPriceWarnedMu.Lock()
	if _, seen := kiroPriceWarned[key]; !seen {
		kiroPriceWarned[key] = struct{}{}
		log.Warnf("kiro: no credit pricing entry for model %q, falling back to %s",
			key, kiroPriceFallbackModel)
	}
	kiroPriceWarnedMu.Unlock()
	return kiroPriceTable[kiroPriceFallbackModel]
}

// CreditPricingParams configures how credits are split into the four token
// buckets. v1 keeps these as constants; the struct is exposed so future
// configuration plumbing can override without touching the algorithm.
type CreditPricingParams struct {
	// CacheReadRatio is the share of context tokens that should be reported as
	// cache_read_input_tokens.
	CacheReadRatio float64

	// CacheCreationRatio is the share of context tokens that should be
	// reported as cache_creation_input_tokens.
	CacheCreationRatio float64
	// The implicit raw-input ratio is 1 - CacheReadRatio - CacheCreationRatio.
}

// DefaultCreditPricingParams returns the v1 hardcoded defaults:
// an 80/5/15 cache-read / cache-creation / raw-input split.
func DefaultCreditPricingParams() CreditPricingParams {
	return CreditPricingParams{
		CacheReadRatio:     0.80,
		CacheCreationRatio: 0.05,
	}
}

// ComputeUsageFromCredits reverse-engineers a usage.Detail from the credits
// reported by Kiro and the upstream context-usage percentage.
//
// Inputs:
//   - model: normalized backend id (e.g. "claude-opus-4.7"). Use
//     KiroExecutor.mapModelToKiro before calling.
//   - credits: meteringEvent.usage value (in credits, not USD).
//   - contextPct: contextUsageEvent.contextUsagePercentage (0–1 in observed
//     payloads, but values up to 100 are tolerated and treated as percent).
//   - outputTokens: locally computed output tokens (kept as-is).
//   - params: split parameters.
//
// Returns a usage.Detail where the dollar cost evaluated at Anthropic prices
// equals credits * 0.02 within rounding error.
func ComputeUsageFromCredits(model string, credits, contextPct float64, outputTokens int64, params CreditPricingParams) usage.Detail {
	// Defensive defaults: an unconfigured params struct collapses to all-input.
	if params.CacheReadRatio < 0 {
		params.CacheReadRatio = 0
	}
	if params.CacheCreationRatio < 0 {
		params.CacheCreationRatio = 0
	}
	if params.CacheReadRatio+params.CacheCreationRatio > 1 {
		params.CacheReadRatio = 0.80
		params.CacheCreationRatio = 0.05
	}

	prices := lookupKiroPrice(model)

	// Target dollar amount at Anthropic's published prices.
	targetUSD := credits * kiroUSDPerCredit

	out := usage.Detail{OutputTokens: outputTokens}

	// Output cost is fixed (we trust local tiktoken).
	outCost := float64(outputTokens) * prices.Output
	budget := targetUSD - outCost
	if budget < 0 {
		budget = 0
	}

	// Normalize contextPct: payloads are usually fractions (0.04 = 4%) but
	// some logs show whole percent (3.99). Treat anything > 1 as percent.
	pct := contextPct
	if pct > 1 {
		pct = pct / 100.0
	}
	if pct < 0 {
		pct = 0
	}

	ctxTokens := pct * kiroMaxContextTokens

	rIn := 1 - params.CacheReadRatio - params.CacheCreationRatio
	rCw := params.CacheCreationRatio
	rCr := params.CacheReadRatio

	// Compute the unit cost of one "context token" given the desired ratios.
	unit := rIn*prices.Input + rCw*prices.CacheWrite + rCr*prices.CacheRead

	// All-input fallback when we cannot do the three-way split: either no
	// context signal, or the price/ratio combination collapses to zero.
	if ctxTokens <= 0 || unit <= 0 {
		if prices.Input > 0 {
			out.InputTokens = int64(math.Round(budget / prices.Input))
		}
		out.TotalTokens = out.InputTokens + out.OutputTokens
		return out
	}

	k := budget / (ctxTokens * unit)

	// Sanity warn — outside this band the pricing table or model mapping is
	// likely wrong, but we still emit values so the response remains valid.
	if k > 0 && (k < 1e-2 || k > 1e3) {
		log.Warnf("kiro: credit→token scaling factor out of expected range "+
			"(model=%s, credits=%.4f, ctxPct=%.4f, k=%.4g)",
			model, credits, contextPct, k)
	}

	scaled := k * ctxTokens
	out.InputTokens = int64(math.Round(scaled * rIn))
	out.CacheCreationTokens = int64(math.Round(scaled * rCw))
	out.CacheReadTokens = int64(math.Round(scaled * rCr))

	out.TotalTokens = out.InputTokens + out.CacheCreationTokens + out.CacheReadTokens + out.OutputTokens
	return out
}
