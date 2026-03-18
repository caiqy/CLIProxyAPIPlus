package main

import (
	"path/filepath"
	"testing"
)

func TestConfigureCustomModelsOverlay_UsesConfigDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	configuredPath, err := configureCustomModelsOverlay(configPath)
	if err != nil {
		t.Fatalf("configureCustomModelsOverlay() error = %v", err)
	}
	want := filepath.Join(filepath.Dir(configPath), "custom_models.json")
	if configuredPath != want {
		t.Fatalf("configureCustomModelsOverlay() path = %q, want %q", configuredPath, want)
	}
}
