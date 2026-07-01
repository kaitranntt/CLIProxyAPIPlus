package executor

import (
	"strings"
	"testing"
)

func TestParseOpenAIRequest_AssistantToolCalls(t *testing.T) {
	tests := []struct {
		name                string
		payload             string
		wantToolCallInTurns bool
		wantUserText        string
		wantToolResults     int
	}{
		{
			name: "assistant message with tool_calls and tool results preserves tool calls in turn",
			payload: `{
				"model": "cursor/composer-2.5",
				"messages": [
					{"role": "user", "content": "call a tool"},
					{
						"role": "assistant",
						"content": null,
						"tool_calls": [
							{
								"id": "call_abc123",
								"type": "function",
								"function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}
							}
						]
					},
					{"role": "tool", "tool_call_id": "call_abc123", "content": "sunny"}
				]
			}`,
			wantToolCallInTurns: true,
			wantUserText:        "",
			wantToolResults:     1,
		},
		{
			name: "assistant message with content and tool_calls retains both when tool results present",
			payload: `{
				"model": "cursor/composer-2.5",
				"messages": [
					{"role": "user", "content": "call a tool"},
					{
						"role": "assistant",
						"content": "I'll check the weather.",
						"tool_calls": [
							{
								"id": "call_def456",
								"type": "function",
								"function": {"name": "get_weather", "arguments": "{\"city\":\"London\"}"}
							}
						]
					},
					{"role": "tool", "tool_call_id": "call_def456", "content": "rainy"}
				]
			}`,
			wantToolCallInTurns: true,
			wantUserText:        "",
			wantToolResults:     1,
		},
		{
			name: "plain assistant message without tool_calls behaves as before",
			payload: `{
				"model": "cursor/composer-2.5",
				"messages": [
					{"role": "user", "content": "hello"},
					{"role": "assistant", "content": "Hi there."}
				]
			}`,
			wantToolCallInTurns: false,
			wantUserText:        "hello",
			wantToolResults:     0,
		},
		{
			name: "malformed tool_calls entries are skipped",
			payload: `{
				"model": "cursor/composer-2.5",
				"messages": [
					{"role": "user", "content": "call a tool"},
					{
						"role": "assistant",
						"content": null,
						"tool_calls": [
							{"type": "function"}
						]
					},
					{"role": "tool", "tool_call_id": "", "content": "result"}
				]
			}`,
			wantToolCallInTurns: false,
			wantUserText:        "",
			wantToolResults:     1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseOpenAIRequest([]byte(tc.payload))
			if parsed == nil {
				t.Fatal("parseOpenAIRequest returned nil")
			}

			foundToolCall := false
			for _, turn := range parsed.Turns {
				if strings.Contains(turn.AssistantText, "[tool_call") {
					foundToolCall = true
				}
			}

			if tc.wantToolCallInTurns && !foundToolCall {
				t.Errorf("expected assistant turn to contain a tool_call marker, got turns=%+v", parsed.Turns)
			}
			if !tc.wantToolCallInTurns && foundToolCall {
				t.Errorf("did not expect assistant turn to contain a tool_call marker, got turns=%+v", parsed.Turns)
			}
			if parsed.UserText != tc.wantUserText {
				t.Errorf("UserText = %q, want %q", parsed.UserText, tc.wantUserText)
			}
			if len(parsed.ToolResults) != tc.wantToolResults {
				t.Errorf("len(ToolResults) = %d, want %d", len(parsed.ToolResults), tc.wantToolResults)
			}
		})
	}
}
