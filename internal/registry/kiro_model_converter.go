// Package registry provides Kiro model conversion utilities.
// This file handles converting dynamic Kiro API model lists to the internal ModelInfo format,
// and merging with static metadata for thinking support and other capabilities.
package registry

import (
	"strings"
	"time"

	kirocommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/common"
)

// KiroAPIModel represents a model from Kiro API response.
// This is a local copy to avoid import cycles with the kiro package.
// The structure mirrors kiro.KiroModel for easy data conversion.
type KiroAPIModel struct {
	// ModelID is the unique identifier for the model (e.g., "claude-sonnet-4.5")
	ModelID string
	// ModelName is the human-readable name
	ModelName string
	// Description is the model description
	Description string
	// RateMultiplier is the credit multiplier for this model
	RateMultiplier float64
	// RateUnit is the unit for rate calculation (e.g., "credit")
	RateUnit string
	// MaxInputTokens is the maximum input token limit
	MaxInputTokens int
}

// DefaultKiroThinkingSupport defines the default thinking configuration for Kiro models.
// All Kiro models support thinking with the following budget range.
var DefaultKiroThinkingSupport = &ThinkingSupport{
	Min:            1024,  // Minimum thinking budget tokens
	Max:            32000, // Maximum thinking budget tokens
	ZeroAllowed:    true,  // Allow disabling thinking with 0
	DynamicAllowed: true,  // Allow dynamic thinking budget (-1)
}

// DefaultKiroContextLength is the fallback context window for Kiro models whose
// real window we have not measured.
//
// Kiro never reports absolute input tokens, only contextUsagePercentage, so this
// value doubles as the denominator used to reconstruct input_tokens. The two
// error directions are not symmetric: advertising a window larger than the real
// one makes clients under-count usage and skip compaction until Kiro rejects the
// turn outright, while advertising a smaller one only compacts early. The
// default therefore stays slightly under the 1M ceiling of the newest models to
// absorb the tokens Kiro counts but we cannot see (injected system prompt, tool
// schemas re-serialized upstream, per-message framing) and to leave room for the
// completion, which shares the same window.
const DefaultKiroContextLength = 800000

// kiroContextLengths maps normalized Kiro model IDs to their context window.
// Keys carry no "kiro-" prefix and no "-agentic" suffix; see
// KiroContextLengthForModel for the lookup rules.
//
// A single global window cannot serve this fleet: the measured values span more
// than 4x, so reusing one constant necessarily breaks every model it does not
// match. Values below were derived by dividing the local tokenizer count by
// contextUsagePercentage, then rounding to the vendor's advertised window.
var kiroContextLengths = map[string]int{
	// Measured ~264K, and the Codex client's built-in catalog independently
	// advertises 272000 for these slugs.
	"gpt-5-6-sol":   272000,
	"gpt-5-6-luna":  272000,
	"gpt-5-6-terra": 272000,

	// Measured ~693K, calibrated against the tokenizer's known undercount.
	"claude-opus-5":     800000,
	"claude-opus-4-6":   800000,
	"claude-opus-4-7":   800000,
	"claude-opus-4-8":   800000,
	"claude-sonnet-4-5": 800000,
	"claude-sonnet-4-6": 800000,

	// Claude 4.x models share the 200K family window: claude-haiku-4-5 measured
	// ~155K (matching that window), and the remainder follow the same family.
	"claude-haiku-4-5": 200000,
	"claude-opus-4-5":  200000,
	"claude-sonnet-4":  200000,
}

// KiroContextLengthForModel returns the context window for a Kiro model.
//
// The model ID reaching callers varies by request path: it may carry the
// "kiro-" prefix or not, use dots instead of hyphens, and carry an "-agentic"
// or thinking-level suffix. Lookup therefore normalizes first, then falls back
// to the longest matching prefix so suffixed variants resolve to their base
// model instead of silently landing on the default.
func KiroContextLengthForModel(modelID string) int {
	key := normalizeKiroContextKey(modelID)
	if key == "" {
		return DefaultKiroContextLength
	}
	if length, ok := kiroContextLengths[key]; ok {
		return length
	}

	bestMatch := ""
	for candidate := range kiroContextLengths {
		if !strings.HasPrefix(key, candidate) {
			continue
		}
		if len(candidate) > len(bestMatch) {
			bestMatch = candidate
		}
	}
	if bestMatch != "" {
		return kiroContextLengths[bestMatch]
	}
	return DefaultKiroContextLength
}

// KiroContextLengthForAPIModel resolves the window to advertise for a model
// whose upstream catalog entry carries its own maxInputTokens.
//
// The upstream number is only trusted when it is smaller than what we resolve
// locally. Kiro advertises the raw model ceiling (gpt-5.6 is listed at 1M) but
// bills the turn against a window that also holds the injected system prompt,
// the re-serialized tool schemas and the completion, none of which we can see.
// Worse, applyKiroContextUsageFallback reconstructs input_tokens by dividing
// contextUsagePercentage by KiroContextLengthForModel, so honoring a larger
// upstream ceiling here would advertise 1M while still counting against 272K —
// clients would skip compaction until Kiro rejects the turn outright. Taking
// the smaller of the two keeps the advertised window and the reconstruction
// denominator in agreement, and still lets upstream lower a window on us.
func KiroContextLengthForAPIModel(modelID string, apiMaxInputTokens int) int {
	length := KiroContextLengthForModel(modelID)
	if apiMaxInputTokens > 0 && apiMaxInputTokens < length {
		return apiMaxInputTokens
	}
	return length
}

// normalizeKiroContextKey reduces a model ID to the form used as a
// kiroContextLengths key.
func normalizeKiroContextKey(modelID string) string {
	key := strings.ToLower(strings.TrimSpace(modelID))
	if key == "" {
		return ""
	}
	key = strings.ReplaceAll(key, ".", "-")
	key = strings.TrimPrefix(key, "kiro-")
	key = strings.TrimSuffix(key, "-agentic")
	return key
}

// DefaultKiroMaxCompletionTokens is the default max completion tokens for Kiro models.
const DefaultKiroMaxCompletionTokens = 64000

// ConvertKiroAPIModels converts Kiro API models to internal ModelInfo format.
// It performs the following transformations:
//   - Normalizes model ID (e.g., claude-sonnet-4.5 → kiro-claude-sonnet-4-5)
//   - Adds default thinking support metadata
//   - Sets default context length and max completion tokens if not provided
//
// Parameters:
//   - kiroModels: List of models from Kiro API response
//
// Returns:
//   - []*ModelInfo: Converted model information list
func ConvertKiroAPIModels(kiroModels []*KiroAPIModel) []*ModelInfo {
	if len(kiroModels) == 0 {
		return nil
	}

	now := time.Now().Unix()
	result := make([]*ModelInfo, 0, len(kiroModels))

	for _, km := range kiroModels {
		// Skip nil models
		if km == nil {
			continue
		}

		// Skip models without valid ID
		if km.ModelID == "" {
			continue
		}

		// Normalize the model ID to kiro-* format
		normalizedID := normalizeKiroModelID(km.ModelID)

		// Create ModelInfo with converted data
		info := &ModelInfo{
			ID:          normalizedID,
			Object:      "model",
			Created:     now,
			OwnedBy:     "aws",
			Type:        "kiro",
			DisplayName: generateKiroDisplayName(km.ModelName, normalizedID),
			Description: km.Description,
			// Local measurements win; the API value only applies when smaller.
			ContextLength:       KiroContextLengthForAPIModel(normalizedID, km.MaxInputTokens),
			MaxCompletionTokens: DefaultKiroMaxCompletionTokens,
			// All Kiro models support thinking
			Thinking: cloneThinkingSupport(DefaultKiroThinkingSupport),
		}

		result = append(result, info)
	}

	return result
}

// GenerateAgenticVariants creates -agentic variants for each model.
// Agentic variants are optimized for coding agents with chunked writes and
// only make sense when Kiro system-prompt injection is enabled — the agentic
// behavior is layered on top of the wrapped system prompt. If injection is
// disabled (the default), variants are not generated and the input list is
// returned unchanged so clients only see models that will actually behave
// differently from their base counterparts.
//
// Parameters:
//   - models: Base models to generate variants for
//
// Returns:
//   - []*ModelInfo: Base models, plus agentic variants when injection is enabled.
func GenerateAgenticVariants(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}

	// Agentic variants are a no-op unless system-prompt injection is on.
	// Without it, the "-agentic" suffix doesn't change backend behavior, so
	// exposing both the base and the variant is just noise.
	if !kirocommon.IsSystemPromptInjectEnabled() {
		// Return a copy so callers don't mutate our input slice.
		result := make([]*ModelInfo, 0, len(models))
		for _, model := range models {
			if model == nil {
				continue
			}
			result = append(result, model)
		}
		return result
	}

	// Pre-allocate result with capacity for both base models and variants
	result := make([]*ModelInfo, 0, len(models)*2)

	for _, model := range models {
		if model == nil {
			continue
		}

		// Add the base model first
		result = append(result, model)

		// Skip if model already has -agentic suffix
		if strings.HasSuffix(model.ID, "-agentic") {
			continue
		}

		// Skip special models that shouldn't have agentic variants
		if model.ID == "kiro-auto" {
			continue
		}

		// Create agentic variant
		agenticModel := &ModelInfo{
			ID:                  model.ID + "-agentic",
			Object:              model.Object,
			Created:             model.Created,
			OwnedBy:             model.OwnedBy,
			Type:                model.Type,
			DisplayName:         model.DisplayName + " (Agentic)",
			Description:         generateAgenticDescription(model.Description),
			ContextLength:       model.ContextLength,
			MaxCompletionTokens: model.MaxCompletionTokens,
			Thinking:            cloneThinkingSupport(model.Thinking),
		}

		result = append(result, agenticModel)
	}

	return result
}

// MergeWithStaticMetadata merges dynamic models with static metadata.
// Static metadata takes priority for any overlapping fields.
// This allows manual overrides for specific models while keeping dynamic discovery.
//
// Parameters:
//   - dynamicModels: Models from Kiro API (converted to ModelInfo)
//   - staticModels: Predefined model metadata (from GetKiroModels())
//
// Returns:
//   - []*ModelInfo: Merged model list with static metadata taking priority
func MergeWithStaticMetadata(dynamicModels, staticModels []*ModelInfo) []*ModelInfo {
	if len(dynamicModels) == 0 && len(staticModels) == 0 {
		return nil
	}

	// Build a map of static models for quick lookup
	staticMap := make(map[string]*ModelInfo, len(staticModels))
	for _, sm := range staticModels {
		if sm != nil && sm.ID != "" {
			staticMap[sm.ID] = sm
		}
	}

	// Build result, preferring static metadata where available
	seenIDs := make(map[string]struct{})
	result := make([]*ModelInfo, 0, len(dynamicModels)+len(staticModels))

	// First, process dynamic models and merge with static if available
	for _, dm := range dynamicModels {
		if dm == nil || dm.ID == "" {
			continue
		}

		// Skip duplicates
		if _, seen := seenIDs[dm.ID]; seen {
			continue
		}
		seenIDs[dm.ID] = struct{}{}

		// Check if static metadata exists for this model
		if sm, exists := staticMap[dm.ID]; exists {
			// Static metadata takes priority - use static model
			result = append(result, sm)
		} else {
			// No static metadata - use dynamic model
			result = append(result, dm)
		}
	}

	// Add any static models not in dynamic list
	for _, sm := range staticModels {
		if sm == nil || sm.ID == "" {
			continue
		}
		if _, seen := seenIDs[sm.ID]; seen {
			continue
		}
		seenIDs[sm.ID] = struct{}{}
		result = append(result, sm)
	}

	return result
}

// normalizeKiroModelID converts Kiro API model IDs to internal format.
// Transformation rules:
//   - Adds "kiro-" prefix if not present
//   - Replaces dots with hyphens (e.g., 4.5 → 4-5)
//   - Handles special cases like "auto" → "kiro-auto"
//
// Examples:
//   - "claude-sonnet-4.5" → "kiro-claude-sonnet-4-5"
//   - "claude-opus-4.5" → "kiro-claude-opus-4-5"
//   - "auto" → "kiro-auto"
//   - "kiro-claude-sonnet-4-5" → "kiro-claude-sonnet-4-5" (unchanged)
func normalizeKiroModelID(modelID string) string {
	if modelID == "" {
		return ""
	}

	// Trim whitespace
	modelID = strings.TrimSpace(modelID)

	// Replace dots with hyphens (e.g., 4.5 → 4-5)
	normalized := strings.ReplaceAll(modelID, ".", "-")

	// Add kiro- prefix if not present
	if !strings.HasPrefix(normalized, "kiro-") {
		normalized = "kiro-" + normalized
	}

	return normalized
}

// generateKiroDisplayName creates a human-readable display name.
// Uses the API-provided model name if available, otherwise generates from ID.
func generateKiroDisplayName(modelName, normalizedID string) string {
	if modelName != "" {
		return "Kiro " + modelName
	}

	// Generate from normalized ID by removing kiro- prefix and formatting
	displayID := strings.TrimPrefix(normalizedID, "kiro-")
	// Capitalize first letter of each word
	words := strings.Split(displayID, "-")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return "Kiro " + strings.Join(words, " ")
}

// generateAgenticDescription creates description for agentic variants.
func generateAgenticDescription(baseDescription string) string {
	if baseDescription == "" {
		return "Optimized for coding agents with chunked writes"
	}
	return baseDescription + " (Agentic mode: chunked writes)"
}

// cloneThinkingSupport creates a deep copy of ThinkingSupport.
// Returns nil if input is nil.
func cloneThinkingSupport(ts *ThinkingSupport) *ThinkingSupport {
	if ts == nil {
		return nil
	}

	clone := &ThinkingSupport{
		Min:            ts.Min,
		Max:            ts.Max,
		ZeroAllowed:    ts.ZeroAllowed,
		DynamicAllowed: ts.DynamicAllowed,
	}

	// Deep copy Levels slice if present
	if len(ts.Levels) > 0 {
		clone.Levels = make([]string, len(ts.Levels))
		copy(clone.Levels, ts.Levels)
	}

	return clone
}
