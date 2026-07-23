package responses

import (
	"encoding/base64"
	"strings"
	"testing"

	sigcompat "github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestConvertOpenAIResponsesRequestToClaude_SanitizesToolCallIDsForClaude(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"input": [
			{
				"type": "function_call",
				"call_id": "call.with space:1",
				"name": "Read",
				"arguments": "{\"path\":\"README.md\"}"
			},
			{
				"type": "function_call_output",
				"call_id": "call.with space:1",
				"output": "ok"
			}
		]
	}`

	result := ConvertOpenAIResponsesRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)
	toolUseID := resultJSON.Get("messages.0.content.0.id").String()
	toolResultID := resultJSON.Get("messages.1.content.0.tool_use_id").String()

	if toolUseID != "call_with_space_1" {
		t.Fatalf("tool_use id = %q, want %q", toolUseID, "call_with_space_1")
	}
	if toolResultID != toolUseID {
		t.Fatalf("tool_result tool_use_id = %q, want same sanitized id %q", toolResultID, toolUseID)
	}
}

func TestConvertOpenAIResponsesRequestToClaude_ReasoningItemToThinkingBlock(t *testing.T) {
	rawSignature, expectedSignature := testClaudeResponsesThinkingSignature(t)
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{
				"type":"reasoning",
				"encrypted_content":"` + rawSignature + `",
				"summary":[{"type":"summary_text","text":"internal reasoning"}]
			},
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"visible answer"}]
			},
			{
				"type":"message",
				"role":"user",
				"content":[{"type":"input_text","text":"continue"}]
			}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	assistant := root.Get("messages.0")
	if got := assistant.Get("role").String(); got != "assistant" {
		t.Fatalf("first message role = %q, want assistant. Output: %s", got, string(out))
	}
	if got := assistant.Get("content.0.type").String(); got != "thinking" {
		t.Fatalf("first content type = %q, want thinking. Output: %s", got, string(out))
	}
	if got := assistant.Get("content.0.signature").String(); got != expectedSignature {
		t.Fatalf("thinking signature = %q, want %q", got, expectedSignature)
	}
	if got := assistant.Get("content.0.thinking").String(); got != "internal reasoning" {
		t.Fatalf("thinking text = %q, want internal reasoning", got)
	}
	if got := assistant.Get("content.1.type").String(); got != "text" {
		t.Fatalf("second content type = %q, want text. Output: %s", got, string(out))
	}
	if got := assistant.Get("content.1.text").String(); got != "visible answer" {
		t.Fatalf("assistant text = %q, want visible answer", got)
	}
	if got := root.Get("messages.1.role").String(); got != "user" {
		t.Fatalf("second message role = %q, want user. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_SignatureOnlyReasoningFlushesBeforeUser(t *testing.T) {
	rawSignature, expectedSignature := testClaudeResponsesThinkingSignature(t)
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{
				"type":"reasoning",
				"encrypted_content":"` + rawSignature + `",
				"summary":[]
			},
			{
				"type":"message",
				"role":"user",
				"content":[{"type":"input_text","text":"continue"}]
			}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	thinking := root.Get("messages.0.content.0")
	if got := thinking.Get("type").String(); got != "thinking" {
		t.Fatalf("first content type = %q, want thinking. Output: %s", got, string(out))
	}
	if got := thinking.Get("signature").String(); got != expectedSignature {
		t.Fatalf("thinking signature = %q, want %q", got, expectedSignature)
	}
	if got := thinking.Get("thinking").String(); got != "" {
		t.Fatalf("thinking text = %q, want empty", got)
	}
	if got := root.Get("messages.1.role").String(); got != "user" {
		t.Fatalf("second message role = %q, want user. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_DropsIncompatibleReasoningSignature(t *testing.T) {
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{
				"type":"reasoning",
				"encrypted_content":"` + testGPTResponsesReasoningSignature() + `",
				"summary":[{"type":"summary_text","text":"must not become Claude thinking"}]
			},
			{
				"type":"message",
				"role":"user",
				"content":[{"type":"input_text","text":"continue"}]
			}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)

	if gjson.GetBytes(out, "messages.0.content.0.type").String() == "thinking" {
		t.Fatalf("GPT encrypted_content should not become Claude thinking. Output: %s", string(out))
	}
	if gjson.GetBytes(out, "messages.0.content.0.signature").Exists() {
		t.Fatalf("incompatible signature should not be forwarded. Output: %s", string(out))
	}
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "user" {
		t.Fatalf("first message role = %q, want user. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_FunctionCallOutputPreservesInputImage(t *testing.T) {
	const imageB64 = "iVBORw0KGgo="
	dataURL := "data:image/png;base64," + imageB64
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{
				"type":"function_call",
				"call_id":"call_view_image_1",
				"name":"view_image",
				"arguments":"{}"
			},
			{
				"type":"function_call_output",
				"call_id":"call_view_image_1",
				"output":[
					{
						"type":"input_image",
						"image_url":"` + dataURL + `",
						"detail":"high"
					}
				]
			}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	toolResult := root.Get("messages.1.content.0")
	if got := toolResult.Get("type").String(); got != "tool_result" {
		t.Fatalf("tool_result type = %q, want tool_result. Output: %s", got, string(out))
	}
	if got := toolResult.Get("content.0.type").String(); got != "image" {
		t.Fatalf("tool_result content block type = %q, want image. Output: %s", got, string(out))
	}
	if got := toolResult.Get("content.0.source.media_type").String(); got != "image/png" {
		t.Fatalf("image media_type = %q, want image/png. Output: %s", got, string(out))
	}
	if got := toolResult.Get("content.0.source.data").String(); got != imageB64 {
		t.Fatalf("image data = %q, want raw base64 without data URL prefix", got)
	}
	if strings.Contains(toolResult.Get("content").Raw, "data:image") {
		t.Fatalf("tool_result content must not embed data URL as text. Output: %s", string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_KeepsToolUseAdjacentToToolResult(t *testing.T) {
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{
				"type":"function_call",
				"call_id":"call_00_awGuheXs4aRbtedNK8LE3743",
				"name":"js",
				"arguments":"{\"code\":\"nodeRepl.write('ok')\",\"title\":\"List Obsidian vault contents\"}"
			},
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"I'll check your Obsidian vault for articles."}]
			},
			{
				"type":"function_call_output",
				"call_id":"call_00_awGuheXs4aRbtedNK8LE3743",
				"output":"Wall time: 0.1963 seconds\nOutput:\n[{\"type\":\"text\",\"text\":\"\"}]"
			}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.0.role").String(); got != "assistant" {
		t.Fatalf("first message role = %q, want assistant. Output: %s", got, string(out))
	}
	if got := root.Get("messages.0.content").String(); got != "I'll check your Obsidian vault for articles." {
		t.Fatalf("first message content = %q, want assistant text. Output: %s", got, string(out))
	}
	if got := root.Get("messages.1.content.0.type").String(); got != "tool_use" {
		t.Fatalf("second message first content type = %q, want tool_use. Output: %s", got, string(out))
	}
	if got := root.Get("messages.1.content.0.id").String(); got != "call_00_awGuheXs4aRbtedNK8LE3743" {
		t.Fatalf("tool_use id = %q, want call_00_awGuheXs4aRbtedNK8LE3743. Output: %s", got, string(out))
	}
	if got := root.Get("messages.2.content.0.type").String(); got != "tool_result" {
		t.Fatalf("third message first content type = %q, want tool_result. Output: %s", got, string(out))
	}
	if got := root.Get("messages.2.content.0.tool_use_id").String(); got != "call_00_awGuheXs4aRbtedNK8LE3743" {
		t.Fatalf("tool_result id = %q, want call_00_awGuheXs4aRbtedNK8LE3743. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_AcceptsPlainStringInput(t *testing.T) {
	raw := []byte(`{"model":"claude-test","input":"Hi","stream":true}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, true)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.#").Int(); got != 1 {
		t.Fatalf("messages count = %d, want 1. Output: %s", got, string(out))
	}
	if got := root.Get("messages.0.role").String(); got != "user" {
		t.Fatalf("messages.0.role = %q, want user. Output: %s", got, string(out))
	}
	if got := root.Get("messages.0.content").String(); got != "Hi" {
		t.Fatalf("messages.0.content = %q, want Hi. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_MapsCustomToolCallOutputItem(t *testing.T) {
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{"type":"custom_tool_call","call_id":"call_exec","name":"exec","input":"const r = await tools.exec_command({cmd:\"ls\"})"},
			{"type":"custom_tool_call_output","call_id":"call_exec","output":[
				{"type":"input_text","text":"Script completed"},
				{"type":"input_text","text":"file1.txt"}
			]}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.0.role").String(); got != "assistant" {
		t.Fatalf("messages.0.role = %q, want assistant. Output: %s", got, string(out))
	}
	if got := root.Get("messages.1.role").String(); got != "user" {
		t.Fatalf("messages.1.role = %q, want user. Output: %s", got, string(out))
	}
	toolResult := root.Get("messages.1.content.0")
	if got := toolResult.Get("type").String(); got != "tool_result" {
		t.Fatalf("content.0.type = %q, want tool_result. Output: %s", got, string(out))
	}
	if got := toolResult.Get("tool_use_id").String(); got != "call_exec" {
		t.Fatalf("tool_result.tool_use_id = %q, want call_exec. Output: %s", got, string(out))
	}
	if got := toolResult.Get("content.0.text").String(); got != "Script completed" {
		t.Fatalf("tool_result content.0.text = %q. Output: %s", got, string(out))
	}
	if got := toolResult.Get("content.1.text").String(); got != "file1.txt" {
		t.Fatalf("tool_result content.1.text = %q. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_MapsCustomToolCallInputItem(t *testing.T) {
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{"type":"custom_tool_call","call_id":"call_patch","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},
			{"type":"function_call_output","call_id":"call_patch","output":"patch applied"}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.0.role").String(); got != "assistant" {
		t.Fatalf("messages.0.role = %q, want assistant. Output: %s", got, string(out))
	}
	toolUse := root.Get("messages.0.content.0")
	if got := toolUse.Get("type").String(); got != "tool_use" {
		t.Fatalf("content.0.type = %q, want tool_use. Output: %s", got, string(out))
	}
	if got := toolUse.Get("id").String(); got != "call_patch" {
		t.Fatalf("tool_use.id = %q, want call_patch. Output: %s", got, string(out))
	}
	if got := toolUse.Get("name").String(); got != "apply_patch" {
		t.Fatalf("tool_use.name = %q, want apply_patch. Output: %s", got, string(out))
	}
	if got := toolUse.Get("input.input").String(); got != "*** Begin Patch\n*** End Patch" {
		t.Fatalf("tool_use input.input = %q, want wrapped freeform payload. Output: %s", got, string(out))
	}
	if got := root.Get("messages.1.role").String(); got != "user" {
		t.Fatalf("messages.1.role = %q, want user. Output: %s", got, string(out))
	}
	if got := root.Get("messages.1.content.0.type").String(); got != "tool_result" {
		t.Fatalf("messages.1.content.0.type = %q, want tool_result. Output: %s", got, string(out))
	}
	if got := root.Get("messages.1.content.0.tool_use_id").String(); got != "call_patch" {
		t.Fatalf("tool_result.tool_use_id = %q, want call_patch. Output: %s", got, string(out))
	}
}

func TestConvertOpenAIResponsesRequestToClaude_AdditionalToolsItem(t *testing.T) {
	raw := []byte(`{
		"model":"claude-test",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"custom","name":"exec","description":"Run JS code.","format":{"type":"grammar","syntax":"lark","definition":"start: js"}},
				{"type":"function","name":"wait","description":"Waits on a cell.","parameters":{"type":"object","properties":{"id":{"type":"string"}}}},
				{"type":"namespace","name":"collaboration","description":"Sub-agents.","tools":[
					{"type":"function","name":"followup_task","description":"Send task.","parameters":{"type":"object","properties":{"message":{"type":"string"}}}}
				]}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}
		],
		"tool_choice":"auto"
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("tools.#").Int(); got != 3 {
		t.Fatalf("tools count = %d, want 3. Output: %s", got, string(out))
	}
	for _, name := range []string{"exec", "wait", "collaboration__followup_task"} {
		if !root.Get(`tools.#(name=="` + name + `")`).Exists() {
			t.Fatalf("missing tool %q from additional_tools. Output: %s", name, string(out))
		}
	}
	if got := root.Get(`tools.#(name=="exec").input_schema.properties.input.type`).String(); got != "string" {
		t.Fatalf("exec input_schema.properties.input.type = %q, want string", got)
	}
	if desc := root.Get(`tools.#(name=="exec").description`).String(); !strings.Contains(desc, "start: js") {
		t.Fatalf("exec description should carry grammar definition, got %q", desc)
	}
}

func TestConvertOpenAIResponsesRequestToClaude_MapsApplyPatchCustomTool(t *testing.T) {
	raw := []byte(`{
		"model":"claude-test",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}],
		"tools":[
			{
				"type":"custom",
				"name":"apply_patch",
				"description":"Use the apply_patch tool to edit files.",
				"format":{"type":"grammar","syntax":"lark","definition":"start: patch"}
			},
			{
				"type":"function",
				"name":"exec_command",
				"description":"Runs a command.",
				"parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}
			}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-test", raw, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("tools.#").Int(); got != 2 {
		t.Fatalf("tools count = %d, want 2. Output: %s", got, string(out))
	}
	customTool := root.Get("tools.#(name==\"apply_patch\")")
	if !customTool.Exists() {
		t.Fatalf("apply_patch custom tool should be preserved. Output: %s", string(out))
	}
	if got := customTool.Get("input_schema.properties.input.type").String(); got != "string" {
		t.Fatalf("apply_patch input_schema.properties.input.type = %q, want string. Output: %s", got, string(out))
	}
	if got := customTool.Get("input_schema.required.0").String(); got != "input" {
		t.Fatalf("apply_patch input_schema.required = %q, want [input]. Output: %s", got, string(out))
	}
	desc := customTool.Get("description").String()
	if !strings.Contains(desc, "Use the apply_patch tool to edit files.") || !strings.Contains(desc, "start: patch") {
		t.Fatalf("apply_patch description should keep the original text and grammar definition, got %q", desc)
	}
}

func testClaudeResponsesThinkingSignature(t *testing.T) (string, string) {
	t.Helper()
	channelBlock := []byte{}
	channelBlock = protowire.AppendTag(channelBlock, 1, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 12)
	channelBlock = protowire.AppendTag(channelBlock, 2, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 2)
	channelBlock = protowire.AppendTag(channelBlock, 6, protowire.BytesType)
	channelBlock = protowire.AppendString(channelBlock, "claude-sonnet-4-6")

	container := []byte{}
	container = protowire.AppendTag(container, 1, protowire.BytesType)
	container = protowire.AppendBytes(container, channelBlock)

	payload := []byte{}
	payload = protowire.AppendTag(payload, 2, protowire.BytesType)
	payload = protowire.AppendBytes(payload, container)
	payload = protowire.AppendTag(payload, 3, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 1)

	rawSignature := base64.StdEncoding.EncodeToString(payload)
	normalized, ok := sigcompat.CompatibleSignatureForProvider(sigcompat.SignatureProviderClaude, rawSignature)
	if !ok {
		t.Fatal("test Claude signature should be compatible")
	}
	return rawSignature, normalized
}

func testGPTResponsesReasoningSignature() string {
	payload := make([]byte, 1+8+16+16+32)
	payload[0] = 0x80
	payload[8] = 1
	for i := 9; i < len(payload); i++ {
		payload[i] = byte(i)
	}
	return base64.URLEncoding.EncodeToString(payload)
}

func TestConvertOpenAIResponsesRequestToClaude_PreservesContentPartCacheControl(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [
					{"type": "input_text", "text": "cached prefix", "cache_control": {"type": "ephemeral"}},
					{"type": "input_text", "text": "fresh question"}
				]
			}
		]
	}`

	result := ConvertOpenAIResponsesRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	content := resultJSON.Get("messages.0.content")
	if !content.IsArray() {
		t.Fatalf("expected content array when cache_control is present, got %s", result)
	}
	if got := content.Get("0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("content.0.cache_control.type = %q, want ephemeral. Output: %s", got, result)
	}
	if content.Get("1.cache_control").Exists() {
		t.Fatalf("content.1 should not have cache_control. Output: %s", result)
	}
}
