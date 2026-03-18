package main

import (
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

func configureCustomModelsOverlay(configFilePath string) (string, error) {
	trimmed := strings.TrimSpace(configFilePath)
	if trimmed == "" {
		if err := registry.SetCustomModelsPath(""); err != nil {
			return "", err
		}
		return "", nil
	}
	customPath := filepath.Join(filepath.Dir(trimmed), "custom_models.json")
	if err := registry.SetCustomModelsPath(customPath); err != nil {
		return "", err
	}
	return customPath, nil
}
