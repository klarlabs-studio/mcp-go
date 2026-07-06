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

func TestInMemoryStore_GetSessionReturnsCopy(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	orig := []byte(`{"secret":"a"}`)
	if err := store.StoreSession(ctx, "c1", orig); err != nil {
		t.Fatalf("StoreSession: %v", err)
	}

	// Mutating the caller's slice after storing must not change stored state.
	orig[2] = 'X'

	got, err := store.GetSession(ctx, "c1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if string(got) != `{"secret":"a"}` {
		t.Fatalf("stored data mutated via caller alias: got %q", got)
	}

	// Mutating the returned slice must not corrupt stored state either.
	got[2] = 'Z'
	again, _ := store.GetSession(ctx, "c1")
	if string(again) != `{"secret":"a"}` {
		t.Fatalf("stored data mutated via returned alias: got %q", again)
	}
}

func TestInMemoryStore_BoundedEviction(t *testing.T) {
	store := NewInMemoryStore(WithMaxSessions(3))
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := store.StoreSession(ctx, id, []byte(id)); err != nil {
			t.Fatalf("StoreSession %s: %v", id, err)
		}
	}
	// Inserting a 4th new key evicts the oldest ("a").
	if err := store.StoreSession(ctx, "d", []byte("d")); err != nil {
		t.Fatalf("StoreSession d: %v", err)
	}

	ids, _ := store.ListSessions(ctx)
	if len(ids) != 3 {
		t.Fatalf("len(sessions) = %d, want 3 (bound not enforced)", len(ids))
	}
	if got, _ := store.GetSession(ctx, "a"); got != nil {
		t.Fatalf("oldest entry 'a' not evicted: %q", got)
	}
	for _, id := range []string{"b", "c", "d"} {
		if got, _ := store.GetSession(ctx, id); string(got) != id {
			t.Fatalf("entry %q = %q, want retained", id, got)
		}
	}

	// Updating an existing key must not grow the store or evict.
	if err := store.StoreSession(ctx, "b", []byte("b2")); err != nil {
		t.Fatalf("update b: %v", err)
	}
	ids, _ = store.ListSessions(ctx)
	if len(ids) != 3 {
		t.Fatalf("after update len = %d, want 3", len(ids))
	}
	if got, _ := store.GetSession(ctx, "b"); string(got) != "b2" {
		t.Fatalf("b = %q, want b2", got)
	}
}
