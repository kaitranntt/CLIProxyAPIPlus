// Package helps provides supporting helpers for runtime executors.
package helps

import (
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// modelPricing holds Anthropic per-bucket prices ($/MTok) for a model. All
// buckets are needed to map a Kiro credit total onto token counts under
// official pricing. The 5m cache-write tier is used for cacheCreation (the
// default Anthropic cache TTL); the 1h tier is not modeled.
type modelPricing struct {
	input         float64 // base input tokens ($/MTok)
	cacheRead     float64 // cache hits & refreshes ($/MTok)
	cacheCreation float64 // 5m cache writes ($/MTok)
	output        float64 // output tokens ($/MTok)
}

// officialBasePrices holds Anthropic prices used to derive per-token credit
// rates. Models absent from this table fall back to fallbackPricing.
var officialBasePrices = map[string]modelPricing{
	"claude-opus-4.8":   {input: 5, cacheRead: 0.5, cacheCreation: 6.25, output: 25},
	"claude-opus-4.7":   {input: 5, cacheRead: 0.5, cacheCreation: 6.25, output: 25},
	"claude-opus-4.6":   {input: 5, cacheRead: 0.5, cacheCreation: 6.25, output: 25},
	"claude-opus-4.5":   {input: 5, cacheRead: 0.5, cacheCreation: 6.25, output: 25},
	"claude-opus-4.1":   {input: 15, cacheRead: 1.5, cacheCreation: 18.75, output: 75},
	"claude-sonnet-4.6": {input: 3, cacheRead: 0.3, cacheCreation: 3.75, output: 15},
	"claude-sonnet-4.5": {input: 3, cacheRead: 0.3, cacheCreation: 3.75, output: 15},
	"claude-haiku-4.5":  {input: 1, cacheRead: 0.1, cacheCreation: 1.25, output: 5},
}

// Kiro credit pricing basis.
//   - creditToUSD: 1 credit = $0.03.
//
// Derived credit rate per base-input token: c = (input/1e6) / creditToUSD.
//
//	Opus 4.7   ($5/MTok): (5/1e6)/0.03 = 0.000167 credits/token
//	Sonnet 4.6 ($3/MTok): (3/1e6)/0.03 = 0.000100 credits/token
const (
	creditToUSD = 0.03 // USD per credit
)

// fallbackPricing is used when a model is absent from officialBasePrices.
// It mirrors the Opus tier (the most common Claude family on Kiro).
var fallbackPricing = modelPricing{input: 5, cacheRead: 0.5, cacheCreation: 6.25, output: 25}

// ApplyKiroCreditCacheSplit derives a cache token split from credits using a
// price heuristic and mutates detail in place. It sets CacheReadTokens,
// CachedTokens, CacheCreationTokens, and the uncached InputTokens.
//
// Per-token credit rates come from the model's official Anthropic prices
// (modelPricing) divided by creditToUSD. Let T be the original input total and
// O the output. With the invariant uncached + cache_read == T, the billed
// credits from the input+output base range over:
//
//	min = cRead*T  + cOut*O   (all input billed as cache_read)
//	max = cIn*T    + cOut*O   (all input billed as uncached)
//
// The split is chosen to match the upstream credit value:
//
//   - credits within [min, max]: back-solve cache_read R from
//     credits = cIn*(T-R) + cRead*R + cOut*O  =>  R = (cIn*T + cOut*O - credits)
//     / (cIn - cRead), clamped to [0, T]; cache_creation stays 0.
//   - credits below min: cache_read clamps to T (cheapest base), overflow
//     discarded, cache_creation stays 0. Billing will exceed credits, but token
//     splits cannot go lower while preserving the context base.
//   - credits above max (even all-uncached input cannot reach credits): bill all
//     input as uncached and make up the remaining credits with cache_creation
//     tokens: cc = round((credits - max) / cCre). This is the only case where
//     cache_creation is non-zero.
//
// Invariant: input_tokens + cache_read_input_tokens always equals the original
// input total T, so downstream context-size accounting covers the full prompt.
// cache_creation_input_tokens, when present, is reported in addition to T as a
// pure billing make-up and never reduces the context base.
func ApplyKiroCreditCacheSplit(model string, detail *usage.Detail) {
	if detail == nil {
		return
	}
	// model is kiroModelID (mapModelToKiro's output) — already the canonical
	// backend ID, so it can be used directly as the rate-table key.
	pricing, ok := officialBasePrices[model]
	totalInput, output, credits := detail.InputTokens, detail.OutputTokens, detail.Credits

	detail.CacheCreationTokens = 0

	if !ok {
		log.Warnf("kiro: model %q absent from credit rate table, using fallback Opus pricing", model)
		pricing = fallbackPricing
	}
	if credits <= 0 || totalInput <= 0 {
		return
	}

	// Per-token credit rates for each bucket (credits per token).
	cIn := (pricing.input / 1e6) / creditToUSD
	cRead := (pricing.cacheRead / 1e6) / creditToUSD
	cCre := (pricing.cacheCreation / 1e6) / creditToUSD
	cOut := (pricing.output / 1e6) / creditToUSD
	if cIn <= cRead {
		// Degenerate pricing (no spread between uncached and cache_read); cannot
		// back-solve a meaningful split. Leave the upstream input total untouched
		// (no cache split), matching the credits<=0 / totalInput<=0 early returns.
		return
	}

	outputCredits := cOut * float64(output)
	maxNoCacheCreation := cIn*float64(totalInput) + outputCredits

	if credits > maxNoCacheCreation && cCre > 0 {
		// Even billing all input as uncached cannot reach credits; make up the
		// shortfall with cache_creation tokens.
		cacheCreation := int64((credits-maxNoCacheCreation)/cCre + 0.5)
		cacheCreation = max(cacheCreation, 0)
		detail.InputTokens = totalInput
		detail.CacheReadTokens = 0
		detail.CachedTokens = 0
		detail.CacheCreationTokens = cacheCreation
		detail.TotalTokens = totalInput + output
		return
	}

	// Back-solve cache_read R from credits = cIn*(T-R) + cRead*R + cOut*O.
	solvedCacheRead := int64((maxNoCacheCreation - credits) / (cIn - cRead))
	uncachedInput, cacheRead := fitKiroCacheSplit(totalInput, solvedCacheRead)

	detail.CacheReadTokens = cacheRead
	detail.CachedTokens = cacheRead
	detail.InputTokens = uncachedInput
	// input_tokens + cache_read_input_tokens stays equal to the original input
	// total, so context-size accounting is unchanged. Restate the token count for
	// callers that pass an unset total.
	detail.TotalTokens = totalInput + output
}

// fitKiroCacheSplit distributes the back-solved cache_read across the input
// buckets while guaranteeing input_tokens + cache_read_input_tokens == totalInput.
// The solved cache_read is clamped to [0, totalInput]; any overflow beyond
// totalInput is discarded (cache_creation stays 0) rather than billed as the
// expensive cache-creation bucket.
func fitKiroCacheSplit(totalInput, solvedCacheRead int64) (uncachedInput, cacheRead int64) {
	switch {
	case solvedCacheRead <= 0:
		uncachedInput, cacheRead = totalInput, 0
	case solvedCacheRead >= totalInput:
		// Clamp to T: fill cache_read to the full input total, discard overflow.
		uncachedInput, cacheRead = 0, totalInput
	default:
		uncachedInput, cacheRead = totalInput-solvedCacheRead, solvedCacheRead
	}

	// Guarantee at least one uncached input token so callers never report
	// input_tokens <= 0; borrow it from cache_read to keep input + cache_read
	// equal to totalInput. totalInput >= 1 is ensured by ApplyKiroCreditCacheSplit.
	if uncachedInput < 1 {
		cacheRead--
		uncachedInput = 1
	}
	return uncachedInput, cacheRead
}
