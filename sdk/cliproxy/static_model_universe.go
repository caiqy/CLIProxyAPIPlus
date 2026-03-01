package cliproxy

import (
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

// GetStaticProviderModelUniverse returns the static model ID universe for a provider.
// The returned list is normalized, deduplicated and sorted.
func GetStaticProviderModelUniverse(provider string) []string {
	key := normalizeStaticUniverseProvider(provider)
	if key == "" {
		return nil
	}

	infos := registry.GetStaticModelDefinitionsByChannel(key)
	if len(infos) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(infos))
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		id := strings.TrimSpace(info.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func normalizeStaticUniverseProvider(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch key {
	case "openai", "openai-chat-completions", "openai-compatibility", "openai-chat", "openai-chatcompletion", "openai-chat-completion", "openai-chatcompletions", "openai-chat-completion-v1":
		return "codex"
	default:
		return key
	}
}
