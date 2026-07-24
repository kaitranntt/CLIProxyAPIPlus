package executor

import (
	"context"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeOpenAIResponsesItemIDs rewrites function_call and custom_tool_call
// item IDs in an OpenAI Responses request so the id prefix matches the item
// type. Strict Responses upstreams validate the pair:
//
//	function_call     -> id must start with "fc_"
//	custom_tool_call  -> id must start with "ctc_"
//
// History replayed from a different provider channel (or re-typed by the
// client between turns) can arrive with a mismatched pair — e.g. a
// custom_tool_call item carrying the "fc_<call_id>" id minted when the call
// was previously surfaced as a function_call — which the upstream rejects:
//
//	Invalid 'input[29].id': 'fc_call_...'. Expected an ID that begins with 'ctc'.
//
// The rewrite preserves the remainder of the id (which embeds the call_id) so
// cross-references stay stable; items without a usable id are left untouched.
func normalizeOpenAIResponsesItemIDs(ctx context.Context, provider string, body []byte) []byte {
	inputResult := gjson.GetBytes(body, "input")
	if !inputResult.Exists() || !inputResult.IsArray() {
		return body
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "openai responses upstream"
	}

	items := inputResult.Array()

	// rebuilt accumulates the edited "input" array lazily: it stays nil while
	// no item needs editing so the common case does no allocation.
	var rebuilt []byte
	itemsWritten := 0
	keep := func(raw string) {
		if rebuilt == nil {
			return
		}
		if itemsWritten > 0 {
			rebuilt = append(rebuilt, ',')
		}
		rebuilt = append(rebuilt, raw...)
		itemsWritten++
	}
	startRebuild := func(index int) {
		if rebuilt != nil {
			return
		}
		rebuilt = make([]byte, 0, len(inputResult.Raw))
		rebuilt = append(rebuilt, '[')
		for i := range index {
			keep(items[i].Raw)
		}
	}

	for index, item := range items {
		var wantPrefix string
		switch strings.TrimSpace(item.Get("type").String()) {
		case "function_call":
			wantPrefix = "fc_"
		case "custom_tool_call":
			wantPrefix = "ctc_"
		default:
			keep(item.Raw)
			continue
		}

		idField := item.Get("id")
		if !idField.Exists() || idField.Type != gjson.String {
			keep(item.Raw)
			continue
		}
		itemID := idField.String()
		if itemID == "" || strings.HasPrefix(itemID, wantPrefix) {
			keep(item.Raw)
			continue
		}

		// Strip a known tool-call prefix so the remainder (embedding the
		// call_id) is preserved; unknown shapes are prefixed as-is.
		base := itemID
		if rest, ok := strings.CutPrefix(base, "fc_"); ok {
			base = rest
		} else if rest, ok := strings.CutPrefix(base, "ctc_"); ok {
			base = rest
		}
		nextID := wantPrefix + base

		nextItem, err := sjson.Set(item.Raw, "id", nextID)
		if err != nil {
			helps.LogWithRequestID(ctx).Debugf("%s: failed to normalize item id at input[%d]: %v", provider, index, err)
			keep(item.Raw)
			continue
		}
		startRebuild(index)
		keep(nextItem)
		helps.LogWithRequestID(ctx).Debugf("%s: normalized mismatched item id at input[%d] type=%q old_id=%q new_id=%q", provider, index, item.Get("type").String(), itemID, nextID)
	}

	if rebuilt == nil {
		return body
	}
	rebuilt = append(rebuilt, ']')

	updated, err := sjson.SetRawBytes(body, "input", rebuilt)
	if err != nil {
		helps.LogWithRequestID(ctx).Debugf("%s: failed to rebuild input array while normalizing item ids: %v", provider, err)
		return body
	}
	return updated
}
