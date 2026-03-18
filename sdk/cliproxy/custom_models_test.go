package cliproxy

import (
	"context"
	"path/filepath"
	"testing"
)

func TestConfigureModelCatalog_UsesConfigDirAndStartsUpdater(t *testing.T) {
	oldSet := setCustomModelsPath
	oldStart := startModelsUpdater
	t.Cleanup(func() {
		setCustomModelsPath = oldSet
		startModelsUpdater = oldStart
	})

	var gotPath string
	var started bool
	setCustomModelsPath = func(path string) error {
		gotPath = path
		return nil
	}
	startModelsUpdater = func(ctx context.Context) {
		started = true
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configuredPath, err := configureModelCatalog(context.Background(), configPath)
	if err != nil {
		t.Fatalf("configureModelCatalog() error = %v", err)
	}
	want := filepath.Join(filepath.Dir(configPath), "custom_models.json")
	if configuredPath != want {
		t.Fatalf("configureModelCatalog() path = %q, want %q", configuredPath, want)
	}
	if gotPath != want {
		t.Fatalf("setCustomModelsPath path = %q, want %q", gotPath, want)
	}
	if !started {
		t.Fatal("expected configureModelCatalog() to start models updater")
	}
}
