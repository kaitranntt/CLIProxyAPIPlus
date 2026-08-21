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

	lines := strings.Split(string(out[0]), "\n")
	hasSig := false
	for i, line := range lines {
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
		_ = i
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
