package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSetCustomModelsPath_ReloadsCurrentEmbeddedCatalog(t *testing.T) {
	resetModelUpdaterStateForTest(t)
	base := mustCatalogJSON(t, buildValidCatalog("base"))
	if err := loadModelsFromBytes(base, "test-base"); err != nil {
		t.Fatalf("loadModelsFromBytes() error = %v", err)
	}

	customPath := writeTempJSONFile(t, "custom_models.json", `{
		"claude": [{"id": "base-claude", "owned_by": "custom-claude"}]
	}`)

	if err := SetCustomModelsPath(customPath); err != nil {
		t.Fatalf("SetCustomModelsPath() error = %v", err)
	}

	if got := getModels().Claude[0].OwnedBy; got != "custom-claude" {
		t.Fatalf("getModels().Claude[0].OwnedBy = %q, want %q", got, "custom-claude")
	}
	if modelsCatalogStore.base == nil {
		t.Fatal("base catalog should remain available after custom reload")
	}
	if got := modelsCatalogStore.base.Claude[0].OwnedBy; got != "base-owner" {
		t.Fatalf("base.Claude[0].OwnedBy = %q, want %q", got, "base-owner")
	}
}

func TestTryRefreshModels_AppliesOverlayAfterRemoteSuccess(t *testing.T) {
	resetModelUpdaterStateForTest(t)
	if err := loadModelsFromBytes(mustCatalogJSON(t, buildValidCatalog("base")), "test-base"); err != nil {
		t.Fatalf("loadModelsFromBytes() error = %v", err)
	}
	customPath := writeTempJSONFile(t, "custom_models.json", `{
		"claude": [{"id": "remote-claude", "owned_by": "custom-after-refresh"}]
	}`)
	if err := SetCustomModelsPath(customPath); err != nil {
		t.Fatalf("SetCustomModelsPath() error = %v", err)
	}

	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(buildValidCatalog("remote"))
	}))
	defer remoteSrv.Close()
	oldURLs := modelsURLs
	modelsURLs = []string{remoteSrv.URL}
	t.Cleanup(func() { modelsURLs = oldURLs })

	tryRefreshModels(context.Background(), "test-refresh")

	if got := modelsCatalogStore.base.Claude[0].ID; got != "remote-claude" {
		t.Fatalf("base.Claude[0].ID = %q, want %q", got, "remote-claude")
	}
	if got := getModels().Claude[0].OwnedBy; got != "custom-after-refresh" {
		t.Fatalf("final.Claude[0].OwnedBy = %q, want %q", got, "custom-after-refresh")
	}
}

func TestTryRefreshModels_ReappliesCustomOnRemoteFailureUsingLastBase(t *testing.T) {
	resetModelUpdaterStateForTest(t)
	if err := loadModelsFromBytes(mustCatalogJSON(t, buildValidCatalog("base")), "test-base"); err != nil {
		t.Fatalf("loadModelsFromBytes() error = %v", err)
	}
	customPath := writeTempJSONFile(t, "custom_models.json", `{
		"claude": [{"id": "base-claude", "owned_by": "custom-v1"}]
	}`)
	if err := SetCustomModelsPath(customPath); err != nil {
		t.Fatalf("SetCustomModelsPath() error = %v", err)
	}
	if got := getModels().Claude[0].OwnedBy; got != "custom-v1" {
		t.Fatalf("initial final owned_by = %q, want %q", got, "custom-v1")
	}
	if err := os.WriteFile(customPath, []byte(`{
		"claude": [{"id": "base-claude", "owned_by": "custom-v2"}]
	}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	oldURLs := modelsURLs
	modelsURLs = []string{"http://127.0.0.1:1"}
	t.Cleanup(func() { modelsURLs = oldURLs })

	tryRefreshModels(context.Background(), "test-refresh-fail")

	if got := modelsCatalogStore.base.Claude[0].OwnedBy; got != "base-owner" {
		t.Fatalf("base.Claude[0].OwnedBy = %q, want %q", got, "base-owner")
	}
	if got := getModels().Claude[0].OwnedBy; got != "custom-v2" {
		t.Fatalf("final.Claude[0].OwnedBy = %q, want %q", got, "custom-v2")
	}
}

func TestDetectChangedProviders_UsesFinalCatalogAndKeepsCodexGrouping(t *testing.T) {
	oldBase := buildValidCatalog("shared")
	newBase := buildValidCatalog("shared")
	oldBase.CodexFree[0].OwnedBy = "old-codex"
	newBase.CodexFree[0].OwnedBy = "new-codex"
	oldFinal, _ := overlayModels(oldBase, nil)
	newFinal, _ := overlayModels(newBase, nil)

	changed := detectChangedProviders(oldFinal, newFinal)
	if len(changed) != 1 || changed[0] != "codex" {
		t.Fatalf("detectChangedProviders() = %v, want [codex]", changed)
	}

	maskedOldBase := buildValidCatalog("masked")
	maskedNewBase := buildValidCatalog("masked")
	maskedOldBase.Claude[0].OwnedBy = "old-base"
	maskedNewBase.Claude[0].OwnedBy = "new-base"
	maskedOld, _ := overlayModels(maskedOldBase, &staticModelsJSON{
		Claude: []*ModelInfo{{ID: "masked-claude", OwnedBy: "same-final"}},
	})
	maskedNew, _ := overlayModels(maskedNewBase, &staticModelsJSON{
		Claude: []*ModelInfo{{ID: "masked-claude", OwnedBy: "same-final"}},
	})
	changed = detectChangedProviders(maskedOld, maskedNew)
	if len(changed) != 0 {
		t.Fatalf("detectChangedProviders() with identical final catalogs = %v, want empty", changed)
	}
}

func TestSetCustomModelsPath_RepeatedCallsReplacePreviousState(t *testing.T) {
	resetModelUpdaterStateForTest(t)
	if err := loadModelsFromBytes(mustCatalogJSON(t, buildValidCatalog("base")), "test-base"); err != nil {
		t.Fatalf("loadModelsFromBytes() error = %v", err)
	}
	first := writeTempJSONFile(t, "custom_models_a.json", `{"claude": [{"id": "base-claude", "owned_by": "first"}]}`)
	second := writeTempJSONFile(t, "custom_models_b.json", `{"claude": [{"id": "base-claude", "owned_by": "second"}]}`)

	if err := SetCustomModelsPath(first); err != nil {
		t.Fatalf("SetCustomModelsPath(first) error = %v", err)
	}
	if err := SetCustomModelsPath(second); err != nil {
		t.Fatalf("SetCustomModelsPath(second) error = %v", err)
	}

	if got := getModels().Claude[0].OwnedBy; got != "second" {
		t.Fatalf("final.Claude[0].OwnedBy = %q, want %q", got, "second")
	}
	if got := customModelsPath(); got != second {
		t.Fatalf("customModelsPath() = %q, want %q", got, second)
	}
}

func TestConcurrentCustomPathUpdateAndRefresh_KeepStateConsistent(t *testing.T) {
	resetModelUpdaterStateForTest(t)
	if err := loadModelsFromBytes(mustCatalogJSON(t, buildValidCatalog("base")), "test-base"); err != nil {
		t.Fatalf("loadModelsFromBytes() error = %v", err)
	}
	first := writeTempJSONFile(t, "custom_models_a.json", `{"claude": [{"id": "base-claude", "owned_by": "first"}]}`)
	second := writeTempJSONFile(t, "custom_models_b.json", `{"claude": [{"id": "base-claude", "owned_by": "second"}]}`)

	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(buildValidCatalog("base"))
	}))
	defer remoteSrv.Close()
	oldURLs := modelsURLs
	modelsURLs = []string{remoteSrv.URL}
	t.Cleanup(func() { modelsURLs = oldURLs })

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = SetCustomModelsPath(first)
		}()
		go func() {
			defer wg.Done()
			_ = SetCustomModelsPath(second)
		}()
		go func() {
			defer wg.Done()
			tryRefreshModels(context.Background(), "concurrent-refresh")
		}()
	}
	wg.Wait()

	path := customModelsPath()
	owner := getModels().Claude[0].OwnedBy
	switch path {
	case first:
		if owner != "first" {
			t.Fatalf("path=%q but final owner=%q, want first", path, owner)
		}
	case second:
		if owner != "second" {
			t.Fatalf("path=%q but final owner=%q, want second", path, owner)
		}
	default:
		t.Fatalf("unexpected custom path after concurrent updates: %q", path)
	}
}

func resetModelUpdaterStateForTest(t *testing.T) {
	t.Helper()
	modelsCatalogStore.mu.Lock()
	modelsCatalogStore.base = nil
	modelsCatalogStore.final = nil
	modelsCatalogStore.mu.Unlock()
	if err := SetCustomModelsPath(""); err != nil {
		t.Fatalf("SetCustomModelsPath(\"\") error = %v", err)
	}
}

func writeTempJSONFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}

func mustCatalogJSON(t *testing.T, catalog *staticModelsJSON) []byte {
	t.Helper()
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}

func buildValidCatalog(prefix string) *staticModelsJSON {
	return &staticModelsJSON{
		Claude:      []*ModelInfo{{ID: prefix + "-claude", OwnedBy: "base-owner"}},
		Gemini:      []*ModelInfo{{ID: prefix + "-gemini", OwnedBy: "base-owner"}},
		Vertex:      []*ModelInfo{{ID: prefix + "-vertex", OwnedBy: "base-owner"}},
		GeminiCLI:   []*ModelInfo{{ID: prefix + "-gemini-cli", OwnedBy: "base-owner"}},
		AIStudio:    []*ModelInfo{{ID: prefix + "-aistudio", OwnedBy: "base-owner"}},
		CodexFree:   []*ModelInfo{{ID: prefix + "-codex-free", OwnedBy: "base-owner"}},
		CodexTeam:   []*ModelInfo{{ID: prefix + "-codex-team", OwnedBy: "base-owner"}},
		CodexPlus:   []*ModelInfo{{ID: prefix + "-codex-plus", OwnedBy: "base-owner"}},
		CodexPro:    []*ModelInfo{{ID: prefix + "-codex-pro", OwnedBy: "base-owner"}},
		Qwen:        []*ModelInfo{{ID: prefix + "-qwen", OwnedBy: "base-owner"}},
		IFlow:       []*ModelInfo{{ID: prefix + "-iflow", OwnedBy: "base-owner"}},
		Kimi:        []*ModelInfo{{ID: prefix + "-kimi", OwnedBy: "base-owner"}},
		Antigravity: []*ModelInfo{{ID: prefix + "-antigravity", OwnedBy: "base-owner"}},
	}
}
