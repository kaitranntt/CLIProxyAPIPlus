package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestApplyKiroCreditCacheSplit(t *testing.T) {
	// Opus 4.7 rates: cIn=(5/1e6)/0.02=0.000250, cRead=0.000025,
	// cCre=0.0003125, cOut=0.00125 credits/token.
	tests := []struct {
		name                                   string
		model                                  string
		input, output                          int64
		credits                                float64
		wantInput, wantCacheRead, wantCacheCre int64
	}{
		{
			// credits below the min billable base (cRead*T + cOut*O), so cache_read
			// clamps to T (minus 1 borrowed for uncached); cache_creation stays 0.
			name: "opus-4.7 real log", model: "claude-opus-4.7",
			input: 7982, output: 52, credits: 0.1847,
			wantInput: 1, wantCacheRead: 7981, wantCacheCre: 0,
		},
		{
			// Unknown model falls back to Opus pricing; credits below min base, so
			// cache_read clamps to T-1, cache_creation stays 0.
			name: "unknown model uses fallback price", model: "gpt-4o",
			input: 1000, output: 100, credits: 0.05,
			wantInput: 1, wantCacheRead: 999, wantCacheCre: 0,
		},
		{
			// credits within [min,max]: back-solve R = (cIn*T - credits)/(cIn-cRead)
			// = (0.25 - 0.125)/0.000225 = 555. Uncached = T - R = 445.
			name: "cache read within total", model: "claude-opus-4.7",
			input: 1000, output: 0, credits: 0.125,
			wantInput: 445, wantCacheRead: 555, wantCacheCre: 0,
		},
		{
			name: "zero credits no split", model: "claude-opus-4.7",
			input: 1000, output: 100, credits: 0,
			wantInput: 1000, wantCacheRead: 0, wantCacheCre: 0,
		},
		{
			name: "zero input no split", model: "claude-opus-4.7",
			input: 0, output: 100, credits: 0.05,
			wantInput: 0, wantCacheRead: 0, wantCacheCre: 0,
		},
		{
			// credits above max base (cIn*T + cOut*O = 0.25 + 0.0125 = 0.2625):
			// bill all input as uncached and make up the rest with cache_creation.
			// cc = round((1.0 - 0.2625)/0.0003125) = 2360.
			name: "credits above base make up with cache creation", model: "claude-opus-4.7",
			input: 1000, output: 10, credits: 1.0,
			wantInput: 1000, wantCacheRead: 0, wantCacheCre: 2360,
		},
		{
			// Cheap call (credits below min base) => cache_read clamps to T-1 and the
			// overflow is discarded, so cache_creation stays 0.
			name: "credits below base discards excess, no cache creation", model: "claude-opus-4.7",
			input: 1000, output: 0, credits: 0.000001,
			wantInput: 1, wantCacheRead: 999, wantCacheCre: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := usage.Detail{InputTokens: tc.input, OutputTokens: tc.output, Credits: tc.credits}
			ApplyKiroCreditCacheSplit(tc.model, &d)
			if d.InputTokens != tc.wantInput {
				t.Errorf("InputTokens = %d, want %d", d.InputTokens, tc.wantInput)
			}
			if d.CacheReadTokens != tc.wantCacheRead {
				t.Errorf("CacheReadTokens = %d, want %d", d.CacheReadTokens, tc.wantCacheRead)
			}
			if d.CachedTokens != tc.wantCacheRead {
				t.Errorf("CachedTokens = %d, want %d", d.CachedTokens, tc.wantCacheRead)
			}
			if d.CacheCreationTokens != tc.wantCacheCre {
				t.Errorf("CacheCreationTokens = %d, want %d", d.CacheCreationTokens, tc.wantCacheCre)
			}
			// Context-size invariant: uncached input + cache_read always equals
			// the original total input, so context accounting covers the full
			// prompt. cache_creation, when present, is a billing make-up reported
			// on top of T, never carved out of the context base.
			gotContextBase := d.InputTokens + d.CacheReadTokens
			if gotContextBase != tc.input {
				t.Errorf("input+cache_read = %d, want T = %d", gotContextBase, tc.input)
			}
			// Uncached input is never reported as zero or negative when a split occurs.
			if tc.credits > 0 && tc.input > 0 && d.InputTokens < 1 {
				t.Errorf("InputTokens = %d, want >= 1", d.InputTokens)
			}
		})
	}
}

// TestApplyKiroCreditCacheSplitMakeUpReconstructsCredits verifies that when
// credits exceed the all-uncached base, the cache_creation make-up brings the
// official-price billing back to (approximately) the upstream credit value.
func TestApplyKiroCreditCacheSplitMakeUpReconstructsCredits(t *testing.T) {
	const creditToUSD = 20.0 / 1000.0
	// Opus pricing ($/MTok).
	cIn := (5.0 / 1e6) / creditToUSD
	cCre := (6.25 / 1e6) / creditToUSD
	cOut := (25.0 / 1e6) / creditToUSD

	d := usage.Detail{InputTokens: 1000, OutputTokens: 10, Credits: 1.0}
	ApplyKiroCreditCacheSplit("claude-opus-4.7", &d)

	billed := cIn*float64(d.InputTokens) + cCre*float64(d.CacheCreationTokens) + cOut*float64(d.OutputTokens)
	if diff := billed - d.Credits; diff < -cCre || diff > cCre {
		t.Errorf("billed credits = %.6f, want within one cache_creation token of %.6f", billed, d.Credits)
	}
}

func TestApplyKiroCreditCacheSplitNilDetail(t *testing.T) {
	ApplyKiroCreditCacheSplit("claude-opus-4.7", nil) // must not panic
}
