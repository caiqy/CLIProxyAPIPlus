package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitiatorBypass_AllowOncePerWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	m := newInitiatorBypassManager(time.Hour, filepath.Join(t.TempDir(), "state.json"), func() time.Time { return now })

	if ok := m.ShouldBypass("gpt-4o", "copilot-token-a", false); !ok {
		t.Fatal("first user-only request should bypass, got false")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-a", false); ok {
		t.Fatal("second user-only request in same window should not bypass, got true")
	}

	now = now.Add(time.Hour)
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-a", false); !ok {
		t.Fatal("request at window boundary should bypass again, got false")
	}
}

func TestInitiatorBypass_AgentRequestDoesNotConsume(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 11, 11, 0, 0, 0, time.UTC)
	m := newInitiatorBypassManager(time.Hour, filepath.Join(t.TempDir(), "state.json"), func() time.Time { return now })

	if ok := m.ShouldBypass("gpt-4o", "copilot-token-b", true); ok {
		t.Fatal("agent request should not bypass")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-b", false); !ok {
		t.Fatal("first user-only request should still bypass because agent request must not consume chance")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-b", false); ok {
		t.Fatal("second user-only request in same window should not bypass")
	}
}

func TestInitiatorBypass_PersistAndReload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	stateFile := filepath.Join(t.TempDir(), "state.json")

	m1 := newInitiatorBypassManager(time.Hour, stateFile, func() time.Time { return now })
	if ok := m1.ShouldBypass("claude-sonnet-4.5", "copilot-token-c", false); !ok {
		t.Fatal("first request should bypass")
	}

	m2 := newInitiatorBypassManager(time.Hour, stateFile, func() time.Time { return now })
	if ok := m2.ShouldBypass("claude-sonnet-4.5", "copilot-token-c", false); ok {
		t.Fatal("reloaded manager should honor persisted window and deny second bypass")
	}

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if strings.Contains(string(raw), "copilot-token-c") {
		t.Fatal("state file should not contain plaintext token")
	}
}

func TestInitiatorBypass_CorruptStateFile_FailOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 11, 13, 0, 0, 0, time.UTC)
	stateFile := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(stateFile, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write corrupt state file: %v", err)
	}

	m := newInitiatorBypassManager(time.Hour, stateFile, func() time.Time { return now })
	if m == nil {
		t.Fatal("manager should not be nil")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-d", false); !ok {
		t.Fatal("corrupt state file should fail-open and allow first bypass")
	}
}

func TestInitiatorBypass_PersistFailure_DoesNotBlockDecision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 11, 14, 0, 0, 0, time.UTC)
	stateDir := t.TempDir()

	// Use an existing directory path as stateFile so final rename fails.
	m := newInitiatorBypassManager(time.Hour, stateDir, func() time.Time { return now })
	if m == nil {
		t.Fatal("manager should not be nil")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-e", false); !ok {
		t.Fatal("persist failure should not block first bypass decision")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-e", false); ok {
		t.Fatal("second request in same window should still be denied by in-memory state")
	}
}

func TestInitiatorBypass_SubSecondWindow_Enforced(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 11, 15, 0, 0, 0, time.UTC)
	m := newInitiatorBypassManager(500*time.Millisecond, filepath.Join(t.TempDir(), "state.json"), func() time.Time { return now })

	if ok := m.ShouldBypass("gpt-4o", "copilot-token-f", false); !ok {
		t.Fatal("first request should bypass")
	}
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-f", false); ok {
		t.Fatal("second request inside 500ms window should not bypass")
	}

	now = now.Add(500 * time.Millisecond)
	if ok := m.ShouldBypass("gpt-4o", "copilot-token-f", false); !ok {
		t.Fatal("request at 500ms boundary should bypass again")
	}
}
