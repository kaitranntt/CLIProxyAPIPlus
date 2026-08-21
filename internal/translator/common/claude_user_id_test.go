package common

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"
)

func TestDeriveClaudeUserID_SameConversationIsStable(t *testing.T) {
	raw := []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`)
	first := DeriveClaudeUserID(raw)
	second := DeriveClaudeUserID(raw)
	if first == "" {
		t.Fatal("expected non-empty user_id")
	}
	if first != second {
		t.Fatalf("same conversation produced different user_id: %q vs %q", first, second)
	}
}

func TestDeriveClaudeUserID_PreservesCallerSuppliedMetadataUserID(t *testing.T) {
	raw := []byte(`{"model":"claude-test","metadata":{"user_id":"caller-123"},"messages":[{"role":"user","content":"hello"}]}`)
	if got := DeriveClaudeUserID(raw); got != "caller-123" {
		t.Fatalf("caller-supplied metadata.user_id not preserved, got %q", got)
	}
}

func TestDeriveClaudeUserID_PreservesOpenAIUserField(t *testing.T) {
	raw := []byte(`{"model":"claude-test","user":"openai-user-456","messages":[{"role":"user","content":"hello"}]}`)
	if got := DeriveClaudeUserID(raw); got != "openai-user-456" {
		t.Fatalf("caller-supplied user not preserved, got %q", got)
	}
}

func TestDeriveClaudeUserID_DifferentSessionsAreDifferent(t *testing.T) {
	a := []byte(`{"model":"claude-test","prompt_cache_key":"session-a","messages":[{"role":"user","content":"hello"}]}`)
	b := []byte(`{"model":"claude-test","prompt_cache_key":"session-b","messages":[{"role":"user","content":"hello"}]}`)
	idA := DeriveClaudeUserID(a)
	idB := DeriveClaudeUserID(b)
	if idA == idB {
		t.Fatalf("different prompt_cache_key produced same user_id: %q", idA)
	}
}

func TestDeriveClaudeUserID_TurnGrowthKeepsSameUserID(t *testing.T) {
	first := []byte(`{"model":"claude-test","prompt_cache_key":"session-1","messages":[{"role":"user","content":"hello"}]}`)
	second := []byte(`{"model":"claude-test","prompt_cache_key":"session-1","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"follow up"}]}`)
	idFirst := DeriveClaudeUserID(first)
	idSecond := DeriveClaudeUserID(second)
	if idFirst != idSecond {
		t.Fatalf("conversation turn growth changed user_id: %q vs %q", idFirst, idSecond)
	}
}

func TestDeriveClaudeUserID_FirstMessageFallback(t *testing.T) {
	raw := []byte(`{"model":"claude-test","messages":[{"role":"user","content":"stable message"}]}`)
	id1 := DeriveClaudeUserID(raw)
	id2 := DeriveClaudeUserID(raw)
	if id1 != id2 {
		t.Fatalf("same first message produced different user_id: %q vs %q", id1, id2)
	}
}

func TestDeriveClaudeUserID_ResponsesInput(t *testing.T) {
	raw := []byte(`{"model":"claude-test","prompt_cache_key":"resp-session","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	id1 := DeriveClaudeUserID(raw)
	id2 := DeriveClaudeUserID(raw)
	if id1 != id2 {
		t.Fatalf("same responses input produced different user_id: %q vs %q", id1, id2)
	}
}

func TestDeriveClaudeUserID_GeminiContents(t *testing.T) {
	raw := []byte(`{"model":"claude-test","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	id1 := DeriveClaudeUserID(raw)
	id2 := DeriveClaudeUserID(raw)
	if id1 != id2 {
		t.Fatalf("same gemini contents produced different user_id: %q vs %q", id1, id2)
	}
}

func TestConvertOpenAIRequestToClaude_DeterministicMetadataUserID(t *testing.T) {
	// This lives in common because the translator packages cannot import each other.
	raw := []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`)
	out1 := DeriveClaudeUserID(raw)
	out2 := DeriveClaudeUserID(raw)
	if !bytes.Equal([]byte(out1), []byte(out2)) {
		t.Fatalf("byte-identical user_id expected, got %q vs %q", out1, out2)
	}
	if gjson.GetBytes(raw, "metadata.user_id").Exists() {
		t.Fatal("input should not have metadata.user_id")
	}
}
