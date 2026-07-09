package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
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
	if parsed.ToolCalls[0].Function.Arguments[0] != '"' {
		t.Errorf("arguments should be a JSON string, got %s", string(parsed.ToolCalls[0].Function.Arguments))
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

func TestParseComposerToolCalls(t *testing.T) {
	t.Run("no markers", func(t *testing.T) {
		got := parseComposerToolCalls("Hello")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("basic", func(t *testing.T) {
		got := parseComposerToolCalls("<|tool_calls_begin|>[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"/tmp\\\"}\"}}]<|tool_calls_end|>")
		if len(got) != 1 || got[0].Id != "call_1" {
			t.Errorf("parseComposerToolCalls() = %v, want 1 tool_call with id=call_1", got)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		got := parseComposerToolCalls("<|tool_calls_begin|>[{\"id\":\"a\",\"type\":\"function\",\"function\":{\"name\":\"read\"}},{\"id\":\"b\",\"type\":\"function\",\"function\":{\"name\":\"write\"}}]<|tool_calls_end|>")
		if len(got) != 2 || got[0].Id != "a" || got[1].Id != "b" {
			t.Errorf("parseComposerToolCalls() = %v, want 2 tool_calls", got)
		}
	})

	t.Run("partial marker", func(t *testing.T) {
		got := parseComposerToolCalls("no marker here")
		if got != nil {
			t.Errorf("expected nil for no marker, got %v", got)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		got := parseComposerToolCalls("<|tool_calls_begin|>{bad]json<|tool_calls_end|>")
		if got != nil {
			t.Errorf("expected nil for malformed JSON, got %v", got)
		}
	})

	t.Run("empty block", func(t *testing.T) {
		got := parseComposerToolCalls("<|tool_calls_begin|>[]<|tool_calls_end|>")
		if len(got) != 0 {
			t.Errorf("expected empty slice, got %v", got)
		}
	})
}

func TestExecuteNonStreamingResponseFormat(t *testing.T) {
	payload, err := helps.BuildChatCompletion("chatcmpl-test", 1234567890, "cursor/test-model", "hello world", "stop", 1, 2, 3)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}

	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("failed to unmarshal completion response: %v", err)
	}

	if resp.ID != "chatcmpl-test" {
		t.Errorf("id = %q, want chatcmpl-test", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", resp.Object)
	}
	if resp.Created != 1234567890 {
		t.Errorf("created = %d, want 1234567890", resp.Created)
	}
	if resp.Model != "cursor/test-model" {
		t.Errorf("model = %q, want cursor/test-model", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("len(choices) = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Index != 0 {
		t.Errorf("choices[0].index = %d, want 0", resp.Choices[0].Index)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("choices[0].message.role = %q, want assistant", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].Message.Content != "hello world" {
		t.Errorf("choices[0].message.content = %q, want hello world", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("choices[0].finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 1 {
		t.Errorf("usage.prompt_tokens = %d, want 1", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 2 {
		t.Errorf("usage.completion_tokens = %d, want 2", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 3 {
		t.Errorf("usage.total_tokens = %d, want 3", resp.Usage.TotalTokens)
	}
}
