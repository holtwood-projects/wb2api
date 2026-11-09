package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Account is an in-memory account entry with its file path.
type Account struct {
	Auth *Auth
	Path string
}

// Rotation strategy for picking the next account.
type Strategy int

const (
	// StrategyRoundRobin cycles accounts on every pick.
	StrategyRoundRobin Strategy = iota
	// StrategyFillFirst always picks the first available account.
	StrategyFillFirst
)

// Manager holds multiple accounts and picks one per request.
type Manager struct {
	mu       sync.Mutex
	accounts []*Account
	strategy Strategy
	next     int
	// affinity 把会话键固定到账号路径，保证上游缓存的前缀落在同一账号。
	// 每个 Manager 独占一份，随 manager.mu 一起保护；不使用包级变量，
	// 否则多个 Manager 会各自持不同锁却写同一张 map。
	affinity map[string]string
}

// NewManager creates an empty account manager.
func NewManager(strategy Strategy) *Manager {
	return &Manager{strategy: strategy, affinity: map[string]string{}}
}

// LoadDir scans dir for *.json auth files and adds them. Existing accounts
// are replaced by their file path.
func (m *Manager) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	for _, f := range files {
		a, err := LoadAuth(f)
		if err != nil {
			continue
		}
		m.upsert(&Account{Auth: a, Path: f})
	}
	return nil
}

func (m *Manager) upsert(acct *Account) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.accounts {
		if a.Path == acct.Path {
			m.accounts[i] = acct
			return
		}
	}
	m.accounts = append(m.accounts, acct)
}

// Add inserts or replaces an account by path.
func (m *Manager) Add(a *Auth, path string) {
	m.upsert(&Account{Auth: a, Path: path})
}

// Accounts returns a snapshot of the loaded accounts.
func (m *Manager) Accounts() []*Account {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Account, len(m.accounts))
	copy(out, m.accounts)
	return out
}

// Remove deletes the account stored at path.
func (m *Manager) Remove(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.accounts {
		if a.Path == path {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			if m.next >= len(m.accounts) {
				m.next = 0
			}
			return
		}
	}
}

// Pick selects an account for a request, honouring cache-affinity hints.
//
// affinity is an optional opaque session key. When non-empty, the first
// account that already owns that key is reused so prompt-cache state stays
// on one account; otherwise a fresh account is bound to the key. This is the
// hook the cache-optimisation phase (Phase 4) relies on.
func (m *Manager) Pick(affinity string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.accounts) == 0 {
		return nil, os.ErrNotExist
	}
	if affinity != "" {
		if acct := m.pickAffinityLocked(affinity); acct != nil {
			return acct, nil
		}
	}
	switch m.strategy {
	case StrategyFillFirst:
		return m.accounts[0], nil
	default:
		acct := m.accounts[m.next%len(m.accounts)]
		m.next++
		if affinity != "" {
			m.bindLocked(affinity, acct)
		}
		return acct, nil
	}
}

// pickAffinityLocked returns the account bound to key, if any.
func (m *Manager) pickAffinityLocked(key string) *Account {
	path, ok := m.affinity[key]
	if !ok {
		return nil
	}
	for _, a := range m.accounts {
		if a.Path == path {
			return a
		}
	}
	delete(m.affinity, key)
	return nil
}

// bindLocked pins key to an account path.
func (m *Manager) bindLocked(key string, acct *Account) {
	m.affinity[key] = acct.Path
}

// SessionKeyFromBody extracts a cache-affinity session key from an OpenAI
// request body, if the client supplied one (e.g. prompt_cache_key or a
// conversation id under metadata).
//
// The exact official field name is not yet confirmed by capture (see
// notes/workbuddy-protocol-notes.md, W1); this is a best-effort probe.
func SessionKeyFromBody(body map[string]any) string {
	if v, ok := body["prompt_cache_key"].(string); ok && v != "" {
		return v
	}
	meta, _ := body["metadata"].(map[string]any)
	if meta != nil {
		if v, ok := meta["session_id"].(string); ok && v != "" {
			return v
		}
		if v, ok := meta["conversation_id"].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
