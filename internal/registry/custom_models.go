package registry

import (
	"encoding/json"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

type customOverlaySummary struct {
	Overridden int `json:"overridden"`
	Added      int `json:"added"`
}

type modelSectionSpec struct {
	key string
	get func(*staticModelsJSON) []*ModelInfo
	set func(*staticModelsJSON, []*ModelInfo)
}

var staticModelSectionSpecs = []modelSectionSpec{
	{key: "claude", get: func(data *staticModelsJSON) []*ModelInfo { return data.Claude }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.Claude = models }},
	{key: "gemini", get: func(data *staticModelsJSON) []*ModelInfo { return data.Gemini }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.Gemini = models }},
	{key: "vertex", get: func(data *staticModelsJSON) []*ModelInfo { return data.Vertex }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.Vertex = models }},
	{key: "gemini-cli", get: func(data *staticModelsJSON) []*ModelInfo { return data.GeminiCLI }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.GeminiCLI = models }},
	{key: "aistudio", get: func(data *staticModelsJSON) []*ModelInfo { return data.AIStudio }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.AIStudio = models }},
	{key: "codex-free", get: func(data *staticModelsJSON) []*ModelInfo { return data.CodexFree }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.CodexFree = models }},
	{key: "codex-team", get: func(data *staticModelsJSON) []*ModelInfo { return data.CodexTeam }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.CodexTeam = models }},
	{key: "codex-plus", get: func(data *staticModelsJSON) []*ModelInfo { return data.CodexPlus }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.CodexPlus = models }},
	{key: "codex-pro", get: func(data *staticModelsJSON) []*ModelInfo { return data.CodexPro }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.CodexPro = models }},
	{key: "qwen", get: func(data *staticModelsJSON) []*ModelInfo { return data.Qwen }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.Qwen = models }},
	{key: "iflow", get: func(data *staticModelsJSON) []*ModelInfo { return data.IFlow }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.IFlow = models }},
	{key: "kimi", get: func(data *staticModelsJSON) []*ModelInfo { return data.Kimi }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.Kimi = models }},
	{key: "antigravity", get: func(data *staticModelsJSON) []*ModelInfo { return data.Antigravity }, set: func(data *staticModelsJSON, models []*ModelInfo) { data.Antigravity = models }},
}

func parseCustomModelsFile(path string) (*staticModelsJSON, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(trimmedPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warnf("custom models file not found, skipping overlay: %s", trimmedPath)
			return nil, nil
		}
		log.Warnf("custom models read failed, skipping overlay: %v", err)
		return nil, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Warnf("custom models parse failed, skipping overlay: %v", err)
		return nil, nil
	}

	parsed := &staticModelsJSON{}
	known := make(map[string]modelSectionSpec, len(staticModelSectionSpecs))
	for _, spec := range staticModelSectionSpecs {
		known[spec.key] = spec
	}
	for key, value := range raw {
		spec, ok := known[strings.ToLower(strings.TrimSpace(key))]
		if !ok {
			log.Warnf("custom models contains unsupported provider section %q, skipping", key)
			continue
		}
		var models []*ModelInfo
		if err := json.Unmarshal(value, &models); err != nil {
			log.Warnf("custom models section %q is invalid, skipping: %v", key, err)
			continue
		}
		spec.set(parsed, sanitizeCustomModelItems(models))
	}

	if customCatalogEmpty(parsed) {
		return nil, nil
	}
	return parsed, nil
}

func sanitizeCustomModelItems(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		clone := cloneModelInfo(model)
		clone.ID = id
		out = append(out, clone)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func customCatalogEmpty(data *staticModelsJSON) bool {
	if data == nil {
		return true
	}
	for _, spec := range staticModelSectionSpecs {
		if len(spec.get(data)) > 0 {
			return false
		}
	}
	return true
}

func overlayModels(base, custom *staticModelsJSON) (*staticModelsJSON, map[string]customOverlaySummary) {
	merged := cloneStaticModels(base)
	if merged == nil {
		merged = &staticModelsJSON{}
	}
	summaries := make(map[string]customOverlaySummary)
	if custom == nil {
		return merged, summaries
	}
	for _, spec := range staticModelSectionSpecs {
		section, summary := overlaySection(spec.get(merged), spec.get(custom))
		spec.set(merged, section)
		if summary.Added > 0 || summary.Overridden > 0 {
			summaries[spec.key] = summary
		}
	}
	return merged, summaries
}

func overlaySection(base, custom []*ModelInfo) ([]*ModelInfo, customOverlaySummary) {
	if len(base) == 0 && len(custom) == 0 {
		return nil, customOverlaySummary{}
	}
	result := cloneModelInfos(base)
	index := make(map[string]int, len(result))
	for i, model := range result {
		if model == nil || strings.TrimSpace(model.ID) == "" {
			continue
		}
		index[model.ID] = i
	}
	var summary customOverlaySummary
	for _, model := range custom {
		if model == nil || strings.TrimSpace(model.ID) == "" {
			continue
		}
		clone := cloneModelInfo(model)
		if pos, ok := index[clone.ID]; ok {
			result[pos] = clone
			summary.Overridden++
			continue
		}
		index[clone.ID] = len(result)
		result = append(result, clone)
		summary.Added++
	}
	if len(result) == 0 {
		return nil, summary
	}
	return result, summary
}

func cloneStaticModels(data *staticModelsJSON) *staticModelsJSON {
	if data == nil {
		return nil
	}
	cloned := &staticModelsJSON{}
	for _, spec := range staticModelSectionSpecs {
		spec.set(cloned, cloneModelInfos(spec.get(data)))
	}
	return cloned
}
