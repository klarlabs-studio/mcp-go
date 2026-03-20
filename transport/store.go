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

type InMemoryStore struct {
	sessions map[string][]byte
	mu       sync.RWMutex
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		sessions: make(map[string][]byte),
	}
}

func (s *InMemoryStore) StoreSession(ctx context.Context, clientID string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[clientID] = data
	return nil
}

func (s *InMemoryStore) GetSession(ctx context.Context, clientID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.sessions[clientID]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func (s *InMemoryStore) DeleteSession(ctx context.Context, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, clientID)
	return nil
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
