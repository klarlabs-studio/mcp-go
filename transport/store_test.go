package transport

import (
	"context"
	"testing"
)

func TestInMemoryStore(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

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
		store2 := NewInMemoryStore()

		_ = store2.StoreSession(ctx, "client-a", []byte("data-a"))
		_ = store2.StoreSession(ctx, "client-b", []byte("data-b"))
		_ = store2.StoreSession(ctx, "client-c", []byte("data-c"))

		ids, err := store2.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}

		if len(ids) != 3 {
			t.Errorf("ListSessions returned %d sessions, want 3", len(ids))
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

	t.Run("Concurrent access", func(t *testing.T) {
		store3 := NewInMemoryStore()

		done := make(chan struct{})
		n := 100

		for i := 0; i < n; i++ {
			go func(id int) {
				clientID := "concurrent-client"
				data := []byte(`{"id":` + string(rune('0'+id%10)) + `}`)
				_ = store3.StoreSession(ctx, clientID, data)
				_, _ = store3.GetSession(ctx, clientID)
				done <- struct{}{}
			}(i)
		}

		for i := 0; i < n; i++ {
			<-done
		}
	})
}
