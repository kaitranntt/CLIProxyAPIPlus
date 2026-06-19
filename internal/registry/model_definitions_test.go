package registry

import (
	"strings"
	"testing"
)

func TestWithXAIBuiltinsIncludesVideoPreviewModel(t *testing.T) {
	models := WithXAIBuiltins(nil)

	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == xaiBuiltinVideo15PreviewModelID {
			return
		}
	}

	t.Fatalf("expected xAI builtin model %s", xaiBuiltinVideo15PreviewModelID)
}

func TestGetKiroModelsReturnsStaticFallbackSet(t *testing.T) {
	models := GetKiroModels()
	if len(models) == 0 {
		t.Fatal("expected Kiro static fallback models")
	}

	seen := make(map[string]struct{}, len(models))
	required := map[string]bool{
		"kiro-claude-sonnet-4-6": false,
		"kiro-claude-sonnet-4-5": false,
		"kiro-claude-sonnet-4":   false,
		"kiro-claude-opus-4-7":   false,
		"kiro-claude-opus-4-6":   false,
		"kiro-claude-opus-4-5":   false,
		"kiro-claude-haiku-4-5":  false,
	}

	for _, model := range models {
		if model == nil {
			t.Fatal("expected no nil Kiro models")
		}
		if !strings.HasPrefix(model.ID, "kiro-") {
			t.Fatalf("expected Kiro model ID to use kiro- prefix, got %q", model.ID)
		}
		if model.Type != "kiro" {
			t.Fatalf("model %q type = %q, want kiro", model.ID, model.Type)
		}
		if model.Thinking == nil {
			t.Fatalf("model %q missing Kiro thinking metadata", model.ID)
		}
		if model.Thinking.Min != DefaultKiroThinkingSupport.Min ||
			model.Thinking.Max != DefaultKiroThinkingSupport.Max ||
			model.Thinking.ZeroAllowed != DefaultKiroThinkingSupport.ZeroAllowed ||
			model.Thinking.DynamicAllowed != DefaultKiroThinkingSupport.DynamicAllowed {
			t.Fatalf("model %q thinking = %+v, want %+v", model.ID, model.Thinking, DefaultKiroThinkingSupport)
		}
		if _, exists := seen[model.ID]; exists {
			t.Fatalf("duplicate Kiro model ID %q", model.ID)
		}
		seen[model.ID] = struct{}{}
		if _, ok := required[model.ID]; ok {
			required[model.ID] = true
		}
	}

	for modelID, found := range required {
		if !found {
			t.Fatalf("expected fallback model %q", modelID)
		}
	}
}
