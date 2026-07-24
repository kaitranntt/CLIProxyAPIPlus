package executor

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIResponsesItemIDs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "rewrites fc id on custom_tool_call",
			body: `{"input":[{"type":"custom_tool_call","id":"fc_call_ba945334-9f37-4a5-abc3-0b43820ba5a1","call_id":"call_ba945334-9f37-4a5-abc3-0b43820ba5a1","name":"apply_patch","input":"***"}]}`,
			want: `{"input":[{"type":"custom_tool_call","id":"ctc_call_ba945334-9f37-4a5-abc3-0b43820ba5a1","call_id":"call_ba945334-9f37-4a5-abc3-0b43820ba5a1","name":"apply_patch","input":"***"}]}`,
		},
		{
			name: "rewrites ctc id on function_call",
			body: `{"input":[{"type":"function_call","id":"ctc_call_abc","call_id":"call_abc","name":"shell","arguments":"{}"}]}`,
			want: `{"input":[{"type":"function_call","id":"fc_call_abc","call_id":"call_abc","name":"shell","arguments":"{}"}]}`,
		},
		{
			name: "prefixes unknown id shape",
			body: `{"input":[{"type":"function_call","id":"plain-uuid","call_id":"call_1","name":"shell","arguments":"{}"}]}`,
			want: `{"input":[{"type":"function_call","id":"fc_plain-uuid","call_id":"call_1","name":"shell","arguments":"{}"}]}`,
		},
		{
			name: "keeps matching function_call id",
			body: `{"input":[{"type":"function_call","id":"fc_call_abc","call_id":"call_abc","name":"shell","arguments":"{}"}]}`,
			want: `{"input":[{"type":"function_call","id":"fc_call_abc","call_id":"call_abc","name":"shell","arguments":"{}"}]}`,
		},
		{
			name: "keeps matching custom_tool_call id",
			body: `{"input":[{"type":"custom_tool_call","id":"ctc_call_abc","call_id":"call_abc","name":"apply_patch","input":"x"}]}`,
			want: `{"input":[{"type":"custom_tool_call","id":"ctc_call_abc","call_id":"call_abc","name":"apply_patch","input":"x"}]}`,
		},
		{
			name: "leaves items without id untouched",
			body: `{"input":[{"type":"function_call","call_id":"call_abc","name":"shell","arguments":"{}"}]}`,
			want: `{"input":[{"type":"function_call","call_id":"call_abc","name":"shell","arguments":"{}"}]}`,
		},
		{
			name: "leaves other item types untouched",
			body: `{"input":[{"type":"message","id":"msg_1","role":"user","content":[]},{"type":"reasoning","id":"rs_1"},{"type":"function_call_output","call_id":"call_abc","output":"ok"}]}`,
			want: `{"input":[{"type":"message","id":"msg_1","role":"user","content":[]},{"type":"reasoning","id":"rs_1"},{"type":"function_call_output","call_id":"call_abc","output":"ok"}]}`,
		},
		{
			name: "mixed array rewrites only mismatched items and preserves order",
			body: `{"input":[{"type":"message","id":"msg_1","role":"user","content":[]},{"type":"function_call","id":"fc_call_1","call_id":"call_1","name":"a","arguments":"{}"},{"type":"custom_tool_call","id":"fc_call_2","call_id":"call_2","name":"b","input":"y"},{"type":"function_call_output","call_id":"call_2","output":"done"}]}`,
			want: `{"input":[{"type":"message","id":"msg_1","role":"user","content":[]},{"type":"function_call","id":"fc_call_1","call_id":"call_1","name":"a","arguments":"{}"},{"type":"custom_tool_call","id":"ctc_call_2","call_id":"call_2","name":"b","input":"y"},{"type":"function_call_output","call_id":"call_2","output":"done"}]}`,
		},
		{
			name: "returns body unchanged without input array",
			body: `{"model":"gpt-5","input":"plain string"}`,
			want: `{"model":"gpt-5","input":"plain string"}`,
		},
		{
			name: "returns body unchanged without input field",
			body: `{"model":"gpt-5"}`,
			want: `{"model":"gpt-5"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOpenAIResponsesItemIDs(ctx, "test", []byte(tt.body))
			if string(got) != tt.want {
				t.Fatalf("normalizeOpenAIResponsesItemIDs() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNormalizeOpenAIResponsesItemIDsNoChangeReturnsSameBody(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","id":"fc_call_1","call_id":"call_1","name":"a","arguments":"{}"}]}`)
	got := normalizeOpenAIResponsesItemIDs(context.Background(), "test", body)
	if string(got) != string(body) {
		t.Fatalf("expected unchanged body, got %s", got)
	}
	if gjson.GetBytes(got, "input.0.id").String() != "fc_call_1" {
		t.Fatalf("unexpected id rewrite: %s", got)
	}
}
