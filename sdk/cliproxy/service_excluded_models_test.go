package cliproxy

import (
	"strings"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestRegisterModelsForAuth_UsesPreMergedExcludedModelsAttribute(t *testing.T) {
	service := &Service{
		cfg: &config.Config{
			OAuthExcludedModels: map[string][]string{
				"gemini-cli": {"gemini-2.5-pro"},
			},
		},
	}
	auth := &coreauth.Auth{
		ID:       "auth-gemini-cli",
		Provider: "gemini-cli",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind":       "oauth",
			"excluded_models": "gemini-2.5-flash",
		},
	}

	registry := GlobalModelRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		registry.UnregisterClient(auth.ID)
	})

	service.registerModelsForAuth(auth)

	models := registry.GetAvailableModelsByProvider("gemini-cli")
	if len(models) == 0 {
		t.Fatal("expected gemini-cli models to be registered")
	}

	for _, model := range models {
		if model == nil {
			continue
		}
		modelID := strings.TrimSpace(model.ID)
		if strings.EqualFold(modelID, "gemini-2.5-flash") {
			t.Fatalf("expected model %q to be excluded by auth attribute", modelID)
		}
	}

	seenGlobalExcluded := false
	for _, model := range models {
		if model == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(model.ID), "gemini-2.5-pro") {
			seenGlobalExcluded = true
			break
		}
	}
	if !seenGlobalExcluded {
		t.Fatal("expected global excluded model to be present when attribute override is set")
	}
}

func TestApplyExcludedModelsWithAlias_ExcludesAliasWhenOriginalIsExcluded(t *testing.T) {
	cfg := &config.Config{
		OAuthModelAlias: map[string][]config.OAuthModelAlias{
			"codex": {
				{Name: "gpt-5", Alias: "gpt-5-renamed", Fork: false},
			},
		},
	}

	models := []*ModelInfo{
		{ID: "gpt-5-renamed"},
		{ID: "gpt-4.1"},
	}

	got := applyExcludedModelsWithAlias(cfg, "codex", "oauth", models, []string{"gpt-5"})
	if len(got) != 1 {
		t.Fatalf("expected 1 model after exclusion, got %d", len(got))
	}
	if got[0] == nil || !strings.EqualFold(strings.TrimSpace(got[0].ID), "gpt-4.1") {
		t.Fatalf("unexpected remaining model: %+v", got[0])
	}
}

func TestApplyExcludedModelsWithAlias_ExcludesOriginalWhenAliasIsExcluded(t *testing.T) {
	cfg := &config.Config{
		OAuthModelAlias: map[string][]config.OAuthModelAlias{
			"codex": {
				{Name: "gpt-5", Alias: "gpt-5-renamed", Fork: false},
			},
		},
	}

	models := []*ModelInfo{
		{ID: "gpt-5"},
		{ID: "gpt-4.1"},
	}

	got := applyExcludedModelsWithAlias(cfg, "codex", "oauth", models, []string{"gpt-5-renamed"})
	if len(got) != 1 {
		t.Fatalf("expected 1 model after exclusion, got %d", len(got))
	}
	if got[0] == nil || !strings.EqualFold(strings.TrimSpace(got[0].ID), "gpt-4.1") {
		t.Fatalf("unexpected remaining model: %+v", got[0])
	}
}
