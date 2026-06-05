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

// TestBuildKiroPayload_MidConversationSystem reproduces the production 400
// ("Improperly formed request") triggered by Anthropic's
// mid-conversation-system-2026-04-07 beta: the messages array contains
// role:"system" entries interleaved with user/assistant turns, and the LAST
// message is a system message. Without folding, the trailing system breaks
// isLastMessage detection so currentMessage becomes a placeholder and history
// ends with a user turn, which Kiro rejects.
//
// Expected: the real last user turn becomes currentMessage, the trailing system
// text is folded onto it, and no content is dropped.
func TestBuildKiroPayload_MidConversationSystem(t *testing.T) {
	claudeReq := `{
		"model": "claude-opus-4",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "system", "content": "SKILL_LIST_MARKER"},
			{"role": "assistant", "content": [{"type": "text", "text": "ok"}]},
			{"role": "user", "content": "do the thing"},
			{"role": "system", "content": "DATE_CHANGED_MARKER"}
		]
	}`

	out, _ := BuildKiroPayload([]byte(claudeReq), "claude-opus-4", "arn:test", "test", true, false, http.Header{}, nil)
	if len(out) == 0 {
		t.Fatal("expected non-empty payload")
	}

	currentContent := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.content").String()
	// The real last user turn must drive currentMessage (not a placeholder).
	if !strings.Contains(currentContent, "do the thing") {
		t.Fatalf("expected last user turn as currentMessage, got: %q", currentContent)
	}
	// The trailing system text must be folded onto the last user turn, not dropped.
	if !strings.Contains(currentContent, "DATE_CHANGED_MARKER") {
		t.Fatalf("expected trailing system text folded into currentMessage, got: %q", currentContent)
	}

	// The earlier system text must survive too: it should be folded onto the
	// preceding user turn ("hi") which is now in history.
	allOut := string(out)
	if !strings.Contains(allOut, "SKILL_LIST_MARKER") {
		t.Fatalf("expected earlier system text preserved somewhere in payload, got: %s", allOut)
	}

	// History must not end with a dangling user turn caused by misdetection:
	// with proper folding, the last history entry is the assistant response.
	history := gjson.GetBytes(out, "conversationState.history").Array()
	if len(history) == 0 {
		t.Fatal("expected non-empty history")
	}
	last := history[len(history)-1]
	if last.Get("assistantResponseMessage").Exists() == false {
		t.Fatalf("expected history to end with assistant response, got: %s", last.Raw)
	}
}

// TestBuildKiroPayload_LeadingSystemMessage covers a system message that has no
// preceding user turn: its text must fold forward onto the following user turn.
func TestBuildKiroPayload_LeadingSystemMessage(t *testing.T) {
	claudeReq := `{
		"model": "claude-opus-4",
		"max_tokens": 256,
		"messages": [
			{"role": "system", "content": "LEADING_SYSTEM_MARKER"},
			{"role": "user", "content": "question"}
		]
	}`
	out, _ := BuildKiroPayload([]byte(claudeReq), "claude-opus-4", "arn:test", "test", false, true, http.Header{}, nil)
	currentContent := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.content").String()
	if !strings.Contains(currentContent, "question") {
		t.Fatalf("expected user turn in currentMessage, got: %q", currentContent)
	}
	if !strings.Contains(currentContent, "LEADING_SYSTEM_MARKER") {
		t.Fatalf("expected leading system text folded forward into user turn, got: %q", currentContent)
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
