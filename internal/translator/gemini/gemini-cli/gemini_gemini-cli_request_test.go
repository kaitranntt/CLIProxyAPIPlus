package geminiCLI

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiCLIRequestToGemini_HappyPath(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"request": {
			"contents": [
				{"role": "user", "parts": [{"text": "hello"}]}
			]
		}
	}`)

	out := ConvertGeminiCLIRequestToGemini("gemini-3-flash", input, false)
	if got := gjson.GetBytes(out, "model").String(); got != "gemini-3-flash" {
		t.Fatalf("model = %q, want gemini-3-flash. Output: %s", got, out)
	}
	contents := gjson.GetBytes(out, "contents").Array()
	if len(contents) != 1 {
		t.Fatalf("contents length = %d, want 1. Output: %s", len(contents), out)
	}
	if got := contents[0].Get("role").String(); got != "user" {
		t.Fatalf("content role = %q, want user. Output: %s", got, out)
	}
	if got := contents[0].Get("parts.0.text").String(); got != "hello" {
		t.Fatalf("content text = %q, want hello. Output: %s", got, out)
	}
	if !gjson.GetBytes(out, "safetySettings").Exists() {
		t.Fatalf("safetySettings missing. Output: %s", out)
	}
}

func TestConvertGeminiCLIRequestToGemini_SystemInstruction(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"request": {
			"systemInstruction": {"parts": [{"text": "sys"}]},
			"contents": [{"role": "user", "parts": [{"text": "hello"}]}]
		}
	}`)

	out := ConvertGeminiCLIRequestToGemini("gemini-3-flash", input, false)
	if got := gjson.GetBytes(out, "system_instruction.parts.0.text").String(); got != "sys" {
		t.Fatalf("system_instruction text = %q, want sys. Output: %s", got, out)
	}
	if gjson.GetBytes(out, "systemInstruction").Exists() {
		t.Fatalf("systemInstruction should be renamed. Output: %s", out)
	}
}

func TestConvertGeminiCLIRequestToGemini_Tools(t *testing.T) {
	input := []byte(`{
		"model": "gemini-3-flash",
		"request": {
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
		}
	}`)

	out := ConvertGeminiCLIRequestToGemini("gemini-3-flash", input, false)
	if got := gjson.GetBytes(out, "tools.0.function_declarations.0.name").String(); got != "read" {
		t.Fatalf("tool name = %q, want read. Output: %s", got, out)
	}
	if !gjson.GetBytes(out, "tools.0.function_declarations.0.parametersJsonSchema").Exists() {
		t.Fatalf("parametersJsonSchema missing. Output: %s", out)
	}
}

func TestConvertGeminiResponseToGeminiCLI_Stream(t *testing.T) {
	chunk := []byte(`data:{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`)
	var param any
	out := ConvertGeminiResponseToGeminiCLI(context.Background(), "gemini-3-flash", nil, nil, chunk, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 output, got %d", len(out))
	}
	if got := gjson.GetBytes(out[0], "response.candidates.0.content.parts.0.text").String(); got != "Hi" {
		t.Fatalf("text = %q, want Hi. Output: %s", got, out[0])
	}
	if got := gjson.GetBytes(out[0], "response.usageMetadata.totalTokenCount").Int(); got != 3 {
		t.Fatalf("totalTokenCount = %d, want 3. Output: %s", got, out[0])
	}
}

func TestConvertGeminiResponseToGeminiCLI_NonStream(t *testing.T) {
	resp := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`)
	out := ConvertGeminiResponseToGeminiCLINonStream(context.Background(), "gemini-3-flash", nil, nil, resp, nil)
	if got := gjson.GetBytes(out, "response.candidates.0.content.parts.0.text").String(); got != "Hi" {
		t.Fatalf("text = %q, want Hi. Output: %s", got, out)
	}
	if got := gjson.GetBytes(out, "response.usageMetadata.totalTokenCount").Int(); got != 3 {
		t.Fatalf("totalTokenCount = %d, want 3. Output: %s", got, out)
	}
}
