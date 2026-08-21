package common

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/tidwall/gjson"
)

// DeriveClaudeUserID returns a stable value for the Claude request field
// metadata.user_id. It preserves any caller-supplied metadata.user_id or
// OpenAI Chat Completions user field, then derives a deterministic value from
// stable client signals (prompt_cache_key, conversation_id, first user message
// content, and the model/instructions). The same conversation therefore gets
// the same user_id on every worker and every turn, while different
// conversations get different values.
func DeriveClaudeUserID(rawJSON []byte) string {
	root := gjson.ParseBytes(rawJSON)

	if v := root.Get("metadata.user_id"); v.Exists() {
		if value := strings.TrimSpace(v.String()); value != "" {
			return value
		}
	}
	if v := root.Get("user"); v.Exists() {
		if value := strings.TrimSpace(v.String()); value != "" {
			return value
		}
	}

	var seed strings.Builder

	if v := root.Get("prompt_cache_key"); v.Exists() {
		if value := strings.TrimSpace(v.String()); value != "" {
			seed.WriteString("prompt_cache_key:")
			seed.WriteString(value)
		}
	}

	if seed.Len() == 0 {
		if v := root.Get("conversation_id"); v.Exists() {
			if value := strings.TrimSpace(v.String()); value != "" {
				seed.WriteString("conversation_id:")
				seed.WriteString(value)
			}
		}
	}

	if seed.Len() == 0 {
		if content := firstStableRequestContent(root); content != "" {
			seed.WriteString(content)
		}
	}

	if seed.Len() == 0 {
		if v := root.Get("model"); v.Exists() {
			if value := strings.TrimSpace(v.String()); value != "" {
				seed.WriteString("model:")
				seed.WriteString(value)
			}
		}
		if v := root.Get("instructions"); v.Exists() {
			seed.WriteString(";instructions:")
			seed.WriteString(v.String())
		}
		if v := root.Get("system"); v.Exists() {
			seed.WriteString(";system:")
			seed.WriteString(v.String())
		}
		if v := root.Get("system_instruction"); v.Exists() {
			seed.WriteString(";system_instruction:")
			seed.WriteString(v.String())
		}
	}

	if seed.Len() == 0 {
		return "unknown"
	}

	sum := sha256.Sum256([]byte(seed.String()))
	return hex.EncodeToString(sum[:])
}

func firstStableRequestContent(root gjson.Result) string {
	if messages := root.Get("messages"); messages.IsArray() {
		var content string
		messages.ForEach(func(_, message gjson.Result) bool {
			if message.Get("role").String() == "user" {
				content = extractTextContent(message.Get("content"))
				if content != "" {
					return false
				}
			}
			return true
		})
		if content != "" {
			return content
		}
	}

	if input := root.Get("input"); input.IsArray() {
		var content string
		input.ForEach(func(_, item gjson.Result) bool {
			if isResponsesUserItem(item) {
				content = extractResponsesItemText(item.Get("content"))
				if content != "" {
					return false
				}
			}
			return true
		})
		if content != "" {
			return content
		}
	}

	if contents := root.Get("contents"); contents.IsArray() {
		var content string
		contents.ForEach(func(_, contentItem gjson.Result) bool {
			if contentItem.Get("role").String() == "user" {
				if parts := contentItem.Get("parts"); parts.IsArray() {
					parts.ForEach(func(_, part gjson.Result) bool {
						if text := part.Get("text"); text.Exists() {
							content = text.String()
							return false
						}
						return true
					})
				}
				if content != "" {
					return false
				}
			}
			return true
		})
		if content != "" {
			return content
		}
	}

	return ""
}

func extractTextContent(content gjson.Result) string {
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String())
	}
	if !content.IsArray() {
		return ""
	}
	var texts []string
	content.ForEach(func(_, part gjson.Result) bool {
		if part.Get("type").String() == "text" {
			if text := part.Get("text"); text.Exists() {
				texts = append(texts, text.String())
			}
		}
		return true
	})
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

func isResponsesUserItem(item gjson.Result) bool {
	role := item.Get("role").String()
	typ := item.Get("type").String()
	if role == "user" {
		return true
	}
	if typ == "message" && role != "assistant" {
		return true
	}
	return false
}

func extractResponsesItemText(content gjson.Result) string {
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String())
	}
	if !content.IsArray() {
		return ""
	}
	var texts []string
	content.ForEach(func(_, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "input_text", "output_text", "text":
			if text := part.Get("text"); text.Exists() {
				texts = append(texts, text.String())
			}
		}
		return true
	})
	return strings.TrimSpace(strings.Join(texts, "\n"))
}
