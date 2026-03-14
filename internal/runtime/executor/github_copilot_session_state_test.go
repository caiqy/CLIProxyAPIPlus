package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCopilotSessionState_UserGeneratesAndPersistsPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}

	pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "user")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	if pair.SessionID == "" {
		t.Fatal("session_id should not be empty")
	}
	if pair.InteractionID == "" {
		t.Fatal("interaction_id should not be empty")
	}
	if pair.SessionID == pair.InteractionID {
		t.Fatal("session_id and interaction_id should use same strategy with different values")
	}
	assertUUIDv7String(t, pair.SessionID)
	assertUUIDv7String(t, pair.InteractionID)

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var disk githubCopilotSessionStateDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	if disk.Version != githubCopilotSessionStateVersion {
		t.Fatalf("unexpected version: got %d want %d", disk.Version, githubCopilotSessionStateVersion)
	}

	key := githubCopilotSessionStateKey("gpt-4o", "copilot-token-a")
	persisted, ok := disk.Pairs[key]
	if !ok {
		t.Fatalf("expected persisted pair for key %q", key)
	}
	if persisted != pair {
		t.Fatalf("persisted pair mismatch: got %+v want %+v", persisted, pair)
	}
}

func TestCopilotSessionState_AgentReusesExistingPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	key := githubCopilotSessionStateKey("gpt-4o", "copilot-token-a")
	existing := githubCopilotSessionPair{
		SessionID:     "01949ca9-b7f2-7e30-8de7-4d7f640fbc6e",
		InteractionID: "01949ca9-b7f2-74ce-aaca-2c3f3b4ab9d0",
	}

	raw, err := json.Marshal(githubCopilotSessionStateDisk{
		Version: githubCopilotSessionStateVersion,
		Pairs: map[string]githubCopilotSessionPair{
			key: existing,
		},
	})
	if err != nil {
		t.Fatalf("marshal state file: %v", err)
	}
	if err := os.WriteFile(stateFile, raw, 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	mgr := newGitHubCopilotSessionStateManager(stateFile)
	pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	if pair != existing {
		t.Fatalf("agent should reuse existing pair: got %+v want %+v", pair, existing)
	}
}

func TestCopilotSessionState_AgentWithoutStateGeneratesAndPersists(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}

	pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	if pair.SessionID == "" {
		t.Fatal("session_id should not be empty")
	}
	if pair.InteractionID == "" {
		t.Fatal("interaction_id should not be empty")
	}
	if pair.SessionID == pair.InteractionID {
		t.Fatal("session_id and interaction_id should use same strategy with different values")
	}
	assertUUIDv7String(t, pair.SessionID)
	assertUUIDv7String(t, pair.InteractionID)

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var disk githubCopilotSessionStateDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	if disk.Version != githubCopilotSessionStateVersion {
		t.Fatalf("unexpected version: got %d want %d", disk.Version, githubCopilotSessionStateVersion)
	}

	key := githubCopilotSessionStateKey("gpt-4o", "copilot-token-a")
	persisted, ok := disk.Pairs[key]
	if !ok {
		t.Fatalf("expected persisted pair for key %q", key)
	}
	if persisted != pair {
		t.Fatalf("persisted pair mismatch: got %+v want %+v", persisted, pair)
	}
}

func assertUUIDv7String(t *testing.T, id string) {
	t.Helper()

	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	if got := parsed.Version(); got != 7 {
		t.Fatalf("uuid should be v7, got v%d (%s)", got, id)
	}
}

func TestCopilotSessionState_LockTimeoutFallsBackToFreshPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.acquireBucketLock = func(string, time.Duration, time.Duration) (func() error, error) {
		return nil, errGitHubCopilotSessionStateLockTimeout
	}

	pairA, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair A: %v", err)
	}
	assertPairBothOrNeither(t, pairA)
	assertPairPresent(t, pairA)
	if pairA.SessionID == pairA.InteractionID {
		t.Fatal("session_id and interaction_id should use same strategy with different values")
	}

	pairB, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair B: %v", err)
	}
	assertPairBothOrNeither(t, pairB)
	assertPairPresent(t, pairB)
	if pairA == pairB {
		t.Fatal("lock-timeout fallback should regenerate fresh pair")
	}
}

func TestCopilotSessionState_PersistFailureStillReturnsGeneratedPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.rename = func(string, string) error {
		return errors.New("forced rename failure")
	}

	pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	assertPairBothOrNeither(t, pair)
	assertPairPresent(t, pair)
	if pair.SessionID == pair.InteractionID {
		t.Fatal("session_id and interaction_id should use same strategy with different values")
	}
}

func TestCopilotSessionState_PersistFailure_BothOrNeither(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.rename = func(string, string) error {
		return errors.New("forced rename failure")
	}

	pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "user")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	assertPairBothOrNeither(t, pair)
	assertPairPresent(t, pair)
}

func TestCopilotSessionState_ReadFailureFallsBackToFreshPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.readFile = func(string) ([]byte, error) {
		return nil, errors.New("forced read failure")
	}

	pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	assertPairBothOrNeither(t, pair)
	assertPairPresent(t, pair)
}

func TestCopilotSessionState_PersistFailure_NextRequestRegeneratesPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.rename = func(string, string) error {
		return errors.New("forced rename failure")
	}

	pairA, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair A: %v", err)
	}
	pairB, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair B: %v", err)
	}
	assertPairBothOrNeither(t, pairA)
	assertPairBothOrNeither(t, pairB)
	assertPairPresent(t, pairA)
	assertPairPresent(t, pairB)
	if pairA == pairB {
		t.Fatal("pair should be regenerated when previous persist failed")
	}
}

func TestCopilotSessionState_PrimaryAndShadowUseDifferentLockPaths(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	primaryStateFile := filepath.Join(baseDir, "primary-session-state.json")
	shadowStateFile := filepath.Join(baseDir, "shadow-session-state.json")
	primaryLockPath := filepath.Join(baseDir, "primary-locks")
	shadowLockPath := filepath.Join(baseDir, "shadow-locks")

	primary := newGitHubCopilotSessionStateManagerWithLockPath(primaryStateFile, primaryLockPath)
	shadow := newGitHubCopilotSessionStateManagerWithLockPath(shadowStateFile, shadowLockPath)
	primary.lockTimeout = 20 * time.Millisecond
	primary.lockRetryInterval = 2 * time.Millisecond

	key := githubCopilotSessionStateKey("gpt-4o", "copilot-token-a")
	primaryLockFile := primary.bucketLockPath(key)
	if err := os.MkdirAll(filepath.Dir(primaryLockFile), 0o755); err != nil {
		t.Fatalf("mkdir primary lock dir: %v", err)
	}
	if err := os.WriteFile(primaryLockFile, []byte("locked"), 0o644); err != nil {
		t.Fatalf("create primary lock file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(primaryLockFile)
	})

	primaryPair, err := primary.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve primary pair: %v", err)
	}
	shadowPair, err := shadow.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve shadow pair: %v", err)
	}
	assertPairBothOrNeither(t, primaryPair)
	assertPairBothOrNeither(t, shadowPair)
	assertPairPresent(t, primaryPair)
	assertPairPresent(t, shadowPair)

	if primary.bucketLockPath(key) == shadow.bucketLockPath(key) {
		t.Fatal("primary and shadow should use different lock paths")
	}

	if _, err := os.Stat(primaryStateFile); err == nil {
		t.Fatal("primary lock timeout fallback should not persist state")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat primary state file: %v", err)
	}

	shadowDisk, err := readStateDisk(shadowStateFile)
	if err != nil {
		t.Fatalf("read shadow state file: %v", err)
	}
	persisted, ok := shadowDisk.Pairs[key]
	if !ok {
		t.Fatalf("expected shadow persisted pair for key %q", key)
	}
	if persisted != shadowPair {
		t.Fatalf("shadow persisted pair mismatch: got %+v want %+v", persisted, shadowPair)
	}
}

func TestCopilotSessionState_ConcurrentSameBucket_NoPartialPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.lockTimeout = 2 * time.Second
	mgr.lockRetryInterval = 2 * time.Millisecond

	const workers = 32
	pairs := make([]githubCopilotSessionPair, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
			if err != nil {
				errCh <- err
				return
			}
			pairs[idx] = pair
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("resolve pair: %v", err)
	}

	for _, pair := range pairs {
		assertPairBothOrNeither(t, pair)
		assertPairPresent(t, pair)
	}
}

func TestCopilotSessionState_ConcurrentDifferentBuckets_NoLostUpdates(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.lockTimeout = 2 * time.Second
	mgr.lockRetryInterval = 2 * time.Millisecond

	realReadFile := mgr.readFile
	releaseReads := make(chan struct{})
	var releaseOnce sync.Once
	mgr.readFile = func(path string) ([]byte, error) {
		releaseOnce.Do(func() {
			go func() {
				time.Sleep(30 * time.Millisecond)
				close(releaseReads)
			}()
		})
		<-releaseReads
		return realReadFile(path)
	}

	const workers = 24
	pairsByKey := make(map[string]githubCopilotSessionPair, workers)
	errCh := make(chan error, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup

	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		bucket := fmt.Sprintf("copilot-token-%02d", i)
		wg.Add(1)
		go func(bucketIdentity string) {
			defer wg.Done()
			<-start
			pair, err := mgr.ResolvePair("gpt-4o", bucketIdentity, "agent")
			if err != nil {
				errCh <- err
				return
			}
			assertPairBothOrNeither(t, pair)
			assertPairPresent(t, pair)

			key := githubCopilotSessionStateKey("gpt-4o", bucketIdentity)
			mu.Lock()
			pairsByKey[key] = pair
			mu.Unlock()
		}(bucket)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("resolve pair: %v", err)
	}

	disk, err := readStateDisk(stateFile)
	if err != nil {
		t.Fatalf("read state disk: %v", err)
	}
	if got, want := len(disk.Pairs), workers; got != want {
		t.Fatalf("unexpected persisted pair count: got %d want %d", got, want)
	}
	for key, expected := range pairsByKey {
		actual, ok := disk.Pairs[key]
		if !ok {
			t.Fatalf("missing persisted pair for key %q", key)
		}
		if actual != expected {
			t.Fatalf("persisted pair mismatch for key %q: got %+v want %+v", key, actual, expected)
		}
	}
}

func TestCopilotSessionState_StaleLockCanBeRecovered(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.lockTimeout = 150 * time.Millisecond
	mgr.lockRetryInterval = 2 * time.Millisecond
	mgr.lockStaleAge = 50 * time.Millisecond

	bucket := "copilot-token-a"
	key := githubCopilotSessionStateKey("gpt-4o", bucket)
	lockFile := mgr.bucketLockPath(key)

	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("create lock file: %v", err)
	}
	staleAt := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(lockFile, staleAt, staleAt); err != nil {
		t.Fatalf("set stale mtime: %v", err)
	}

	pair, err := mgr.ResolvePair("gpt-4o", bucket, "agent")
	if err != nil {
		t.Fatalf("resolve pair: %v", err)
	}
	assertPairBothOrNeither(t, pair)
	assertPairPresent(t, pair)

	if _, err := os.Stat(lockFile); err == nil {
		t.Fatal("stale lock file should be recovered and removed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat lock file: %v", err)
	}

	disk, err := readStateDisk(stateFile)
	if err != nil {
		t.Fatalf("read state disk: %v", err)
	}
	persisted, ok := disk.Pairs[key]
	if !ok {
		t.Fatalf("expected persisted pair for key %q", key)
	}
	if persisted != pair {
		t.Fatalf("persisted pair mismatch: got %+v want %+v", persisted, pair)
	}
}

func TestCopilotSessionState_PairAlwaysBothPresentOrBothAbsent(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	key := githubCopilotSessionStateKey("gpt-4o", "copilot-token-a")
	partial := githubCopilotSessionPair{SessionID: "partial-session", InteractionID: ""}

	raw, err := json.Marshal(githubCopilotSessionStateDisk{
		Version: githubCopilotSessionStateVersion,
		Pairs: map[string]githubCopilotSessionPair{
			key: partial,
		},
	})
	if err != nil {
		t.Fatalf("marshal state file: %v", err)
	}
	if err := os.WriteFile(stateFile, raw, 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	mgr := newGitHubCopilotSessionStateManager(stateFile)

	nonGenerating, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "assistant")
	if err != nil {
		t.Fatalf("resolve pair for non-generating initiator: %v", err)
	}
	assertPairBothOrNeither(t, nonGenerating)
	if nonGenerating != (githubCopilotSessionPair{}) {
		t.Fatalf("invalid partial state should be treated as absent, got %+v", nonGenerating)
	}

	agentPair, err := mgr.ResolvePair("gpt-4o", "copilot-token-a", "agent")
	if err != nil {
		t.Fatalf("resolve pair for agent: %v", err)
	}
	assertPairBothOrNeither(t, agentPair)
	assertPairPresent(t, agentPair)
}

func assertPairPresent(t *testing.T, pair githubCopilotSessionPair) {
	t.Helper()

	if pair.SessionID == "" {
		t.Fatal("session_id should not be empty")
	}
	if pair.InteractionID == "" {
		t.Fatal("interaction_id should not be empty")
	}
	assertUUIDv7String(t, pair.SessionID)
	assertUUIDv7String(t, pair.InteractionID)
}

func assertPairBothOrNeither(t *testing.T, pair githubCopilotSessionPair) {
	t.Helper()

	hasSession := pair.SessionID != ""
	hasInteraction := pair.InteractionID != ""
	if hasSession != hasInteraction {
		t.Fatalf("pair must include both fields or neither: %+v", pair)
	}
}

func readStateDisk(stateFile string) (githubCopilotSessionStateDisk, error) {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return githubCopilotSessionStateDisk{}, err
	}

	var disk githubCopilotSessionStateDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return githubCopilotSessionStateDisk{}, err
	}
	return disk, nil
}
