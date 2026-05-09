// Package claude provides response translation functionality for Kiro API to Claude format.
// This package handles the conversion of Kiro API responses into Claude-compatible format,
// including support for thinking blocks and tool use.
package claude

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// BuildClaudeResponse constructs a Claude-compatible response.
// Supports tool_use blocks when tools are present in the response.
// Content is treated as plain text — thinking blocks are handled via
// reasoningContentEvent in the streaming path, not by parsing tags.
// stopReason is passed from upstream; fallback logic applied if empty.
func BuildClaudeResponse(content string, toolUses []KiroToolUse, model string, usageInfo usage.Detail, stopReason string) []byte {
	var contentBlocks []map[string]interface{}

	if content != "" {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": content,
		})
	}

	// Add tool_use blocks — skip truncated tools when detector is enabled
	for _, toolUse := range toolUses {
		if toolUse.IsTruncated && toolUse.TruncationInfo != nil {
			log.Warnf("kiro: skipping truncated tool: %s (ID: %s)", toolUse.Name, toolUse.ToolUseID)
			continue
		}
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type":  "tool_use",
			"id":    toolUse.ToolUseID,
			"name":  toolUse.Name,
			"input": toolUse.Input,
		})
	}

	// Ensure at least one content block (Claude API requires non-empty content)
	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": "",
		})
	}

	// Use upstream stopReason; apply fallback logic if not provided
	// SOFT_LIMIT_REACHED: Keep stop_reason = "tool_use" so Claude continues the loop
	if stopReason == "" {
		stopReason = "end_turn"
		if len(toolUses) > 0 {
			stopReason = "tool_use"
		}
		log.Debugf("kiro: buildClaudeResponse using fallback stop_reason: %s", stopReason)
	}

	// Log warning if response was truncated due to max_tokens
	if stopReason == "max_tokens" {
		log.Warnf("kiro: response truncated due to max_tokens limit (buildClaudeResponse)")
	}

	response := map[string]interface{}{
		"id":          "msg_" + uuid.New().String()[:24],
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     contentBlocks,
		"stop_reason": stopReason,
		"usage": map[string]interface{}{
			"input_tokens":  usageInfo.InputTokens,
			"output_tokens": usageInfo.OutputTokens,
		},
	}
	result, _ := json.Marshal(response)
	return result
}
