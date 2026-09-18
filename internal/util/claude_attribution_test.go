package util

import "testing"

func TestIsClaudeCodeAttributionSystemText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "Claude Code attribution block",
			text: "x-anthropic-billing-header: cc_version=2.1.63.abc; cc_entrypoint=cli; cch=12345;",
			want: true,
		},
		{
			name: "leading whitespace",
			text: "\n\t x-anthropic-billing-header: cc_version=2.1.63.abc; cch=12345;",
			want: true,
		},
		{
			name: "regular system prompt",
			text: "You are helpful.",
			want: false,
		},
		{
			name: "empty text",
			text: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsClaudeCodeAttributionSystemText(tt.text); got != tt.want {
				t.Fatalf("IsClaudeCodeAttributionSystemText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsAgentIdentitySystemText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "claude code identity",
			text: "You are Claude Code, Anthropic's official CLI for Claude.",
			want: true,
		},
		{
			name: "codex merged prompt",
			text: "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# Personality",
			want: true,
		},
		{
			name: "leading whitespace",
			text: "  You are Bitto, an AI pair programmer.",
			want: true,
		},
		{
			name: "non-identity prompt",
			text: "Always write idiomatic Go.",
			want: false,
		},
		{
			name: "empty text",
			text: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAgentIdentitySystemText(tt.text); got != tt.want {
				t.Fatalf("IsAgentIdentitySystemText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestFilterAgentSystemLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "identity sentence dropped, sibling sentence on same line kept",
			text: "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# Personality",
			want: "You and the user share one workspace.\n\n# Personality",
		},
		{
			name: "attribution line in the middle dropped",
			text: "First instruction.\nx-anthropic-billing-header: cc_version=1.0.abc; cch=00000;\nSecond instruction.",
			want: "First instruction.\nSecond instruction.",
		},
		{
			name: "no agent lines",
			text: "Always write idiomatic Go.\nKeep it simple.",
			want: "Always write idiomatic Go.\nKeep it simple.",
		},
		{
			name: "all lines filtered",
			text: "x-anthropic-billing-header: cc_version=1.0.abc; cch=00000;",
			want: "",
		},
		{
			name: "empty text",
			text: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FilterAgentSystemLines(tt.text); got != tt.want {
				t.Fatalf("FilterAgentSystemLines(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
