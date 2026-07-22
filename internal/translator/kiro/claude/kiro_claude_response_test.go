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
