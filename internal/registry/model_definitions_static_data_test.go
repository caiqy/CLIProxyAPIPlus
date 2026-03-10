package registry

import "testing"

func TestGetAntigravityModelConfig_Gemini31SupportsThinkingLevels(t *testing.T) {
	cfg := GetAntigravityModelConfig()

	tests := []struct {
		modelID string
	}{
		{modelID: "gemini-3.1-pro-high"},
		{modelID: "gemini-3.1-pro-low"},
	}

	for _, tc := range tests {
		t.Run(tc.modelID, func(t *testing.T) {
			entry := cfg[tc.modelID]
			if entry == nil {
				t.Fatalf("model %s not found in antigravity config", tc.modelID)
			}
			if entry.Thinking == nil {
				t.Fatalf("model %s should define thinking support", tc.modelID)
			}

			levels := entry.Thinking.Levels
			expected := []string{"low", "medium", "high"}
			if len(levels) != len(expected) {
				t.Fatalf("model %s levels length mismatch: expected %d, got %d (%v)", tc.modelID, len(expected), len(levels), levels)
			}
			for i := range expected {
				if levels[i] != expected[i] {
					t.Fatalf("model %s levels mismatch: expected %v, got %v", tc.modelID, expected, levels)
				}
			}
		})
	}
}

func TestGetGitHubCopilotModels_IncludesGPT54(t *testing.T) {
	models := GetGitHubCopilotModels()
	var target *ModelInfo
	for _, m := range models {
		if m != nil && m.ID == "gpt-5.4" {
			target = m
			break
		}
	}
	if target == nil {
		t.Fatal("expected static GitHub Copilot models to include gpt-5.4")
	}
	if len(target.SupportedEndpoints) != 1 || target.SupportedEndpoints[0] != "/responses" {
		t.Fatalf("gpt-5.4 supported_endpoints = %v, want [/responses]", target.SupportedEndpoints)
	}
}

func TestGetGitHubCopilotModels_IncludesExactIDsFromModelsAPI(t *testing.T) {
	models := GetGitHubCopilotModels()
	index := make(map[string]struct{}, len(models))
	for _, m := range models {
		if m != nil {
			index[m.ID] = struct{}{}
		}
	}

	wantIDs := []string{
		"gpt-4.1-2025-04-14",
		"gpt-4o-mini-2024-07-18",
		"gpt-41-copilot",
		"text-embedding-3-small",
		"text-embedding-3-small-inference",
	}

	for _, wantID := range wantIDs {
		if _, ok := index[wantID]; !ok {
			t.Fatalf("expected static GitHub Copilot models to include %s", wantID)
		}
	}
}

func TestGetGitHubCopilotModels_ExcludesObsoleteIDs(t *testing.T) {
	models := GetGitHubCopilotModels()
	index := make(map[string]struct{}, len(models))
	for _, m := range models {
		if m != nil {
			index[m.ID] = struct{}{}
		}
	}

	obsoleteIDs := []string{
		"gpt-5",
		"gpt-5-codex",
		"claude-opus-4.1",
		"oswe-vscode-prime",
	}

	for _, obsoleteID := range obsoleteIDs {
		if _, ok := index[obsoleteID]; ok {
			t.Fatalf("expected static GitHub Copilot models to exclude obsolete model %s", obsoleteID)
		}
	}
}
