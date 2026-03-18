package cliproxy

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

var (
	setCustomModelsPath = registry.SetCustomModelsPath
	startModelsUpdater  = registry.StartModelsUpdater
)

func configureModelCatalog(ctx context.Context, configPath string) (string, error) {
	_ = ctx
	trimmed := strings.TrimSpace(configPath)
	customPath := ""
	if trimmed != "" {
		customPath = filepath.Join(filepath.Dir(trimmed), "custom_models.json")
	}
	if err := setCustomModelsPath(customPath); err != nil {
		return "", err
	}
	startModelsUpdater(context.Background())
	return customPath, nil
}
