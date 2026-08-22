package geminiCLI

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiCLIRequestToOpenAI_HappyPath(t *testing.T) {
	input := []byte(`{
		"model": "gpt-5.4",
		"request": {
			"contents": [
				{"role": "user", "parts": [{"text": "hello"}]}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToOpenAI("gpt-5.4", input, false)
	if got := gjson.GetBytes(out, "model").String(); got != "gpt-5.4" {
		t.Fatalf("model = %q, want gpt-5.4. Output: %s", got, out)
	}
	messages := gjson.GetBytes(out, "messages").Array()
	if len(messages) != 1 {
		t.Fatalf("messages length = %d, want 1. Output: %s", len(messages), out)
	}
	if got := messages[0].Get("role").String(); got != "user" {
		t.Fatalf("message role = %q, want user. Output: %s", got, out)
	}
	if got := messages[0].Get("content").String(); got != "hello" {
		t.Fatalf("message content = %q, want hello. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIRequestToOpenAI_SystemInstruction(t *testing.T) {
	input := []byte(`{
		"model": "gpt-5.4",
		"request": {
			"systemInstruction": {"parts": [{"text": "sys"}]},
			"contents": [{"role": "user", "parts": [{"text": "hello"}]}]
		}
	}`)

	out := ConvertGeminiCLIRequestToOpenAI("gpt-5.4", input, false)
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "system" {
		t.Fatalf("first message role = %q, want system. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.text").String(); got != "sys" {
		t.Fatalf("system content = %q, want sys. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIRequestToOpenAI_Tools(t *testing.T) {
	input := []byte(`{
		"model": "gpt-5.4",
		"request": {
			"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
			"tools": [
				{
					"functionDeclarations": [
						{
							"name": "read",
							"parameters": {"type": "object", "properties": {"path": {"type": "string"}}}
						}
					]
				}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToOpenAI("gpt-5.4", input, false)
	tools := gjson.GetBytes(out, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1. Output: %s", len(tools), out)
	}
	if got := tools[0].Get("type").String(); got != "function" {
		t.Fatalf("tool type = %q, want function. Output: %s", got, out)
	}
	if got := tools[0].Get("function.name").String(); got != "read" {
		t.Fatalf("tool name = %q, want read. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIRequestToOpenAI_ToolCallAndResponse(t *testing.T) {
	input := []byte(`{
		"model": "gpt-5.4",
		"request": {
			"contents": [
				{"role": "model", "parts": [{"functionCall": {"name": "lookup", "args": {"q": "x"}, "id": "call_1"}}]},
				{"role": "user", "parts": [{"functionResponse": {"name": "lookup", "response": {"result": "ok"}, "id": "call_1"}}]}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToOpenAI("gpt-5.4", input, false)
	toolCallID := gjson.GetBytes(out, "messages.0.tool_calls.0.id").String()
	if toolCallID == "" {
		t.Fatalf("tool call id missing. Output: %s", out)
	}
	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != toolCallID {
		t.Fatalf("tool response id = %q, want %q. Output: %s", got, toolCallID, out)
	}
	content := gjson.GetBytes(out, "messages.1.content").String()
	if !strings.Contains(content, "ok") {
		t.Fatalf("tool response content = %q, want ok. Output: %s", content, out)
	}
}

func TestConvertOpenAIResponseToGeminiCLI_NonStream(t *testing.T) {
	resp := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"Hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	out := ConvertOpenAIResponseToGeminiCLINonStream(context.Background(), "gemini-3-flash", nil, nil, resp, nil)
	if got := gjson.GetBytes(out, "response.candidates.0.content.parts.0.text").String(); got != "Hi" {
		t.Fatalf("text = %q, want Hi. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.promptTokenCount").Int(); got != 1 {
		t.Fatalf("promptTokenCount = %d, want 1. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.candidatesTokenCount").Int(); got != 2 {
		t.Fatalf("candidatesTokenCount = %d, want 2. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.totalTokenCount").Int(); got != 3 {
		t.Fatalf("totalTokenCount = %d, want 3. Output: %s", got, out)
	}
}

func TestConvertOpenAIResponseToGeminiCLI_ToolCall(t *testing.T) {
	resp := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}`)
	out := ConvertOpenAIResponseToGeminiCLINonStream(context.Background(), "gemini-3-flash", nil, nil, resp, nil)
	if got := gjson.GetBytes(out, "response.candidates.0.content.parts.0.functionCall.name").String(); got != "lookup" {
		t.Fatalf("functionCall name = %q, want lookup. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.candidates.0.content.parts.0.functionCall.id").String(); got != "call_1" {
		t.Fatalf("functionCall id = %q, want call_1. Output: %s", got, out)
	}
}
