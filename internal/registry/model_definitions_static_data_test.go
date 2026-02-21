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
