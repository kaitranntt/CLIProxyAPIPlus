package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToGeminiWithCompatReplaysForeignSignatureAsBypass(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"opaque-claude-id"}]}]}`)

	withCompat := ConvertClaudeRequestToGeminiWithCompat("gemini-2.5-pro", payload, false)
	part := gjson.GetBytes(withCompat, "contents.0.parts.0")
	if !part.Get("thought").Bool() || part.Get("text").String() != "reason" {
		t.Fatalf("compat translation missing thought part: %s", withCompat)
	}
	if part.Get("thoughtSignature").String() != "skip_thought_signature_validator" {
		t.Fatalf("foreign thinking signature should be replaced with Gemini bypass: %s", withCompat)
	}
}

func TestConvertClaudeRequestToGeminiWithCompatPreservesEmptyThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":""}]}]}`)

	withoutCompat := ConvertClaudeRequestToGemini("deepseek-v4", payload, false)
	if gjson.GetBytes(withoutCompat, "contents.0.parts.#").Int() != 0 {
		t.Fatalf("default translation preserved thinking: %s", withoutCompat)
	}

	withCompat := ConvertClaudeRequestToGeminiWithCompat("deepseek-v4", payload, false)
	part := gjson.GetBytes(withCompat, "contents.0.parts.0")
	if !part.Get("thought").Bool() || part.Get("text").String() != "reason" {
		t.Fatalf("compat translation missing thought part: %s", withCompat)
	}
	if !part.Get("thoughtSignature").Exists() || part.Get("thoughtSignature").String() != "" {
		t.Fatalf("compat translation did not preserve empty signature: %s", withCompat)
	}
}
