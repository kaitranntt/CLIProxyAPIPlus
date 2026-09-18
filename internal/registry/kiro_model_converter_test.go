package registry

import "testing"

func TestKiroContextLengthForModel(t *testing.T) {
	cases := []struct {
		name    string
		modelID string
		want    int
	}{
		{
			name:    "gpt-5.6 keeps its 272k window",
			modelID: "kiro-gpt-5-6-sol",
			want:    272000,
		},
		{
			name:    "opus 5 keeps the large window",
			modelID: "kiro-claude-opus-5",
			want:    800000,
		},
		{
			name:    "claude 4.x family uses the 200k window",
			modelID: "kiro-claude-haiku-4-5",
			want:    200000,
		},
		{
			name:    "prefix is optional",
			modelID: "claude-opus-5",
			want:    800000,
		},
		{
			name:    "dots normalize to hyphens",
			modelID: "claude-opus-4.8",
			want:    800000,
		},
		{
			name:    "agentic variants resolve to the base model",
			modelID: "kiro-claude-opus-5-agentic",
			want:    800000,
		},
		{
			name:    "suffixed variants fall back to the longest prefix match",
			modelID: "kiro-claude-haiku-4-5-thinking",
			want:    200000,
		},
		{
			// claude-sonnet-4 (200K) and claude-sonnet-4-6 (800K) both prefix
			// this ID, so a shortest-match lookup would report the wrong window.
			name:    "longest prefix wins over a shorter one",
			modelID: "kiro-claude-sonnet-4-6-20260101",
			want:    800000,
		},
		{
			name:    "unknown models use the default",
			modelID: "kiro-some-future-model",
			want:    DefaultKiroContextLength,
		},
		{
			name:    "empty model uses the default",
			modelID: "",
			want:    DefaultKiroContextLength,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := KiroContextLengthForModel(tc.modelID); got != tc.want {
				t.Fatalf("KiroContextLengthForModel(%q) = %d, want %d", tc.modelID, got, tc.want)
			}
		})
	}
}

// Kiro's ListAvailableModels advertises the raw model ceiling (gpt-5.6 is
// listed at 1M) while billing against a smaller effective window, and
// input_tokens is reconstructed from KiroContextLengthForModel, so an upstream
// value may only lower the local one.
func TestKiroContextLengthForAPIModel(t *testing.T) {
	cases := []struct {
		name              string
		modelID           string
		apiMaxInputTokens int
		want              int
	}{
		{
			name:              "upstream 1M does not raise the measured window",
			modelID:           "kiro-gpt-5-6-sol",
			apiMaxInputTokens: 1000000,
			want:              272000,
		},
		{
			name:              "a smaller upstream window wins",
			modelID:           "kiro-gpt-5-6-sol",
			apiMaxInputTokens: 128000,
			want:              128000,
		},
		{
			name:              "missing upstream value falls back to the local table",
			modelID:           "kiro-gpt-5-6-luna",
			apiMaxInputTokens: 0,
			want:              272000,
		},
		{
			name:              "unknown model with no upstream value uses the default",
			modelID:           "kiro-some-future-model",
			apiMaxInputTokens: 0,
			want:              DefaultKiroContextLength,
		},
		{
			name:              "unknown model is capped by the default, not raised",
			modelID:           "kiro-some-future-model",
			apiMaxInputTokens: 1000000,
			want:              DefaultKiroContextLength,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := KiroContextLengthForAPIModel(tc.modelID, tc.apiMaxInputTokens); got != tc.want {
				t.Fatalf("KiroContextLengthForAPIModel(%q, %d) = %d, want %d", tc.modelID, tc.apiMaxInputTokens, got, tc.want)
			}
		})
	}
}

// ConvertKiroAPIModels must apply the same clamp: the upstream catalog reaches
// ModelInfo through this path on every dynamic discovery cycle.
func TestConvertKiroAPIModelsClampsUpstreamWindow(t *testing.T) {
	models := ConvertKiroAPIModels([]*KiroAPIModel{
		{ModelID: "gpt-5.6-sol", ModelName: "GPT-5.6 Sol", MaxInputTokens: 1000000},
	})
	if len(models) != 1 {
		t.Fatalf("ConvertKiroAPIModels() returned %d models, want 1", len(models))
	}
	if got := models[0].ContextLength; got != 272000 {
		t.Fatalf("ContextLength = %d, want 272000", got)
	}
}

// The reconstruction of input_tokens divides contextUsagePercentage by these
// windows, so a mismatch silently mis-reports usage rather than failing loudly.
func TestGetKiroModelsAdvertisePerModelContextLength(t *testing.T) {
	want := map[string]int{
		"kiro-gpt-5-6-sol":      272000,
		"kiro-gpt-5-6-luna":     272000,
		"kiro-gpt-5-6-terra":    272000,
		"kiro-claude-haiku-4-5": 200000,
		"kiro-claude-opus-4-8":  800000,
	}

	models := GetKiroModels()
	if len(models) == 0 {
		t.Fatal("GetKiroModels() returned no models")
	}

	seen := make(map[string]int, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		seen[model.ID] = model.ContextLength
	}

	for id, expected := range want {
		got, ok := seen[id]
		if !ok {
			t.Fatalf("GetKiroModels() is missing %q", id)
		}
		if got != expected {
			t.Fatalf("%s ContextLength = %d, want %d", id, got, expected)
		}
	}
}
