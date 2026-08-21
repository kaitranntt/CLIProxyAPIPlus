package gemini

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeResponseToGemini_StreamPreservesToolUseID(t *testing.T) {
	ctx := context.Background()
	var param any

	start := []byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_gateway","name":"lookup"}}`)
	out := ConvertClaudeResponseToGemini(ctx, "gemini-2.5-pro", nil, nil, start, &param)
	if len(out) != 0 {
		t.Fatalf("expected content_block_start to be buffered, got %d chunks", len(out))
	}

	delta := []byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"status\"}"}}`)
	out = ConvertClaudeResponseToGemini(ctx, "gemini-2.5-pro", nil, nil, delta, &param)
	if len(out) != 0 {
		t.Fatalf("expected input_json_delta to be buffered, got %d chunks", len(out))
	}

	stop := []byte(`data: {"type":"content_block_stop","index":0}`)
	out = ConvertClaudeResponseToGemini(ctx, "gemini-2.5-pro", nil, nil, stop, &param)
	if len(out) != 1 {
		t.Fatalf("expected content_block_stop to emit 1 chunk, got %d", len(out))
	}

	got := gjson.GetBytes(out[0], "candidates.0.content.parts.0.functionCall.id").String()
	if got != "toolu_gateway" {
		t.Fatalf("expected functionCall.id %q, got %q; chunk=%s", "toolu_gateway", got, string(out[0]))
	}
}

func TestConvertClaudeResponseToGeminiNonStreamPreservesToolUseID(t *testing.T) {
	ctx := context.Background()
	raw := []byte(strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_gateway","name":"lookup"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"status\"}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
	}, "\n"))

	out := ConvertClaudeResponseToGeminiNonStream(ctx, "gemini-2.5-pro", nil, nil, raw, nil)

	got := gjson.GetBytes(out, "candidates.0.content.parts.0.functionCall.id").String()
	if got != "toolu_gateway" {
		t.Fatalf("expected functionCall.id %q, got %q; chunk=%s", "toolu_gateway", got, string(out))
	}
}

func TestConvertClaudeResponseToGemini_PreservesThinkingSignatureAsBypass(t *testing.T) {
	ctx := context.Background()
	var param any

	events := []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step one"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-claude-id"}}`,
		`data: {"type":"content_block_stop","index":0}`,
	}

	var sigOut [][]byte
	for i, ev := range events {
		out := ConvertClaudeResponseToGemini(ctx, "gemini-2.5-pro", nil, nil, []byte(ev), &param)
		if i == 2 {
			sigOut = out
		}
		if i == len(events)-1 && len(out) != 0 {
			t.Fatalf("expected no output from content_block_stop, got %d", len(out))
		}
	}

	if len(sigOut) != 1 {
		t.Fatalf("expected 1 signature chunk, got %d", len(sigOut))
	}
	part := gjson.GetBytes(sigOut[0], "candidates.0.content.parts.0")
	if !part.Get("thought").Bool() {
		t.Fatalf("expected a thought part for signature, got %s", sigOut[0])
	}
	if part.Get("thoughtSignature").String() != "skip_thought_signature_validator" {
		t.Fatalf("expected Gemini bypass signature, got %s", sigOut[0])
	}
}

func TestConvertClaudeResponseToGeminiNonStream_PreservesThinkingSignatureAsBypass(t *testing.T) {
	ctx := context.Background()
	raw := []byte(fmt.Sprintf(`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step one"}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-claude-id"}}
data: {"type":"content_block_stop","index":0}
`))

	out := ConvertClaudeResponseToGeminiNonStream(ctx, "gemini-2.5-pro", nil, nil, raw, nil)
	part := gjson.GetBytes(out, "candidates.0.content.parts.0")
	if !part.Get("thought").Bool() || part.Get("text").String() != "step one" {
		t.Fatalf("expected thinking part with text, got %s", out)
	}
	if part.Get("thoughtSignature").String() != "skip_thought_signature_validator" {
		t.Fatalf("expected Gemini bypass signature, got %s", out)
	}
}
