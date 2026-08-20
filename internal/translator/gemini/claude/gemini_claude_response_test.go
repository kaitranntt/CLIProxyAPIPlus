package claude

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiResponseToClaude_SignatureOnlyPartDoesNotOpenEmptyTextBlock(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	thinkingChunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "thinking text", "thought": true}]
			}
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)
	signatureChunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "", "thoughtSignature": "sig-test"}]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"thoughtsTokenCount": 2,
			"totalTokenCount": 12
		},
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	var param any
	ctx := context.Background()
	output := bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, thinkingChunk, &param), nil)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, signatureChunk, &param), nil)...)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)
	outputText := string(output)

	if strings.Contains(outputText, `"content_block":{"type":"text"`) {
		t.Fatalf("signature-only part must not open an empty text block: %s", outputText)
	}
	if strings.Contains(outputText, `"type":"content_block_stop","index":1`) {
		t.Fatalf("signature-only part must not produce a stop for unopened index 1: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"signature_delta"`) || !strings.Contains(outputText, `"signature":"sig-test"`) {
		t.Fatalf("signature-only part must be emitted as a thinking signature delta: %s", outputText)
	}
	if got := strings.Count(outputText, `"type":"content_block_stop","index":0`); got != 1 {
		t.Fatalf("expected exactly one stop for thinking index 0, got %d: %s", got, outputText)
	}
	if !strings.Contains(outputText, `"type":"message_delta"`) || !strings.Contains(outputText, `"output_tokens":2`) {
		t.Fatalf("finish chunk without candidatesTokenCount must still emit final message_delta: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"message_stop"`) {
		t.Fatalf("DONE chunk must still emit message_stop after final events: %s", outputText)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_ThoughtSignature(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "thinking text", "thought": true, "thoughtSignature": "sig-test"},
					{"text": "hello world"}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.0.type").String() != "thinking" {
		t.Fatalf("expected first block to be thinking, got: %s", output)
	}
	if parsed.Get("content.0.thinking").String() != "thinking text" {
		t.Fatalf("expected thinking text, got: %s", output)
	}
	if parsed.Get("content.0.signature").String() != "sig-test" {
		t.Fatalf("expected signature sig-test in thinking block, got: %s", output)
	}
	if parsed.Get("content.1.type").String() != "text" {
		t.Fatalf("expected second block to be text, got: %s", output)
	}
	if parsed.Get("content.1.text").String() != "hello world" {
		t.Fatalf("expected text hello world, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_TextWithThoughtSignatureWithoutThoughtFlag(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "Tokyo: 20C", "thoughtSignature": "sig-carrier"}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.#").Int() != 1 {
		t.Fatalf("expected exactly 1 content block, got: %s", output)
	}
	if parsed.Get("content.0.type").String() != "text" {
		t.Fatalf("text part with thoughtSignature without thought flag must remain text, got: %s", output)
	}
	if parsed.Get("content.0.text").String() != "Tokyo: 20C" {
		t.Fatalf("expected text 'Tokyo: 20C', got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_FunctionCallWithThoughtSignature(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{
						"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}},
						"thoughtSignature": "sig-fc"
					}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.#").Int() != 1 {
		t.Fatalf("expected exactly 1 block without empty thinking block, got: %s", output)
	}
	if parsed.Get("content.0.type").String() != "tool_use" {
		t.Fatalf("expected tool_use block, got: %s", output)
	}
	if parsed.Get("content.0.name").String() != "get_weather" {
		t.Fatalf("expected tool name get_weather, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_ThinkingSignatureNotOverwrittenByFunctionCall(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "thinking reasoning", "thought": true, "thoughtSignature": "sig-thinking"},
					{
						"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}},
						"thoughtSignature": "sig-function-call"
					}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.#").Int() != 2 {
		t.Fatalf("expected 2 blocks (thinking and tool_use), got: %s", output)
	}
	if parsed.Get("content.0.type").String() != "thinking" {
		t.Fatalf("expected first block to be thinking, got: %s", output)
	}
	if parsed.Get("content.0.signature").String() != "sig-thinking" {
		t.Fatalf("expected thinking signature to be sig-thinking, not overwritten by functionCall, got: %s", output)
	}
	if parsed.Get("content.1.type").String() != "tool_use" {
		t.Fatalf("expected second block to be tool_use, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaude_TextWithThoughtSignatureWithoutThoughtFlag(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	chunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "Tokyo: 20C", "thoughtSignature": "sig-carrier"}]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	var param any
	ctx := context.Background()
	output := bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, chunk, &param), nil)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)
	outputText := string(output)

	if strings.Contains(outputText, `"content_block":{"type":"thinking"`) {
		t.Fatalf("text with thoughtSignature without thought flag must not start a thinking block in stream: %s", outputText)
	}
	if !strings.Contains(outputText, `"content_block":{"type":"text"`) {
		t.Fatalf("expected text content block in stream, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"text":"Tokyo: 20C"`) {
		t.Fatalf("expected text delta Tokyo: 20C in stream, got: %s", outputText)
	}
}

func TestConvertGeminiResponseToClaude_FunctionCallWithThoughtSignature(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	chunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{
					"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}},
					"thoughtSignature": "sig-fc"
				}]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	var param any
	ctx := context.Background()
	output := bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, chunk, &param), nil)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)
	outputText := string(output)

	if strings.Contains(outputText, `"content_block":{"type":"thinking"`) {
		t.Fatalf("functionCall with thoughtSignature must not start an empty thinking block in stream: %s", outputText)
	}
	if !strings.Contains(outputText, `"content_block":{"type":"tool_use"`) {
		t.Fatalf("expected tool_use block in stream, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"name":"get_weather"`) {
		t.Fatalf("expected tool name get_weather in stream, got: %s", outputText)
	}
}

func TestConvertGeminiResponseToClaude_ThinkingSignatureNotOverwrittenByFunctionCall(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	thinkingChunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "thinking reasoning", "thought": true, "thoughtSignature": "sig-thinking"}]
			}
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)
	toolChunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{
					"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}},
					"thoughtSignature": "sig-fc"
				}]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	var param any
	ctx := context.Background()
	output := bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, thinkingChunk, &param), nil)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, toolChunk, &param), nil)...)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)
	outputText := string(output)

	if !strings.Contains(outputText, `"signature":"sig-thinking"`) {
		t.Fatalf("expected thinking signature to be sig-thinking, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"content_block":{"type":"tool_use"`) {
		t.Fatalf("expected tool_use block, got: %s", outputText)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_ThinkingWithoutSignature(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "thinking without sig", "thought": true},
					{"text": "response text"}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.0.type").String() != "thinking" {
		t.Fatalf("expected thinking block, got: %s", output)
	}
	if parsed.Get("content.0.thinking").String() != "thinking without sig" {
		t.Fatalf("expected thinking content, got: %s", output)
	}
	if parsed.Get("content.0.signature").Exists() {
		t.Fatalf("expected no signature field when thoughtSignature absent, got: %s", output)
	}
	if parsed.Get("content.1.type").String() != "text" {
		t.Fatalf("expected text block, got: %s", output)
	}
	if parsed.Get("content.1.text").String() != "response text" {
		t.Fatalf("expected text content, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_RegularTextOnly(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "plain response"}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.#").Int() != 1 {
		t.Fatalf("expected exactly 1 content block, got: %s", output)
	}
	if parsed.Get("content.0.type").String() != "text" {
		t.Fatalf("expected text block, got: %s", output)
	}
	if parsed.Get("content.0.text").String() != "plain response" {
		t.Fatalf("expected text content, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_SnakeCaseThoughtSignature(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "thinking snake", "thought": true, "thought_signature": "sig-snake"},
					{"text": "output"}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.0.signature").String() != "sig-snake" {
		t.Fatalf("expected snake_case thought_signature to be extracted, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_EmptyThoughtSignatureNotBoundToLaterBlock(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"thought": true, "thoughtSignature": "sigA"},
					{"text": "visible text"},
					{"text": "reasoning", "thought": true}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.0.type").String() != "text" || parsed.Get("content.0.text").String() != "visible text" {
		t.Fatalf("expected block 0 to be text 'visible text', got: %s", output)
	}
	if parsed.Get("content.1.type").String() != "thinking" || parsed.Get("content.1.thinking").String() != "reasoning" {
		t.Fatalf("expected block 1 to be thinking 'reasoning', got: %s", output)
	}
	if parsed.Get("content.1.signature").Exists() {
		t.Fatalf("later thinking block must NOT carry stale signature sigA, got: %s", output)
	}
}

func TestConvertGeminiResponseToClaudeNonStream_MultipleSignedThoughtPartsSplitBlocks(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	rawResponse := []byte(`{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "thinking 1", "thought": true, "thoughtSignature": "sig1"},
					{"text": "thinking 2", "thought": true, "thoughtSignature": "sig2"},
					{"text": "final answer"}
				]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	ctx := context.Background()
	output := ConvertGeminiResponseToClaudeNonStream(ctx, "gemini-test", requestJSON, requestJSON, rawResponse, nil)
	parsed := gjson.ParseBytes(output)

	if parsed.Get("content.#").Int() != 3 {
		t.Fatalf("expected 3 blocks (2 thinking + 1 text), got: %s", output)
	}
	if parsed.Get("content.0.type").String() != "thinking" || parsed.Get("content.0.thinking").String() != "thinking 1" || parsed.Get("content.0.signature").String() != "sig1" {
		t.Fatalf("expected block 0 to be thinking 1 with sig1, got: %s", output)
	}
	if parsed.Get("content.1.type").String() != "thinking" || parsed.Get("content.1.thinking").String() != "thinking 2" || parsed.Get("content.1.signature").String() != "sig2" {
		t.Fatalf("expected block 1 to be thinking 2 with sig2, got: %s", output)
	}
	if parsed.Get("content.2.type").String() != "text" || parsed.Get("content.2.text").String() != "final answer" {
		t.Fatalf("expected block 2 to be text 'final answer', got: %s", output)
	}
}

func TestConvertGeminiResponseToClaude_MultipleSignedThoughtChunksSplitBlocks(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	chunk1 := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "thinking 1", "thought": true, "thoughtSignature": "sig1"}]
			}
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)
	chunk2 := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "thinking 2", "thought": true, "thoughtSignature": "sig2"}]
			},
			"finishReason": "STOP"
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	var param any
	ctx := context.Background()
	output := bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, chunk1, &param), nil)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, chunk2, &param), nil)...)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)
	outputText := string(output)

	if !strings.Contains(outputText, `"type":"content_block_start","index":0,"content_block":{"type":"thinking"`) {
		t.Fatalf("expected thinking content_block_start at index 0, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"content_block_stop","index":0`) {
		t.Fatalf("expected thinking content_block_stop at index 0 before second signed block, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"content_block_start","index":1,"content_block":{"type":"thinking"`) {
		t.Fatalf("expected thinking content_block_start at index 1 for second signed block, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"signature_delta","signature":"sig1"`) {
		t.Fatalf("expected signature_delta sig1, got: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"signature_delta","signature":"sig2"`) {
		t.Fatalf("expected signature_delta sig2, got: %s", outputText)
	}
}
