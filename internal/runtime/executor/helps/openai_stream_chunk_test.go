package helps

import (
	"encoding/json"
	"testing"
)

func TestBuildToolCallStartDelta(t *testing.T) {
	b, err := BuildToolCallStartDelta(0, "call_abc", "get_weather")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	toolCalls, ok := got["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %v", toolCalls)
	}

	tc := toolCalls[0].(map[string]interface{})
	if tc["index"].(float64) != 0 {
		t.Errorf("index = %v, want 0", tc["index"])
	}
	if tc["id"] != "call_abc" {
		t.Errorf("id = %v, want call_abc", tc["id"])
	}
	if tc["type"] != "function" {
		t.Errorf("type = %v, want function", tc["type"])
	}

	fn := tc["function"].(map[string]interface{})
	if fn["name"] != "get_weather" {
		t.Errorf("name = %v, want get_weather", fn["name"])
	}
	if fn["arguments"] != "" {
		t.Errorf("arguments = %v, want empty string", fn["arguments"])
	}
}

func TestBuildToolCallArgumentsDelta(t *testing.T) {
	b, err := BuildToolCallArgumentsDelta(0, `{"city":"Paris"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	toolCalls := got["tool_calls"].([]interface{})
	fn := toolCalls[0].(map[string]interface{})["function"].(map[string]interface{})
	args := fn["arguments"].(string)
	if args != `{"city":"Paris"}` {
		t.Errorf("arguments = %q, want JSON-encoded object string", args)
	}
}

func TestBuildChatCompletionChunk(t *testing.T) {
	b, err := BuildChatCompletionChunk("chatcmpl-test", 1, "composer-2.5", map[string]interface{}{
		"content": "hello",
	}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if got["object"] != "chat.completion.chunk" {
		t.Errorf("object = %v, want chat.completion.chunk", got["object"])
	}
	if got["model"] != "composer-2.5" {
		t.Errorf("model = %v, want composer-2.5", got["model"])
	}
	if _, ok := got["usage"]; ok {
		t.Errorf("usage field should be omitted when nil")
	}

	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != nil {
		t.Errorf("finish_reason = %v, want nil", choice["finish_reason"])
	}

	delta := choice["delta"].(map[string]interface{})
	if delta["content"] != "hello" {
		t.Errorf("delta.content = %v, want hello", delta["content"])
	}
}

func TestBuildChatCompletion(t *testing.T) {
	b, err := BuildChatCompletion("chatcmpl-test", 1, "composer-2.5", "hi", "stop", 5, 1, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if got["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", got["object"])
	}
	if got["model"] != "composer-2.5" {
		t.Errorf("model = %v, want composer-2.5", got["model"])
	}

	usage := got["usage"].(map[string]interface{})
	if usage["prompt_tokens"].(float64) != 5 {
		t.Errorf("prompt_tokens = %v, want 5", usage["prompt_tokens"])
	}
	if usage["completion_tokens"].(float64) != 1 {
		t.Errorf("completion_tokens = %v, want 1", usage["completion_tokens"])
	}
	if usage["total_tokens"].(float64) != 6 {
		t.Errorf("total_tokens = %v, want 6", usage["total_tokens"])
	}

	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", choice["finish_reason"])
	}

	message := choice["message"].(map[string]interface{})
	if message["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", message["role"])
	}
	if message["content"] != "hi" {
		t.Errorf("content = %v, want hi", message["content"])
	}
}
