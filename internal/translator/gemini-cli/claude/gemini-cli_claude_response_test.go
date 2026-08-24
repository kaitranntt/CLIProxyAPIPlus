package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiCLIResponseToClaude_PreservesThoughtSignature(t *testing.T) {
	ctx := context.Background()
	var param any
	raw := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"step one","thoughtSignature":"opaque-gemini-id"}]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":5,"promptTokenCount":10}}}`)

	out := ConvertGeminiCLIResponseToClaude(ctx, "gemini-cli", nil, nil, raw, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 SSE chunk, got %d", len(out))
	}

	hasSig := false
	for _, line := range strings.Split(string(out[0]), "\n") {
		data := strings.TrimPrefix(line, "data: ")
		if data == line {
			continue
		}
		if gjson.Get(data, "type").String() == "content_block_delta" &&
			gjson.Get(data, "delta.type").String() == "signature_delta" {
			if got := gjson.Get(data, "delta.signature").String(); got == "opaque-gemini-id" {
				hasSig = true
			}
		}
	}
	if !hasSig {
		t.Fatalf("expected a signature_delta with opaque-gemini-id, got %s", out[0])
	}
}

func TestConvertGeminiCLIResponseToClaudeNonStream_PreservesThoughtSignature(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"step one","thoughtSignature":"opaque-gemini-id"}]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":5,"promptTokenCount":10}}}`)

	out := ConvertGeminiCLIResponseToClaudeNonStream(ctx, "gemini-cli", nil, nil, raw, nil)
	sig := gjson.GetBytes(out, "content.0.signature").String()
	if sig != "opaque-gemini-id" {
		t.Fatalf("expected thinking signature to be preserved, got %q; response=%s", sig, out)
	}
}

func TestConvertGeminiCLIResponseToClaude_VisibleTextWithSignatureStaysText(t *testing.T) {
	ctx := context.Background()
	var param any
	raw := []byte(`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"visible answer","thoughtSignature":"sig-visible"}]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":5,"promptTokenCount":10}}}`)

	out := ConvertGeminiCLIResponseToClaude(ctx, "gemini-cli", nil, nil, raw, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 SSE chunk, got %d", len(out))
	}

	joined := string(out[0])
	sawVisibleAsThinking := false
	sawVisibleAsText := false
	for _, line := range strings.Split(joined, "\n") {
		data := strings.TrimPrefix(line, "data: ")
		if data == line {
			continue
		}
		if gjson.Get(data, "type").String() != "content_block_delta" {
			continue
		}
		deltaType := gjson.Get(data, "delta.type").String()
		if deltaType == "thinking_delta" && gjson.Get(data, "delta.thinking").String() == "visible answer" {
			sawVisibleAsThinking = true
		}
		if deltaType == "text_delta" && gjson.Get(data, "delta.text").String() == "visible answer" {
			sawVisibleAsText = true
		}
	}
	if sawVisibleAsThinking {
		t.Fatalf("visible text with thoughtSignature was rerouted into thinking: %s", joined)
	}
	if !sawVisibleAsText {
		t.Fatalf("visible text with thoughtSignature was not emitted as text: %s", joined)
	}
}
