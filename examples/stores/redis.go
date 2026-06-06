package stores

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"go.klarlabs.de/mcp/transport"
)

type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

type RedisStoreOption func(*RedisStore)

func WithRedisTTL(ttl time.Duration) RedisStoreOption {
	return func(s *RedisStore) {
		s.ttl = ttl
	}
}

func NewRedisStore(client *redis.Client, opts ...RedisStoreOption) *RedisStore {
	s := &RedisStore{
		client: client,
		ttl:    24 * time.Hour,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *RedisStore) sessionKey(clientID string) string {
	return fmt.Sprintf("mcp:session:%s", clientID)
}

func (s *RedisStore) StoreSession(ctx context.Context, clientID string, data []byte) error {
	key := s.sessionKey(clientID)
	return s.client.Set(ctx, key, data, s.ttl).Err()
}

func (s *RedisStore) GetSession(ctx context.Context, clientID string) ([]byte, error) {
	key := s.sessionKey(clientID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *RedisStore) DeleteSession(ctx context.Context, clientID string) error {
	key := s.sessionKey(clientID)
	return s.client.Del(ctx, key).Err()
}

func (s *RedisStore) ListSessions(ctx context.Context) ([]string, error) {
	pattern := "mcp:session:*"
	var sessions []string

	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		clientID := key[len("mcp:session:"):]
		sessions = append(sessions, clientID)
	}

	if err := iter.Err(); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (s *RedisStore) Lookup(ctx context.Context, clientID string) ([]byte, error) {
	return s.GetSession(ctx, clientID)
}

func (s *RedisStore) Delete(ctx context.Context, clientID string) error {
	return s.DeleteSession(ctx, clientID)
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

type SessionData struct {
	Capabilities map[string]bool     `json:"capabilities,omitempty"`
	Roots        []map[string]string `json:"roots,omitempty"`
	LogLevel     string              `json:"logLevel,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
}

func StoreSessionData(ctx context.Context, store *RedisStore, clientID string, data *SessionData) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal session data: %w", err)
	}
	return store.StoreSession(ctx, clientID, bytes)
}

func GetSessionData(ctx context.Context, store *RedisStore, clientID string) (*SessionData, error) {
	data, err := store.GetSession(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}

	var sessionData SessionData
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return nil, fmt.Errorf("unmarshal session data: %w", err)
	}

	return &sessionData, nil
}

var _ transport.SessionStore = (*RedisStore)(nil)
