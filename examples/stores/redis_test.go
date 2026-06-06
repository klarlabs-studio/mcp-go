package stores

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"go.klarlabs.de/mcp/transport"
)

func TestRedisStore(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis not available: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	ctx := context.Background()
	store := NewRedisStore(client)

	t.Run("Store and Get session", func(t *testing.T) {
		clientID := "test-client-1"
		data := []byte(`{"capabilities":{"tools":true}}`)

		err := store.StoreSession(ctx, clientID, data)
		if err != nil {
			t.Fatalf("StoreSession failed: %v", err)
		}

		got, err := store.GetSession(ctx, clientID)
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}

		if string(got) != string(data) {
			t.Errorf("GetSession = %q, want %q", string(got), string(data))
		}
	})

	t.Run("Get non-existent session", func(t *testing.T) {
		got, err := store.GetSession(ctx, "non-existent")
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}
		if got != nil {
			t.Errorf("GetSession for non-existent = %v, want nil", got)
		}
	})

	t.Run("Delete session", func(t *testing.T) {
		clientID := "test-client-2"
		data := []byte(`{"capabilities":{}}`)

		_ = store.StoreSession(ctx, clientID, data)

		err := store.DeleteSession(ctx, clientID)
		if err != nil {
			t.Fatalf("DeleteSession failed: %v", err)
		}

		got, err := store.GetSession(ctx, clientID)
		if err != nil {
			t.Fatalf("GetSession after delete failed: %v", err)
		}
		if got != nil {
			t.Errorf("GetSession after delete = %v, want nil", got)
		}
	})

	t.Run("List sessions", func(t *testing.T) {
		_ = store.StoreSession(ctx, "client-a", []byte("data-a"))
		_ = store.StoreSession(ctx, "client-b", []byte("data-b"))
		_ = store.StoreSession(ctx, "client-c", []byte("data-c"))

		ids, err := store.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}

		if len(ids) < 3 {
			t.Errorf("ListSessions returned %d sessions, want at least 3", len(ids))
		}
	})

	t.Run("TTL expiration", func(t *testing.T) {
		storeTTL := NewRedisStore(client, WithRedisTTL(100*time.Millisecond))

		clientID := "ttl-test"
		data := []byte(`{"ttl":"test"}`)

		_ = storeTTL.StoreSession(ctx, clientID, data)

		got, err := storeTTL.GetSession(ctx, clientID)
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}
		if string(got) != string(data) {
			t.Errorf("GetSession = %q, want %q", string(got), string(data))
		}

		mr.FastForward(150 * time.Millisecond)

		got, err = storeTTL.GetSession(ctx, clientID)
		if err != nil {
			t.Fatalf("GetSession after TTL failed: %v", err)
		}
		if got != nil {
			t.Errorf("GetSession after TTL expired = %v, want nil", got)
		}
	})

	t.Run("Lookup", func(t *testing.T) {
		clientID := "test-client-3"
		data := []byte(`{"lookup":"test"}`)

		_ = store.StoreSession(ctx, clientID, data)

		got, err := store.Lookup(ctx, clientID)
		if err != nil {
			t.Fatalf("Lookup failed: %v", err)
		}

		if string(got) != string(data) {
			t.Errorf("Lookup = %q, want %q", string(got), string(data))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		clientID := "test-client-4"
		data := []byte(`{"delete":"test"}`)

		_ = store.StoreSession(ctx, clientID, data)

		err := store.Delete(ctx, clientID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		got, err := store.GetSession(ctx, clientID)
		if err != nil {
			t.Fatalf("GetSession after Delete failed: %v", err)
		}
		if got != nil {
			t.Errorf("GetSession after Delete = %v, want nil", got)
		}
	})

	t.Run("Ping", func(t *testing.T) {
		err := store.Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed: %v", err)
		}
	})
}

func TestSessionData(t *testing.T) {
	ctx := context.Background()

	mr, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis not available: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	store := NewRedisStore(client)

	t.Run("Store and retrieve session data", func(t *testing.T) {
		clientID := "data-client"
		data := &SessionData{
			Capabilities: map[string]bool{"tools": true, "resources": true},
			Roots:        []map[string]string{{"uri": "file:///tmp"}},
			LogLevel:     "info",
		}

		err := StoreSessionData(ctx, store, clientID, data)
		if err != nil {
			t.Fatalf("StoreSessionData failed: %v", err)
		}

		got, err := GetSessionData(ctx, store, clientID)
		if err != nil {
			t.Fatalf("GetSessionData failed: %v", err)
		}

		if got == nil {
			t.Fatal("expected session data, got nil")
		}

		if !got.Capabilities["tools"] {
			t.Error("expected tools capability")
		}
		if !got.Capabilities["resources"] {
			t.Error("expected resources capability")
		}
		if len(got.Roots) != 1 {
			t.Errorf("expected 1 root, got %d", len(got.Roots))
		}
		if got.LogLevel != "info" {
			t.Errorf("logLevel = %q, want info", got.LogLevel)
		}
	})
}

var _ transport.SessionStore = (*RedisStore)(nil)
