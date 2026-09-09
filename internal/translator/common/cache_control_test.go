package common

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestAttachCacheControl_CopiesObject(t *testing.T) {
	src := gjson.Parse(`{"text":"hi","cache_control":{"type":"ephemeral","ttl":"5m"}}`)
	dst := []byte(`{"type":"text","text":"hi"}`)

	out := AttachCacheControl(dst, src)
	if got := gjson.GetBytes(out, "cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("cache_control.type = %q, want ephemeral; out=%s", got, out)
	}
	if got := gjson.GetBytes(out, "cache_control.ttl").String(); got != "5m" {
		t.Fatalf("cache_control.ttl = %q, want 5m; out=%s", got, out)
	}
}

func TestAttachCacheControl_IgnoresMissing(t *testing.T) {
	src := gjson.Parse(`{"text":"hi"}`)
	dst := []byte(`{"type":"text","text":"hi"}`)

	out := AttachCacheControl(dst, src)
	if gjson.GetBytes(out, "cache_control").Exists() {
		t.Fatalf("cache_control should be absent; out=%s", out)
	}
}

func TestAttachMessageCacheControl_PromotesStringContent(t *testing.T) {
	src := gjson.Parse(`{"role":"user","content":"hi","cache_control":{"type":"ephemeral"}}`)
	msg := []byte(`{"role":"user","content":"hi"}`)

	out := AttachMessageCacheControl(msg, src)
	if got := gjson.GetBytes(out, "content.0.type").String(); got != "text" {
		t.Fatalf("content.0.type = %q, want text; out=%s", got, out)
	}
	if got := gjson.GetBytes(out, "content.0.text").String(); got != "hi" {
		t.Fatalf("content.0.text = %q, want hi; out=%s", got, out)
	}
	if got := gjson.GetBytes(out, "content.0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("content.0.cache_control.type = %q, want ephemeral; out=%s", got, out)
	}
}

func TestAttachMessageCacheControl_SkipsWhenLastPartHasCacheControl(t *testing.T) {
	src := gjson.Parse(`{"cache_control":{"type":"ephemeral","ttl":"1h"}}`)
	msg := []byte(`{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}`)

	out := AttachMessageCacheControl(msg, src)
	if gjson.GetBytes(out, "content.0.cache_control.ttl").Exists() {
		t.Fatalf("part-level cache_control should win; out=%s", out)
	}
}

func TestModelSupportsExplicitPromptCache(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-5.6", true},
		{"gpt-5.6-2025-08-01", true},
		{"gpt-5.7", true},
		{"daybreak-mini", true},
		{"gpt-5.4", false},
		{"gpt-4.1", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ModelSupportsExplicitPromptCache(c.model); got != c.want {
			t.Fatalf("ModelSupportsExplicitPromptCache(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}

func TestNormalizeCodexServiceTier(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"priority"`, "priority"},
		{`"fast"`, "priority"},
		{`"auto"`, "auto"},
		{`"default"`, "default"},
		{`"flex"`, "flex"},
		{`"standard"`, "default"},
		{`"unknown"`, ""},
		{`true`, ""},
	}
	for _, c := range cases {
		src := gjson.Parse(c.in)
		if got := NormalizeCodexServiceTier(src); got != c.want {
			t.Fatalf("NormalizeCodexServiceTier(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAttachPromptCacheBreakpoint_CopiesObject(t *testing.T) {
	src := gjson.Parse(`{"text":"hi","cache_control":{"type":"ephemeral"}}`)
	dst := []byte(`{"type":"text","text":"hi"}`)

	out := AttachPromptCacheBreakpoint(dst, src)
	if got := gjson.GetBytes(out, "prompt_cache_breakpoint.mode").String(); got != "explicit" {
		t.Fatalf("prompt_cache_breakpoint.mode = %q, want explicit; out=%s", got, out)
	}
}

func TestAttachPromptCacheBreakpoint_SkipsExisting(t *testing.T) {
	src := gjson.Parse(`{"text":"hi","cache_control":{"type":"ephemeral"}}`)
	dst := []byte(`{"type":"text","text":"hi","prompt_cache_breakpoint":{"mode":"existing"}}`)

	out := AttachPromptCacheBreakpoint(dst, src)
	if got := gjson.GetBytes(out, "prompt_cache_breakpoint.mode").String(); got != "existing" {
		t.Fatalf("prompt_cache_breakpoint.mode = %q, want existing; out=%s", got, out)
	}
}

func TestAttachMessagePromptCacheBreakpoint_PromotesStringContent(t *testing.T) {
	src := gjson.Parse(`{"role":"user","content":"hi","cache_control":{"type":"ephemeral"}}`)
	msg := []byte(`{"role":"user","content":"hi"}`)

	out := AttachMessagePromptCacheBreakpoint(msg, src)
	if got := gjson.GetBytes(out, "content.0.type").String(); got != "text" {
		t.Fatalf("content.0.type = %q, want text; out=%s", got, out)
	}
	if got := gjson.GetBytes(out, "content.0.text").String(); got != "hi" {
		t.Fatalf("content.0.text = %q, want hi; out=%s", got, out)
	}
	if got := gjson.GetBytes(out, "content.0.prompt_cache_breakpoint.mode").String(); got != "explicit" {
		t.Fatalf("content.0.prompt_cache_breakpoint.mode = %q, want explicit; out=%s", got, out)
	}
}
