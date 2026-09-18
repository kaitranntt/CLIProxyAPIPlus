package util

import (
	"strings"
	"unicode"
)

const claudeCodeAttributionSystemPrefix = "x-anthropic-billing-header:"

// IsClaudeCodeAttributionSystemText reports whether text is the Claude Code
// attribution block that carries per-request billing and prompt fingerprint data.
func IsClaudeCodeAttributionSystemText(text string) bool {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	return strings.HasPrefix(text, claudeCodeAttributionSystemPrefix)
}

// IsAgentIdentitySystemText reports whether text opens with "You are" or
// mentions "Claude Code" anywhere, the shape of the agent self-identification
// that starts many CLI clients' system prompts ("You are Claude Code, ...",
// "You are Codex, ...") or refers back to it later in the same sentence.
// Upstream providers set their own model identity, so a second-person product
// identity injected into the request can conflict with it and trigger an
// injection-refusal preamble in the reply. The prefix check trims leading
// whitespace, mirroring IsClaudeCodeAttributionSystemText.
func IsAgentIdentitySystemText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "You are") || strings.Contains(trimmed, "Claude Code")
}

// splitSentences splits a line into sentences on ". " (period-space)
// boundaries, keeping the terminating period on the preceding sentence. This
// avoids breaking on dots that don't separate sentences, such as version
// numbers ("cc_version=2.1.178.8ae") or domain-like tokens, which never have
// a space right after the dot.
func splitSentences(line string) []string {
	var sentences []string
	rest := line
	for {
		idx := strings.Index(rest, ". ")
		if idx == -1 {
			return append(sentences, rest)
		}
		sentences = append(sentences, rest[:idx+1])
		rest = rest[idx+2:]
	}
}

// FilterAgentSystemLines removes sentences carrying first-party agent data —
// the Claude Code attribution header (IsClaudeCodeAttributionSystemText) and
// agent identity statements (IsAgentIdentitySystemText) — line by line,
// keeping any other sentence that shares the same line. A blank line is
// preserved as-is; a line whose every sentence is filtered out is dropped
// entirely rather than left as a stray blank line. Filtering is line by line
// so that content following a dropped line survives: clients may deliver the
// system prompt as one merged string, and a leading identity line must not
// take the rest of the prompt down with it.
func FilterAgentSystemLines(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			kept = append(kept, "")
			continue
		}
		sentences := splitSentences(line)
		keptSentences := make([]string, 0, len(sentences))
		for _, sentence := range sentences {
			if IsClaudeCodeAttributionSystemText(sentence) || IsAgentIdentitySystemText(sentence) {
				continue
			}
			keptSentences = append(keptSentences, sentence)
		}
		if len(keptSentences) == 0 {
			continue
		}
		kept = append(kept, strings.Join(keptSentences, " "))
	}
	return strings.Join(kept, "\n")
}
