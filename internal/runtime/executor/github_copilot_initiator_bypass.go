package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const initiatorBypassStateVersion = 1

type initiatorBypassManager struct {
	mu        sync.Mutex
	window    time.Duration
	stateFile string
	now       func() time.Time
	buckets   map[string]initiatorBypassBucketState
}

type initiatorBypassBucketState struct {
	// Legacy second-level fields (kept for backward compatibility).
	NextEligibleAtUnix int64 `json:"next_eligible_at_unix,omitempty"`
	UpdatedAtUnix      int64 `json:"updated_at_unix,omitempty"`

	// High-precision fields for sub-second windows.
	NextEligibleAtUnixNano int64 `json:"next_eligible_at_unix_nano,omitempty"`
	UpdatedAtUnixNano      int64 `json:"updated_at_unix_nano,omitempty"`
}

type initiatorBypassStateDisk struct {
	Version int64                                 `json:"version"`
	Buckets map[string]initiatorBypassBucketState `json:"buckets"`
}

func newInitiatorBypassManager(window time.Duration, stateFile string, nowFn func() time.Time) *initiatorBypassManager {
	if window <= 0 {
		return nil
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	m := &initiatorBypassManager{
		window:    window,
		stateFile: strings.TrimSpace(stateFile),
		now:       nowFn,
		buckets:   map[string]initiatorBypassBucketState{},
	}
	m.loadState()
	return m
}

func (m *initiatorBypassManager) ShouldBypass(model, apiToken string, hasAgentRole bool) bool {
	if m == nil || hasAgentRole || m.window <= 0 {
		return false
	}
	key := initiatorBypassBucketKey(model, apiToken)
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	if st, ok := m.buckets[key]; ok && now.Before(st.nextEligibleAt()) {
		return false
	}
	nextEligibleAt := now.Add(m.window)

	m.buckets[key] = initiatorBypassBucketState{
		NextEligibleAtUnix:     nextEligibleAt.Unix(),
		UpdatedAtUnix:          now.Unix(),
		NextEligibleAtUnixNano: nextEligibleAt.UnixNano(),
		UpdatedAtUnixNano:      now.UnixNano(),
	}
	if err := m.persistLocked(); err != nil {
		log.Warnf("github-copilot executor: persist initiator bypass state failed: %v", err)
	}
	return true
}

func (s initiatorBypassBucketState) nextEligibleAt() time.Time {
	if s.NextEligibleAtUnixNano > 0 {
		return time.Unix(0, s.NextEligibleAtUnixNano)
	}
	if s.NextEligibleAtUnix > 0 {
		return time.Unix(s.NextEligibleAtUnix, 0)
	}
	return time.Unix(0, 0)
}

func (m *initiatorBypassManager) loadState() {
	if m == nil || m.stateFile == "" {
		return
	}
	raw, err := os.ReadFile(m.stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("github-copilot executor: load initiator bypass state failed: %v", err)
		}
		return
	}
	var disk initiatorBypassStateDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		log.Warnf("github-copilot executor: parse initiator bypass state failed: %v", err)
		return
	}
	if len(disk.Buckets) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets = disk.Buckets
}

func (m *initiatorBypassManager) persistLocked() error {
	if m == nil || m.stateFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.stateFile), 0o755); err != nil {
		return err
	}

	disk := initiatorBypassStateDisk{
		Version: initiatorBypassStateVersion,
		Buckets: m.buckets,
	}
	raw, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	tmp := m.stateFile + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.stateFile); err != nil {
		_ = os.Remove(m.stateFile)
		if errRetry := os.Rename(tmp, m.stateFile); errRetry != nil {
			_ = os.Remove(tmp)
			return errRetry
		}
	}
	return nil
}

func initiatorBypassBucketKey(model, apiToken string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(apiToken)))
	return strings.TrimSpace(model) + "|" + hex.EncodeToString(h[:])
}
