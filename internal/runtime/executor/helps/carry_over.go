package helps

import (
	"fmt"
	"strings"

	sigcompat "github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	carryOverLabel        = "Prior assistant reasoning (unverified context)"
	carryOverMaxBlocks    = 3
	carryOverMaxBlockSize = 4000
)

// CarryOverThinkingToSystem extracts reasoning_content from assistant messages
// in an OpenAI Chat Completions payload and rewrites it as a labeled system
// instruction. It drops assistant messages that become empty after the move.
// Existing first system message is extended; otherwise a new one is inserted.
//
// The function does not add reasoning to response bodies; it is only for
// request bodies being sent to a target without a canonical thought field.
func CarryOverThinkingToSystem(payload []byte) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}

	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return payload
	}

	var reasoningBlocks []string
	keptMessages := make([][]byte, 0, len(messages.Array()))

	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()

		var reasoning string
		if role == "assistant" {
			if rc := msg.Get("reasoning_content"); rc.Exists() && rc.Type == gjson.String {
				reasoning = rc.String()
			}
		}

		if reasoning != "" {
			reasoningBlocks = append(reasoningBlocks, reasoning)
		}

		updated := []byte(msg.Raw)
		if msg.Get("reasoning_content").Exists() {
			updated, _ = sjson.DeleteBytes(updated, "reasoning_content")
		}

		if role == "assistant" && !assistantMessageHasContent(updated) {
			return true
		}

		keptMessages = append(keptMessages, updated)
		return true
	})

	if len(reasoningBlocks) == 0 {
		return payload
	}

	systemText := carryOverLabel + ":\n\n" + formatCarryOverText(reasoningBlocks)

	if len(keptMessages) > 0 && gjson.GetBytes(keptMessages[0], "role").String() == "system" {
		keptMessages[0] = mergeCarryOverIntoSystemMessage(keptMessages[0], systemText)
	} else {
		systemMsg := []byte(`{"role":"system","content":""}`)
		systemMsg, _ = sjson.SetBytes(systemMsg, "content", systemText)
		keptMessages = append([][]byte{systemMsg}, keptMessages...)
	}

	return translatorcommon.SetRawArrayItems(payload, "messages", keptMessages)
}

func assistantMessageHasContent(msg []byte) bool {
	if gjson.GetBytes(msg, "tool_calls").IsArray() && len(gjson.GetBytes(msg, "tool_calls").Array()) > 0 {
		return true
	}

	c := gjson.GetBytes(msg, "content")
	if !c.Exists() || c.Type == gjson.Null {
		return false
	}

	if c.Type == gjson.String {
		return strings.TrimSpace(c.String()) != ""
	}

	if c.IsArray() && len(c.Array()) > 0 {
		for _, part := range c.Array() {
			if part.Get("type").String() == "text" {
				if strings.TrimSpace(part.Get("text").String()) != "" {
					return true
				}
			} else if part.Get("type").Exists() {
				return true
			}
		}
	}

	return false
}

func formatCarryOverText(blocks []string) string {
	omitted := 0
	if len(blocks) > carryOverMaxBlocks {
		omitted = len(blocks) - carryOverMaxBlocks
		blocks = blocks[len(blocks)-carryOverMaxBlocks:]
	}

	var parts []string
	if omitted > 0 {
		parts = append(parts, fmt.Sprintf("[... %d older reasoning block(s) omitted; showing the most recent %d.]", omitted, carryOverMaxBlocks))
	}

	for i, block := range blocks {
		if i > 0 || omitted > 0 {
			parts = append(parts, "")
		}

		runes := []rune(block)
		if len(runes) > carryOverMaxBlockSize {
			block = string(runes[:carryOverMaxBlockSize]) + "\n\n... [reasoning truncated]"
		}
		parts = append(parts, block)
	}

	return strings.Join(parts, "\n")
}

func newCarryOverTextPart(text string) []byte {
	part := []byte(`{"type":"text","text":""}`)
	part, _ = sjson.SetBytes(part, "text", text)
	return part
}

func mergeCarryOverIntoSystemMessage(msg []byte, carryOverText string) []byte {
	c := gjson.GetBytes(msg, "content")

	switch {
	case !c.Exists() || c.Type == gjson.Null:
		msg, _ = sjson.SetBytes(msg, "content", carryOverText)

	case c.Type == gjson.String:
		merged := carryOverText + "\n\n" + c.String()
		msg, _ = sjson.SetBytes(msg, "content", merged)

	case c.IsArray():
		items := [][]byte{newCarryOverTextPart(carryOverText)}
		c.ForEach(func(_, part gjson.Result) bool {
			switch {
			case part.IsObject():
				items = append(items, []byte(part.Raw))
			case part.Type == gjson.String:
				items = append(items, newCarryOverTextPart(part.String()))
			}
			return true
		})

		msg, _ = sjson.SetRawBytes(msg, "content", translatorcommon.JoinRawArray(items))

	default:
		msg, _ = sjson.SetBytes(msg, "content", carryOverText)
	}

	return msg
}

// carryOverClaudeSource extracts unsigned assistant thinking blocks from a
// Claude request and rewrites them as a top-level system instruction. Signed
// thinking with a compatible signature is left in place so the normal registry
// path can map it to reasoning_content. This runs before registry translation
// so plugin NormalizeRequest hooks still see a Claude-shaped payload.
func carryOverClaudeSource(payload []byte) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}

	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return payload
	}

	var blocks []string
	keptMessages := make([][]byte, 0, len(messages.Array()))

	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		if role != "assistant" {
			keptMessages = append(keptMessages, []byte(msg.Raw))
			return true
		}

		content := msg.Get("content")
		if !content.IsArray() {
			keptMessages = append(keptMessages, []byte(msg.Raw))
			return true
		}

		var keptParts [][]byte
		hasToolUse := false
		extractedFromThis := false

		content.ForEach(func(_, part gjson.Result) bool {
			partType := part.Get("type").String()
			if partType == "tool_use" {
				hasToolUse = true
			}
			if partType != "thinking" {
				if part.IsObject() {
					keptParts = append(keptParts, []byte(part.Raw))
				}
				return true
			}

			text := thinking.GetThinkingText(part)
			if strings.TrimSpace(text) == "" {
				return true
			}

			if isUnsignedClaudeThinking(part) {
				extractedFromThis = true
				blocks = append(blocks, text)
				return true
			}

			keptParts = append(keptParts, []byte(part.Raw))
			return true
		})

		if !extractedFromThis {
			keptMessages = append(keptMessages, []byte(msg.Raw))
			return true
		}

		if len(keptParts) == 0 && !hasToolUse {
			// assistant turn was only unsigned thinking; drop it
			return true
		}

		updated := []byte(msg.Raw)
		if len(keptParts) == 0 {
			updated, _ = sjson.SetRawBytes(updated, "content", []byte("[]"))
		} else {
			updated, _ = sjson.SetRawBytes(updated, "content", translatorcommon.JoinRawArray(keptParts))
		}
		keptMessages = append(keptMessages, updated)
		return true
	})

	if len(blocks) == 0 {
		return payload
	}

	systemText := carryOverLabel + ":\n\n" + formatCarryOverText(blocks)
	payload = injectClaudeCarryOverSystem(payload, systemText)
	return translatorcommon.SetRawArrayItems(payload, "messages", keptMessages)
}

func isUnsignedClaudeThinking(part gjson.Result) bool {
	sig := part.Get("signature").String()
	if strings.TrimSpace(sig) == "" {
		return true
	}
	_, ok := sigcompat.CompatibleSignatureForProvider(sigcompat.SignatureProviderGPT, sig)
	return !ok
}

func injectClaudeCarryOverSystem(payload []byte, carryOverText string) []byte {
	system := gjson.GetBytes(payload, "system")

	switch {
	case !system.Exists() || system.Type == gjson.Null:
		payload, _ = sjson.SetBytes(payload, "system", carryOverText)

	case system.Type == gjson.String:
		var merged string
		if strings.TrimSpace(system.String()) != "" {
			merged = carryOverText + "\n\n" + system.String()
		} else {
			merged = carryOverText
		}
		payload, _ = sjson.SetBytes(payload, "system", merged)

	case system.IsArray():
		items := [][]byte{newCarryOverTextPart(carryOverText)}
		system.ForEach(func(_, part gjson.Result) bool {
			switch {
			case part.IsObject():
				items = append(items, []byte(part.Raw))
			case part.Type == gjson.String:
				items = append(items, newCarryOverTextPart(part.String()))
			}
			return true
		})

		payload, _ = sjson.SetRawBytes(payload, "system", translatorcommon.JoinRawArray(items))

	default:
		payload, _ = sjson.SetBytes(payload, "system", carryOverText)
	}

	return payload
}
