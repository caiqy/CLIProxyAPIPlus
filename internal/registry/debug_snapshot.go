package registry

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	debugClientHashKey     []byte
	debugClientHashKeyOnce sync.Once
)

// DebugRegistrySummary returns a safe, JSON-friendly summary of registry state.
// It is intended for unauthenticated debug endpoints.
func (r *ModelRegistry) DebugRegistrySummary() map[string]any {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	providers := make(map[string]int)
	for _, provider := range r.clientProviders {
		p := strings.ToLower(strings.TrimSpace(provider))
		if p == "" {
			continue
		}
		providers[p]++
	}

	return map[string]any{
		"models_count":           len(r.models),
		"clients_count":          len(r.clientModels),
		"client_providers_count": len(r.clientProviders),
		"providers":              providers,
	}
}

// DebugModelSnapshot returns safe, JSON-friendly diagnostics for a specific model.
// It never includes raw client IDs; client IDs are hashed.
func (r *ModelRegistry) DebugModelSnapshot(modelID string) map[string]any {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return map[string]any{"model_id": "", "exists": false}
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	registration, ok := r.models[modelID]
	if !ok || registration == nil {
		return map[string]any{"model_id": modelID, "exists": false}
	}

	quotaExpiredDuration := 5 * time.Minute
	now := time.Now()

	providers := make(map[string]int)
	for name, count := range registration.Providers {
		if strings.TrimSpace(name) == "" || count <= 0 {
			continue
		}
		providers[name] = count
	}

	type providerCount struct {
		name  string
		count int
	}
	sortedProviders := make([]providerCount, 0, len(providers))
	for name, count := range providers {
		sortedProviders = append(sortedProviders, providerCount{name: name, count: count})
	}
	sort.Slice(sortedProviders, func(i, j int) bool {
		if sortedProviders[i].count == sortedProviders[j].count {
			return sortedProviders[i].name < sortedProviders[j].name
		}
		return sortedProviders[i].count > sortedProviders[j].count
	})
	providerNames := make([]string, 0, len(sortedProviders))
	for _, item := range sortedProviders {
		providerNames = append(providerNames, item.name)
	}

	providersFromClients := make(map[string]int)
	providersFromClientsUnique := make(map[string]int)
	for clientID, modelIDs := range r.clientModels {
		if clientID == "" || len(modelIDs) == 0 {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(r.clientProviders[clientID]))
		if provider == "" {
			continue
		}
		matchedCount := 0
		for _, candidate := range modelIDs {
			if strings.EqualFold(strings.TrimSpace(candidate), modelID) {
				matchedCount++
			}
		}
		if matchedCount == 0 {
			continue
		}
		providersFromClients[provider] += matchedCount
		providersFromClientsUnique[provider]++
	}
	providersFromClientsSorted := make([]providerCount, 0, len(providersFromClients))
	for name, count := range providersFromClients {
		providersFromClientsSorted = append(providersFromClientsSorted, providerCount{name: name, count: count})
	}
	sort.Slice(providersFromClientsSorted, func(i, j int) bool {
		if providersFromClientsSorted[i].count == providersFromClientsSorted[j].count {
			return providersFromClientsSorted[i].name < providersFromClientsSorted[j].name
		}
		return providersFromClientsSorted[i].count > providersFromClientsSorted[j].count
	})
	providersFromClientsNames := make([]string, 0, len(providersFromClientsSorted))
	for _, item := range providersFromClientsSorted {
		providersFromClientsNames = append(providersFromClientsNames, item.name)
	}

	providersFromClientsUniqueSorted := make([]providerCount, 0, len(providersFromClientsUnique))
	for name, count := range providersFromClientsUnique {
		providersFromClientsUniqueSorted = append(providersFromClientsUniqueSorted, providerCount{name: name, count: count})
	}
	sort.Slice(providersFromClientsUniqueSorted, func(i, j int) bool {
		if providersFromClientsUniqueSorted[i].count == providersFromClientsUniqueSorted[j].count {
			return providersFromClientsUniqueSorted[i].name < providersFromClientsUniqueSorted[j].name
		}
		return providersFromClientsUniqueSorted[i].count > providersFromClientsUniqueSorted[j].count
	})
	providersFromClientsUniqueNames := make([]string, 0, len(providersFromClientsUniqueSorted))
	for _, item := range providersFromClientsUniqueSorted {
		providersFromClientsUniqueNames = append(providersFromClientsUniqueNames, item.name)
	}

	providerIndexMissing := len(providers) == 0 && registration.Count > 0
	providerIndexMismatch := false
	providerIndexDifferences := make(map[string]any)
	if providerIndexMissing {
		providerIndexMismatch = len(providersFromClients) > 0
	} else {
		for name, count := range providersFromClients {
			if idxCount, ok := providers[name]; !ok || idxCount != count {
				providerIndexMismatch = true
				providerIndexDifferences[name] = map[string]any{"index": providers[name], "from_clients": count}
			}
		}
		for name, count := range providers {
			if cliCount, ok := providersFromClients[name]; !ok || cliCount != count {
				providerIndexMismatch = true
				providerIndexDifferences[name] = map[string]any{"index": count, "from_clients": providersFromClients[name]}
			}
		}
	}

	expiredClients := 0
	if registration.QuotaExceededClients != nil {
		for _, quotaTime := range registration.QuotaExceededClients {
			if quotaTime != nil && now.Sub(*quotaTime) < quotaExpiredDuration {
				expiredClients++
			}
		}
	}
	cooldownSuspended := 0
	otherSuspended := 0
	if registration.SuspendedClients != nil {
		for _, reason := range registration.SuspendedClients {
			if strings.EqualFold(strings.TrimSpace(reason), "quota") {
				cooldownSuspended++
				continue
			}
			otherSuspended++
		}
	}
	effective := registration.Count - expiredClients - otherSuspended
	if effective < 0 {
		effective = 0
	}

	const maxClientEntries = 200
	clientEntries := make([]map[string]any, 0)
	clientEntriesTruncated := false
	missingProviderCount := 0
	for clientID, modelIDs := range r.clientModels {
		if clientID == "" || len(modelIDs) == 0 {
			continue
		}
		matchedCount := 0
		for _, candidate := range modelIDs {
			if strings.EqualFold(strings.TrimSpace(candidate), modelID) {
				matchedCount++
			}
		}
		if matchedCount == 0 {
			continue
		}

		provider := strings.TrimSpace(r.clientProviders[clientID])
		providerMissing := provider == ""
		if providerMissing {
			missingProviderCount++
		}

		quotaRecent := false
		if registration.QuotaExceededClients != nil {
			if quotaTime := registration.QuotaExceededClients[clientID]; quotaTime != nil {
				quotaRecent = now.Sub(*quotaTime) < quotaExpiredDuration
			}
		}
		suspendedReason := ""
		if registration.SuspendedClients != nil {
			suspendedReason = strings.TrimSpace(registration.SuspendedClients[clientID])
		}
		if len(clientEntries) >= maxClientEntries {
			clientEntriesTruncated = true
			continue
		}
		clientEntries = append(clientEntries, map[string]any{
			"client":             hashClientID(clientID),
			"provider":           provider,
			"provider_missing":   providerMissing,
			"registration_count": matchedCount,
			"quota_recent":       quotaRecent,
			"suspended_reason":   suspendedReason,
		})
	}
	sort.Slice(clientEntries, func(i, j int) bool {
		ci, _ := clientEntries[i]["client"].(string)
		cj, _ := clientEntries[j]["client"].(string)
		return ci < cj
	})

	info := map[string]any{}
	if registration.Info != nil {
		info["owned_by"] = registration.Info.OwnedBy
		info["type"] = registration.Info.Type
		info["display_name"] = registration.Info.DisplayName
		info["context_length"] = registration.Info.ContextLength
		info["max_completion_tokens"] = registration.Info.MaxCompletionTokens
		if registration.Info.Thinking != nil {
			info["thinking"] = map[string]any{
				"min":             registration.Info.Thinking.Min,
				"max":             registration.Info.Thinking.Max,
				"zero_allowed":    registration.Info.Thinking.ZeroAllowed,
				"dynamic_allowed": registration.Info.Thinking.DynamicAllowed,
			}
		}
	}

	infoByProvider := make(map[string]any)
	if registration.InfoByProvider != nil {
		for provider, mi := range registration.InfoByProvider {
			if provider == "" || mi == nil {
				continue
			}
			infoByProvider[provider] = map[string]any{
				"owned_by":              mi.OwnedBy,
				"type":                  mi.Type,
				"display_name":          mi.DisplayName,
				"context_length":        mi.ContextLength,
				"max_completion_tokens": mi.MaxCompletionTokens,
			}
		}
	}

	return map[string]any{
		"model_id": modelID,
		"exists":   true,
		"registration": map[string]any{
			"count":                                registration.Count,
			"effective_clients":                    effective,
			"expired_quota_clients_recent":         expiredClients,
			"cooldown_suspended_clients":           cooldownSuspended,
			"other_suspended_clients":              otherSuspended,
			"providers":                            providers,
			"providers_sorted":                     providerNames,
			"providers_from_clients":               providersFromClients,
			"providers_from_clients_sorted":        providersFromClientsNames,
			"providers_from_clients_unique":        providersFromClientsUnique,
			"providers_from_clients_unique_sorted": providersFromClientsUniqueNames,
			"provider_index_missing":               providerIndexMissing,
			"provider_index_mismatch":              providerIndexMismatch,
			"provider_index_differences":           providerIndexDifferences,
			"client_entries":                       clientEntries,
			"client_entries_count":                 len(clientEntries),
			"client_entries_truncated":             clientEntriesTruncated,
			"client_provider_missing_count":        missingProviderCount,
			"model_info":                           info,
			"model_info_by_provider":               infoByProvider,
		},
	}
}

// DebugCustomModelsState returns safe diagnostics about the current custom model overlay state.
func DebugCustomModelsState() map[string]any {
	modelsCatalogStore.mu.RLock()
	defer modelsCatalogStore.mu.RUnlock()

	providerSummaries := make(map[string]any, len(modelsCatalogStore.customSummaries))
	for provider, summary := range modelsCatalogStore.customSummaries {
		providerSummaries[provider] = map[string]any{
			"overridden": summary.Overridden,
			"added":      summary.Added,
		}
	}
	return map[string]any{
		"path":               customModelsPath(),
		"exists":             modelsCatalogStore.customFileExists,
		"overlay_enabled":    len(modelsCatalogStore.customModelIDs) > 0,
		"provider_summaries": providerSummaries,
	}
}

// IsModelFromCustomOverlay reports whether the current final catalog entry for modelID comes from custom_models.json.
func IsModelFromCustomOverlay(modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	modelsCatalogStore.mu.RLock()
	defer modelsCatalogStore.mu.RUnlock()
	_, ok := modelsCatalogStore.customModelIDs[modelID]
	return ok
}

func hashClientID(clientID string) string {
	trimmed := strings.TrimSpace(clientID)
	if trimmed == "" {
		return ""
	}
	key := debugClientHashSecret()
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(trimmed))
	sum := mac.Sum(nil)
	// Keep this reasonably long to reduce collision/ambiguity in logs.
	return hex.EncodeToString(sum)[:32]
}

func debugClientHashSecret() []byte {
	debugClientHashKeyOnce.Do(func() {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			// Extremely unlikely; fall back to a fixed key to avoid nil panics.
			buf = []byte("debug-client-hash-fallback-key")
		}
		debugClientHashKey = buf
	})
	return debugClientHashKey
}
