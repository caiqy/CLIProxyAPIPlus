package api

import (
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

var processStartTime = time.Now().UTC()

type debugMemorySnapshot struct {
	GoVersion      string `json:"go_version"`
	NumGoroutine   int    `json:"num_goroutine"`
	GOMAXPROCS     int    `json:"gomaxprocs"`
	Alloc          uint64 `json:"alloc"`
	TotalAlloc     uint64 `json:"total_alloc"`
	Sys            uint64 `json:"sys"`
	HeapAlloc      uint64 `json:"heap_alloc"`
	HeapSys        uint64 `json:"heap_sys"`
	HeapInuse      uint64 `json:"heap_inuse"`
	HeapIdle       uint64 `json:"heap_idle"`
	HeapReleased   uint64 `json:"heap_released"`
	HeapObjects    uint64 `json:"heap_objects"`
	StackInuse     uint64 `json:"stack_inuse"`
	StackSys       uint64 `json:"stack_sys"`
	NumGC          uint32 `json:"num_gc"`
	PauseTotalNs   uint64 `json:"pause_total_ns"`
	LastGCUnixNano uint64 `json:"last_gc_unix_nano"`
	NextGC         uint64 `json:"next_gc"`
	GCCPUFraction  uint64 `json:"gc_cpu_fraction_ppm"`
	DebugGCEnabled bool   `json:"debug_gc_enabled"`
}

type debugModelRouteDiagnostic struct {
	ModelID               string         `json:"model_id"`
	Providers             []string       `json:"providers"`
	PresentInOpenAIModels bool           `json:"present_in_openai_models"`
	OpenAIOwnedBy         string         `json:"openai_owned_by,omitempty"`
	PresentInClaudeModels bool           `json:"present_in_claude_models"`
	ClaudeOwnedBy         string         `json:"claude_owned_by,omitempty"`
	Registry              map[string]any `json:"registry"`
}

type debugAliasEntry struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Fork  bool   `json:"fork"`
}

func (s *Server) debugModelRouteMemoryHandler(c *gin.Context) {
	// This endpoint is intentionally unauthenticated.
	// It is meant for field debugging; deploy only in controlled environments.

	// Defensive limits to avoid accidental large responses.
	const maxModels = 50

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	memSnapshot := debugMemorySnapshot{
		GoVersion:      runtime.Version(),
		NumGoroutine:   runtime.NumGoroutine(),
		GOMAXPROCS:     runtime.GOMAXPROCS(0),
		Alloc:          mem.Alloc,
		TotalAlloc:     mem.TotalAlloc,
		Sys:            mem.Sys,
		HeapAlloc:      mem.HeapAlloc,
		HeapSys:        mem.HeapSys,
		HeapInuse:      mem.HeapInuse,
		HeapIdle:       mem.HeapIdle,
		HeapReleased:   mem.HeapReleased,
		HeapObjects:    mem.HeapObjects,
		StackInuse:     mem.StackInuse,
		StackSys:       mem.StackSys,
		NumGC:          mem.NumGC,
		PauseTotalNs:   mem.PauseTotalNs,
		LastGCUnixNano: mem.LastGC,
		NextGC:         mem.NextGC,
		GCCPUFraction:  uint64(mem.GCCPUFraction * 1_000_000),
		DebugGCEnabled: mem.DebugGC,
	}

	openAIModels := toModelOwnedByMap(registry.GetGlobalRegistry().GetAvailableModels("openai"))
	claudeModels := toModelOwnedByMap(registry.GetGlobalRegistry().GetAvailableModels("claude"))

	requestedModels := parseTargetModels(c.Query("models"))
	targetModels := requestedModels
	truncated := false
	if len(targetModels) > maxModels {
		targetModels = targetModels[:maxModels]
		truncated = true
	}
	diagnostics := make([]debugModelRouteDiagnostic, 0, len(targetModels))
	for _, modelID := range targetModels {
		providers := registry.GetGlobalRegistry().GetModelProviders(modelID)
		_, inOpenAI := openAIModels[modelID]
		_, inClaude := claudeModels[modelID]
		diagnostics = append(diagnostics, debugModelRouteDiagnostic{
			ModelID:               modelID,
			Providers:             providers,
			PresentInOpenAIModels: inOpenAI,
			OpenAIOwnedBy:         openAIModels[modelID],
			PresentInClaudeModels: inClaude,
			ClaudeOwnedBy:         claudeModels[modelID],
			Registry:              registry.GetGlobalRegistry().DebugModelSnapshot(modelID),
		})
	}

	hostname, _ := os.Hostname()
	regSummary := registry.GetGlobalRegistry().DebugRegistrySummary()

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"process": gin.H{
			"pid":           os.Getpid(),
			"hostname":      strings.TrimSpace(hostname),
			"start_time":    processStartTime.Format(time.RFC3339Nano),
			"start_unix_ms": processStartTime.UnixMilli(),
			"uptime_ms":     int64(time.Since(processStartTime).Milliseconds()),
		},
		"runtime":           memSnapshot,
		"registry":          regSummary,
		"model_diagnostics": diagnostics,
		"notes": gin.H{
			"present_in_openai_models":  "表示该 model 是否会出现在 OpenAI handler (/v1/models) 的输出中（与 provider 无关）",
			"present_in_claude_models":  "表示该 model 是否会出现在 Claude handler (claude-cli UA 的 /v1/models) 的输出中（与 provider 无关）",
			"model_registry_snapshot":   "model_diagnostics[*].registry 为 registry 的安全快照；client 为哈希化标识（进程内稳定，不保证跨重启一致）",
			"debug_endpoint_visibility": "该接口为无鉴权调试接口，默认直接暴露（请自行确保在受控网络环境内使用）",
		},
		"limits": gin.H{
			"max_models":       maxModels,
			"models_truncated": truncated,
			"models_requested": len(requestedModels),
			"models_returned":  len(diagnostics),
		},
		"config": gin.H{
			"github_copilot_aliases": extractGitHubCopilotAliases(s.cfg),
			"oauth_excluded_models":  extractOAuthExcludedModels(s.cfg),
			"oauth_model_alias":      extractOAuthModelAlias(s.cfg),
		},
	})
}

func parseTargetModels(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{"claude-sonnet-4-6", "claude-opus-4-6", "claude-sonnet-4.6", "claude-opus-4.6"}
	}

	seen := make(map[string]struct{})
	out := make([]string, 0)
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return []string{"claude-sonnet-4-6", "claude-opus-4-6", "claude-sonnet-4.6", "claude-opus-4.6"}
	}
	return out
}

func toModelOwnedByMap(models []map[string]any) map[string]string {
	result := make(map[string]string, len(models))
	for _, item := range models {
		id, _ := item["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ownedBy, _ := item["owned_by"].(string)
		result[id] = strings.TrimSpace(ownedBy)
	}
	return result
}

func extractGitHubCopilotAliases(cfg *config.Config) []debugAliasEntry {
	if cfg == nil || len(cfg.OAuthModelAlias) == 0 {
		return nil
	}

	entries := make([]debugAliasEntry, 0)
	for key, aliases := range cfg.OAuthModelAlias {
		if !strings.EqualFold(strings.TrimSpace(key), "github-copilot") {
			continue
		}
		for _, item := range aliases {
			name := strings.TrimSpace(item.Name)
			alias := strings.TrimSpace(item.Alias)
			if name == "" || alias == "" {
				continue
			}
			entries = append(entries, debugAliasEntry{Name: name, Alias: alias, Fork: item.Fork})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			return entries[i].Alias < entries[j].Alias
		}
		return entries[i].Name < entries[j].Name
	})
	if len(entries) == 0 {
		return nil
	}
	return entries
}

func extractOAuthExcludedModels(cfg *config.Config) map[string][]string {
	if cfg == nil || len(cfg.OAuthExcludedModels) == 0 {
		return nil
	}
	out := make(map[string][]string, len(cfg.OAuthExcludedModels))
	for k, v := range cfg.OAuthExcludedModels {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		items := make([]string, 0, len(v))
		for _, item := range v {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			items = append(items, trimmed)
		}
		if len(items) == 0 {
			continue
		}
		sort.Strings(items)
		out[key] = items
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractOAuthModelAlias(cfg *config.Config) map[string][]debugAliasEntry {
	if cfg == nil || len(cfg.OAuthModelAlias) == 0 {
		return nil
	}
	out := make(map[string][]debugAliasEntry, len(cfg.OAuthModelAlias))
	for channel, entries := range cfg.OAuthModelAlias {
		key := strings.TrimSpace(channel)
		if key == "" {
			continue
		}
		converted := make([]debugAliasEntry, 0, len(entries))
		for _, entry := range entries {
			name := strings.TrimSpace(entry.Name)
			alias := strings.TrimSpace(entry.Alias)
			if name == "" || alias == "" {
				continue
			}
			converted = append(converted, debugAliasEntry{Name: name, Alias: alias, Fork: entry.Fork})
		}
		if len(converted) == 0 {
			continue
		}
		sort.Slice(converted, func(i, j int) bool {
			if converted[i].Name == converted[j].Name {
				return converted[i].Alias < converted[j].Alias
			}
			return converted[i].Name < converted[j].Name
		})
		out[key] = converted
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
