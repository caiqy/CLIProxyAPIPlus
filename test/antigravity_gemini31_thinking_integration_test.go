package test

import (
	"fmt"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/thinking/provider/antigravity"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/thinking/provider/openai"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/translator"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestAntigravityGemini31ThinkingConfigIncludesThoughts(t *testing.T) {
	tests := []struct {
		name          string
		modelWithMode string
		expectLevel   string
	}{
		{
			name:          "gemini-3.1-pro-high supports medium",
			modelWithMode: "gemini-3.1-pro-high(medium)",
			expectLevel:   "medium",
		},
		{
			name:          "gemini-3.1-pro-low supports high",
			modelWithMode: "gemini-3.1-pro-low(high)",
			expectLevel:   "high",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			baseModel := thinking.ParseSuffix(tc.modelWithMode).ModelName
			input := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}]}`, tc.modelWithMode)

			translated := sdktranslator.TranslateRequest(
				sdktranslator.FromString("openai"),
				sdktranslator.FromString("antigravity"),
				baseModel,
				[]byte(input),
				true,
			)

			output, err := thinking.ApplyThinking(translated, tc.modelWithMode, "openai", "antigravity", "antigravity")
			if err != nil {
				t.Fatalf("unexpected error: %v, body=%s", err, string(output))
			}

			level := gjson.GetBytes(output, "request.generationConfig.thinkingConfig.thinkingLevel")
			if !level.Exists() {
				t.Fatalf("expected thinkingLevel but not found, body=%s", string(output))
			}
			if level.String() != tc.expectLevel {
				t.Fatalf("thinkingLevel mismatch: expected=%s actual=%s body=%s", tc.expectLevel, level.String(), string(output))
			}

			includeThoughts := gjson.GetBytes(output, "request.generationConfig.thinkingConfig.includeThoughts")
			if !includeThoughts.Exists() {
				t.Fatalf("expected includeThoughts but not found, body=%s", string(output))
			}
			if !includeThoughts.Bool() {
				t.Fatalf("includeThoughts should be true, body=%s", string(output))
			}
		})
	}
}
