package gemini

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiRequestToGeminiCLI_HappyPath(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"contents": [
			{"role": "user", "parts": [{"text": "hello"}]}
		]
	}`)

	out := ConvertGeminiRequestToGeminiCLI("gemini-3-flash", input, false)
	if got := gjson.GetBytes(out, "project").String(); got != "" {
		t.Fatalf("project = %q, want empty. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "model").String(); got != "gemini-3-flash" {
		t.Fatalf("model = %q, want gemini-3-flash. Output: %s", got, out)
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

func TestConvertGeminiRequestToGeminiCLI_Tools(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
		"tools": [
			{
				"function_declarations": [
					{
						"name": "read",
						"parameters": {"type": "object", "properties": {"path": {"type": "string"}}}
					}
				]
			}
		]
	}`)

	out := ConvertGeminiRequestToGeminiCLI("gemini-3-flash", input, false)
	if got := gjson.GetBytes(out, "request.tools.0.function_declarations.0.name").String(); got != "read" {
		t.Fatalf("tool name = %q, want read. Output: %s", got, out)
	}
	if !gjson.GetBytes(out, "request.tools.0.function_declarations.0.parametersJsonSchema").Exists() {
		t.Fatalf("parametersJsonSchema missing. Output: %s", out)
	}
}

func TestConvertGeminiRequestToGeminiCLI_ToolCallAndResponse(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"contents": [
			{"role": "model", "parts": [{"functionCall": {"name": "read", "args": {"path": "/tmp"}, "id": "call_1"}}]},
			{"role": "user", "parts": [
				{"functionResponse": {"name": "read", "response": {"result": "ok"}, "id": "call_1"}}
			]}
		]
	}`)

	out := ConvertGeminiRequestToGeminiCLI("gemini-3-flash", input, false)
	contents := gjson.GetBytes(out, "request.contents").Array()
	if len(contents) != 2 {
		t.Fatalf("contents length = %d, want 2. Output: %s", len(contents), out)
	}
	if got := contents[1].Get("role").String(); got != "user" {
		t.Fatalf("response content role = %q, want user. Output: %s", got, out)
	}
	if got := contents[1].Get("parts.0.functionResponse.name").String(); got != "read" {
		t.Fatalf("functionResponse name = %q, want read. Output: %s", got, out)
	}
	if got := contents[1].Get("parts.0.functionResponse.id").String(); got != "call_1" {
		t.Fatalf("functionResponse id = %q, want call_1. Output: %s", got, out)
	}
}

func TestConvertGeminiRequestToGeminiCLI_PreservesSiblingToolImage(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"contents": [
			{"role": "model", "parts": [{"functionCall": {"name": "read", "args": {"path": "/tmp"}, "id": "call_1"}}]},
			{"role": "user", "parts": [
				{"functionResponse": {"name": "read", "response": {"result": "Read image file [image/png]"}, "id": "call_1"}},
				{"inline_data": {"mime_type": "image/png", "data": "QUJD"}}
			]}
		]
	}`)

	out := ConvertGeminiRequestToGeminiCLI("gemini-3-flash", input, false)
	contents := gjson.GetBytes(out, "request.contents").Array()
	if len(contents) != 2 {
		t.Fatalf("contents length = %d, want 2. Output: %s", len(contents), out)
	}
	funcResp := contents[1].Get("parts.0.functionResponse")
	if !funcResp.Exists() {
		t.Fatalf("functionResponse missing. Output: %s", out)
	}
	if got := funcResp.Get("id").String(); got != "call_1" {
		t.Fatalf("id = %q, want call_1. Output: %s", got, out)
	}
	inlineData := funcResp.Get("parts.0.inlineData")
	if !inlineData.Exists() {
		t.Fatalf("functionResponse.parts.0.inlineData missing. Output: %s", out)
	}
	if got := inlineData.Get("mimeType").String(); got != "image/png" {
		t.Fatalf("mimeType = %q, want image/png. Output: %s", got, out)
	}
	if got := inlineData.Get("data").String(); got != "QUJD" {
		t.Fatalf("data = %q, want QUJD. Output: %s", got, out)
	}
	if contents[1].Get("parts.1.inline_data").Exists() || contents[1].Get("parts.1.inlineData").Exists() {
		t.Fatalf("sibling inline data should be absorbed into functionResponse.parts. Output: %s", out)
	}
}

func TestConvertGeminiCliResponseToGemini_Stream(t *testing.T) {
	ctx := context.WithValue(context.Background(), "alt", "")
	resp := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"totalTokenCount":5}}}`)
	out := ConvertGeminiCliResponseToGemini(ctx, "gemini-3-flash", nil, nil, resp, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 output, got %d", len(out))
	}
	if got := gjson.GetBytes(out[0], "candidates.0.content.parts.0.text").String(); got != "Hi" {
		t.Fatalf("text = %q, want Hi. Output: %s", got, out[0])
	}
}

func TestConvertGeminiCliResponseToGemini_NonStream(t *testing.T) {
	resp := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"totalTokenCount":5}}}`)
	out := ConvertGeminiCliResponseToGeminiNonStream(context.Background(), "gemini-3-flash", nil, nil, resp, nil)
	if got := gjson.GetBytes(out, "candidates.0.content.parts.0.text").String(); got != "Hi" {
		t.Fatalf("text = %q, want Hi. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "usageMetadata.totalTokenCount").Int(); got != 5 {
		t.Fatalf("totalTokenCount = %d, want 5. Output: %s", got, out)
	}
}
