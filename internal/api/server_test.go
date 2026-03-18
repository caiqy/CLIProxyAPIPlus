package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internallogging "github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{"test-key"},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
	}

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := sdkaccess.NewManager()

	configPath := filepath.Join(tmpDir, "config.yaml")
	return NewServer(cfg, authManager, accessManager, configPath)
}

func TestAmpProviderModelRoutes(t *testing.T) {
	testCases := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains string
	}{
		{
			name:         "openai root models",
			path:         "/api/provider/openai/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "groq root models",
			path:         "/api/provider/groq/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "openai models",
			path:         "/api/provider/openai/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "anthropic models",
			path:         "/api/provider/anthropic/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"data"`,
		},
		{
			name:         "google models v1",
			path:         "/api/provider/google/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
		{
			name:         "google models v1beta",
			path:         "/api/provider/google/v1beta/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer test-key")

			rr := httptest.NewRecorder()
			server.engine.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("unexpected status code for %s: got %d want %d; body=%s", tc.path, rr.Code, tc.wantStatus, rr.Body.String())
			}
			if body := rr.Body.String(); !strings.Contains(body, tc.wantContains) {
				t.Fatalf("response body for %s missing %q: %s", tc.path, tc.wantContains, body)
			}
		})
	}
}

func TestDefaultRequestLoggerFactory_UsesResolvedLogDirectory(t *testing.T) {
	t.Setenv("WRITABLE_PATH", "")
	t.Setenv("writable_path", "")

	originalWD, errGetwd := os.Getwd()
	if errGetwd != nil {
		t.Fatalf("failed to get current working directory: %v", errGetwd)
	}

	tmpDir := t.TempDir()
	if errChdir := os.Chdir(tmpDir); errChdir != nil {
		t.Fatalf("failed to switch working directory: %v", errChdir)
	}
	defer func() {
		if errChdirBack := os.Chdir(originalWD); errChdirBack != nil {
			t.Fatalf("failed to restore working directory: %v", errChdirBack)
		}
	}()

	// Force ResolveLogDirectory to fallback to auth-dir/logs by making ./logs not a writable directory.
	if errWriteFile := os.WriteFile(filepath.Join(tmpDir, "logs"), []byte("not-a-directory"), 0o644); errWriteFile != nil {
		t.Fatalf("failed to create blocking logs file: %v", errWriteFile)
	}

	configDir := filepath.Join(tmpDir, "config")
	if errMkdirConfig := os.MkdirAll(configDir, 0o755); errMkdirConfig != nil {
		t.Fatalf("failed to create config dir: %v", errMkdirConfig)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	authDir := filepath.Join(tmpDir, "auth")
	if errMkdirAuth := os.MkdirAll(authDir, 0o700); errMkdirAuth != nil {
		t.Fatalf("failed to create auth dir: %v", errMkdirAuth)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: proxyconfig.SDKConfig{
			RequestLog: false,
		},
		AuthDir:           authDir,
		ErrorLogsMaxFiles: 10,
	}

	logger := defaultRequestLoggerFactory(cfg, configPath)
	fileLogger, ok := logger.(*internallogging.FileRequestLogger)
	if !ok {
		t.Fatalf("expected *FileRequestLogger, got %T", logger)
	}

	errLog := fileLogger.LogRequestWithOptions(
		"/v1/chat/completions",
		http.MethodPost,
		map[string][]string{"Content-Type": []string{"application/json"}},
		[]byte(`{"input":"hello"}`),
		http.StatusBadGateway,
		map[string][]string{"Content-Type": []string{"application/json"}},
		[]byte(`{"error":"upstream failure"}`),
		nil,
		nil,
		nil,
		true,
		"issue-1711",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("failed to write forced error request log: %v", errLog)
	}

	authLogsDir := filepath.Join(authDir, "logs")
	authEntries, errReadAuthDir := os.ReadDir(authLogsDir)
	if errReadAuthDir != nil {
		t.Fatalf("failed to read auth logs dir %s: %v", authLogsDir, errReadAuthDir)
	}
	foundErrorLogInAuthDir := false
	for _, entry := range authEntries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			foundErrorLogInAuthDir = true
			break
		}
	}
	if !foundErrorLogInAuthDir {
		t.Fatalf("expected forced error log in auth fallback dir %s, got entries: %+v", authLogsDir, authEntries)
	}

	configLogsDir := filepath.Join(configDir, "logs")
	configEntries, errReadConfigDir := os.ReadDir(configLogsDir)
	if errReadConfigDir != nil && !os.IsNotExist(errReadConfigDir) {
		t.Fatalf("failed to inspect config logs dir %s: %v", configLogsDir, errReadConfigDir)
	}
	for _, entry := range configEntries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			t.Fatalf("unexpected forced error log in config dir %s", configLogsDir)
		}
	}
}

func TestDebugModelRouteMemoryEndpoint_NoAuthAndContainsModelDiagnostics(t *testing.T) {
	server := newTestServer(t)
	customPath := filepath.Join(filepath.Dir(server.configFilePath), "custom_models.json")
	if err := os.WriteFile(customPath, []byte(`{
		"claude": [{"id": "claude-sonnet-4-6", "owned_by": "custom-overlay-owner"}]
	}`), 0o600); err != nil {
		t.Fatalf("failed to write custom_models.json: %v", err)
	}
	if err := registry.SetCustomModelsPath(customPath); err != nil {
		t.Fatalf("SetCustomModelsPath() error = %v", err)
	}
	t.Cleanup(func() {
		_ = registry.SetCustomModelsPath("")
	})

	server.cfg.OAuthModelAlias = map[string][]proxyconfig.OAuthModelAlias{
		"github-copilot": {
			{Name: "claude-sonnet-4.6", Alias: "claude-sonnet-4-6", Fork: true},
			{Name: "claude-opus-4.6", Alias: "claude-opus-4-6", Fork: true},
		},
	}

	modelRegistry := registry.GetGlobalRegistry()
	clientID := "test-debug-model-route-memory-copilot"
	modelRegistry.RegisterClient(clientID, "github-copilot", []*registry.ModelInfo{
		{ID: "claude-sonnet-4-6", OwnedBy: "github-copilot"},
		{ID: "claude-opus-4-6", OwnedBy: "github-copilot"},
	})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	req := httptest.NewRequest(http.MethodGet, "/debug/model-route-memory", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var payload struct {
		Runtime          map[string]any `json:"runtime"`
		Process          map[string]any `json:"process"`
		Registry         map[string]any `json:"registry"`
		Limits           map[string]any `json:"limits"`
		ModelDiagnostics []struct {
			ModelID           string         `json:"model_id"`
			Providers         []string       `json:"providers"`
			Reg               map[string]any `json:"registry"`
			FromCustomOverlay bool           `json:"from_custom_overlay"`
		} `json:"model_diagnostics"`
		Config struct {
			GitHubCopilotAliases []map[string]any `json:"github_copilot_aliases"`
			CustomModels         map[string]any   `json:"custom_models"`
		} `json:"config"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response json: %v, body=%s", err, rr.Body.String())
	}

	if payload.Runtime == nil {
		t.Fatal("runtime field should not be nil")
	}
	if payload.Process == nil {
		t.Fatal("process field should not be nil")
	}
	if payload.Registry == nil {
		t.Fatal("registry field should not be nil")
	}
	if payload.Limits == nil {
		t.Fatal("limits field should not be nil")
	}
	if got, ok := payload.Limits["max_models"].(float64); !ok || got <= 0 {
		t.Fatalf("expected limits.max_models to be positive number, got %v", payload.Limits["max_models"])
	}

	if len(payload.ModelDiagnostics) == 0 {
		t.Fatal("model_diagnostics should not be empty")
	}

	foundSonnet := false
	for _, item := range payload.ModelDiagnostics {
		if item.ModelID == "claude-sonnet-4-6" {
			foundSonnet = true
			if len(item.Providers) == 0 || item.Providers[0] != "github-copilot" {
				t.Fatalf("unexpected providers for claude-sonnet-4-6: %v", item.Providers)
			}
			if item.Reg == nil {
				t.Fatal("expected registry field in model diagnostics")
			}
			if _, ok := item.Reg["model_id"]; !ok {
				t.Fatalf("expected registry snapshot to include model_id, got %v", item.Reg)
			}
			if !item.FromCustomOverlay {
				t.Fatal("expected claude-sonnet-4-6 to be marked as from custom overlay")
			}
		}
	}
	if !foundSonnet {
		t.Fatal("expected model_diagnostics to include claude-sonnet-4-6")
	}

	if len(payload.Config.GitHubCopilotAliases) == 0 {
		t.Fatal("expected github_copilot_aliases to be present")
	}
	if payload.Config.CustomModels == nil {
		t.Fatal("expected custom_models diagnostics to be present")
	}
	if got := payload.Config.CustomModels["path"]; got != customPath {
		t.Fatalf("custom_models.path = %v, want %v", got, customPath)
	}
	if got := payload.Config.CustomModels["exists"]; got != true {
		t.Fatalf("custom_models.exists = %v, want true", got)
	}
	if got := payload.Config.CustomModels["overlay_enabled"]; got != true {
		t.Fatalf("custom_models.overlay_enabled = %v, want true", got)
	}
	summaries, ok := payload.Config.CustomModels["provider_summaries"].(map[string]any)
	if !ok || summaries["claude"] == nil {
		t.Fatalf("custom_models.provider_summaries = %v, want claude entry", payload.Config.CustomModels["provider_summaries"])
	}
}
