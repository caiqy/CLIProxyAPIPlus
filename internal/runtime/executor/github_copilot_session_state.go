package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

const githubCopilotSessionStateVersion = 1

const (
	githubCopilotSessionStateDefaultLockTimeout       = 500 * time.Millisecond
	githubCopilotSessionStateDefaultLockRetryInterval = 10 * time.Millisecond
	githubCopilotSessionStateDefaultStaleLockAge      = 30 * time.Second
)

var errGitHubCopilotSessionStateLockTimeout = errors.New("github copilot session state lock timeout")

type githubCopilotSessionPair struct {
	SessionID     string `json:"session_id,omitempty"`
	InteractionID string `json:"interaction_id,omitempty"`
}

type githubCopilotSessionStateDisk struct {
	Version int64                               `json:"version"`
	Pairs   map[string]githubCopilotSessionPair `json:"pairs"`
}

type githubCopilotSessionStateManager struct {
	stateFile string
	lockRoot  string

	lockTimeout       time.Duration
	lockRetryInterval time.Duration
	lockStaleAge      time.Duration

	readFile          func(string) ([]byte, error)
	mkdirAll          func(string, os.FileMode) error
	createTemp        func(string, string) (*os.File, error)
	rename            func(string, string) error
	remove            func(string) error
	acquireBucketLock func(string, time.Duration, time.Duration) (func() error, error)
}

func newGitHubCopilotSessionStateManager(stateFile string) *githubCopilotSessionStateManager {
	trimmedStateFile := strings.TrimSpace(stateFile)
	defaultLockRoot := filepath.Join(filepath.Dir(trimmedStateFile), filepath.Base(trimmedStateFile)+".locks")
	return newGitHubCopilotSessionStateManagerWithLockPath(trimmedStateFile, defaultLockRoot)
}

func newGitHubCopilotSessionStateManagerWithLockPath(stateFile, lockRoot string) *githubCopilotSessionStateManager {
	mgr := &githubCopilotSessionStateManager{
		stateFile:         strings.TrimSpace(stateFile),
		lockRoot:          strings.TrimSpace(lockRoot),
		lockTimeout:       githubCopilotSessionStateDefaultLockTimeout,
		lockRetryInterval: githubCopilotSessionStateDefaultLockRetryInterval,
		lockStaleAge:      githubCopilotSessionStateDefaultStaleLockAge,
		readFile:          os.ReadFile,
		mkdirAll:          os.MkdirAll,
		createTemp:        os.CreateTemp,
		rename:            os.Rename,
		remove:            os.Remove,
		acquireBucketLock: nil,
	}
	if mgr.lockRoot == "" {
		mgr.lockRoot = filepath.Join(filepath.Dir(mgr.stateFile), "copilot-session-state.locks")
	}
	mgr.acquireBucketLock = mgr.acquireBucketLockFile
	return mgr
}

func (m *githubCopilotSessionStateManager) ResolvePair(model, bucketIdentity, initiator string) (githubCopilotSessionPair, error) {
	if m == nil {
		return githubCopilotSessionPair{}, nil
	}

	key := githubCopilotSessionStateKey(model, bucketIdentity)
	allowGenerate := githubCopilotSessionInitiatorCanGenerate(initiator)

	bucketUnlock, err := m.acquireBucketLock(m.bucketLockPath(key), m.lockTimeout, m.lockRetryInterval)
	if err != nil {
		if errors.Is(err, errGitHubCopilotSessionStateLockTimeout) {
			return m.fallbackPair(allowGenerate), nil
		}
		log.Warnf("github-copilot executor: acquire session state lock failed: %v", err)
		return m.fallbackPair(allowGenerate), nil
	}
	defer func() {
		if unlockErr := bucketUnlock(); unlockErr != nil {
			log.Debugf("github-copilot executor: release session state lock failed: %v", unlockErr)
		}
	}()

	stateUnlock, err := m.acquireBucketLock(m.stateLockPath(), m.lockTimeout, m.lockRetryInterval)
	if err != nil {
		if errors.Is(err, errGitHubCopilotSessionStateLockTimeout) {
			return m.fallbackPair(allowGenerate), nil
		}
		log.Warnf("github-copilot executor: acquire session state lock failed: %v", err)
		return m.fallbackPair(allowGenerate), nil
	}
	defer func() {
		if unlockErr := stateUnlock(); unlockErr != nil {
			log.Debugf("github-copilot executor: release session state lock failed: %v", unlockErr)
		}
	}()

	disk, readStateErr := m.readStateDisk()
	if readStateErr != nil {
		log.Warnf("github-copilot executor: read session state failed: %v", readStateErr)
		return m.fallbackPair(allowGenerate), nil
	}

	pair := githubCopilotSessionPair{}
	if disk.Pairs != nil {
		pair = normalizeGitHubCopilotSessionPair(disk.Pairs[key])
	}
	if pair != (githubCopilotSessionPair{}) {
		return pair, nil
	}
	if !allowGenerate {
		return githubCopilotSessionPair{}, nil
	}

	generated, err := newGitHubCopilotSessionPair()
	if err != nil {
		return githubCopilotSessionPair{}, err
	}

	if disk.Pairs == nil {
		disk.Pairs = map[string]githubCopilotSessionPair{}
	}
	disk.Version = githubCopilotSessionStateVersion
	disk.Pairs[key] = generated

	if err := m.persistStateDisk(disk); err != nil {
		log.Warnf("github-copilot executor: persist session state failed: %v", err)
		return generated, nil
	}
	return generated, nil
}

func (m *githubCopilotSessionStateManager) fallbackPair(allowGenerate bool) githubCopilotSessionPair {
	if !allowGenerate {
		return githubCopilotSessionPair{}
	}
	pair, err := newGitHubCopilotSessionPair()
	if err != nil {
		log.Warnf("github-copilot executor: generate fallback session pair failed: %v", err)
		return githubCopilotSessionPair{}
	}
	return pair
}

func (m *githubCopilotSessionStateManager) readStateDisk() (githubCopilotSessionStateDisk, error) {
	if strings.TrimSpace(m.stateFile) == "" {
		return githubCopilotSessionStateDisk{
			Version: githubCopilotSessionStateVersion,
			Pairs:   map[string]githubCopilotSessionPair{},
		}, nil
	}

	raw, err := m.readFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return githubCopilotSessionStateDisk{
				Version: githubCopilotSessionStateVersion,
				Pairs:   map[string]githubCopilotSessionPair{},
			}, nil
		}
		return githubCopilotSessionStateDisk{}, err
	}

	var disk githubCopilotSessionStateDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return githubCopilotSessionStateDisk{}, err
	}
	if disk.Version != githubCopilotSessionStateVersion {
		disk.Version = githubCopilotSessionStateVersion
	}
	if disk.Pairs == nil {
		disk.Pairs = map[string]githubCopilotSessionPair{}
		return disk, nil
	}
	for key, pair := range disk.Pairs {
		normalized := normalizeGitHubCopilotSessionPair(pair)
		if normalized == (githubCopilotSessionPair{}) {
			delete(disk.Pairs, key)
			continue
		}
		disk.Pairs[key] = normalized
	}
	return disk, nil
}

func (m *githubCopilotSessionStateManager) persistStateDisk(disk githubCopilotSessionStateDisk) error {
	if strings.TrimSpace(m.stateFile) == "" {
		return nil
	}

	dir := filepath.Dir(m.stateFile)
	if err := m.mkdirAll(dir, 0o755); err != nil {
		return err
	}

	disk.Version = githubCopilotSessionStateVersion
	if disk.Pairs == nil {
		disk.Pairs = map[string]githubCopilotSessionPair{}
	}
	raw, err := json.Marshal(disk)
	if err != nil {
		return err
	}

	tmp, err := m.createTemp(dir, filepath.Base(m.stateFile)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanup := func(closeFile bool) {
		if closeFile {
			_ = tmp.Close()
		}
		_ = m.remove(tmpPath)
	}

	if _, err := tmp.Write(raw); err != nil {
		cleanup(true)
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup(true)
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup(false)
		return err
	}
	if err := m.rename(tmpPath, m.stateFile); err != nil {
		_ = m.remove(tmpPath)
		return err
	}
	if err := syncDirectory(dir); err != nil {
		log.Debugf("github-copilot executor: sync session state directory failed: %v", err)
	}
	return nil
}

func (m *githubCopilotSessionStateManager) bucketLockPath(key string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(key)))
	fileName := hex.EncodeToString(hash[:]) + ".lock"
	return filepath.Join(m.lockRoot, fileName)
}

func (m *githubCopilotSessionStateManager) stateLockPath() string {
	return filepath.Join(m.lockRoot, "state.lock")
}

func (m *githubCopilotSessionStateManager) acquireBucketLockFile(lockFile string, timeout, retryInterval time.Duration) (func() error, error) {
	if strings.TrimSpace(lockFile) == "" {
		return func() error { return nil }, nil
	}
	if timeout <= 0 {
		timeout = githubCopilotSessionStateDefaultLockTimeout
	}
	if retryInterval <= 0 {
		retryInterval = githubCopilotSessionStateDefaultLockRetryInterval
	}
	staleAge := m.lockStaleAge
	if staleAge <= 0 {
		staleAge = githubCopilotSessionStateDefaultStaleLockAge
	}

	if err := m.mkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockFile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_ = f.Close()
			return func() error {
				err := m.remove(lockFile)
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}, nil
		}
		if !isGitHubCopilotSessionStateLockBusyError(err) {
			return nil, err
		}

		recovered, recoverErr := m.recoverStaleLockFile(lockFile, staleAge)
		if recoverErr != nil {
			log.Debugf("github-copilot executor: recover stale lock failed: %v", recoverErr)
		} else if recovered {
			continue
		}

		if time.Now().After(deadline) {
			return nil, errGitHubCopilotSessionStateLockTimeout
		}
		time.Sleep(retryInterval)
	}
}

func (m *githubCopilotSessionStateManager) recoverStaleLockFile(lockFile string, staleAge time.Duration) (bool, error) {
	if staleAge <= 0 {
		return false, nil
	}
	info, err := os.Stat(lockFile)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if time.Since(info.ModTime()) < staleAge {
		return false, nil
	}
	if err := m.remove(lockFile); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func isGitHubCopilotSessionStateLockBusyError(err error) bool {
	if err == nil {
		return false
	}
	return os.IsExist(err) || errors.Is(err, os.ErrExist) || os.IsPermission(err)
}

func githubCopilotSessionInitiatorCanGenerate(initiator string) bool {
	switch strings.ToLower(strings.TrimSpace(initiator)) {
	case "agent", "user":
		return true
	default:
		return false
	}
}

func githubCopilotSessionStateKey(model, bucketIdentity string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(bucketIdentity)))
	return strings.TrimSpace(model) + "|" + hex.EncodeToString(h[:])
}

func normalizeGitHubCopilotSessionPair(pair githubCopilotSessionPair) githubCopilotSessionPair {
	pair.SessionID = strings.TrimSpace(pair.SessionID)
	pair.InteractionID = strings.TrimSpace(pair.InteractionID)
	hasSession := pair.SessionID != ""
	hasInteraction := pair.InteractionID != ""
	if hasSession != hasInteraction {
		return githubCopilotSessionPair{}
	}
	return pair
}

func newGitHubCopilotSessionPair() (githubCopilotSessionPair, error) {
	sessionID, err := uuid.NewV7()
	if err != nil {
		return githubCopilotSessionPair{}, err
	}
	interactionID, err := uuid.NewV7()
	if err != nil {
		return githubCopilotSessionPair{}, err
	}
	return githubCopilotSessionPair{
		SessionID:     sessionID.String(),
		InteractionID: interactionID.String(),
	}, nil
}
