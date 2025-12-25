package server

import (
	"context"
	"encoding/json"
	"sync"
)

// CancelledNotification is sent when a request is cancelled.
type CancelledNotification struct {
	// RequestID is the ID of the request to cancel.
	RequestID json.RawMessage `json:"requestId"`
	// Reason is an optional human-readable reason for cancellation.
	Reason string `json:"reason,omitempty"`
}

// CancellationManager tracks in-progress requests and allows cancellation.
type CancellationManager struct {
	mu       sync.RWMutex
	requests map[string]context.CancelFunc
}

// NewCancellationManager creates a new cancellation manager.
func NewCancellationManager() *CancellationManager {
	return &CancellationManager{
		requests: make(map[string]context.CancelFunc),
	}
}

// Track starts tracking a request for potential cancellation.
// Returns a derived context that can be cancelled.
func (m *CancellationManager) Track(ctx context.Context, requestID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.requests[requestID] = cancel
	m.mu.Unlock()

	return ctx, func() {
		cancel()
		m.mu.Lock()
		delete(m.requests, requestID)
		m.mu.Unlock()
	}
}

// Cancel cancels a request by its ID.
// Returns true if the request was found and cancelled.
func (m *CancellationManager) Cancel(requestID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, ok := m.requests[requestID]; ok {
		cancel()
		delete(m.requests, requestID)
		return true
	}
	return false
}

// Untrack removes a request from tracking without cancelling it.
// Call this when a request completes normally.
func (m *CancellationManager) Untrack(requestID string) {
	m.mu.Lock()
	delete(m.requests, requestID)
	m.mu.Unlock()
}

// ActiveRequests returns the number of currently tracked requests.
func (m *CancellationManager) ActiveRequests() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.requests)
}

// cancellationManagerKey is the context key for the cancellation manager.
type cancellationManagerKey struct{}

// ContextWithCancellationManager returns a context with the cancellation manager attached.
func ContextWithCancellationManager(ctx context.Context, manager *CancellationManager) context.Context {
	return context.WithValue(ctx, cancellationManagerKey{}, manager)
}

// CancellationManagerFromContext returns the cancellation manager from context, or nil if none.
func CancellationManagerFromContext(ctx context.Context) *CancellationManager {
	manager, _ := ctx.Value(cancellationManagerKey{}).(*CancellationManager)
	return manager
}
