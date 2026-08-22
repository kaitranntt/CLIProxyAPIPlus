package geminiCLI

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiCLIRequestToClaude_HappyPath(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"request": {
			"contents": [
				{"role": "user", "parts": [{"text": "hello"}]},
				{"role": "model", "parts": [{"text": "hi"}]}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToClaude("claude-sonnet-4-6", input, false)
	if got := gjson.GetBytes(out, "model").String(); got != "claude-sonnet-4-6" {
		t.Fatalf("model = %q, want claude-sonnet-4-6. Output: %s", got, out)
	}
	messages := gjson.GetBytes(out, "messages").Array()
	if len(messages) != 2 {
		t.Fatalf("messages length = %d, want 2. Output: %s", len(messages), out)
	}
	if got := messages[0].Get("role").String(); got != "user" {
		t.Fatalf("first message role = %q, want user. Output: %s", got, out)
	}
	if got := messages[1].Get("role").String(); got != "assistant" {
		t.Fatalf("second message role = %q, want assistant. Output: %s", got, out)
	}
	if got := messages[1].Get("content.0.text").String(); got != "hi" {
		t.Fatalf("second message text = %q, want hi. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIRequestToClaude_SystemInstruction(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"request": {
			"systemInstruction": {"parts": [{"text": "sys"}]},
			"contents": [{"role": "user", "parts": [{"text": "hello"}]}]
		}
	}`)

	out := ConvertGeminiCLIRequestToClaude("claude-sonnet-4-6", input, false)
	messages := gjson.GetBytes(out, "messages").Array()
	if len(messages) < 1 {
		t.Fatalf("expected at least one message, got %d. Output: %s", len(messages), out)
	}
	found := false
	for _, msg := range messages {
		for _, part := range msg.Get("content").Array() {
			if part.Get("text").String() == "sys" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("system instruction text not found in messages. Output: %s", out)
	}
}

func TestConvertGeminiCLIRequestToClaude_ToolCallAndResponse(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"request": {
			"contents": [
				{"role": "model", "parts": [{"functionCall": {"name": "lookup", "args": {"q": "x"}, "id": "call_1"}}]},
				{"role": "user", "parts": [{"functionResponse": {"name": "lookup", "response": {"result": "ok"}, "id": "call_1"}}]}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToClaude("claude-sonnet-4-6", input, false)
	toolCallID := gjson.GetBytes(out, "messages.0.content.0.id").String()
	if toolCallID == "" {
		t.Fatalf("tool call id missing. Output: %s", out)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.name").String(); got != "lookup" {
		t.Fatalf("tool call name = %q, want lookup. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.1.content.0.tool_use_id").String(); got != toolCallID {
		t.Fatalf("tool result id = %q, want %q. Output: %s", got, toolCallID, out)
	}
	if got := gjson.GetBytes(out, "messages.1.content.0.content").String(); got != "ok" {
		t.Fatalf("tool result content = %q, want ok. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIRequestToClaude_Tools(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"request": {
			"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
			"tools": [
				{
					"functionDeclarations": [
						{
							"name": "read",
							"description": "Read file",
							"parameters": {"type": "object", "properties": {"path": {"type": "string"}}}
						}
					]
				}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToClaude("claude-sonnet-4-6", input, false)
	tools := gjson.GetBytes(out, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1. Output: %s", len(tools), out)
	}
	if got := tools[0].Get("name").String(); got != "read" {
		t.Fatalf("tool name = %q, want read. Output: %s", got, out)
	}
	if got := tools[0].Get("input_schema.properties.path.type").String(); got != "string" {
		t.Fatalf("tool schema path type = %q, want string. Output: %s", got, out)
	}
}

func TestConvertClaudeResponseToGeminiCLI_TextAndUsage(t *testing.T) {
	events := []byte(`data:{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
data:{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":5}}
`)

	out := ConvertClaudeResponseToGeminiCLINonStream(context.Background(), "gemini-3-flash", nil, nil, events, nil)
	if got := gjson.GetBytes(out, "response.candidates.0.content.parts.0.text").String(); got != "Hello" {
		t.Fatalf("text = %q, want Hello. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.promptTokenCount").Int(); got != 10 {
		t.Fatalf("promptTokenCount = %d, want 10. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.candidatesTokenCount").Int(); got != 5 {
		t.Fatalf("candidatesTokenCount = %d, want 5. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.totalTokenCount").Int(); got != 15 {
		t.Fatalf("totalTokenCount = %d, want 15. Output: %s", got, out)
	}
}

func TestConvertClaudeResponseToGeminiCLI_ToolCall(t *testing.T) {
	events := []byte(`data:{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"list_dir","id":"tu_1"}}
data:{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\": \"/\"}"}}
data:{"type":"content_block_stop","index":0}
`)

	out := ConvertClaudeResponseToGeminiCLINonStream(context.Background(), "gemini-3-flash", nil, nil, events, nil)
	parts := gjson.GetBytes(out, "response.candidates.0.content.parts").Array()
	if len(parts) != 1 {
		t.Fatalf("parts length = %d, want 1. Output: %s", len(parts), out)
	}
	if got := parts[0].Get("functionCall.name").String(); got != "list_dir" {
		t.Fatalf("functionCall.name = %q, want list_dir. Output: %s", got, out)
	}
	if got := parts[0].Get("functionCall.args.path").String(); got != "/" {
		t.Fatalf("functionCall.args.path = %q, want /. Output: %s", got, out)
	}
	if got := parts[0].Get("functionCall.id").String(); got != "tu_1" {
		t.Fatalf("functionCall.id = %q, want tu_1. Output: %s", got, out)
	}
}
