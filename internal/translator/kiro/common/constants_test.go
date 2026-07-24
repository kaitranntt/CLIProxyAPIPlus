package common

import (
	"strings"
	"testing"
)

func TestWrapSystemPromptForInject_RewritesIdentityStatement(t *testing.T) {
	cases := []struct {
		name      string
		prompt    string
		original  string
		rewritten string
	}{
		{
			name:      "claude code",
			prompt:    "You are Claude Code, Anthropic's official CLI for Claude.\n\nTone and style: be concise.",
			original:  "You are Claude Code",
			rewritten: "The client application for this session is Claude Code.",
		},
		{
			name:      "codex",
			prompt:    "You are Codex, a coding agent.\nAlways run tests.",
			original:  "You are Codex",
			rewritten: "The client application for this session is Codex.",
		},
		{
			name:      "capitalized are",
			prompt:    "You Are Bitto, an AI pair programmer.\nBe helpful.",
			original:  "You Are Bitto",
			rewritten: "The client application for this session is Bitto.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := WrapSystemPromptForInject(tc.prompt)
			if strings.Contains(wrapped, tc.original) {
				t.Errorf("expected identity statement %q to be rewritten, got: %q", tc.original, wrapped)
			}
			if !strings.Contains(wrapped, tc.rewritten) {
				t.Errorf("expected third-person rewrite %q, got: %q", tc.rewritten, wrapped)
			}
		})
	}
}

func TestWrapSystemPromptForInject_KeepsBehavioralInstructions(t *testing.T) {
	prompt := "You MUST chunk large writes.\nYou are an interactive agent that edits files."
	wrapped := WrapSystemPromptForInject(prompt)

	if !strings.Contains(wrapped, "You MUST chunk large writes.") {
		t.Errorf("expected behavioral instructions to be untouched, got: %q", wrapped)
	}
	if !strings.Contains(wrapped, "You are an interactive agent that edits files.") {
		t.Errorf("expected non-product identity line to be untouched, got: %q", wrapped)
	}
}

func TestWrapSystemPromptForInject_NoIdentityStatement(t *testing.T) {
	prompt := "Always write idiomatic Go."
	wrapped := WrapSystemPromptForInject(prompt)

	if !strings.Contains(wrapped, "<system-reminder>\n"+prompt+"\n</system-reminder>") {
		t.Errorf("expected prompt to be wrapped verbatim, got: %q", wrapped)
	}
}

func TestWrapSystemPromptForInject_Structure(t *testing.T) {
	wrapped := WrapSystemPromptForInject("some instructions")

	if !strings.HasPrefix(wrapped, "<system-reminder>\n") {
		t.Errorf("expected bare system-reminder block with no lead-in, got: %q", wrapped)
	}
	if !strings.HasSuffix(wrapped, "\n</system-reminder>\n\n") {
		t.Errorf("expected closing system-reminder tag, got: %q", wrapped)
	}
}
