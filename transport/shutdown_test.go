package transport_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.klarlabs.de/mcp/transport"
)

func TestShutdownManager(t *testing.T) {
	t.Run("tracks in-flight requests", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.DefaultShutdownConfig())

		if sm.InFlightRequests() != 0 {
			t.Error("expected 0 in-flight requests initially")
		}

		if !sm.TrackRequest() {
			t.Error("expected TrackRequest to succeed")
		}

		if sm.InFlightRequests() != 1 {
			t.Errorf("expected 1 in-flight request, got %d", sm.InFlightRequests())
		}

		sm.CompleteRequest()

		if sm.InFlightRequests() != 0 {
			t.Errorf("expected 0 in-flight requests after completion, got %d", sm.InFlightRequests())
		}
	})

	t.Run("rejects requests when draining", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.ShutdownConfig{
			Timeout: 100 * time.Millisecond,
		})

		// Start shutdown in background
		ctx := context.Background()
		go sm.Shutdown(ctx)

		// Wait for draining to start
		time.Sleep(20 * time.Millisecond)

		// Now requests should be rejected
		if sm.TrackRequest() {
			t.Error("expected TrackRequest to fail during draining")
		}

		if !sm.IsDraining() {
			t.Error("expected IsDraining to return true")
		}
	})

	t.Run("waits for in-flight requests", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.ShutdownConfig{
			Timeout: 1 * time.Second,
		})

		// Start a request
		if !sm.TrackRequest() {
			t.Fatal("failed to track request")
		}

		// Start shutdown in background
		shutdownDone := make(chan error, 1)
		go func() {
			shutdownDone <- sm.Shutdown(context.Background())
		}()

		// Verify shutdown is not complete yet
		select {
		case <-shutdownDone:
			t.Error("shutdown completed before request was done")
		case <-time.After(50 * time.Millisecond):
			// Expected - shutdown is waiting
		}

		// Complete the request
		sm.CompleteRequest()

		// Now shutdown should complete
		select {
		case err := <-shutdownDone:
			if err != nil {
				t.Errorf("unexpected shutdown error: %v", err)
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("shutdown did not complete after request finished")
		}
	})

	t.Run("times out if requests don't complete", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.ShutdownConfig{
			Timeout: 100 * time.Millisecond,
		})

		// Start a request but never complete it
		if !sm.TrackRequest() {
			t.Fatal("failed to track request")
		}

		err := sm.Shutdown(context.Background())
		if err == nil {
			t.Error("expected timeout error")
		}

		// The request is still tracked
		if sm.InFlightRequests() != 1 {
			t.Errorf("expected 1 in-flight request, got %d", sm.InFlightRequests())
		}
	})

	t.Run("respects drain delay", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.ShutdownConfig{
			Timeout:    1 * time.Second,
			DrainDelay: 50 * time.Millisecond,
		})

		start := time.Now()

		err := sm.Shutdown(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		elapsed := time.Since(start)
		if elapsed < 50*time.Millisecond {
			t.Errorf("shutdown completed too quickly (%v), expected at least 50ms drain delay", elapsed)
		}
	})

	t.Run("calls lifecycle hooks", func(t *testing.T) {
		var startCalled, drainCalled, completeCalled atomic.Bool
		var completeErr error

		sm := transport.NewShutdownManager(transport.ShutdownConfig{
			Timeout: 100 * time.Millisecond,
			OnShutdownStart: func() {
				startCalled.Store(true)
			},
			OnDrainStart: func() {
				drainCalled.Store(true)
			},
			OnShutdownComplete: func(err error) {
				completeCalled.Store(true)
				completeErr = err
			},
		})

		err := sm.Shutdown(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !startCalled.Load() {
			t.Error("OnShutdownStart not called")
		}
		if !drainCalled.Load() {
			t.Error("OnDrainStart not called")
		}
		if !completeCalled.Load() {
			t.Error("OnShutdownComplete not called")
		}
		if completeErr != nil {
			t.Errorf("unexpected error in OnShutdownComplete: %v", completeErr)
		}
	})

	t.Run("done channel closes on completion", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.DefaultShutdownConfig())

		// Channel should be open initially
		select {
		case <-sm.Done():
			t.Error("done channel closed before shutdown")
		default:
			// Expected
		}

		go sm.Shutdown(context.Background())

		// Wait for shutdown to complete
		select {
		case <-sm.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Error("done channel not closed after shutdown")
		}
	})

	t.Run("respects context cancellation during drain delay", func(t *testing.T) {
		sm := transport.NewShutdownManager(transport.ShutdownConfig{
			Timeout:    1 * time.Second,
			DrainDelay: 1 * time.Second, // Long drain delay
		})

		ctx, cancel := context.WithCancel(context.Background())

		start := time.Now()
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := sm.Shutdown(ctx)
		elapsed := time.Since(start)

		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}

		if elapsed > 200*time.Millisecond {
			t.Errorf("shutdown took too long (%v), should have cancelled quickly", elapsed)
		}
	})
}

func TestDefaultShutdownConfig(t *testing.T) {
	config := transport.DefaultShutdownConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", config.Timeout)
	}

	if config.DrainDelay != 0 {
		t.Errorf("expected 0 drain delay, got %v", config.DrainDelay)
	}
}

func TestHTTPShutdownOptions(t *testing.T) {
	t.Run("WithShutdownTimeout", func(t *testing.T) {
		// This is tested via the HTTP transport integration
		// Just verify the function exists and is callable
		opt := transport.WithShutdownTimeout(5 * time.Second)
		if opt == nil {
			t.Error("expected non-nil option")
		}
	})

	t.Run("WithShutdownDrainDelay", func(t *testing.T) {
		opt := transport.WithShutdownDrainDelay(2 * time.Second)
		if opt == nil {
			t.Error("expected non-nil option")
		}
	})
}
