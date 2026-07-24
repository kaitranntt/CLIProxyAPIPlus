package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	clauderesponses "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/claude/openai/responses"
	kiroopenai "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIResponsesRequestToKiro converts an OpenAI Responses request into
// the Claude request format consumed by the Kiro payload builders. The shared
// Responses→Claude converter downgrades system-level content (the top-level
// "instructions" field and role:"system" input items) into plain user
// messages. Kiro translators, however, only extract and wrap the Claude
// top-level "system" field, so downgraded instructions would bypass system
// prompt wrapping entirely. Extract all system-level content up front and
// re-attach it as the top-level "system" field after conversion.
func ConvertOpenAIResponsesRequestToKiro(modelName string, inputRawJSON []byte, stream bool) []byte {
	systemPrompt, stripped := extractResponsesSystemContent(inputRawJSON)
	out := clauderesponses.ConvertOpenAIResponsesRequestToClaude(modelName, stripped, stream)
	if systemPrompt != "" {
		out, _ = sjson.SetBytes(out, "system", systemPrompt)
	}
	return out
}

// extractResponsesSystemContent removes system-level content from an OpenAI
// Responses request — the top-level "instructions" field and every
// role:"system" item in the input array — returning it as a single text block
// (instructions first, then system items in input order) along with the
// stripped request JSON.
func extractResponsesSystemContent(inputRawJSON []byte) (string, []byte) {
	var sb strings.Builder
	out := inputRawJSON

	if instr := gjson.GetBytes(out, "instructions"); instr.Exists() && instr.Type == gjson.String {
		if text := instr.String(); text != "" {
			sb.WriteString(text)
		}
		out, _ = sjson.DeleteBytes(out, "instructions")
	}

	input := gjson.GetBytes(out, "input")
	if !input.Exists() || !input.IsArray() {
		return sb.String(), out
	}

	items := input.Array()
	kept := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		if !strings.EqualFold(item.Get("role").String(), "system") {
			kept = append(kept, json.RawMessage(item.Raw))
			continue
		}
		if text := responsesSystemItemText(item); text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(text)
		}
	}

	if len(kept) == len(items) {
		return sb.String(), out
	}
	arr, err := json.Marshal(kept)
	if err != nil {
		return sb.String(), out
	}
	out, _ = sjson.SetRawBytes(out, "input", arr)
	return sb.String(), out
}

// responsesSystemItemText flattens the text of a role:"system" input item,
// mirroring the shared converter's extraction (content part texts joined by
// newlines, or the raw string content).
func responsesSystemItemText(item gjson.Result) string {
	content := item.Get("content")
	if content.Exists() && content.IsArray() {
		var sb strings.Builder
		for _, part := range content.Array() {
			text := part.Get("text").String()
			if sb.Len() > 0 && text != "" {
				sb.WriteByte('\n')
			}
			sb.WriteString(text)
		}
		return sb.String()
	}
	if content.Type == gjson.String {
		return content.String()
	}
	return ""
}

func ConvertKiroStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	_, eventData := kiroopenai.ParseClaudeEvent(rawJSON)
	if len(eventData) == 0 {
		return [][]byte{}
	}
	normalized := make([]byte, 0, len(eventData)+len("data: "))
	normalized = append(normalized, "data: "...)
	normalized = append(normalized, eventData...)
	return clauderesponses.ConvertClaudeResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, normalized, param)
}

func ConvertKiroNonStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	claudeSSE := convertKiroClaudeResponseToSSE(rawJSON)
	return clauderesponses.ConvertClaudeResponseToOpenAIResponsesNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, claudeSSE, param)
}

func convertKiroClaudeResponseToSSE(rawJSON []byte) []byte {
	root := gjson.ParseBytes(rawJSON)
	if root.Get("type").String() != "message" {
		return rawJSON
	}

	var stream bytes.Buffer
	appendClaudeDataEvent := func(event []byte) {
		stream.WriteString("data: ")
		stream.Write(event)
		stream.WriteByte('\n')
	}

	messageStart := []byte(`{"type":"message_start","message":{"id":"","usage":{}}}`)
	messageStart, _ = sjson.SetBytes(messageStart, "message.id", root.Get("id").String())
	if usage := root.Get("usage"); usage.Exists() {
		messageStart, _ = sjson.SetRawBytes(messageStart, "message.usage", []byte(usage.Raw))
	}
	appendClaudeDataEvent(messageStart)

	for index, block := range root.Get("content").Array() {
		blockStart := []byte(`{"type":"content_block_start","index":0,"content_block":{}}`)
		blockStart, _ = sjson.SetBytes(blockStart, "index", index)
		blockStart, _ = sjson.SetRawBytes(blockStart, "content_block", []byte(block.Raw))
		appendClaudeDataEvent(blockStart)

		switch block.Get("type").String() {
		case "text":
			delta := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`)
			delta, _ = sjson.SetBytes(delta, "index", index)
			delta, _ = sjson.SetBytes(delta, "delta.text", block.Get("text").String())
			appendClaudeDataEvent(delta)
		case "thinking":
			delta := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`)
			delta, _ = sjson.SetBytes(delta, "index", index)
			delta, _ = sjson.SetBytes(delta, "delta.thinking", block.Get("thinking").String())
			appendClaudeDataEvent(delta)
			if signature := block.Get("signature"); signature.Exists() && signature.String() != "" {
				signatureDelta := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":""}}`)
				signatureDelta, _ = sjson.SetBytes(signatureDelta, "index", index)
				signatureDelta, _ = sjson.SetBytes(signatureDelta, "delta.signature", signature.String())
				appendClaudeDataEvent(signatureDelta)
			}
		case "tool_use":
			input := block.Get("input").Raw
			if input == "" {
				input = "{}"
			}
			delta := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`)
			delta, _ = sjson.SetBytes(delta, "index", index)
			delta, _ = sjson.SetBytes(delta, "delta.partial_json", input)
			appendClaudeDataEvent(delta)
		}

		blockStop := []byte(`{"type":"content_block_stop","index":0}`)
		blockStop, _ = sjson.SetBytes(blockStop, "index", index)
		appendClaudeDataEvent(blockStop)
	}

	messageDelta := []byte(`{"type":"message_delta","delta":{"stop_reason":"","stop_sequence":null},"usage":{}}`)
	messageDelta, _ = sjson.SetBytes(messageDelta, "delta.stop_reason", root.Get("stop_reason").String())
	if usage := root.Get("usage"); usage.Exists() {
		messageDelta, _ = sjson.SetRawBytes(messageDelta, "usage", []byte(usage.Raw))
	}
	appendClaudeDataEvent(messageDelta)
	appendClaudeDataEvent([]byte(`{"type":"message_stop"}`))

	return stream.Bytes()
}
