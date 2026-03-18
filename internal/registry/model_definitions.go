// Package registry provides model definitions and lookup helpers for various AI providers.
// Static model metadata is loaded from the embedded models.json file and can be refreshed from network.
package registry

import (
	"strings"
)

// staticModelsJSON mirrors the top-level structure of models.json.
type staticModelsJSON struct {
	Claude      []*ModelInfo `json:"claude"`
	Gemini      []*ModelInfo `json:"gemini"`
	Vertex      []*ModelInfo `json:"vertex"`
	GeminiCLI   []*ModelInfo `json:"gemini-cli"`
	AIStudio    []*ModelInfo `json:"aistudio"`
	CodexFree   []*ModelInfo `json:"codex-free"`
	CodexTeam   []*ModelInfo `json:"codex-team"`
	CodexPlus   []*ModelInfo `json:"codex-plus"`
	CodexPro    []*ModelInfo `json:"codex-pro"`
	Qwen        []*ModelInfo `json:"qwen"`
	IFlow       []*ModelInfo `json:"iflow"`
	Kimi        []*ModelInfo `json:"kimi"`
	Antigravity []*ModelInfo `json:"antigravity"`
}

// GetClaudeModels returns the standard Claude model definitions.
func GetClaudeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Claude)
}

// GetGeminiModels returns the standard Gemini model definitions.
func GetGeminiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Gemini)
}

// GetGeminiVertexModels returns Gemini model definitions for Vertex AI.
func GetGeminiVertexModels() []*ModelInfo {
	return cloneModelInfos(getModels().Vertex)
}

// GetGeminiCLIModels returns Gemini model definitions for the Gemini CLI.
func GetGeminiCLIModels() []*ModelInfo {
	return cloneModelInfos(getModels().GeminiCLI)
}

// GetAIStudioModels returns model definitions for AI Studio.
func GetAIStudioModels() []*ModelInfo {
	return cloneModelInfos(getModels().AIStudio)
}

// GetCodexFreeModels returns model definitions for the Codex free plan tier.
func GetCodexFreeModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexFree)
}

// GetCodexTeamModels returns model definitions for the Codex team plan tier.
func GetCodexTeamModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexTeam)
}

// GetCodexPlusModels returns model definitions for the Codex plus plan tier.
func GetCodexPlusModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPlus)
}

// GetCodexProModels returns model definitions for the Codex pro plan tier.
func GetCodexProModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPro)
}

// GetQwenModels returns the standard Qwen model definitions.
func GetQwenModels() []*ModelInfo {
	return cloneModelInfos(getModels().Qwen)
}

// GetIFlowModels returns the standard iFlow model definitions.
func GetIFlowModels() []*ModelInfo {
	return cloneModelInfos(getModels().IFlow)
}

// GetKimiModels returns the standard Kimi (Moonshot AI) model definitions.
func GetKimiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Kimi)
}

// GetAntigravityModels returns the standard Antigravity model definitions.
func GetAntigravityModels() []*ModelInfo {
	return cloneModelInfos(getModels().Antigravity)
}

// GetAntigravityModelConfig preserves the legacy map-based access pattern for
// Antigravity model metadata while using the current upstream model catalog as
// the single source of truth.
func GetAntigravityModelConfig() map[string]*ModelInfo {
	models := GetAntigravityModels()
	if len(models) == 0 {
		return nil
	}
	config := make(map[string]*ModelInfo, len(models))
	for _, model := range models {
		if model == nil || strings.TrimSpace(model.ID) == "" {
			continue
		}
		config[model.ID] = cloneModelInfo(model)
	}
	return config
}

// cloneModelInfos returns a shallow copy of the slice with each element deep-cloned.
func cloneModelInfos(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*ModelInfo, len(models))
	for i, m := range models {
		out[i] = cloneModelInfo(m)
	}
	return out
}

// GetStaticModelDefinitionsByChannel returns static model definitions for a given channel/provider.
// It returns nil when the channel is unknown.
//
// Supported channels:
//   - claude
//   - gemini
//   - vertex
//   - gemini-cli
//   - aistudio
//   - codex
//   - qwen
//   - iflow
//   - kimi
//   - kilo
//   - github-copilot
//   - amazonq
//   - antigravity (returns static overrides only)
func GetStaticModelDefinitionsByChannel(channel string) []*ModelInfo {
	key := strings.ToLower(strings.TrimSpace(channel))
	switch key {
	case "claude":
		return GetClaudeModels()
	case "gemini":
		return GetGeminiModels()
	case "vertex":
		return GetGeminiVertexModels()
	case "gemini-cli":
		return GetGeminiCLIModels()
	case "aistudio":
		return GetAIStudioModels()
	case "codex":
		return GetCodexProModels()
	case "qwen":
		return GetQwenModels()
	case "iflow":
		return GetIFlowModels()
	case "kimi":
		return GetKimiModels()
	case "github-copilot":
		return GetGitHubCopilotModels()
	case "kiro":
		return GetKiroModels()
	case "kilo":
		return GetKiloModels()
	case "amazonq":
		return GetAmazonQModels()
	case "antigravity":
		return GetAntigravityModels()
	default:
		return nil
	}
}

// LookupStaticModelInfo searches all static model definitions for a model by ID.
// Returns nil if no matching model is found.
func LookupStaticModelInfo(modelID string) *ModelInfo {
	if modelID == "" {
		return nil
	}

	data := getModels()
	allModels := [][]*ModelInfo{
		data.Claude,
		data.Gemini,
		data.Vertex,
		data.GeminiCLI,
		data.AIStudio,
		data.CodexPro,
		data.Qwen,
		data.IFlow,
		data.Kimi,
		data.Antigravity,
		GetGitHubCopilotModels(),
		GetKiroModels(),
		GetKiloModels(),
		GetAmazonQModels(),
	}
	for _, models := range allModels {
		for _, m := range models {
			if m != nil && m.ID == modelID {
				return cloneModelInfo(m)
			}
		}
	}

	return nil
}

// GetGitHubCopilotModels returns the available models for GitHub Copilot.
// These models are available through the GitHub Copilot API at api.business.githubcopilot.com.
func GetGitHubCopilotModels() []*ModelInfo {
	now := int64(1772755200) // 2026-03-06, synced from Copilot /models snapshot

	type copilotModelDef struct {
		ID                  string
		DisplayName         string
		Description         string
		ContextLength       int
		MaxCompletionTokens int
		SupportedEndpoints  []string
		ThinkingLevels      []string
		// ThinkingMinBudget/ThinkingMaxBudget enable budget-based (or hybrid) thinking.
		// When both ThinkingLevels and ThinkingMin/MaxBudget are set, the model is CapabilityHybrid.
		ThinkingMinBudget   int
		ThinkingMaxBudget   int
		ThinkingZeroAllowed bool
		ThinkingDynamic     bool
	}

	defs := []copilotModelDef{
		{ID: "claude-opus-4.6", DisplayName: "Claude Opus 4.6", Description: "Anthropic Claude Opus 4.6 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/v1/messages", "/chat/completions"}, ThinkingLevels: []string{"low", "medium", "high"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 64000, ThinkingZeroAllowed: true, ThinkingDynamic: true},
		{ID: "claude-sonnet-4.6", DisplayName: "Claude Sonnet 4.6", Description: "Anthropic Claude Sonnet 4.6 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 32000, ThinkingZeroAllowed: true, ThinkingDynamic: true},
		{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro", Description: "Google Gemini 3.1 Pro via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/chat/completions"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-5.2-codex", DisplayName: "GPT-5.2-Codex", Description: "OpenAI GPT-5.2-Codex via GitHub Copilot", ContextLength: 400000, MaxCompletionTokens: 128000, SupportedEndpoints: []string{"/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3-Codex", Description: "OpenAI GPT-5.3-Codex via GitHub Copilot", ContextLength: 400000, MaxCompletionTokens: 128000, SupportedEndpoints: []string{"/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-5.4", DisplayName: "GPT-5.4", Description: "OpenAI GPT-5.4 via GitHub Copilot", ContextLength: 400000, MaxCompletionTokens: 128000, SupportedEndpoints: []string{"/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-5-mini", DisplayName: "GPT-5 mini", Description: "OpenAI GPT-5 mini via GitHub Copilot", ContextLength: 264000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/chat/completions", "/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-4o-mini-2024-07-18", DisplayName: "GPT-4o mini", Description: "OpenAI GPT-4o mini 2024-07-18 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 4096},
		{ID: "gpt-4o-2024-11-20", DisplayName: "GPT-4o", Description: "OpenAI GPT-4o 2024-11-20 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 16384},
		{ID: "gpt-4o-2024-08-06", DisplayName: "GPT-4o", Description: "OpenAI GPT-4o 2024-08-06 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 16384},
		{ID: "grok-code-fast-1", DisplayName: "Grok Code Fast 1", Description: "xAI Grok Code Fast 1 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 64000},
		{ID: "gpt-5.1", DisplayName: "GPT-5.1", Description: "OpenAI GPT-5.1 via GitHub Copilot", ContextLength: 264000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/chat/completions", "/responses"}, ThinkingLevels: []string{"none", "low", "medium", "high"}},
		{ID: "gpt-5.1-codex", DisplayName: "GPT-5.1-Codex", Description: "OpenAI GPT-5.1-Codex via GitHub Copilot", ContextLength: 400000, MaxCompletionTokens: 128000, SupportedEndpoints: []string{"/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-5.1-codex-mini", DisplayName: "GPT-5.1-Codex-Mini", Description: "OpenAI GPT-5.1-Codex-Mini via GitHub Copilot", ContextLength: 400000, MaxCompletionTokens: 128000, SupportedEndpoints: []string{"/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gpt-5.1-codex-max", DisplayName: "GPT-5.1-Codex-Max", Description: "OpenAI GPT-5.1-Codex-Max via GitHub Copilot", ContextLength: 400000, MaxCompletionTokens: 128000, SupportedEndpoints: []string{"/responses"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "text-embedding-3-small", DisplayName: "Embedding V3 small", Description: "OpenAI Embedding V3 small via GitHub Copilot"},
		{ID: "text-embedding-3-small-inference", DisplayName: "Embedding V3 small (Inference)", Description: "OpenAI Embedding V3 small (Inference) via GitHub Copilot"},
		{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", Description: "Anthropic Claude Sonnet 4 via GitHub Copilot", ContextLength: 216000, MaxCompletionTokens: 16000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 16000, ThinkingZeroAllowed: true},
		{ID: "claude-sonnet-4.5", DisplayName: "Claude Sonnet 4.5", Description: "Anthropic Claude Sonnet 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 32000, ThinkingZeroAllowed: true},
		{ID: "claude-opus-4.5", DisplayName: "Claude Opus 4.5", Description: "Anthropic Claude Opus 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 32000, ThinkingZeroAllowed: true},
		{ID: "claude-haiku-4.5", DisplayName: "Claude Haiku 4.5", Description: "Anthropic Claude Haiku 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 32000, ThinkingZeroAllowed: true},
		{ID: "gemini-3-pro-preview", DisplayName: "Gemini 3 Pro (Preview)", Description: "Google Gemini 3 Pro (Preview) via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 64000, ThinkingLevels: []string{"low", "high"}},
		{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash (Preview)", Description: "Google Gemini 3 Flash (Preview) via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 64000, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", Description: "Google Gemini 2.5 Pro via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 64000},
		{ID: "gpt-4.1-2025-04-14", DisplayName: "GPT-4.1", Description: "OpenAI GPT-4.1 2025-04-14 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 16384},
		{ID: "gpt-5.2", DisplayName: "GPT-5.2", Description: "OpenAI GPT-5.2 via GitHub Copilot", ContextLength: 264000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/chat/completions", "/responses"}},
		{ID: "gpt-41-copilot", DisplayName: "GPT-4.1 Copilot", Description: "OpenAI GPT-4.1 Copilot via GitHub Copilot"},
		{ID: "gpt-3.5-turbo-0613", DisplayName: "GPT 3.5 Turbo", Description: "OpenAI GPT 3.5 Turbo 0613 via GitHub Copilot", ContextLength: 16384, MaxCompletionTokens: 4096},
		{ID: "gpt-4", DisplayName: "GPT 4", Description: "OpenAI GPT 4 via GitHub Copilot", ContextLength: 32768, MaxCompletionTokens: 4096},
		{ID: "gpt-4-0613", DisplayName: "GPT 4", Description: "OpenAI GPT 4 0613 via GitHub Copilot", ContextLength: 32768, MaxCompletionTokens: 4096},
		{ID: "gpt-4-0125-preview", DisplayName: "GPT 4 Turbo", Description: "OpenAI GPT 4 Turbo via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 4096},
		{ID: "gpt-4o-2024-05-13", DisplayName: "GPT-4o", Description: "OpenAI GPT-4o 2024-05-13 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 4096},
		{ID: "gpt-4-o-preview", DisplayName: "GPT-4o", Description: "OpenAI GPT-4o Preview via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 4096},
		{ID: "gpt-4.1", DisplayName: "GPT-4.1", Description: "OpenAI GPT-4.1 via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 16384},
		{ID: "gpt-3.5-turbo", DisplayName: "GPT 3.5 Turbo", Description: "OpenAI GPT 3.5 Turbo via GitHub Copilot", ContextLength: 16384, MaxCompletionTokens: 4096},
		{ID: "gpt-4o-mini", DisplayName: "GPT-4o mini", Description: "OpenAI GPT-4o mini via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 4096},
		{ID: "gpt-4o", DisplayName: "GPT-4o", Description: "OpenAI GPT-4o via GitHub Copilot", ContextLength: 128000, MaxCompletionTokens: 4096},
		{ID: "text-embedding-ada-002", DisplayName: "Embedding V2 Ada", Description: "OpenAI Embedding V2 Ada via GitHub Copilot"},
	}

	models := make([]*ModelInfo, 0, len(defs))
	for _, def := range defs {
		m := &ModelInfo{
			ID:          def.ID,
			Object:      "model",
			Created:     now,
			OwnedBy:     "github-copilot",
			Type:        "github-copilot",
			DisplayName: def.DisplayName,
			Description: def.Description,
		}
		if def.ContextLength > 0 {
			m.ContextLength = def.ContextLength
		}
		if def.MaxCompletionTokens > 0 {
			m.MaxCompletionTokens = def.MaxCompletionTokens
		}
		if len(def.SupportedEndpoints) > 0 {
			m.SupportedEndpoints = append([]string(nil), def.SupportedEndpoints...)
		}
		hasBudget := def.ThinkingMinBudget > 0 || def.ThinkingMaxBudget > 0
		if len(def.ThinkingLevels) > 0 || hasBudget {
			m.Thinking = &ThinkingSupport{
				Levels:         append([]string(nil), def.ThinkingLevels...),
				Min:            def.ThinkingMinBudget,
				Max:            def.ThinkingMaxBudget,
				ZeroAllowed:    def.ThinkingZeroAllowed,
				DynamicAllowed: def.ThinkingDynamic,
			}
		}
		models = append(models, m)
	}

	return models
}

// GetKiroModels returns the Kiro (AWS CodeWhisperer) model definitions
func GetKiroModels() []*ModelInfo {
	return []*ModelInfo{
		// --- Base Models ---
		{
			ID:                  "kiro-auto",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Auto",
			Description:         "Automatic model selection by Kiro",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-opus-4-6",
			Object:              "model",
			Created:             1736899200, // 2025-01-15
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Opus 4.6",
			Description:         "Claude Opus 4.6 via Kiro (2.2x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-sonnet-4-6",
			Object:              "model",
			Created:             1739836800, // 2025-02-18
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Sonnet 4.6",
			Description:         "Claude Sonnet 4.6 via Kiro (1.3x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-opus-4-5",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Opus 4.5",
			Description:         "Claude Opus 4.5 via Kiro (2.2x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-sonnet-4-5",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Sonnet 4.5",
			Description:         "Claude Sonnet 4.5 via Kiro (1.3x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-sonnet-4",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Sonnet 4",
			Description:         "Claude Sonnet 4 via Kiro (1.3x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-haiku-4-5",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Haiku 4.5",
			Description:         "Claude Haiku 4.5 via Kiro (0.4x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		// --- 第三方模型 (通过 Kiro 接入) ---
		{
			ID:                  "kiro-deepseek-3-2",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro DeepSeek 3.2",
			Description:         "DeepSeek 3.2 via Kiro",
			ContextLength:       128000,
			MaxCompletionTokens: 32768,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-minimax-m2-1",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro MiniMax M2.1",
			Description:         "MiniMax M2.1 via Kiro",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-qwen3-coder-next",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Qwen3 Coder Next",
			Description:         "Qwen3 Coder Next via Kiro",
			ContextLength:       128000,
			MaxCompletionTokens: 32768,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-gpt-4o",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro GPT-4o",
			Description:         "OpenAI GPT-4o via Kiro",
			ContextLength:       128000,
			MaxCompletionTokens: 16384,
		},
		{
			ID:                  "kiro-gpt-4",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro GPT-4",
			Description:         "OpenAI GPT-4 via Kiro",
			ContextLength:       128000,
			MaxCompletionTokens: 8192,
		},
		{
			ID:                  "kiro-gpt-4-turbo",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro GPT-4 Turbo",
			Description:         "OpenAI GPT-4 Turbo via Kiro",
			ContextLength:       128000,
			MaxCompletionTokens: 16384,
		},
		{
			ID:                  "kiro-gpt-3-5-turbo",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro GPT-3.5 Turbo",
			Description:         "OpenAI GPT-3.5 Turbo via Kiro",
			ContextLength:       16384,
			MaxCompletionTokens: 4096,
		},
		// --- Agentic Variants (Optimized for coding agents with chunked writes) ---
		{
			ID:                  "kiro-claude-opus-4-6-agentic",
			Object:              "model",
			Created:             1736899200, // 2025-01-15
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Opus 4.6 (Agentic)",
			Description:         "Claude Opus 4.6 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-sonnet-4-6-agentic",
			Object:              "model",
			Created:             1739836800, // 2025-02-18
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Sonnet 4.6 (Agentic)",
			Description:         "Claude Sonnet 4.6 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-opus-4-5-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Opus 4.5 (Agentic)",
			Description:         "Claude Opus 4.5 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-sonnet-4-5-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Sonnet 4.5 (Agentic)",
			Description:         "Claude Sonnet 4.5 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-sonnet-4-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Sonnet 4 (Agentic)",
			Description:         "Claude Sonnet 4 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-claude-haiku-4-5-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Claude Haiku 4.5 (Agentic)",
			Description:         "Claude Haiku 4.5 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-deepseek-3-2-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro DeepSeek 3.2 (Agentic)",
			Description:         "DeepSeek 3.2 optimized for coding agents (chunked writes)",
			ContextLength:       128000,
			MaxCompletionTokens: 32768,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-minimax-m2-1-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro MiniMax M2.1 (Agentic)",
			Description:         "MiniMax M2.1 optimized for coding agents (chunked writes)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:                  "kiro-qwen3-coder-next-agentic",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Kiro Qwen3 Coder Next (Agentic)",
			Description:         "Qwen3 Coder Next optimized for coding agents (chunked writes)",
			ContextLength:       128000,
			MaxCompletionTokens: 32768,
			Thinking:            &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
		},
	}
}

// GetAmazonQModels returns the Amazon Q (AWS CodeWhisperer) model definitions.
// These models use the same API as Kiro and share the same executor.
func GetAmazonQModels() []*ModelInfo {
	return []*ModelInfo{
		{
			ID:                  "amazonq-auto",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro", // Uses Kiro executor - same API
			DisplayName:         "Amazon Q Auto",
			Description:         "Automatic model selection by Amazon Q",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		},
		{
			ID:                  "amazonq-claude-opus-4.5",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Amazon Q Claude Opus 4.5",
			Description:         "Claude Opus 4.5 via Amazon Q (2.2x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		},
		{
			ID:                  "amazonq-claude-sonnet-4.5",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Amazon Q Claude Sonnet 4.5",
			Description:         "Claude Sonnet 4.5 via Amazon Q (1.3x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		},
		{
			ID:                  "amazonq-claude-sonnet-4",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Amazon Q Claude Sonnet 4",
			Description:         "Claude Sonnet 4 via Amazon Q (1.3x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		},
		{
			ID:                  "amazonq-claude-haiku-4.5",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "aws",
			Type:                "kiro",
			DisplayName:         "Amazon Q Claude Haiku 4.5",
			Description:         "Claude Haiku 4.5 via Amazon Q (0.4x credit)",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		},
	}
}
