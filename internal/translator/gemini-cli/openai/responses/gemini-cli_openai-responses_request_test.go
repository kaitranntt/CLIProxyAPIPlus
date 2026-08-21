package responses

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToGeminiCLI_HappyPath(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"input": [
			{"role": "user", "content": [{"type": "input_text", "text": "hello"}]}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToGeminiCLI("gemini-3-flash", input, false)
	if !gjson.GetBytes(out, "request.contents").Exists() {
		t.Fatalf("request.contents missing. Output: %s", out)
	}
	contents := gjson.GetBytes(out, "request.contents").Array()
	if len(contents) != 1 {
		t.Fatalf("contents length = %d, want 1. Output: %s", len(contents), out)
	}
	if got := contents[0].Get("role").String(); got != "user" {
		t.Fatalf("content role = %q, want user. Output: %s", got, out)
	}
	if got := contents[0].Get("parts.0.text").String(); got != "hello" {
		t.Fatalf("content text = %q, want hello. Output: %s", got, out)
	}
	if !gjson.GetBytes(out, "request.safetySettings").Exists() {
		t.Fatalf("safetySettings missing. Output: %s", out)
	}
}

func TestConvertOpenAIResponsesRequestToGeminiCLI_ToolCallAndOutput(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"input": [
			{"role": "user", "content": [{"type": "input_text", "text": "read"}]},
			{"type": "function_call", "call_id": "call_1", "name": "read", "arguments": "{\"path\":\"/tmp\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "ok"}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToGeminiCLI("gemini-3-flash", input, false)
	contents := gjson.GetBytes(out, "request.contents").Array()
	if len(contents) != 3 {
		t.Fatalf("contents length = %d, want 3. Output: %s", len(contents), out)
	}
	if got := contents[1].Get("role").String(); got != "model" {
		t.Fatalf("function call content role = %q, want model. Output: %s", got, out)
	}
	if got := contents[1].Get("parts.0.functionCall.name").String(); got != "read" {
		t.Fatalf("functionCall name = %q, want read. Output: %s", got, out)
	}
	if got := contents[2].Get("role").String(); got != "user" {
		t.Fatalf("function response content role = %q, want user. Output: %s", got, out)
	}
	if got := contents[2].Get("parts.0.functionResponse.name").String(); got != "read" {
		t.Fatalf("functionResponse name = %q, want read. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIResponseToOpenAIResponses_NonStream(t *testing.T) {
	resp := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}}`)
	out := ConvertGeminiCLIResponseToOpenAIResponsesNonStream(context.Background(), "gemini-3-flash", nil, nil, resp, nil)
	if got := gjson.GetBytes(out, "status").String(); got != "completed" {
		t.Fatalf("status = %q, want completed. Output: %s", got, out)
	}
	output := gjson.GetBytes(out, "output").Array()
	if len(output) == 0 {
		t.Fatalf("expected non-empty output. Output: %s", out)
	}
	if got := output[0].Get("content.0.text").String(); got != "Hi" {
		t.Fatalf("output text = %q, want Hi. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "usage.total_tokens").Int(); got != 3 {
		t.Fatalf("total_tokens = %d, want 3. Output: %s", got, out)
	}
}

func TestConvertGeminiCLIResponseToOpenAIResponses_Stream(t *testing.T) {
	chunk := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"totalTokenCount":3}}}`)
	var param any
	out := ConvertGeminiCLIResponseToOpenAIResponses(context.Background(), "gemini-3-flash", nil, nil, chunk, &param)
	if len(out) == 0 {
		t.Fatalf("expected non-empty stream output")
	}
	if got := gjson.GetBytes(out[0], "type").String(); got == "" {
		t.Fatalf("event type missing. Output: %s", out[0])
	}
}
