package openai

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertKiroNonStreamToOpenAIIncludesCacheUsage(t *testing.T) {
	rawResponse := []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"kiro-claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":13,"output_tokens":4,"cache_read_input_tokens":22000,"cache_creation_input_tokens":31}}`)

	out := ConvertKiroNonStreamToOpenAI(context.Background(), "kiro-claude", nil, nil, rawResponse, nil)

	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 22044 {
		t.Fatalf("prompt_tokens = %d, want %d", got, 22044)
	}
	if got := gjson.GetBytes(out, "usage.completion_tokens").Int(); got != 4 {
		t.Fatalf("completion_tokens = %d, want %d", got, 4)
	}
	if got := gjson.GetBytes(out, "usage.total_tokens").Int(); got != 22048 {
		t.Fatalf("total_tokens = %d, want %d", got, 22048)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 22000 {
		t.Fatalf("cached_tokens = %d, want %d", got, 22000)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cache_read_input_tokens").Int(); got != 22000 {
		t.Fatalf("cache_read_input_tokens = %d, want %d", got, 22000)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cache_creation_input_tokens").Int(); got != 31 {
		t.Fatalf("cache_creation_input_tokens = %d, want %d", got, 31)
	}
}

func TestConvertKiroStreamToOpenAIIncludesCacheUsage(t *testing.T) {
	var param any
	out := ConvertKiroStreamToOpenAI(
		context.Background(),
		"kiro-claude",
		nil,
		nil,
		[]byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":13,"output_tokens":4,"cache_read_input_tokens":22000,"cache_creation_input_tokens":31}}`),
		&param,
	)

	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want %d", len(out), 2)
	}
	usageChunk := out[1]
	if got := gjson.GetBytes(usageChunk, "usage.prompt_tokens").Int(); got != 22044 {
		t.Fatalf("prompt_tokens = %d, want %d", got, 22044)
	}
	if got := gjson.GetBytes(usageChunk, "usage.completion_tokens").Int(); got != 4 {
		t.Fatalf("completion_tokens = %d, want %d", got, 4)
	}
	if got := gjson.GetBytes(usageChunk, "usage.total_tokens").Int(); got != 22048 {
		t.Fatalf("total_tokens = %d, want %d", got, 22048)
	}
	if got := gjson.GetBytes(usageChunk, "usage.prompt_tokens_details.cached_tokens").Int(); got != 22000 {
		t.Fatalf("cached_tokens = %d, want %d", got, 22000)
	}
	if got := gjson.GetBytes(usageChunk, "usage.prompt_tokens_details.cache_read_input_tokens").Int(); got != 22000 {
		t.Fatalf("cache_read_input_tokens = %d, want %d", got, 22000)
	}
	if got := gjson.GetBytes(usageChunk, "usage.prompt_tokens_details.cache_creation_input_tokens").Int(); got != 31 {
		t.Fatalf("cache_creation_input_tokens = %d, want %d", got, 31)
	}
}
