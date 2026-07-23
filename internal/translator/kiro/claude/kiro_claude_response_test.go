package claude

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestBuildClaudeResponseIncludesCacheUsageFields(t *testing.T) {
	response := BuildClaudeResponse("ok", nil, "claude-haiku-4-5", usage.Detail{
		InputTokens:         290,
		OutputTokens:        1,
		CacheReadTokens:     3822,
		CacheCreationTokens: 17,
	}, "end_turn")

	var payload map[string]interface{}
	if err := json.Unmarshal(response, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	usagePayload, ok := payload["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage payload missing or wrong type: %#v", payload["usage"])
	}
	assertJSONNumber(t, usagePayload, "input_tokens", 290)
	assertJSONNumber(t, usagePayload, "output_tokens", 1)
	assertJSONNumber(t, usagePayload, "cache_read_input_tokens", 3822)
	assertJSONNumber(t, usagePayload, "cache_creation_input_tokens", 17)
}

func TestBuildClaudeResponseIncludesCredits(t *testing.T) {
	response := BuildClaudeResponse("ok", nil, "claude-opus-4-7", usage.Detail{
		InputTokens:  7982,
		OutputTokens: 52,
		Credits:      0.1847,
	}, "end_turn")

	var payload map[string]interface{}
	if err := json.Unmarshal(response, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	usagePayload, ok := payload["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage payload missing or wrong type: %#v", payload["usage"])
	}
	credits, ok := usagePayload["credits"].(float64)
	if !ok {
		t.Fatalf("credits missing or wrong type: %#v", usagePayload["credits"])
	}
	if credits != 0.1847 {
		t.Fatalf("credits = %v, want %v", credits, 0.1847)
	}
}

func TestBuildClaudeResponseOmitsZeroCredits(t *testing.T) {
	response := BuildClaudeResponse("ok", nil, "claude-opus-4-7", usage.Detail{
		InputTokens:  10,
		OutputTokens: 2,
	}, "end_turn")

	var payload map[string]interface{}
	if err := json.Unmarshal(response, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	usagePayload, ok := payload["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage payload missing or wrong type: %#v", payload["usage"])
	}
	if _, exists := usagePayload["credits"]; exists {
		t.Fatalf("credits should be omitted when zero, got: %#v", usagePayload["credits"])
	}
}

func TestBuildClaudeResponseWithThinkingLeadsWithThinkingBlock(t *testing.T) {
	// Kiro streams the reasoning channel after the text, but the non-stream
	// response is fully buffered, so the thinking block must be normalized to
	// the Anthropic convention: thinking first, then text.
	response := BuildClaudeResponseWithThinking(
		"final answer",
		nil,
		"kiro-gpt-5-6-sol",
		usage.Detail{InputTokens: 3, OutputTokens: 7},
		"end_turn",
		"...",
		"sig_upstream_1",
	)

	var payload map[string]interface{}
	if err := json.Unmarshal(response, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	content, ok := payload["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("content blocks = %#v, want 2 blocks", payload["content"])
	}
	first, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("content[0] wrong type: %#v", content[0])
	}
	if got := first["type"]; got != "thinking" {
		t.Fatalf("content[0].type = %v, want thinking", got)
	}
	if got := first["thinking"]; got != "..." {
		t.Fatalf("content[0].thinking = %v, want ...", got)
	}
	if got := first["signature"]; got != "sig_upstream_1" {
		t.Fatalf("content[0].signature = %v, want sig_upstream_1", got)
	}
	second, ok := content[1].(map[string]interface{})
	if !ok {
		t.Fatalf("content[1] wrong type: %#v", content[1])
	}
	if got := second["type"]; got != "text" {
		t.Fatalf("content[1].type = %v, want text", got)
	}
}

func assertJSONNumber(t *testing.T, payload map[string]interface{}, key string, want int64) {
	t.Helper()
	got, ok := payload[key].(float64)
	if !ok {
		t.Fatalf("%s missing or wrong type: %#v", key, payload[key])
	}
	if int64(got) != want {
		t.Fatalf("%s = %d, want %d", key, int64(got), want)
	}
}
