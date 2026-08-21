package password

import (
	"context"
	"sync"
)

// MemStore is an in-memory credential store, suitable for a small fixed set of
// operator-provisioned accounts read from configuration at startup, and for
// tests. It implements [Rehasher], so upgrade-on-verify works — though for an
// in-memory store the upgrade lasts only until restart.
//
// It is NOT a user database: there is no persistence, no enumeration limit
// beyond what the caller provisions, and no bound on entries, because entries
// come from the operator rather than from unauthenticated input. That last
// distinction is why an unbounded map is fine here and is not fine for the
// challenge and token stores, which anyone can cause to grow.
type MemStore struct {
	mu    sync.RWMutex
	creds map[string]string
}

// NewMemStore builds an empty store.
func NewMemStore() *MemStore { return &MemStore{creds: make(map[string]string)} }

// Set stores an already-encoded credential.
func (m *MemStore) Set(subject, encoded string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds[subject] = encoded
}

// Add hashes password with h and stores it.
func (m *MemStore) Add(subject, password string, h Hasher) error {
	encoded, err := h.Hash(password)
	if err != nil {
		return err
	}
	m.Set(subject, encoded)
	return nil
}

// Lookup implements [Store].
func (m *MemStore) Lookup(_ context.Context, subject string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	encoded, ok := m.creds[subject]
	if !ok {
		return "", ErrNoCredential
	}
	return encoded, nil
}

// Rehash implements [Rehasher].
func (m *MemStore) Rehash(_ context.Context, subject, encoded string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.creds[subject]; !ok {
		// Refuse to CREATE a credential through the upgrade path. Only an
		// existing one may be replaced.
		return ErrNoCredential
	}
	m.creds[subject] = encoded
	return nil
}

var (
	_ Store    = (*MemStore)(nil)
	_ Rehasher = (*MemStore)(nil)
)
