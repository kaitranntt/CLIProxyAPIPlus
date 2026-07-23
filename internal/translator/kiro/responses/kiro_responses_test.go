package responses

import (
	"context"
	"strings"
	"testing"

	kiroclaude "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func parseKiroResponsesSSEEvent(t *testing.T, chunk []byte) (string, gjson.Result) {
	t.Helper()

	var event string
	var data string
	for _, line := range strings.Split(string(chunk), "\n") {
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	if data == "" {
		t.Fatalf("SSE chunk has no data line: %s", string(chunk))
	}
	return event, gjson.Parse(data)
}

func TestKiroResponsesTranslatorRegistrationAndRequestDelegation(t *testing.T) {
	kiroFormat := sdktranslator.FromString("kiro")
	if !sdktranslator.HasRequestTransformer(sdktranslator.FormatOpenAIResponse, kiroFormat) {
		t.Fatal("OpenAI Responses to Kiro request transformer is not registered")
	}
	if !sdktranslator.HasStreamResponseTransformer(sdktranslator.FormatOpenAIResponse, kiroFormat) {
		t.Fatal("Kiro to OpenAI Responses stream transformer is not registered")
	}
	if !sdktranslator.HasNonStreamResponseTransformer(sdktranslator.FormatOpenAIResponse, kiroFormat) {
		t.Fatal("Kiro to OpenAI Responses non-stream transformer is not registered")
	}

	raw := []byte(`{
		"model":"client-model",
		"max_output_tokens":128,
		"input":[
			{
				"type":"message",
				"role":"user",
				"content":[{"type":"input_text","text":"Run pwd"}]
			}
		]
	}`)
	out := sdktranslator.TranslateRequest(
		sdktranslator.FormatOpenAIResponse,
		kiroFormat,
		"kiro-claude-sonnet-4-6",
		raw,
		true,
	)
	root := gjson.ParseBytes(out)

	if got := root.Get("model").String(); got != "kiro-claude-sonnet-4-6" {
		t.Fatalf("model = %q, want kiro-claude-sonnet-4-6. Output: %s", got, string(out))
	}
	if got := root.Get("max_tokens").Int(); got != 128 {
		t.Fatalf("max_tokens = %d, want 128. Output: %s", got, string(out))
	}
	if !root.Get("stream").Bool() {
		t.Fatalf("stream = false, want true. Output: %s", string(out))
	}
	if got := root.Get("messages.0.role").String(); got != "user" {
		t.Fatalf("message role = %q, want user. Output: %s", got, string(out))
	}
	if got := root.Get("messages.0.content").String(); got != "Run pwd" {
		t.Fatalf("message content = %q, want Run pwd. Output: %s", got, string(out))
	}
	if root.Get("input").Exists() {
		t.Fatalf("OpenAI Responses input leaked into Claude request. Output: %s", string(out))
	}
}

func TestKiroResponsesStreamFinalizesMessageBeforeFunctionCall(t *testing.T) {
	chunks := [][]byte{
		kiroclaude.BuildClaudeMessageStartEvent("kiro-claude-sonnet-4-6", 1),
		kiroclaude.BuildClaudeContentBlockStartEvent(0, "text", "", ""),
		kiroclaude.BuildClaudeStreamEvent("Checking the workspace.", 0),
		kiroclaude.BuildClaudeContentBlockStopEvent(0),
		kiroclaude.BuildClaudeContentBlockStartEvent(1, "tool_use", "call_123", "exec_command"),
		kiroclaude.BuildClaudeInputJsonDeltaEvent(`{"cmd":"pwd"}`, 1),
		kiroclaude.BuildClaudeContentBlockStopEvent(1),
		kiroclaude.BuildClaudeMessageDeltaEvent("tool_use", usage.Detail{InputTokens: 1, OutputTokens: 8}),
		kiroclaude.BuildClaudeMessageStopOnlyEvent(),
	}

	var param any
	var outputs [][]byte
	for _, chunk := range chunks {
		outputs = append(outputs, sdktranslator.TranslateStream(
			context.Background(),
			sdktranslator.FromString("kiro"),
			sdktranslator.FormatOpenAIResponse,
			"kiro-claude-sonnet-4-6",
			nil,
			nil,
			chunk,
			&param,
		)...)
	}

	messageDonePosition := -1
	functionAddedPosition := -1
	messageDoneCount := 0
	functionDoneCount := 0
	var completed gjson.Result
	for position, output := range outputs {
		event, data := parseKiroResponsesSSEEvent(t, output)
		itemType := data.Get("item.type").String()
		switch {
		case event == "response.output_item.done" && itemType == "message":
			messageDonePosition = position
			messageDoneCount++
			if got := data.Get("output_index").Int(); got != 0 {
				t.Fatalf("message done output_index = %d, want 0", got)
			}
		case event == "response.output_item.added" && itemType == "function_call":
			functionAddedPosition = position
			if got := data.Get("output_index").Int(); got != 1 {
				t.Fatalf("function added output_index = %d, want 1", got)
			}
		case event == "response.output_item.done" && itemType == "function_call":
			functionDoneCount++
		case event == "response.completed":
			completed = data
		}
	}

	if messageDonePosition < 0 || functionAddedPosition < 0 {
		t.Fatalf("missing lifecycle event: message done=%d, function added=%d", messageDonePosition, functionAddedPosition)
	}
	if messageDonePosition >= functionAddedPosition {
		t.Fatalf("message done position = %d, want before function added position %d", messageDonePosition, functionAddedPosition)
	}
	if messageDoneCount != 1 || functionDoneCount != 1 {
		t.Fatalf("output_item.done counts: message=%d function=%d, want 1 each", messageDoneCount, functionDoneCount)
	}
	if got := completed.Get("response.output.0.type").String(); got != "message" {
		t.Fatalf("completed output[0] type = %q, want message", got)
	}
	if got := completed.Get("response.output.1.type").String(); got != "function_call" {
		t.Fatalf("completed output[1] type = %q, want function_call", got)
	}
}

func TestKiroResponsesStreamFinalizesMessageBeforeReasoning(t *testing.T) {
	// Kiro emits the reasoning channel AFTER the text block (unlike Claude).
	// The assistant message lifecycle must close before the reasoning item
	// starts, otherwise clients see message done events trailing the reasoning
	// item (observed as interleaved sequences on /v1/responses).
	chunks := [][]byte{
		kiroclaude.BuildClaudeMessageStartEvent("kiro-gpt-5-6-sol", 1),
		kiroclaude.BuildClaudeContentBlockStartEvent(0, "text", "", ""),
		kiroclaude.BuildClaudeStreamEvent("Hello there.", 0),
		kiroclaude.BuildClaudeContentBlockStopEvent(0),
		kiroclaude.BuildClaudeContentBlockStartEventWithSignature(1, "thinking", "", "", "sig_kiro_1"),
		kiroclaude.BuildClaudeThinkingDeltaEvent("...", 1),
		kiroclaude.BuildClaudeThinkingBlockStopEvent(1),
		kiroclaude.BuildClaudeMessageDeltaEvent("end_turn", usage.Detail{InputTokens: 1, OutputTokens: 3}),
		kiroclaude.BuildClaudeMessageStopOnlyEvent(),
	}

	var param any
	var outputs [][]byte
	for _, chunk := range chunks {
		outputs = append(outputs, sdktranslator.TranslateStream(
			context.Background(),
			sdktranslator.FromString("kiro"),
			sdktranslator.FormatOpenAIResponse,
			"kiro-gpt-5-6-sol",
			nil,
			nil,
			chunk,
			&param,
		)...)
	}

	messageDonePosition := -1
	reasoningAddedPosition := -1
	var reasoningDone, completed gjson.Result
	for position, output := range outputs {
		event, data := parseKiroResponsesSSEEvent(t, output)
		itemType := data.Get("item.type").String()
		switch {
		case event == "response.output_item.done" && itemType == "message":
			if messageDonePosition < 0 {
				messageDonePosition = position
			}
		case event == "response.output_item.added" && itemType == "reasoning":
			if reasoningAddedPosition < 0 {
				reasoningAddedPosition = position
			}
		case event == "response.output_item.done" && itemType == "reasoning":
			reasoningDone = data
		case event == "response.completed":
			completed = data
		}
	}

	if messageDonePosition < 0 || reasoningAddedPosition < 0 {
		t.Fatalf("missing lifecycle event: message done=%d, reasoning added=%d", messageDonePosition, reasoningAddedPosition)
	}
	if messageDonePosition >= reasoningAddedPosition {
		t.Fatalf("message done position = %d, want before reasoning added position %d", messageDonePosition, reasoningAddedPosition)
	}
	if got := reasoningDone.Get("item.encrypted_content").String(); got != "sig_kiro_1" {
		t.Fatalf("reasoning encrypted_content = %q, want sig_kiro_1", got)
	}
	if got := reasoningDone.Get("item.status").String(); got != "completed" {
		t.Fatalf("reasoning status = %q, want completed", got)
	}
	if got := completed.Get("response.output.1.encrypted_content").String(); got != "sig_kiro_1" {
		t.Fatalf("completed reasoning encrypted_content = %q, want sig_kiro_1", got)
	}
}

func TestKiroResponsesNonStreamAcceptsExecutorClaudeResponse(t *testing.T) {
	claudeResponse := kiroclaude.BuildClaudeResponse(
		"Checking the workspace.",
		[]kiroclaude.KiroToolUse{{
			ToolUseID: "call_123",
			Name:      "exec_command",
			Input:     map[string]interface{}{"cmd": "pwd"},
		}},
		"kiro-claude-sonnet-4-6",
		usage.Detail{
			InputTokens:         3,
			OutputTokens:        11,
			CacheReadTokens:     7,
			CacheCreationTokens: 5,
		},
		"tool_use",
	)
	claudeRoot := gjson.ParseBytes(claudeResponse)

	out := sdktranslator.TranslateNonStream(
		context.Background(),
		sdktranslator.FromString("kiro"),
		sdktranslator.FormatOpenAIResponse,
		"kiro-claude-sonnet-4-6",
		nil,
		nil,
		claudeResponse,
		nil,
	)
	root := gjson.ParseBytes(out)

	wantID := "resp_" + strings.TrimPrefix(claudeRoot.Get("id").String(), "msg_")
	if got := root.Get("id").String(); got != wantID {
		t.Fatalf("response id = %q, want %q (derived from executor id %q). Output: %s", got, wantID, claudeRoot.Get("id").String(), string(out))
	}
	if got := root.Get("output.#").Int(); got != 2 {
		t.Fatalf("output count = %d, want 2. Output: %s", got, string(out))
	}
	if got := root.Get("output.0.type").String(); got != "message" {
		t.Fatalf("output[0].type = %q, want message. Output: %s", got, string(out))
	}
	if got := root.Get("output.0.content.0.text").String(); got != "Checking the workspace." {
		t.Fatalf("message text = %q. Output: %s", got, string(out))
	}
	if got := root.Get("output.1.type").String(); got != "function_call" {
		t.Fatalf("output[1].type = %q, want function_call. Output: %s", got, string(out))
	}
	if got := root.Get("output.1.arguments").String(); got != `{"cmd":"pwd"}` {
		t.Fatalf("function arguments = %q. Output: %s", got, string(out))
	}
	if got := root.Get("usage.input_tokens").Int(); got != 15 {
		t.Fatalf("usage.input_tokens = %d, want 15. Output: %s", got, string(out))
	}
	if got := root.Get("usage.input_tokens_details.cached_tokens").Int(); got != 7 {
		t.Fatalf("usage cached_tokens = %d, want 7. Output: %s", got, string(out))
	}
	if got := root.Get("usage.output_tokens").Int(); got != 11 {
		t.Fatalf("usage.output_tokens = %d, want 11. Output: %s", got, string(out))
	}
	if got := root.Get("usage.total_tokens").Int(); got != 26 {
		t.Fatalf("usage.total_tokens = %d, want 26. Output: %s", got, string(out))
	}
}

func TestKiroResponsesNonStreamOrdersReasoningBeforeMessage(t *testing.T) {
	// The executor normalizes the fully-buffered Claude response to the
	// Anthropic convention (thinking first), so the Responses output must lead
	// with the reasoning item and carry the upstream signature as
	// encrypted_content.
	claudeResponse := kiroclaude.BuildClaudeResponseWithThinking(
		"Done.",
		nil,
		"kiro-gpt-5-6-sol",
		usage.Detail{InputTokens: 3, OutputTokens: 2},
		"end_turn",
		"...",
		"sig_kiro_ns",
	)

	out := sdktranslator.TranslateNonStream(
		context.Background(),
		sdktranslator.FromString("kiro"),
		sdktranslator.FormatOpenAIResponse,
		"kiro-gpt-5-6-sol",
		nil,
		nil,
		claudeResponse,
		nil,
	)
	root := gjson.ParseBytes(out)

	if got := root.Get("output.0.type").String(); got != "reasoning" {
		t.Fatalf("output[0].type = %q, want reasoning. Output: %s", got, string(out))
	}
	if got := root.Get("output.0.encrypted_content").String(); got != "sig_kiro_ns" {
		t.Fatalf("reasoning encrypted_content = %q, want sig_kiro_ns. Output: %s", got, string(out))
	}
	if got := root.Get("output.1.type").String(); got != "message" {
		t.Fatalf("output[1].type = %q, want message. Output: %s", got, string(out))
	}
	if got := root.Get("output.1.content.0.text").String(); got != "Done." {
		t.Fatalf("message text = %q. Output: %s", got, string(out))
	}
}
