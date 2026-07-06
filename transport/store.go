package transport

import (
	"context"
	"sync"
)

type SessionStore interface {
	Store
	Lookup(ctx context.Context, clientID string) ([]byte, error)
	Delete(ctx context.Context, clientID string) error
}

type Store interface {
	StoreSession(ctx context.Context, clientID string, data []byte) error
	GetSession(ctx context.Context, clientID string) ([]byte, error)
	DeleteSession(ctx context.Context, clientID string) error
	ListSessions(ctx context.Context) ([]string, error)
}

// DefaultMaxSessions bounds the number of entries an InMemoryStore retains by
// default so an unbounded stream of distinct client ids cannot grow the map
// without limit (a memory-exhaustion vector). When the store is full, storing a
// NEW client id evicts the oldest entry (FIFO). Override via WithMaxSessions.
const DefaultMaxSessions = 10000

type InMemoryStore struct {
	mu         sync.RWMutex
	sessions   map[string][]byte
	order      []string // insertion order of live keys, for FIFO eviction
	maxEntries int
}

// InMemoryStoreOption configures an InMemoryStore.
type InMemoryStoreOption func(*InMemoryStore)

// WithMaxSessions bounds the number of retained sessions. A non-positive value
// restores the default (DefaultMaxSessions).
func WithMaxSessions(n int) InMemoryStoreOption {
	return func(s *InMemoryStore) {
		if n <= 0 {
			n = DefaultMaxSessions
		}
		s.maxEntries = n
	}
}

func NewInMemoryStore(opts ...InMemoryStoreOption) *InMemoryStore {
	s := &InMemoryStore{
		sessions:   make(map[string][]byte),
		maxEntries: DefaultMaxSessions,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *InMemoryStore) StoreSession(ctx context.Context, clientID string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy so a later mutation of the caller's slice cannot corrupt the
	// retained session data.
	cp := append([]byte(nil), data...)

	if _, exists := s.sessions[clientID]; exists {
		s.sessions[clientID] = cp
		return nil
	}

	// New key: enforce the bound before inserting, evicting the oldest entry.
	if s.maxEntries > 0 && len(s.sessions) >= s.maxEntries {
		s.evictOldestLocked()
	}
	s.sessions[clientID] = cp
	s.order = append(s.order, clientID)
	return nil
}

// evictOldestLocked removes the oldest live entry. Callers must hold s.mu.
func (s *InMemoryStore) evictOldestLocked() {
	for len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		if _, ok := s.sessions[oldest]; ok {
			delete(s.sessions, oldest)
			return
		}
	}
}

func (s *InMemoryStore) GetSession(ctx context.Context, clientID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.sessions[clientID]
	if !ok {
		return nil, nil
	}
	// Return a copy: handing out the map's backing slice lets a caller mutate
	// stored state (or race a concurrent StoreSession) through the alias.
	return append([]byte(nil), data...), nil
}

func (s *InMemoryStore) DeleteSession(ctx context.Context, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[clientID]; ok {
		delete(s.sessions, clientID)
		s.removeFromOrderLocked(clientID)
	}
	return nil
}

// removeFromOrderLocked drops clientID from the insertion-order list. Callers
// must hold s.mu.
func (s *InMemoryStore) removeFromOrderLocked(clientID string) {
	for i, id := range s.order {
		if id == clientID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

func (s *InMemoryStore) ListSessions(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *InMemoryStore) Lookup(ctx context.Context, clientID string) ([]byte, error) {
	return s.GetSession(ctx, clientID)
}

func (s *InMemoryStore) Delete(ctx context.Context, clientID string) error {
	return s.DeleteSession(ctx, clientID)
}
