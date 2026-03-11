package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_GitHubCopilotBypassConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `github-copilot:
  force-agent-initiator: true
  force-agent-initiator-bypass:
    enabled: true
    window: 1h
    state-file: /CLIProxyAPIBusiness/data/copilot_initiator_bypass_state.json
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.GitHubCopilot.ForceAgentInitiator {
		t.Fatal("ForceAgentInitiator = false, want true")
	}
	if !cfg.GitHubCopilot.ForceAgentInitiatorBypass.Enabled {
		t.Fatal("ForceAgentInitiatorBypass.Enabled = false, want true")
	}
	if got := cfg.GitHubCopilot.ForceAgentInitiatorBypass.Window; got != "1h" {
		t.Fatalf("ForceAgentInitiatorBypass.Window = %q, want %q", got, "1h")
	}
	if got := cfg.GitHubCopilot.ForceAgentInitiatorBypass.StateFile; got != "/CLIProxyAPIBusiness/data/copilot_initiator_bypass_state.json" {
		t.Fatalf("ForceAgentInitiatorBypass.StateFile = %q, want non-empty expected path", got)
	}
}
