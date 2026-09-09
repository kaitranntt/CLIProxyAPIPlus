package helps

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCarryOverThinkingToSystem_MovesReasoningToSystemMessage(t *testing.T) {
	input := []byte(`{
		"model": "test",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "answer", "reasoning_content": "I should be helpful."}
		]
	}`)

	out := CarryOverThinkingToSystem(input)

	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected first message to be system, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
	content := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(content, carryOverLabel) {
		t.Fatalf("expected system content to contain %q, got %q", carryOverLabel, content)
	}
	if !strings.Contains(content, "I should be helpful") {
		t.Fatalf("expected system content to contain reasoning, got %q", content)
	}

	if gjson.GetBytes(out, "messages.2.reasoning_content").Exists() {
		t.Fatalf("reasoning_content should be removed from assistant message")
	}

	assistant := gjson.GetBytes(out, "messages.2")
	if assistant.Get("role").String() != "assistant" || assistant.Get("content").String() != "answer" {
		t.Fatalf("assistant message should be preserved, got %s", assistant.Raw)
	}
}

func TestCarryOverThinkingToSystem_DropsEmptyAssistantMessage(t *testing.T) {
	input := []byte(`{
		"model": "test",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "", "reasoning_content": "only reasoning"}
		]
	}`)

	out := CarryOverThinkingToSystem(input)

	if gjson.GetBytes(out, "messages.#").Int() != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", gjson.GetBytes(out, "messages.#").Int(), string(out))
	}
	if gjson.GetBytes(out, "messages.1.role").String() != "user" {
		t.Fatalf("user message should remain second")
	}
}

func TestCarryOverThinkingToSystem_KeepsAssistantWithToolCalls(t *testing.T) {
	input := []byte(`{
		"model": "test",
		"messages": [
			{"role": "assistant", "content": "", "tool_calls": [{"id":"1","type":"function"}], "reasoning_content": "tool planning"}
		]
	}`)

	out := CarryOverThinkingToSystem(input)

	if gjson.GetBytes(out, "messages.#").Int() != 2 {
		t.Fatalf("expected 2 messages, got %s", string(out))
	}
	if !gjson.GetBytes(out, "messages.1.tool_calls").Exists() {
		t.Fatalf("tool_calls should be preserved")
	}
	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("reasoning_content should be removed")
	}
}

func TestCarryOverThinkingToSystem_MergesIntoExistingSystemMessage(t *testing.T) {
	input := []byte(`{
		"model": "test",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "assistant", "reasoning_content": "thinking", "content": "hi"}
		]
	}`)

	out := CarryOverThinkingToSystem(input)

	content := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.HasPrefix(content, carryOverLabel) {
		t.Fatalf("expected carry-over label at start of system content, got %q", content)
	}
	if !strings.Contains(content, "You are a helpful assistant.") {
		t.Fatalf("expected original system content to be preserved, got %q", content)
	}
}

func TestCarryOverThinkingToSystem_MergesIntoExistingSystemMessageArray(t *testing.T) {
	input := []byte(`{
		"model": "test",
		"messages": [
			{"role": "system", "content": [{"type":"text","text":"base"}]},
			{"role": "assistant", "reasoning_content": "thinking", "content": "hi"}
		]
	}`)

	out := CarryOverThinkingToSystem(input)

	firstType := gjson.GetBytes(out, "messages.0.content.0.type").String()
	if firstType != "text" {
		t.Fatalf("expected first content part to be text, got %q", firstType)
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.0.content.0.text").String(), carryOverLabel) {
		t.Fatalf("expected carry-over text in first content part, got %q", gjson.GetBytes(out, "messages.0.content.0.text").String())
	}
	if gjson.GetBytes(out, "messages.0.content.1.text").String() != "base" {
		t.Fatalf("expected original content part to be preserved, got %q", gjson.GetBytes(out, "messages.0.content.1.text").String())
	}
}

func TestCarryOverThinkingToSystem_BoundsAndTruncates(t *testing.T) {
	// Build 5 reasoning blocks
	blocks := []string{"old1", "old2", "mid", "recent", "newest"}
	var msgs []string
	for _, b := range blocks {
		msgs = append(msgs, `{"role":"assistant","content":"","reasoning_content":"`+b+`"}`)
	}
	input := []byte(`{"model":"test","messages":[` + strings.Join(msgs, ",") + `]}`)

	out := CarryOverThinkingToSystem(input)

	system := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(system, "older reasoning block(s) omitted") {
		t.Fatalf("expected omission marker, got %q", system)
	}
	if strings.Contains(system, "old1") || strings.Contains(system, "old2") {
		t.Fatalf("expected old1 and old2 to be omitted, got %q", system)
	}
	if !strings.Contains(system, "mid") || !strings.Contains(system, "recent") || !strings.Contains(system, "newest") {
		t.Fatalf("expected mid, recent, newest to be present, got %q", system)
	}
}

func TestCarryOverThinkingToSystem_TruncatesLongBlock(t *testing.T) {
	long := strings.Repeat("x", carryOverMaxBlockSize+50)
	input := []byte(`{"model":"test","messages":[{"role":"assistant","content":"","reasoning_content":"` + long + `"}]}`)

	out := CarryOverThinkingToSystem(input)

	system := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(system, "... [reasoning truncated]") {
		t.Fatalf("expected truncation marker, got %q", system)
	}
}

func TestCarryOverThinkingToSystem_PreservesStringArrayElements(t *testing.T) {
	input := []byte(`{
		"model": "test",
		"messages": [
			{"role": "system", "content": ["plain string", {"type":"text","text":"object part"}]},
			{"role": "assistant", "reasoning_content": "thinking", "content": "hi"}
		]
	}`)

	out := CarryOverThinkingToSystem(input)

	if gjson.GetBytes(out, "messages.0.content.1.type").String() != "text" || gjson.GetBytes(out, "messages.0.content.1.text").String() != "plain string" {
		t.Fatalf("expected plain string to be wrapped and preserved, got %s", string(out))
	}
	if gjson.GetBytes(out, "messages.0.content.2.text").String() != "object part" {
		t.Fatalf("expected object part to be preserved, got %s", string(out))
	}
}

func TestCarryOverThinkingToSystem_NoReasoningLeavesPayloadUnchanged(t *testing.T) {
	input := []byte(`{"model":"test","messages":[{"role":"user","content":"hello"}]}`)
	out := CarryOverThinkingToSystem(input)
	if string(out) != string(input) {
		t.Fatalf("expected payload to be unchanged, got %s", string(out))
	}
}

func TestTranslateRequestWithAPIKeyModelCompatibility_CarryOverClaudeToOpenAI(t *testing.T) {
	cfg := &config.Config{
		Translator: config.TranslatorConfig{
			CarryOverThinkingInSystem: true,
		},
	}

	claudePayload := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": [{"type":"text","text":"hi"},{"type":"thinking","thinking":"internal reasoning"}]}
		]
	}`)

	out := TranslateRequestWithAPIKeyModelCompatibility(context.Background(), nil, cfg, sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "test", claudePayload, false, false)

	firstRole := gjson.GetBytes(out, "messages.0.role").String()
	if firstRole != "system" {
		t.Fatalf("expected first message role to be system, got %q", firstRole)
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "internal reasoning") {
		t.Fatalf("expected reasoning in system message, got %s", string(out))
	}
	if gjson.GetBytes(out, "messages.#").Int() != 3 {
		t.Fatalf("expected 3 messages (system, user, assistant), got %d: %s", gjson.GetBytes(out, "messages.#").Int(), string(out))
	}
	for _, msg := range gjson.GetBytes(out, "messages").Array() {
		if msg.Get("reasoning_content").Exists() {
			t.Fatalf("no message should have reasoning_content, got %s", msg.Raw)
		}
	}
}

func TestTranslateRequestWithAPIKeyModelCompatibility_RespectsCompat(t *testing.T) {
	cfg := &config.Config{
		Translator: config.TranslatorConfig{
			CarryOverThinkingInSystem: true,
		},
	}

	claudePayload := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": [{"type":"text","text":"hi"},{"type":"thinking","thinking":"internal reasoning"}]}
		]
	}`)

	out := TranslateRequestWithAPIKeyModelCompatibility(context.Background(), nil, cfg, sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "test", claudePayload, false, true)

	if gjson.GetBytes(out, "messages.0.role").String() == "system" {
		t.Fatalf("system carry-over should not happen when isCompat is true")
	}
	if !gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("expected canonical reasoning_content on compat path, got %s", string(out))
	}
}

func TestTranslateRequestWithAPIKeyModelCompatibility_DisabledByDefault(t *testing.T) {
	cfg := &config.Config{} // default false

	claudePayload := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "assistant", "content": [{"type":"thinking","thinking":"internal reasoning"},{"type":"text","text":"hi"}]}
		]
	}`)

	out := TranslateRequestWithAPIKeyModelCompatibility(context.Background(), nil, cfg, sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "test", claudePayload, false, false)

	if gjson.GetBytes(out, "messages.0.role").String() == "system" {
		t.Fatalf("carry-over should not happen when disabled")
	}
	// Non-compat default drops unsigned thinking.
	if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Fatalf("unsigned thinking should not become reasoning_content on default non-compat path, got %s", string(out))
	}
}

func validGPTChatReasoningSignature() string {
	raw := make([]byte, 1+8+16+16+32)
	raw[0] = 0x80
	raw[8] = 1
	for i := 9; i < len(raw); i++ {
		raw[i] = byte(i)
	}
	return base64.URLEncoding.EncodeToString(raw)
}

func TestTranslateRequestWithAPIKeyModelCompatibility_CarryOverKeepsSignedReasoningContent(t *testing.T) {
	cfg := &config.Config{
		Translator: config.TranslatorConfig{
			CarryOverThinkingInSystem: true,
		},
	}

	sig := validGPTChatReasoningSignature()
	claudePayload := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "assistant", "content": [
				{"type":"thinking","thinking":"unsigned fallback reasoning"},
				{"type":"thinking","thinking":"signed canonical reasoning","signature":"` + sig + `"},
				{"type":"text","text":"hi"}
			]}
		]
	}`)

	out := TranslateRequestWithAPIKeyModelCompatibility(context.Background(), nil, cfg, sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "test", claudePayload, false, false)

	system := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(system, "unsigned fallback reasoning") {
		t.Fatalf("expected unsigned reasoning in system message, got %s", string(out))
	}
	if strings.Contains(system, "signed canonical reasoning") {
		t.Fatalf("signed reasoning should stay as reasoning_content, got %s", string(out))
	}

	assistant := gjson.GetBytes(out, "messages.1")
	if assistant.Get("role").String() != "assistant" {
		t.Fatalf("expected assistant message, got %s", assistant.Raw)
	}
	if !strings.Contains(assistant.Get("reasoning_content").String(), "signed canonical reasoning") {
		t.Fatalf("expected signed reasoning as reasoning_content, got %s", string(out))
	}
	if assistant.Get("content.0.text").String() != "hi" {
		t.Fatalf("expected assistant content to be preserved, got %s", string(out))
	}
}

func TestTranslateRequestWithAPIKeyModelCompatibility_CarryOverMergesWithSystem(t *testing.T) {
	cfg := &config.Config{
		Translator: config.TranslatorConfig{
			CarryOverThinkingInSystem: true,
		},
	}

	claudePayload := []byte(`{
		"model": "claude-3-opus",
		"system": "Base instructions",
		"messages": [
			{"role": "assistant", "content": [{"type":"thinking","thinking":"prior reasoning"}]}
		]
	}`)

	out := TranslateRequestWithAPIKeyModelCompatibility(context.Background(), nil, cfg, sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "test", claudePayload, false, false)

	system := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(system, carryOverLabel) {
		t.Fatalf("expected carry-over label in system message, got %q", system)
	}
	if !strings.Contains(system, "Base instructions") {
		t.Fatalf("expected original system instructions to be preserved, got %q", system)
	}
	if !strings.Contains(system, "prior reasoning") {
		t.Fatalf("expected prior reasoning in system message, got %q", system)
	}
}

func TestTranslateRequestWithAPIKeyModelCompatibility_CarryOverKeepsToolCalls(t *testing.T) {
	cfg := &config.Config{
		Translator: config.TranslatorConfig{
			CarryOverThinkingInSystem: true,
		},
	}

	claudePayload := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "assistant", "content": [
				{"type":"thinking","thinking":"tool planning"},
				{"type":"tool_use","id":"tu_1","name":"do_work","input":{"x":1}}
			]}
		]
	}`)

	out := TranslateRequestWithAPIKeyModelCompatibility(context.Background(), nil, cfg, sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "test", claudePayload, false, false)

	if gjson.GetBytes(out, "messages.#").Int() != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", gjson.GetBytes(out, "messages.#").Int(), string(out))
	}
	system := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(system, "tool planning") {
		t.Fatalf("expected tool planning in system message, got %q", system)
	}
	if !gjson.GetBytes(out, "messages.1.tool_calls").Exists() {
		t.Fatalf("expected assistant tool_calls to be preserved, got %s", string(out))
	}
}
