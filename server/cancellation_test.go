package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestCancelledNotification(t *testing.T) {
	requestID, _ := json.Marshal(123)
	notification := CancelledNotification{
		RequestID: requestID,
		Reason:    "User requested cancellation",
	}

	if string(notification.RequestID) != "123" {
		t.Errorf("expected requestId '123', got %q", string(notification.RequestID))
	}
	if notification.Reason != "User requested cancellation" {
		t.Errorf("expected reason 'User requested cancellation', got %q", notification.Reason)
	}
}

func TestCancellationManagerTrackAndCancel(t *testing.T) {
	manager := NewCancellationManager()
	ctx := context.Background()

	// Track a request
	trackedCtx, cleanup := manager.Track(ctx, "req-1")
	defer cleanup()

	if manager.ActiveRequests() != 1 {
		t.Errorf("expected 1 active request, got %d", manager.ActiveRequests())
	}

	// Cancel the request
	cancelled := manager.Cancel("req-1")
	if !cancelled {
		t.Error("expected Cancel to return true")
	}

	// Context should be cancelled
	select {
	case <-trackedCtx.Done():
		// Expected
	default:
		t.Error("expected context to be cancelled")
	}

	// Active requests should be 0
	if manager.ActiveRequests() != 0 {
		t.Errorf("expected 0 active requests after cancel, got %d", manager.ActiveRequests())
	}
}

func TestCancellationManagerCancelNonexistent(t *testing.T) {
	manager := NewCancellationManager()

	cancelled := manager.Cancel("nonexistent")
	if cancelled {
		t.Error("expected Cancel to return false for nonexistent request")
	}
}

func TestCancellationManagerUntrack(t *testing.T) {
	manager := NewCancellationManager()
	ctx := context.Background()

	// Track a request
	trackedCtx, cleanup := manager.Track(ctx, "req-1")

	if manager.ActiveRequests() != 1 {
		t.Errorf("expected 1 active request, got %d", manager.ActiveRequests())
	}

	// Untrack without cancelling
	manager.Untrack("req-1")

	if manager.ActiveRequests() != 0 {
		t.Errorf("expected 0 active requests after untrack, got %d", manager.ActiveRequests())
	}

	// Context should still be active (not cancelled)
	select {
	case <-trackedCtx.Done():
		t.Error("context should not be cancelled by Untrack")
	default:
		// Expected
	}

	// Clean up
	cleanup()
}

func TestCancellationManagerCleanup(t *testing.T) {
	manager := NewCancellationManager()
	ctx := context.Background()

	// Track a request
	_, cleanup := manager.Track(ctx, "req-1")

	if manager.ActiveRequests() != 1 {
		t.Errorf("expected 1 active request, got %d", manager.ActiveRequests())
	}

	// Call cleanup (simulating request completion)
	cleanup()

	// Should be removed from tracking
	if manager.ActiveRequests() != 0 {
		t.Errorf("expected 0 active requests after cleanup, got %d", manager.ActiveRequests())
	}
}

func TestCancellationManagerMultipleRequests(t *testing.T) {
	manager := NewCancellationManager()
	ctx := context.Background()

	// Track multiple requests
	_, cleanup1 := manager.Track(ctx, "req-1")
	_, cleanup2 := manager.Track(ctx, "req-2")
	ctx3, cleanup3 := manager.Track(ctx, "req-3")

	defer cleanup1()
	defer cleanup2()
	defer cleanup3()

	if manager.ActiveRequests() != 3 {
		t.Errorf("expected 3 active requests, got %d", manager.ActiveRequests())
	}

	// Cancel one request
	manager.Cancel("req-2")

	if manager.ActiveRequests() != 2 {
		t.Errorf("expected 2 active requests after cancel, got %d", manager.ActiveRequests())
	}

	// req-3 should still be active
	select {
	case <-ctx3.Done():
		t.Error("req-3 should not be cancelled")
	default:
		// Expected
	}
}

func TestCancellationManagerConcurrency(t *testing.T) {
	manager := NewCancellationManager()
	ctx := context.Background()

	var wg sync.WaitGroup
	const numGoroutines = 100

	// Track requests concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reqID := "req-" + string(rune('a'+id%26))
			_, cleanup := manager.Track(ctx, reqID)
			time.Sleep(time.Millisecond)
			cleanup()
		}(i)
	}

	wg.Wait()

	// All requests should be cleaned up
	if manager.ActiveRequests() != 0 {
		t.Errorf("expected 0 active requests after all cleanups, got %d", manager.ActiveRequests())
	}
}

func TestContextWithCancellationManager(t *testing.T) {
	ctx := context.Background()
	manager := NewCancellationManager()

	ctxWithManager := ContextWithCancellationManager(ctx, manager)

	retrieved := CancellationManagerFromContext(ctxWithManager)
	if retrieved != manager {
		t.Error("expected to retrieve the same manager from context")
	}
}

func TestCancellationManagerFromContextNil(t *testing.T) {
	ctx := context.Background()

	retrieved := CancellationManagerFromContext(ctx)
	if retrieved != nil {
		t.Error("expected nil manager from context without manager")
	}
}
