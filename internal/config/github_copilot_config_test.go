package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitHubCopilotHeaderPolicy_DefaultModeIsLegacy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `github-copilot: {}
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := cfg.GitHubCopilot.HeaderPolicy.Mode; got != GitHubCopilotHeaderPolicyModeLegacy {
		t.Fatalf("GitHubCopilot.HeaderPolicy.Mode = %q, want %q", got, GitHubCopilotHeaderPolicyModeLegacy)
	}
}

func TestGitHubCopilotHeaderPolicy_InvalidModeFallsBackToLegacy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `github-copilot:
  header-policy:
    mode: definitely-invalid
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := cfg.GitHubCopilot.HeaderPolicy.Mode; got != GitHubCopilotHeaderPolicyModeLegacy {
		t.Fatalf("GitHubCopilot.HeaderPolicy.Mode = %q, want %q", got, GitHubCopilotHeaderPolicyModeLegacy)
	}
}

func TestGitHubCopilotHeaderPolicy_ParsesHeaderDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `github-copilot:
  header-policy:
    mode: strict
    user-agent: GitHubCopilotChat/0.39.0
    editor-version: vscode/1.111.0
    editor-plugin-version: copilot-chat/0.39.0
    anthropic-beta: advanced-tool-use-2025-11-20
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := cfg.GitHubCopilot.HeaderPolicy.UserAgent; got != "GitHubCopilotChat/0.39.0" {
		t.Fatalf("GitHubCopilot.HeaderPolicy.UserAgent = %q, want %q", got, "GitHubCopilotChat/0.39.0")
	}
	if got := cfg.GitHubCopilot.HeaderPolicy.EditorVersion; got != "vscode/1.111.0" {
		t.Fatalf("GitHubCopilot.HeaderPolicy.EditorVersion = %q, want %q", got, "vscode/1.111.0")
	}
	if got := cfg.GitHubCopilot.HeaderPolicy.EditorPluginVersion; got != "copilot-chat/0.39.0" {
		t.Fatalf("GitHubCopilot.HeaderPolicy.EditorPluginVersion = %q, want %q", got, "copilot-chat/0.39.0")
	}
	if got := cfg.GitHubCopilot.HeaderPolicy.AnthropicBeta; got != "advanced-tool-use-2025-11-20" {
		t.Fatalf("GitHubCopilot.HeaderPolicy.AnthropicBeta = %q, want %q", got, "advanced-tool-use-2025-11-20")
	}
}
