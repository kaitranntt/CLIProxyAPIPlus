package helps

import (
	"encoding/json"
)

// BuildChatCompletionChunk builds a raw OpenAI chat.completion.chunk JSON payload.
// It mirrors the json.Marshal + map pattern used by the translator packages.
// If finishReason is empty, finish_reason is emitted as null. If usage is nil,
// the usage field is omitted.
func BuildChatCompletionChunk(id string, created int64, model string, delta map[string]interface{}, finishReason string, usage map[string]interface{}) ([]byte, error) {
	fr := interface{}(nil)
	if finishReason != "" {
		fr = finishReason
	}

	if delta == nil {
		delta = map[string]interface{}{}
	}

	choice := map[string]interface{}{
		"index":         0,
		"delta":         delta,
		"finish_reason": fr,
	}

	chunk := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{choice},
	}

	if usage != nil {
		chunk["usage"] = usage
	}

	return json.Marshal(chunk)
}

// BuildChatCompletion builds a raw OpenAI chat.completion JSON payload for
// non-streaming responses.
func BuildChatCompletion(id string, created int64, model, content, finishReason string, promptTokens, completionTokens, totalTokens int) ([]byte, error) {
	message := map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}

	choice := map[string]interface{}{
		"index":         0,
		"message":       message,
		"finish_reason": finishReason,
	}

	completion := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{choice},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}

	return json.Marshal(completion)
}

// BuildToolCallStartDelta builds a delta map containing one tool_call with id,
// type, name, and empty arguments.
func BuildToolCallStartDelta(toolIndex int, toolCallID, toolName string) ([]byte, error) {
	delta := map[string]interface{}{
		"tool_calls": []map[string]interface{}{
			{
				"index": toolIndex,
				"id":    toolCallID,
				"type":  "function",
				"function": map[string]interface{}{
					"name":      toolName,
					"arguments": "",
				},
			},
		},
	}

	return json.Marshal(delta)
}

// BuildToolCallArgumentsDelta builds a delta map containing one tool_call with
// arguments only. argumentsJSON is the raw JSON object text; json.Marshal
// encodes it as a JSON string.
func BuildToolCallArgumentsDelta(toolIndex int, argumentsJSON string) ([]byte, error) {
	delta := map[string]interface{}{
		"tool_calls": []map[string]interface{}{
			{
				"index": toolIndex,
				"function": map[string]interface{}{
					"arguments": argumentsJSON,
				},
			},
		},
	}

	return json.Marshal(delta)
}
