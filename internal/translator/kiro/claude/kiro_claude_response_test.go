package claude

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

func TestBuildClaudeResponseIncludesCacheUsage(t *testing.T) {
	response := BuildClaudeResponse("ok", nil, "kiro-claude", usage.Detail{
		InputTokens:         13,
		OutputTokens:        4,
		CacheReadTokens:     22000,
		CacheCreationTokens: 31,
	}, "end_turn")

	if got := gjson.GetBytes(response, "usage.input_tokens").Int(); got != 13 {
		t.Fatalf("input_tokens = %d, want %d", got, 13)
	}
	if got := gjson.GetBytes(response, "usage.output_tokens").Int(); got != 4 {
		t.Fatalf("output_tokens = %d, want %d", got, 4)
	}
	if got := gjson.GetBytes(response, "usage.cache_read_input_tokens").Int(); got != 22000 {
		t.Fatalf("cache_read_input_tokens = %d, want %d", got, 22000)
	}
	if got := gjson.GetBytes(response, "usage.cache_creation_input_tokens").Int(); got != 31 {
		t.Fatalf("cache_creation_input_tokens = %d, want %d", got, 31)
	}
}

func TestBuildClaudeMessageDeltaEventIncludesCacheUsage(t *testing.T) {
	event := BuildClaudeMessageDeltaEvent("end_turn", usage.Detail{
		InputTokens:         13,
		OutputTokens:        4,
		CacheReadTokens:     22000,
		CacheCreationTokens: 31,
	})
	data := []byte(strings.TrimSpace(strings.SplitN(string(event), "\ndata:", 2)[1]))

	if got := gjson.GetBytes(data, "usage.input_tokens").Int(); got != 13 {
		t.Fatalf("input_tokens = %d, want %d", got, 13)
	}
	if got := gjson.GetBytes(data, "usage.output_tokens").Int(); got != 4 {
		t.Fatalf("output_tokens = %d, want %d", got, 4)
	}
	if got := gjson.GetBytes(data, "usage.cache_read_input_tokens").Int(); got != 22000 {
		t.Fatalf("cache_read_input_tokens = %d, want %d", got, 22000)
	}
	if got := gjson.GetBytes(data, "usage.cache_creation_input_tokens").Int(); got != 31 {
		t.Fatalf("cache_creation_input_tokens = %d, want %d", got, 31)
	}
}
