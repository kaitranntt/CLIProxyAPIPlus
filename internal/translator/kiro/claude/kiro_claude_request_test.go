package claude

import (
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// TestBuildKiroPayload_HistoryWithToolUseButNoTools reproduces the 400 case
// observed in production: a follow-up Claude request whose history contains
// a previous assistant tool_use turn, but whose top-level `tools` array was
// not re-attached by the client (e.g. OpenCode after compaction).
//
// Expected behavior: the resulting Kiro payload's
// currentMessage.userInputMessageContext.tools must be a non-empty array,
// because Kiro rejects requests with history tool turns and empty tools as
// "Improperly formed request".
func TestBuildKiroPayload_HistoryWithToolUseButNoTools(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "list files"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "ls"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "file1\nfile2"}
			]},
			{"role": "user", "content": "now what?"}
		]
	}`

	out, _ := BuildKiroPayload([]byte(claudeReq), "claude-sonnet-4-5", "arn:test", "test", true, false, http.Header{}, nil)
	if len(out) == 0 {
		t.Fatal("expected non-empty payload")
	}

	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if !tools.IsArray() {
		t.Fatalf("currentMessage.userInputMessageContext.tools is not an array: %s", tools.Raw)
	}
	if len(tools.Array()) == 0 {
		t.Fatalf("expected synthesized tools, got empty array. payload: %s", string(out))
	}
	// Confirm the synthesized stub references the historical tool name.
	found := false
	for _, t0 := range tools.Array() {
		if t0.Get("toolSpecification.name").String() == "Bash" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stub tool spec named 'Bash', got: %s", tools.Raw)
	}
}

// TestBuildKiroPayload_HistoryWithToolUseAndExplicitTools confirms that when
// the client DOES attach tools, we don't double-add stubs.
func TestBuildKiroPayload_HistoryWithToolUseAndExplicitTools(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 1024,
		"tools": [
			{"name": "Bash", "description": "real desc", "input_schema": {"type": "object", "properties": {"command": {"type": "string"}}}}
		],
		"messages": [
			{"role": "user", "content": "list files"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "ls"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "ok"}
			]},
			{"role": "user", "content": "next"}
		]
	}`

	out, _ := BuildKiroPayload([]byte(claudeReq), "claude-sonnet-4-5", "arn:test", "test", true, false, http.Header{}, nil)
	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if !tools.IsArray() || len(tools.Array()) != 1 {
		t.Fatalf("expected exactly 1 tool, got: %s", tools.Raw)
	}
	if got := tools.Array()[0].Get("toolSpecification.description").String(); got != "real desc" {
		t.Fatalf("expected real description preserved, got %q (likely overwritten by stub)", got)
	}
}

// TestBuildKiroPayload_NoToolsNoHistoryToolUse is the baseline: a plain text
// turn with no tool use anywhere should not introduce any tools.
func TestBuildKiroPayload_NoToolsNoHistoryToolUse(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"messages": [
			{"role": "user", "content": "hello"}
		]
	}`
	out, _ := BuildKiroPayload([]byte(claudeReq), "claude-sonnet-4-5", "arn:test", "test", false, true, http.Header{}, nil)
	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		t.Fatalf("did not expect tools to be synthesized for plain chat turn: %s", tools.Raw)
	}
}

// TestBuildKiroPayload_TrailingSystemMessageKeepsTools reproduces the case
// observed in production: Claude Code's mid-conversation-system beta appends a
// role:"system" message as the FINAL entry of the messages array. Without
// normalization the system message is skipped, no current user message is
// produced, and the converted tools are silently dropped from the payload —
// the model then hallucinates text-format tool calls.
//
// Expected behavior: the trailing system message is carried as user content
// and the client-declared tools land on
// currentMessage.userInputMessageContext.tools.
func TestBuildKiroPayload_TrailingSystemMessageKeepsTools(t *testing.T) {
	claudeReq := `{
		"model": "claude-opus-4-8",
		"max_tokens": 1024,
		"tools": [
			{"name": "Grep", "description": "search", "input_schema": {"type": "object", "properties": {"pattern": {"type": "string"}}}}
		],
		"messages": [
			{"role": "user", "content": "investigate the revision conflict"},
			{"role": "system", "content": "Available agent types for the Agent tool: ..."}
		]
	}`

	out, _ := BuildKiroPayload([]byte(claudeReq), "claude-opus-4-8", "arn:test", "test", true, false, http.Header{}, nil)
	if len(out) == 0 {
		t.Fatal("expected non-empty payload")
	}

	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if !tools.IsArray() || len(tools.Array()) != 1 {
		t.Fatalf("expected exactly 1 tool on currentMessage, got: %s", tools.Raw)
	}
	if got := tools.Array()[0].Get("toolSpecification.name").String(); got != "Grep" {
		t.Fatalf("expected tool named 'Grep', got %q", got)
	}

	content := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.content").String()
	if !strings.Contains(content, "investigate the revision conflict") {
		t.Fatalf("expected user text in current message content, got: %q", content)
	}
	if !strings.Contains(content, "Available agent types for the Agent tool") {
		t.Fatalf("expected system message text carried into current message content, got: %q", content)
	}
}

// TestSynthesizeToolSpecsFromHistory_Dedup ensures repeated tool names yield a
// single stub.
func TestSynthesizeToolSpecsFromHistory_Dedup(t *testing.T) {
	hist := []KiroHistoryMessage{
		{AssistantResponseMessage: &KiroAssistantResponseMessage{
			ToolUses: []KiroToolUse{{Name: "Bash"}, {Name: "Bash"}, {Name: "Read"}},
		}},
		{AssistantResponseMessage: &KiroAssistantResponseMessage{
			ToolUses: []KiroToolUse{{Name: "Read"}, {Name: "Edit"}},
		}},
	}
	got := synthesizeToolSpecsFromHistory(hist)
	if len(got) != 3 {
		t.Fatalf("expected 3 unique stubs, got %d: %+v", len(got), got)
	}
	names := []string{}
	for _, g := range got {
		names = append(names, g.ToolSpecification.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"Bash", "Read", "Edit"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in synthesized names %q", want, joined)
		}
	}
}

// TestExtractSystemPrompt_StripsClaudeCodeAttribution covers the Claude Code
// system layout: system[0] is the x-anthropic-billing-header attribution block,
// system[1] the product identity line. Both are dropped — the attribution
// block by util.IsClaudeCodeAttributionSystemText and the identity block by
// the "You are" prefix check — so only the remaining blocks survive.
func TestExtractSystemPrompt_StripsClaudeCodeAttribution(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"system": [
			{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.178.8ae; cc_entrypoint=cli; cch=abd35;"},
			{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."},
			{"type": "text", "text": "Tone and style: be concise."}
		],
		"messages": [{"role": "user", "content": "hi"}]
	}`

	got := extractSystemPrompt([]byte(claudeReq))
	if strings.Contains(got, "x-anthropic-billing-header") {
		t.Fatalf("attribution block was not stripped: %q", got)
	}
	if strings.Contains(got, "Claude Code") {
		t.Fatalf("identity block was not stripped: %q", got)
	}
	if got != "Tone and style: be concise." {
		t.Fatalf("only the behavioral block should survive, got: %q", got)
	}
}

// TestExtractSystemPrompt_StringAttributionOnly ensures a plain-string system
// field holding only the attribution block yields an empty prompt.
func TestExtractSystemPrompt_StringAttributionOnly(t *testing.T) {
	claudeReq := `{"system": "x-anthropic-billing-header: cc_version=1.0.abc; cch=00000;", "messages": []}`
	if got := extractSystemPrompt([]byte(claudeReq)); got != "" {
		t.Fatalf("expected empty system prompt, got %q", got)
	}
}

// TestNormalizeInArraySystemMessages_FiltersAgentData covers in-array
// role:"system" messages (Claude Code mid-conversation-system beta): they are
// rewritten as <system-reminder> user messages, and lines carrying
// first-party agent data must be filtered the same way as the top-level
// system field. The message structure is preserved even when filtering
// empties the content, since a trailing system message must still produce a
// current user message for tool specs to attach to.
func TestNormalizeInArraySystemMessages_FiltersAgentData(t *testing.T) {
	messages := []gjson.Result{
		gjson.Parse(`{"role": "system", "content": "You are Claude Code"}`),
		gjson.Parse(`{"role": "system", "content": "Note: the user opened main.go.\nYou are Claude Code, Anthropic's official CLI."}`),
		gjson.Parse(`{"role": "user", "content": "hi"}`),
	}

	got := normalizeInArraySystemMessages(messages)

	if len(got) != 3 {
		t.Fatalf("message count must be preserved, got %d", len(got))
	}
	first := got[0].Get("content.0.text").String()
	if !strings.Contains(first, "<system-reminder>") {
		t.Fatalf("system message must still be rewritten as a user message, got: %q", first)
	}
	if strings.Contains(first, "You are Claude Code") {
		t.Fatalf("identity-only system message was not filtered, got: %q", first)
	}
	second := got[1].Get("content.0.text").String()
	if strings.Contains(second, "You are Claude Code") {
		t.Fatalf("identity line inside a mixed system message was not filtered, got: %q", second)
	}
	if !strings.Contains(second, "Note: the user opened main.go.") {
		t.Fatalf("non-agent lines must survive filtering, got: %q", second)
	}
	if got[2].Get("role").String() != "user" {
		t.Fatalf("non-system messages must be untouched, got role: %q", got[2].Get("role").String())
	}
}

// TestExtractSystemPrompt_FiltersAgentIdentityString covers the Codex layout
// arriving via the Responses converter: a single string system field opening
// with the agent identity. Filtering is line by line, so the identity line is
// dropped while the sections after it survive.
func TestExtractSystemPrompt_FiltersAgentIdentityString(t *testing.T) {
	claudeReq := `{
		"model": "gpt-5",
		"system": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# Personality",
		"messages": [{"role": "user", "content": "hi"}]
	}`

	got := extractSystemPrompt([]byte(claudeReq))
	if strings.Contains(got, "Codex") {
		t.Fatalf("identity line was not stripped: %q", got)
	}
	if !strings.Contains(got, "# Personality") {
		t.Fatalf("sections after the identity line must survive: %q", got)
	}
}
