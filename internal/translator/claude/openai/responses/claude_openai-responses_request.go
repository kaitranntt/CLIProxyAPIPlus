package responses

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	sigcompat "github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	user    = ""
	account = ""
	session = ""
)

// ConvertOpenAIResponsesRequestToClaude transforms an OpenAI Responses API request
// into a Claude Messages API request using only gjson/sjson for JSON handling.
// It supports:
// - instructions -> system message
// - input[].type==message with input_text/output_text -> user/assistant messages
// - function_call -> assistant tool_use
// - function_call_output -> user tool_result
// - tools[].parameters -> tools[].input_schema
// - max_output_tokens -> max_tokens
// - stream passthrough via parameter
func ConvertOpenAIResponsesRequestToClaude(modelName string, inputRawJSON []byte, stream bool) []byte {
	rawJSON := inputRawJSON

	if account == "" {
		u, _ := uuid.NewRandom()
		account = u.String()
	}
	if session == "" {
		u, _ := uuid.NewRandom()
		session = u.String()
	}
	if user == "" {
		sum := sha256.Sum256([]byte(account + session))
		user = hex.EncodeToString(sum[:])
	}
	userID := fmt.Sprintf("user_%s_account_%s_session_%s", user, account, session)

	// Base Claude message payload
	out := []byte(fmt.Sprintf(`{"model":"","max_tokens":32000,"messages":[],"metadata":{"user_id":"%s"}}`, userID))

	root := gjson.ParseBytes(rawJSON)

	// Convert OpenAI Responses reasoning.effort to Claude thinking config.
	if v := root.Get("reasoning.effort"); v.Exists() {
		effort := strings.ToLower(strings.TrimSpace(v.String()))
		if effort != "" {
			mi := registry.LookupModelInfo(modelName, "claude")
			supportsAdaptive := mi != nil && mi.Thinking != nil && len(mi.Thinking.Levels) > 0
			supportsMax := supportsAdaptive && thinking.HasLevel(mi.Thinking.Levels, string(thinking.LevelMax))

			// Claude 4.6 supports adaptive thinking with output_config.effort.
			// MapToClaudeEffort normalizes levels (e.g. minimal→low, xhigh→high) to avoid
			// validation errors since validate treats same-provider unsupported levels as errors.
			if supportsAdaptive {
				switch effort {
				case "none":
					out, _ = sjson.SetBytes(out, "thinking.type", "disabled")
					out, _ = sjson.DeleteBytes(out, "thinking.budget_tokens")
					out, _ = sjson.DeleteBytes(out, "output_config.effort")
				case "auto":
					out, _ = sjson.SetBytes(out, "thinking.type", "adaptive")
					out, _ = sjson.DeleteBytes(out, "thinking.budget_tokens")
					out, _ = sjson.DeleteBytes(out, "output_config.effort")
				default:
					if mapped, ok := thinking.MapToClaudeEffort(effort, supportsMax); ok {
						effort = mapped
					}
					out, _ = sjson.SetBytes(out, "thinking.type", "adaptive")
					out, _ = sjson.DeleteBytes(out, "thinking.budget_tokens")
					out, _ = sjson.SetBytes(out, "output_config.effort", effort)
				}
			} else {
				// Legacy/manual thinking (budget_tokens).
				budget, ok := thinking.ConvertLevelToBudget(effort)
				if ok {
					switch budget {
					case 0:
						out, _ = sjson.SetBytes(out, "thinking.type", "disabled")
					case -1:
						out, _ = sjson.SetBytes(out, "thinking.type", "enabled")
					default:
						if budget > 0 {
							out, _ = sjson.SetBytes(out, "thinking.type", "enabled")
							out, _ = sjson.SetBytes(out, "thinking.budget_tokens", budget)
						}
					}
				}
			}
		}
	}

	// Helper for generating tool call IDs when missing
	genToolCallID := func() string {
		const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		var b strings.Builder
		for i := 0; i < 24; i++ {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
			b.WriteByte(letters[n.Int64()])
		}
		return "toolu_" + b.String()
	}

	// Model
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Max tokens
	if mot := root.Get("max_output_tokens"); mot.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", mot.Int())
	}

	// Stream
	out, _ = sjson.SetBytes(out, "stream", stream)

	// instructions -> as a leading message (use role user for Claude API compatibility)
	instructionsText := ""
	extractedFromSystem := false
	if instr := root.Get("instructions"); instr.Exists() && instr.Type == gjson.String {
		instructionsText = instr.String()
		if instructionsText != "" {
			sysMsg := []byte(`{"role":"user","content":""}`)
			sysMsg, _ = sjson.SetBytes(sysMsg, "content", instructionsText)
			out, _ = sjson.SetRawBytes(out, "messages.-1", sysMsg)
		}
	}

	if instructionsText == "" {
		if input := root.Get("input"); input.Exists() && input.IsArray() {
			input.ForEach(func(_, item gjson.Result) bool {
				if strings.EqualFold(item.Get("role").String(), "system") {
					var builder strings.Builder
					if parts := item.Get("content"); parts.Exists() && parts.IsArray() {
						parts.ForEach(func(_, part gjson.Result) bool {
							textResult := part.Get("text")
							text := textResult.String()
							if builder.Len() > 0 && text != "" {
								builder.WriteByte('\n')
							}
							builder.WriteString(text)
							return true
						})
					} else if parts.Type == gjson.String {
						builder.WriteString(parts.String())
					}
					instructionsText = builder.String()
					if instructionsText != "" {
						sysMsg := []byte(`{"role":"user","content":""}`)
						sysMsg, _ = sjson.SetBytes(sysMsg, "content", instructionsText)
						out, _ = sjson.SetRawBytes(out, "messages.-1", sysMsg)
						extractedFromSystem = true
					}
				}
				return instructionsText == ""
			})
		}
	}

	// input array processing
	var pendingReasoningParts []string
	type pendingToolUseMessage struct {
		callID string
		raw    []byte
	}
	var pendingToolUseMessages []pendingToolUseMessage
	appendMessage := func(msg []byte) {
		out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	}
	flushPendingReasoning := func() {
		if len(pendingReasoningParts) == 0 {
			return
		}
		asst := []byte(`{"role":"assistant","content":[]}`)
		for _, partJSON := range pendingReasoningParts {
			asst, _ = sjson.SetRawBytes(asst, "content.-1", []byte(partJSON))
		}
		appendMessage(asst)
		pendingReasoningParts = nil
	}
	flushPendingToolUses := func() {
		for _, pending := range pendingToolUseMessages {
			appendMessage(pending.raw)
		}
		pendingToolUseMessages = nil
	}
	flushPendingToolUseFor := func(callID string) {
		if len(pendingToolUseMessages) == 0 {
			return
		}
		for i, pending := range pendingToolUseMessages {
			if pending.callID == callID {
				appendMessage(pending.raw)
				pendingToolUseMessages = append(pendingToolUseMessages[:i], pendingToolUseMessages[i+1:]...)
				return
			}
		}
		flushPendingToolUses()
	}

	// The Responses API also accepts a plain string input as shorthand for a
	// single user message. Without this branch the input would be dropped
	// silently, leaving downstream providers with an empty conversation.
	if input := root.Get("input"); input.Exists() && input.Type == gjson.String {
		if text := input.String(); text != "" {
			msg := []byte(`{"role":"user","content":""}`)
			msg, _ = sjson.SetBytes(msg, "content", text)
			appendMessage(msg)
		}
	}

	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if extractedFromSystem && strings.EqualFold(item.Get("role").String(), "system") {
				return true
			}
			typ := item.Get("type").String()
			if typ == "" && item.Get("role").String() != "" {
				typ = "message"
			}
			switch typ {
			case "message":
				// Determine role and construct Claude-compatible content parts.
				var role string
				var textAggregate strings.Builder
				var partsJSON []string
				hasImage := false
				hasFile := false
				if parts := item.Get("content"); parts.Exists() && parts.IsArray() {
					parts.ForEach(func(_, part gjson.Result) bool {
						ptype := part.Get("type").String()
						switch ptype {
						case "input_text", "output_text":
							if t := part.Get("text"); t.Exists() {
								txt := t.String()
								textAggregate.WriteString(txt)
								contentPart := []byte(`{"type":"text","text":""}`)
								contentPart, _ = sjson.SetBytes(contentPart, "text", txt)
								contentPart = common.AttachCacheControl(contentPart, part)
								partsJSON = append(partsJSON, string(contentPart))
							}
							if ptype == "input_text" {
								role = "user"
							} else {
								role = "assistant"
							}
						case "input_image":
							url := part.Get("image_url").String()
							if url == "" {
								url = part.Get("url").String()
							}
							if url != "" {
								var contentPart []byte
								if strings.HasPrefix(url, "data:") {
									trimmed := strings.TrimPrefix(url, "data:")
									mediaAndData := strings.SplitN(trimmed, ";base64,", 2)
									mediaType := "application/octet-stream"
									data := ""
									if len(mediaAndData) == 2 {
										if mediaAndData[0] != "" {
											mediaType = mediaAndData[0]
										}
										data = mediaAndData[1]
									}
									if data != "" {
										contentPart = []byte(`{"type":"image","source":{"type":"base64","media_type":"","data":""}}`)
										contentPart, _ = sjson.SetBytes(contentPart, "source.media_type", mediaType)
										contentPart, _ = sjson.SetBytes(contentPart, "source.data", data)
									}
								} else {
									contentPart = []byte(`{"type":"image","source":{"type":"url","url":""}}`)
									contentPart, _ = sjson.SetBytes(contentPart, "source.url", url)
								}
								if len(contentPart) > 0 {
									contentPart = common.AttachCacheControl(contentPart, part)
									partsJSON = append(partsJSON, string(contentPart))
									if role == "" {
										role = "user"
									}
									hasImage = true
								}
							}
						case "input_file":
							fileData := part.Get("file_data").String()
							if fileData != "" {
								mediaType := "application/octet-stream"
								data := fileData
								if strings.HasPrefix(fileData, "data:") {
									trimmed := strings.TrimPrefix(fileData, "data:")
									mediaAndData := strings.SplitN(trimmed, ";base64,", 2)
									if len(mediaAndData) == 2 {
										if mediaAndData[0] != "" {
											mediaType = mediaAndData[0]
										}
										data = mediaAndData[1]
									}
								}
								contentPart := []byte(`{"type":"document","source":{"type":"base64","media_type":"","data":""}}`)
								contentPart, _ = sjson.SetBytes(contentPart, "source.media_type", mediaType)
								contentPart, _ = sjson.SetBytes(contentPart, "source.data", data)
								contentPart = common.AttachCacheControl(contentPart, part)
								partsJSON = append(partsJSON, string(contentPart))
								if role == "" {
									role = "user"
								}
								hasFile = true
							}
						}
						return true
					})
				} else if parts.Type == gjson.String {
					textAggregate.WriteString(parts.String())
				}

				// Fallback to given role if content types not decisive
				if role == "" {
					r := item.Get("role").String()
					switch r {
					case "user", "assistant", "system":
						role = r
					default:
						role = "user"
					}
				}

				hasReasoningParts := false
				if role != "assistant" {
					flushPendingToolUses()
				}
				if len(pendingReasoningParts) > 0 {
					if role == "assistant" {
						if len(partsJSON) == 0 && textAggregate.Len() > 0 {
							contentPart := []byte(`{"type":"text","text":""}`)
							contentPart, _ = sjson.SetBytes(contentPart, "text", textAggregate.String())
							partsJSON = append(partsJSON, string(contentPart))
						}
						partsJSON = append(append([]string{}, pendingReasoningParts...), partsJSON...)
						pendingReasoningParts = nil
						hasReasoningParts = true
					} else {
						flushPendingReasoning()
					}
				}

				if len(partsJSON) > 0 {
					msg := []byte(`{"role":"","content":[]}`)
					msg, _ = sjson.SetBytes(msg, "role", role)
					textPart := gjson.Parse(partsJSON[0])
					hasPartCacheControl := textPart.Get("cache_control").Exists()
					if len(partsJSON) == 1 && !hasImage && !hasFile && !hasReasoningParts && !hasPartCacheControl && !item.Get("cache_control").Exists() {
						// Preserve legacy behavior for single text content without cache markers.
						msg, _ = sjson.DeleteBytes(msg, "content")
						msg, _ = sjson.SetBytes(msg, "content", textPart.Get("text").String())
					} else {
						for _, partJSON := range partsJSON {
							msg, _ = sjson.SetRawBytes(msg, "content.-1", []byte(partJSON))
						}
					}
					msg = common.AttachMessageCacheControl(msg, item)
					appendMessage(msg)
				} else if textAggregate.Len() > 0 || role == "system" {
					msg := []byte(`{"role":"","content":""}`)
					msg, _ = sjson.SetBytes(msg, "role", role)
					msg, _ = sjson.SetBytes(msg, "content", textAggregate.String())
					msg = common.AttachMessageCacheControl(msg, item)
					appendMessage(msg)
				}

			case "reasoning":
				if thinkingPart := convertResponsesReasoningToClaudeThinking(item); len(thinkingPart) > 0 {
					pendingReasoningParts = append(pendingReasoningParts, string(thinkingPart))
				}

			case "function_call":
				// Map to assistant tool_use
				callID := item.Get("call_id").String()
				if callID == "" {
					callID = genToolCallID()
				}
				callID = util.SanitizeClaudeToolID(callID)
				name := item.Get("name").String()
				argsStr := item.Get("arguments").String()

				toolUse := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
				toolUse, _ = sjson.SetBytes(toolUse, "id", callID)
				toolUse, _ = sjson.SetBytes(toolUse, "name", name)
				if argsStr != "" && gjson.Valid(argsStr) {
					argsJSON := gjson.Parse(argsStr)
					if argsJSON.IsObject() {
						toolUse, _ = sjson.SetRawBytes(toolUse, "input", []byte(argsJSON.Raw))
					}
				}

				asst := []byte(`{"role":"assistant","content":[]}`)
				for _, partJSON := range pendingReasoningParts {
					asst, _ = sjson.SetRawBytes(asst, "content.-1", []byte(partJSON))
				}
				pendingReasoningParts = nil
				asst, _ = sjson.SetRawBytes(asst, "content.-1", toolUse)
				pendingToolUseMessages = append(pendingToolUseMessages, pendingToolUseMessage{
					callID: callID,
					raw:    asst,
				})

			case "custom_tool_call":
				// Freeform custom tool calls carry raw text input. Wrap it in the
				// {"input": ...} envelope produced by convertResponsesCustomToolToClaude
				// so the pair round-trips through JSON-only providers.
				callID := item.Get("call_id").String()
				if callID == "" {
					callID = genToolCallID()
				}
				callID = util.SanitizeClaudeToolID(callID)
				name := item.Get("name").String()

				toolUse := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
				toolUse, _ = sjson.SetBytes(toolUse, "id", callID)
				toolUse, _ = sjson.SetBytes(toolUse, "name", name)
				toolUse, _ = sjson.SetBytes(toolUse, "input.input", item.Get("input").String())

				asst := []byte(`{"role":"assistant","content":[]}`)
				for _, partJSON := range pendingReasoningParts {
					asst, _ = sjson.SetRawBytes(asst, "content.-1", []byte(partJSON))
				}
				pendingReasoningParts = nil
				asst, _ = sjson.SetRawBytes(asst, "content.-1", toolUse)
				pendingToolUseMessages = append(pendingToolUseMessages, pendingToolUseMessage{
					callID: callID,
					raw:    asst,
				})

			case "function_call_output", "custom_tool_call_output":
				flushPendingReasoning()
				// Map to user tool_result. Both output item kinds share the
				// call_id/output shape; custom_tool_call_output is emitted for
				// freeform custom tools.
				callID := item.Get("call_id").String()
				callID = util.SanitizeClaudeToolID(callID)
				flushPendingToolUseFor(callID)
				output := item.Get("output")
				toolResult := []byte(`{"type":"tool_result","tool_use_id":"","content":""}`)
				toolResult, _ = sjson.SetBytes(toolResult, "tool_use_id", callID)
				toolResult = applyResponsesToolResultContent(toolResult, output)

				usr := []byte(`{"role":"user","content":[]}`)
				usr, _ = sjson.SetRawBytes(usr, "content.-1", toolResult)
				appendMessage(usr)
			}
			return true
		})
	}
	flushPendingReasoning()
	flushPendingToolUses()

	includedToolNames := map[string]struct{}{}
	toolNameMap := map[string]string{}

	// tools mapping: parameters -> input_schema
	// Tool declarations come from both the top-level tools array and codex's
	// responses-lite input[].type==additional_tools items — skipping the latter
	// leaves the model with no tools at all.
	{
		toolsJSON := []byte("[]")
		toolCount := 0
		forEachResponsesTool(rawJSON, func(tool gjson.Result) bool {
			toolCount++
			convertedTools := convertResponsesToolToClaudeTools(tool, toolNameMap)
			for _, tJSON := range convertedTools {
				toolName := gjson.GetBytes(tJSON, "name").String()
				if toolName != "" {
					includedToolNames[toolName] = struct{}{}
				}
				toolsJSON, _ = sjson.SetRawBytes(toolsJSON, "-1", tJSON)
			}
			return true
		})
		if toolCount > 0 {
			if parsedTools := gjson.ParseBytes(toolsJSON); parsedTools.IsArray() && len(parsedTools.Array()) > 0 {
				out, _ = sjson.SetRawBytes(out, "tools", toolsJSON)
			}
		}
	}

	// Map tool_choice similar to Chat Completions translator (optional in docs, safe to handle)
	if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
		switch toolChoice.Type {
		case gjson.String:
			switch toolChoice.String() {
			case "auto":
				out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(`{"type":"auto"}`))
			case "none":
				// Leave unset; implies no tools
			case "required":
				if len(includedToolNames) > 0 {
					out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(`{"type":"any"}`))
				}
			}
		case gjson.JSON:
			if toolChoice.Get("type").String() == "function" {
				fn := toolChoice.Get("function.name").String()
				if fn == "" {
					fn = toolChoice.Get("name").String()
				}
				if mappedName := toolNameMap[fn]; mappedName != "" {
					fn = mappedName
				}
				if _, ok := includedToolNames[fn]; ok {
					toolChoiceJSON := []byte(`{"name":"","type":"tool"}`)
					toolChoiceJSON, _ = sjson.SetBytes(toolChoiceJSON, "name", fn)
					out, _ = sjson.SetRawBytes(out, "tool_choice", toolChoiceJSON)
				}
			}
		default:

		}
	}

	return out
}

func convertResponsesReasoningToClaudeThinking(item gjson.Result) []byte {
	signature, ok := sigcompat.CompatibleSignatureForProvider(sigcompat.SignatureProviderClaude, item.Get("encrypted_content").String())
	if !ok {
		return nil
	}

	thinkingText := responsesReasoningSummaryText(item)
	thinkingPart := []byte(`{"type":"thinking","thinking":"","signature":""}`)
	thinkingPart, _ = sjson.SetBytes(thinkingPart, "thinking", thinkingText)
	thinkingPart, _ = sjson.SetBytes(thinkingPart, "signature", signature)
	return thinkingPart
}

func responsesReasoningSummaryText(item gjson.Result) string {
	var builder strings.Builder
	if summary := item.Get("summary"); summary.Exists() && summary.IsArray() {
		summary.ForEach(func(_, part gjson.Result) bool {
			if text := part.Get("text"); text.Exists() {
				builder.WriteString(text.String())
			} else if part.Type == gjson.String {
				builder.WriteString(part.String())
			}
			return true
		})
	}
	return builder.String()
}

func applyResponsesToolResultContent(toolResult []byte, output gjson.Result) []byte {
	if output.Exists() && output.IsArray() {
		var partsJSON []string
		hasImage := false
		hasFile := false
		output.ForEach(func(_, part gjson.Result) bool {
			if partJSON := convertResponsesContentPartToClaude(part); len(partJSON) > 0 {
				partsJSON = append(partsJSON, string(partJSON))
				partType := gjson.ParseBytes(partJSON).Get("type").String()
				if partType == "image" {
					hasImage = true
				}
				if partType == "document" {
					hasFile = true
				}
			}
			return true
		})
		if len(partsJSON) == 0 {
			toolResult, _ = sjson.SetBytes(toolResult, "content", output.Raw)
			return toolResult
		}
		if len(partsJSON) == 1 && !hasImage && !hasFile {
			textPart := gjson.Parse(partsJSON[0])
			if textPart.Get("type").String() == "text" {
				toolResult, _ = sjson.SetBytes(toolResult, "content", textPart.Get("text").String())
				return toolResult
			}
		}
		contentJSON := []byte("[]")
		for _, partJSON := range partsJSON {
			contentJSON, _ = sjson.SetRawBytes(contentJSON, "-1", []byte(partJSON))
		}
		toolResult, _ = sjson.DeleteBytes(toolResult, "content")
		toolResult, _ = sjson.SetRawBytes(toolResult, "content", contentJSON)
		return toolResult
	}
	toolResult, _ = sjson.SetBytes(toolResult, "content", output.String())
	return toolResult
}

func convertResponsesContentPartToClaude(part gjson.Result) []byte {
	ptype := part.Get("type").String()
	switch ptype {
	case "input_text", "output_text":
		if t := part.Get("text"); t.Exists() {
			contentPart := []byte(`{"type":"text","text":""}`)
			contentPart, _ = sjson.SetBytes(contentPart, "text", t.String())
			return contentPart
		}
	case "input_image":
		url := part.Get("image_url").String()
		if url == "" {
			url = part.Get("url").String()
		}
		if url == "" {
			return nil
		}
		if strings.HasPrefix(url, "data:") {
			trimmed := strings.TrimPrefix(url, "data:")
			mediaAndData := strings.SplitN(trimmed, ";base64,", 2)
			mediaType := "application/octet-stream"
			data := ""
			if len(mediaAndData) == 2 {
				if mediaAndData[0] != "" {
					mediaType = mediaAndData[0]
				}
				data = mediaAndData[1]
			}
			if data == "" {
				return nil
			}
			contentPart := []byte(`{"type":"image","source":{"type":"base64","media_type":"","data":""}}`)
			contentPart, _ = sjson.SetBytes(contentPart, "source.media_type", mediaType)
			contentPart, _ = sjson.SetBytes(contentPart, "source.data", data)
			return contentPart
		}
		contentPart := []byte(`{"type":"image","source":{"type":"url","url":""}}`)
		contentPart, _ = sjson.SetBytes(contentPart, "source.url", url)
		return contentPart
	case "input_file":
		fileData := part.Get("file_data").String()
		if fileData == "" {
			return nil
		}
		mediaType := "application/octet-stream"
		data := fileData
		if strings.HasPrefix(fileData, "data:") {
			trimmed := strings.TrimPrefix(fileData, "data:")
			mediaAndData := strings.SplitN(trimmed, ";base64,", 2)
			if len(mediaAndData) == 2 {
				if mediaAndData[0] != "" {
					mediaType = mediaAndData[0]
				}
				data = mediaAndData[1]
			}
		}
		contentPart := []byte(`{"type":"document","source":{"type":"base64","media_type":"","data":""}}`)
		contentPart, _ = sjson.SetBytes(contentPart, "source.media_type", mediaType)
		contentPart, _ = sjson.SetBytes(contentPart, "source.data", data)
		return contentPart
	}
	return nil
}

func convertResponsesToolToClaudeTools(tool gjson.Result, toolNameMap map[string]string) [][]byte {
	toolType := strings.TrimSpace(tool.Get("type").String())
	switch toolType {
	case "", "function":
		if tJSON, ok := convertResponsesFunctionToolToClaude(tool, ""); ok {
			return [][]byte{tJSON}
		}
	case "namespace":
		return convertResponsesNamespaceToolToClaude(tool, toolNameMap)
	case "web_search":
		if tJSON, ok := convertResponsesWebSearchToolToClaude(tool); ok {
			if name := gjson.GetBytes(tJSON, "name").String(); name != "" {
				toolNameMap[name] = name
			}
			return [][]byte{tJSON}
		}
	case "custom":
		if tJSON, ok := convertResponsesCustomToolToClaude(tool); ok {
			return [][]byte{tJSON}
		}
	default:
		if isUnsupportedOpenAIBuiltinToolType(toolType) {
			return nil
		}
		if tool.Get("name").String() != "" {
			return [][]byte{[]byte(tool.Raw)}
		}
	}
	return nil
}

// convertResponsesCustomToolToClaude maps an OpenAI Responses freeform custom
// tool (e.g. codex's apply_patch) to a Claude tool. Claude tool inputs must be
// JSON objects, so the freeform payload is wrapped in an {"input": "..."}
// envelope; any grammar definition is preserved in the description so the
// model still knows the required textual format. The response converter
// unwraps the envelope back into a custom_tool_call item.
func convertResponsesCustomToolToClaude(tool gjson.Result) ([]byte, bool) {
	name := responsesToolName(tool)
	if name == "" {
		return nil, false
	}

	description := responsesToolDescription(tool)
	if definition := strings.TrimSpace(tool.Get("format.definition").String()); definition != "" {
		if description != "" {
			description += "\n\n"
		}
		description += "The `input` string MUST follow this grammar:\n" + definition
	}

	tJSON := []byte(`{"name":"","description":"","input_schema":{"type":"object","properties":{"input":{"type":"string","description":"Freeform tool input payload."}},"required":["input"],"additionalProperties":false}}`)
	tJSON, _ = sjson.SetBytes(tJSON, "name", name)
	tJSON, _ = sjson.SetBytes(tJSON, "description", description)
	tJSON = common.AttachCacheControl(tJSON, tool)
	return tJSON, true
}

func convertResponsesNamespaceToolToClaude(tool gjson.Result, toolNameMap map[string]string) [][]byte {
	namespaceName := strings.TrimSpace(tool.Get("name").String())
	children := tool.Get("tools")
	if !children.Exists() || !children.IsArray() {
		return nil
	}

	var out [][]byte
	children.ForEach(func(_, child gjson.Result) bool {
		childName := responsesToolName(child)
		qualifiedName := qualifyResponsesNamespaceToolName(namespaceName, childName)
		if tJSON, ok := convertResponsesFunctionToolToClaude(child, qualifiedName); ok {
			out = append(out, tJSON)
			toolNameMap[qualifiedName] = qualifiedName
			if childName != "" {
				toolNameMap[childName] = qualifiedName
			}
		}
		return true
	})
	return out
}

func convertResponsesFunctionToolToClaude(tool gjson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}

	tJSON := []byte(`{"name":"","description":"","input_schema":{}}`)
	tJSON, _ = sjson.SetBytes(tJSON, "name", name)
	if d := responsesToolDescription(tool); d != "" {
		tJSON, _ = sjson.SetBytes(tJSON, "description", d)
	}
	tJSON, _ = sjson.SetRawBytes(tJSON, "input_schema", normalizeClaudeToolInputSchema(responsesToolParameters(tool)))
	tJSON = common.AttachCacheControl(tJSON, tool)
	if !gjson.GetBytes(tJSON, "cache_control").Exists() {
		tJSON = common.AttachCacheControl(tJSON, tool.Get("function"))
	}
	return tJSON, true
}

func convertResponsesWebSearchToolToClaude(tool gjson.Result) ([]byte, bool) {
	if externalWebAccess := tool.Get("external_web_access"); externalWebAccess.Exists() && !externalWebAccess.Bool() {
		return nil, false
	}

	name := strings.TrimSpace(tool.Get("name").String())
	if name == "" {
		name = "web_search"
	}
	tJSON := []byte(`{"type":"web_search_20250305","name":""}`)
	tJSON, _ = sjson.SetBytes(tJSON, "name", name)
	if maxUses := tool.Get("max_uses"); maxUses.Exists() {
		tJSON, _ = sjson.SetBytes(tJSON, "max_uses", maxUses.Int())
	}
	if allowedDomains := tool.Get("filters.allowed_domains"); allowedDomains.Exists() && allowedDomains.IsArray() {
		tJSON, _ = sjson.SetRawBytes(tJSON, "allowed_domains", []byte(allowedDomains.Raw))
	}
	if userLocation := tool.Get("user_location"); userLocation.Exists() && userLocation.IsObject() {
		tJSON, _ = sjson.SetRawBytes(tJSON, "user_location", []byte(userLocation.Raw))
	}
	return tJSON, true
}

func responsesToolName(tool gjson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("function.name").String())
}

func responsesToolDescription(tool gjson.Result) string {
	if description := tool.Get("description").String(); description != "" {
		return description
	}
	return tool.Get("function.description").String()
}

func responsesToolParameters(tool gjson.Result) gjson.Result {
	for _, path := range []string{
		"parameters",
		"parametersJsonSchema",
		"input_schema",
		"function.parameters",
		"function.parametersJsonSchema",
	} {
		if parameters := tool.Get(path); parameters.Exists() {
			return parameters
		}
	}
	return gjson.Result{}
}

func normalizeClaudeToolInputSchema(parameters gjson.Result) []byte {
	raw := strings.TrimSpace(parameters.Raw)
	if raw == "" || raw == "null" || !gjson.Valid(raw) {
		return []byte(`{"type":"object","properties":{}}`)
	}
	result := gjson.Parse(raw)
	if !result.IsObject() {
		return []byte(`{"type":"object","properties":{}}`)
	}
	schema := []byte(raw)
	schemaType := result.Get("type").String()
	if schemaType == "" {
		schema, _ = sjson.SetBytes(schema, "type", "object")
		schemaType = "object"
	}
	if schemaType == "object" && !result.Get("properties").Exists() {
		schema, _ = sjson.SetRawBytes(schema, "properties", []byte(`{}`))
	}
	return schema
}

func qualifyResponsesNamespaceToolName(namespaceName, childName string) string {
	childName = strings.TrimSpace(childName)
	if childName == "" || namespaceName == "" || strings.HasPrefix(childName, "mcp__") {
		return childName
	}
	if strings.HasPrefix(childName, namespaceName) {
		return childName
	}
	if strings.HasSuffix(namespaceName, "__") {
		return namespaceName + childName
	}
	return namespaceName + "__" + childName
}

func splitResponsesQualifiedFunctionCallFromRequest(requestRawJSON []byte, qualifiedName string) (name, namespace string) {
	qualifiedName = strings.TrimSpace(qualifiedName)
	if qualifiedName == "" {
		return "", ""
	}

	var bestNamespace string
	var bestChild string
	forEachResponsesTool(requestRawJSON, func(tool gjson.Result) bool {
		if strings.TrimSpace(tool.Get("type").String()) != "namespace" {
			return true
		}
		namespaceName := strings.TrimSpace(tool.Get("name").String())
		if namespaceName == "" {
			return true
		}
		children := tool.Get("tools")
		if !children.Exists() || !children.IsArray() {
			return true
		}
		children.ForEach(func(_, child gjson.Result) bool {
			childName := responsesToolName(child)
			if childName == "" {
				return true
			}
			if qualifyResponsesNamespaceToolName(namespaceName, childName) == qualifiedName {
				bestNamespace = namespaceName
				bestChild = childName
			}
			return true
		})
		return true
	})

	if bestNamespace == "" || bestChild == "" {
		return qualifiedName, ""
	}
	return bestChild, bestNamespace
}

// forEachResponsesTool iterates every tool declaration in a Responses request.
// Besides the top-level tools array, codex's responses-lite mode carries tool
// declarations inside input items ({"type":"additional_tools","tools":[...]});
// both locations are authoritative and must be honored.
func forEachResponsesTool(requestRawJSON []byte, fn func(tool gjson.Result) bool) {
	if len(requestRawJSON) == 0 || fn == nil {
		return
	}
	if tools := gjson.GetBytes(requestRawJSON, "tools"); tools.IsArray() {
		for _, tool := range tools.Array() {
			if !fn(tool) {
				return
			}
		}
	}
	if input := gjson.GetBytes(requestRawJSON, "input"); input.IsArray() {
		for _, item := range input.Array() {
			if item.Get("type").String() != "additional_tools" {
				continue
			}
			tools := item.Get("tools")
			if !tools.IsArray() {
				continue
			}
			for _, tool := range tools.Array() {
				if !fn(tool) {
					return
				}
			}
		}
	}
}

func isUnsupportedOpenAIBuiltinToolType(toolType string) bool {
	switch toolType {
	case "image_generation", "file_search", "code_interpreter", "computer_use_preview":
		return true
	default:
		return false
	}
}
