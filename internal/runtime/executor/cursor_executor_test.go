package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
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

func TestExtractToolCallsText(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		expected string
	}{
		{
			name:     "single function call",
			payload:  `[{"id":"call_abc123","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]`,
			expected: "[tool_call call_abc123: get_weather({\"city\":\"Paris\"})]",
		},
		{
			name:     "multiple function calls",
			payload:  `[{"id":"call_1","type":"function","function":{"name":"a","arguments":"{}"}},{"id":"call_2","type":"function","function":{"name":"b","arguments":"{\"x\":1}"}}]`,
			expected: "[tool_call call_1: a({})]\n[tool_call call_2: b({\"x\":1})]",
		},
		{
			name:     "missing id falls back",
			payload:  `[{"type":"function","function":{"name":"no_id","arguments":"{}"}}]`,
			expected: "[tool_call: no_id({})]",
		},
		{
			name:     "non-function type is skipped",
			payload:  `[{"id":"call_x","type":"retrieval","function":{"name":"retrieve","arguments":"{}"}}]`,
			expected: "",
		},
		{
			name:     "malformed tool_calls returns empty",
			payload:  `{}`,
			expected: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractToolCallsText(gjson.Parse(tc.payload))
			if got != tc.expected {
				t.Errorf("extractToolCallsText() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestBuildToolCallDelta_ArgumentsAreJSONObject(t *testing.T) {
	exec := pendingMcpExec{
		ToolCallId: "call_abc123",
		ToolName:   "get_weather",
		Args:       `{"city":"Paris"}`,
	}
	delta := buildToolCallDelta(0, exec)

	var parsed struct {
		ToolCalls []struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(delta), &parsed); err != nil {
		t.Fatalf("delta is not valid JSON: %v\ndelta: %s", err, delta)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(parsed.ToolCalls))
	}
	got := parsed.ToolCalls[0].Function.Name
	if got != "get_weather" {
		t.Errorf("function name = %q, want get_weather", got)
	}
	if len(parsed.ToolCalls[0].Function.Arguments) == 0 {
		t.Fatalf("arguments field is missing")
	}
	if parsed.ToolCalls[0].Function.Arguments[0] != '{' {
		t.Errorf("arguments should be a JSON object, got %s", string(parsed.ToolCalls[0].Function.Arguments))
	}
}

func TestDecodeMcpArgsToJSON(t *testing.T) {
	tests := []struct {
		name string
		args map[string][]byte
		want string
	}{
		{
			name: "empty args",
			args: map[string][]byte{},
			want: "{}",
		},
		{
			name: "simple string arg",
			args: map[string][]byte{"city": []byte(`"Paris"`)},
			want: `{"city":"Paris"}`,
		},
		{
			name: "nested object arg",
			args: map[string][]byte{"query": []byte(`{"city":"Paris","country":"FR"}`)},
			want: `{"query":{"city":"Paris","country":"FR"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeMcpArgsToJSON(tc.args)
			if got != tc.want {
				t.Errorf("decodeMcpArgsToJSON() = %q, want %q", got, tc.want)
			}
		})
	}
}
