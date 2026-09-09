package common

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// AttachCacheControl copies a Claude-compatible cache_control object from src onto dst.
// Returns dst unchanged when cache_control is missing or not an object.
func AttachCacheControl(dst []byte, src gjson.Result) []byte {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return dst
	}
	out, err := sjson.SetRawBytes(dst, "cache_control", []byte(cc.Raw))
	if err != nil {
		return dst
	}
	return out
}

// AttachMessageCacheControl applies message-level cache_control onto the last content block.
// Part-level cache_control wins when the last block already has one.
// String content is promoted to a content array so Claude can accept cache_control.
func AttachMessageCacheControl(msg []byte, src gjson.Result) []byte {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return msg
	}

	content := gjson.GetBytes(msg, "content")
	if content.IsArray() {
		arr := content.Array()
		if len(arr) == 0 {
			return msg
		}
		lastIdx := len(arr) - 1
		if arr[lastIdx].Get("cache_control").Exists() {
			return msg
		}
		path := fmt.Sprintf("content.%d.cache_control", lastIdx)
		out, err := sjson.SetRawBytes(msg, path, []byte(cc.Raw))
		if err != nil {
			return msg
		}
		return out
	}

	if content.Type != gjson.String {
		return msg
	}

	textPart := []byte(`{"type":"text","text":""}`)
	textPart, _ = sjson.SetBytes(textPart, "text", content.String())
	textPart, errSet := sjson.SetRawBytes(textPart, "cache_control", []byte(cc.Raw))
	if errSet != nil {
		return msg
	}
	out, err := sjson.SetRawBytes(msg, "content", []byte("[]"))
	if err != nil {
		return msg
	}
	out, _ = sjson.SetRawBytes(out, "content.-1", textPart)
	return out
}

// modelSupportsExplicitPromptCachePattern matches model families that OpenAI
// documents as supporting explicit prompt_cache_breakpoint and
// prompt_cache_options (gpt-5.6 and later, plus daybreak aliases).
// gpt-5.6 is the first family with explicit cache support; earlier families
// rely on implicit caching and may reject explicit breakpoints.
var modelSupportsExplicitPromptCachePattern = regexp.MustCompile(`^(?:gpt-5\.(?:[6-9]|[1-9][0-9])(?:-|$)|daybreak-|gpt-[6-9])`)

// ModelSupportsExplicitPromptCache reports whether a model name indicates
// support for explicit prompt_cache_breakpoint and prompt_cache_options.
func ModelSupportsExplicitPromptCache(modelName string) bool {
	return modelSupportsExplicitPromptCachePattern.MatchString(strings.ToLower(strings.TrimSpace(modelName)))
}

// NormalizeCodexServiceTier maps a requested service_tier to a value Codex
// (OpenAI) accepts. "fast" and "priority" both resolve to "priority"; "auto",
// "default", and "flex" pass through lowercased; "standard" maps to "default".
// Unknown or non-string values return an empty string so the field is omitted.
//
// See https://platform.openai.com/docs/api-reference/chat/create#chat-create-service_tier
// and https://platform.openai.com/api/docs/guides/fast-mode.
func NormalizeCodexServiceTier(result gjson.Result) string {
	if !result.Exists() || result.Type != gjson.String {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(result.String())) {
	case "fast", "priority":
		return "priority"
	case "auto", "default", "flex":
		return strings.ToLower(strings.TrimSpace(result.String()))
	case "standard":
		return "default"
	default:
		return ""
	}
}

// CopyPromptCacheBreakpoint copies a pre-existing prompt_cache_breakpoint from
// src onto dst. Used when both source and target already speak the OpenAI/Codex
// Responses format (e.g. Chat Completions -> Responses, Responses -> Codex).
// Returns dst unchanged when prompt_cache_breakpoint is missing or not an object.
func CopyPromptCacheBreakpoint(dst []byte, src gjson.Result) []byte {
	if gjson.GetBytes(dst, "prompt_cache_breakpoint").Exists() {
		return dst
	}
	bp := src.Get("prompt_cache_breakpoint")
	if !bp.Exists() || bp.Type == gjson.Null || !bp.IsObject() {
		return dst
	}
	out, err := sjson.SetRawBytes(dst, "prompt_cache_breakpoint", []byte(bp.Raw))
	if err != nil {
		return dst
	}
	return out
}

// AttachPromptCacheBreakpoint maps a Claude-compatible cache_control object
// from src onto dst as an OpenAI/Codex prompt_cache_breakpoint.
// Returns dst unchanged when cache_control is missing or not an object, or when
// dst already carries a prompt_cache_breakpoint.
func AttachPromptCacheBreakpoint(dst []byte, src gjson.Result) []byte {
	if gjson.GetBytes(dst, "prompt_cache_breakpoint").Exists() {
		return dst
	}
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return dst
	}
	out, err := sjson.SetRawBytes(dst, "prompt_cache_breakpoint", []byte(`{"mode":"explicit"}`))
	if err != nil {
		return dst
	}
	return out
}

// AttachMessagePromptCacheBreakpoint applies a message-level cache_control from
// src onto the last content block of msg as an OpenAI/Codex prompt_cache_breakpoint.
// Part-level prompt_cache_breakpoint wins when the last block already has one.
// String content is promoted to a content array.
func AttachMessagePromptCacheBreakpoint(msg []byte, src gjson.Result) []byte {
	cc := src.Get("cache_control")
	if !cc.Exists() || cc.Type == gjson.Null || !cc.IsObject() {
		return msg
	}

	content := gjson.GetBytes(msg, "content")
	if content.IsArray() {
		arr := content.Array()
		if len(arr) == 0 {
			return msg
		}
		lastIdx := len(arr) - 1
		if arr[lastIdx].Get("prompt_cache_breakpoint").Exists() {
			return msg
		}
		path := fmt.Sprintf("content.%d.prompt_cache_breakpoint", lastIdx)
		out, err := sjson.SetRawBytes(msg, path, []byte(`{"mode":"explicit"}`))
		if err != nil {
			return msg
		}
		return out
	}

	if content.Type != gjson.String {
		return msg
	}

	textPart := []byte(`{"type":"text","text":""}`)
	textPart, _ = sjson.SetBytes(textPart, "text", content.String())
	textPart, errSet := sjson.SetRawBytes(textPart, "prompt_cache_breakpoint", []byte(`{"mode":"explicit"}`))
	if errSet != nil {
		return msg
	}
	out, err := sjson.SetRawBytes(msg, "content", []byte("[]"))
	if err != nil {
		return msg
	}
	out, _ = sjson.SetRawBytes(out, "content.-1", textPart)
	return out
}
