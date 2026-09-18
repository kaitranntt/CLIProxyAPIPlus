package common

import (
	"strings"
	"testing"
)

func TestWrapSystemPromptForInject_PassesThroughVerbatim(t *testing.T) {
	prompt := "Always write idiomatic Go."
	wrapped := WrapSystemPromptForInject(prompt)

	if wrapped != prompt {
		t.Errorf("expected prompt to pass through verbatim, got: %q", wrapped)
	}
}

func TestWrapSystemPromptForInject_NoWrappingMarkers(t *testing.T) {
	wrapped := WrapSystemPromptForInject("some instructions")

	if strings.Contains(wrapped, "<system-reminder>") || strings.Contains(wrapped, "</system-reminder>") {
		t.Errorf("expected no system-reminder markers, got: %q", wrapped)
	}
}
