package executor

import "testing"

// TestBuildRunRequestParams_ModelOverride verifies that buildRunRequestParams
// honors the modelOverride parameter when supplied (so the upstream Cursor Run
// call receives the resolved model name from an oauth-model-alias entry),
// and falls back to parsed.Model when the override is empty or whitespace.
func TestBuildRunRequestParams_ModelOverride(t *testing.T) {
	tests := []struct {
		name        string
		parsedModel string
		override    string
		wantModelId string
	}{
		{
			name:        "override wins over parsed.Model",
			parsedModel: "cursor/composer-2.5",
			override:    "composer-2.5",
			wantModelId: "composer-2.5",
		},
		{
			name:        "empty override falls back to parsed.Model",
			parsedModel: "composer-2.5",
			override:    "",
			wantModelId: "composer-2.5",
		},
		{
			name:        "whitespace override falls back to parsed.Model",
			parsedModel: "composer-2.5",
			override:    "   \t  ",
			wantModelId: "composer-2.5",
		},
		{
			name:        "override wins even when parsed.Model is empty",
			parsedModel: "",
			override:    "composer-2.5",
			wantModelId: "composer-2.5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := &parsedOpenAIRequest{Model: tc.parsedModel}
			params := buildRunRequestParams(parsed, "conv-123", tc.override)
			if params == nil {
				t.Fatal("buildRunRequestParams returned nil")
			}
			if params.ModelId != tc.wantModelId {
				t.Errorf("ModelId = %q, want %q", params.ModelId, tc.wantModelId)
			}
			if params.ConversationId != "conv-123" {
				t.Errorf("ConversationId = %q, want %q", params.ConversationId, "conv-123")
			}
		})
	}
}
