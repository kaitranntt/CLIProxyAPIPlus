package responses

import (
	"bytes"
	"context"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertCodexResponseToOpenAIResponses converts OpenAI Chat Completions streaming chunks
// to OpenAI Responses SSE events (response.*).

func ConvertCodexResponseToOpenAIResponses(_ context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) [][]byte {
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
		rawJSON = setResponsesEchoFields(rawJSON, modelName, originalRequestRawJSON, requestRawJSON)
		out := make([]byte, 0, len(rawJSON)+len("data: "))
		out = append(out, []byte("data: ")...)
		out = append(out, rawJSON...)
		return [][]byte{out}
	}
	return [][]byte{setResponsesEchoFields(rawJSON, modelName, originalRequestRawJSON, requestRawJSON)}
}

func setResponsesEchoFields(rawJSON []byte, modelName string, originalRequestRawJSON, requestRawJSON []byte) []byte {
	eventType := gjson.GetBytes(rawJSON, "type").String()
	if eventType == "" {
		return rawJSON
	}
	if !gjson.GetBytes(rawJSON, "response").Exists() {
		return rawJSON
	}

	// Backfill response.model for the initial events if the upstream omitted it.
	if eventType == "response.created" || eventType == "response.in_progress" {
		if !gjson.GetBytes(rawJSON, "response.model").Exists() {
			requestModelName := translatorcommon.RequestModelName(originalRequestRawJSON, requestRawJSON)
			if requestModelName == "" {
				requestModelName = modelName
			}
			if requestModelName != "" {
				rawJSON, _ = sjson.SetBytes(rawJSON, "response.model", requestModelName)
			}
		}
	}

	// Propagate prompt_cache_key from the request echo into the response.
	// Codex Responses echoes the request model but not the prompt_cache_key, so
	// we backfill it when absent to preserve client cache tracking.
	if !gjson.GetBytes(rawJSON, "response.prompt_cache_key").Exists() {
		req := pickRequestJSON(originalRequestRawJSON, requestRawJSON)
		if v := req.Get("prompt_cache_key"); v.Exists() {
			rawJSON, _ = sjson.SetBytes(rawJSON, "response.prompt_cache_key", v.String())
		}
	}

	return rawJSON
}

func pickRequestJSON(originalRequestRawJSON, requestRawJSON []byte) gjson.Result {
	for _, b := range [][]byte{originalRequestRawJSON, requestRawJSON} {
		if len(b) > 0 && gjson.ValidBytes(b) {
			return gjson.ParseBytes(b)
		}
	}
	return gjson.Result{}
}

// ConvertCodexResponseToOpenAIResponsesNonStream builds a single Responses JSON
// from a non-streaming OpenAI Chat Completions response.
func ConvertCodexResponseToOpenAIResponsesNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	rootResult := gjson.ParseBytes(rawJSON)
	// Verify this is a terminal response event.
	responseType := rootResult.Get("type").String()
	if responseType != "response.completed" && responseType != "response.incomplete" {
		return []byte{}
	}
	responseResult := rootResult.Get("response")
	out := []byte(responseResult.Raw)
	if !gjson.GetBytes(out, "prompt_cache_key").Exists() {
		req := pickRequestJSON(originalRequestRawJSON, requestRawJSON)
		if v := req.Get("prompt_cache_key"); v.Exists() {
			out, _ = sjson.SetBytes(out, "prompt_cache_key", v.String())
		}
	}
	return out
}
