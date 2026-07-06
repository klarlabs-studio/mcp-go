package server

import (
	"sync"
)

// defaultMaxSubscriptionsPerClient bounds how many distinct resource URIs a
// single client may subscribe to. Without a cap the subscription map is an
// unbounded, client-controlled allocation — a denial-of-service vector.
const defaultMaxSubscriptionsPerClient = 1000

// SubscribeRequest is sent by the client to subscribe to resource updates.
type SubscribeRequest struct {
	URI string `json:"uri"`
}

// UnsubscribeRequest is sent by the client to unsubscribe from resource updates.
type UnsubscribeRequest struct {
	URI string `json:"uri"`
}

// ResourceUpdatedNotification is sent when a subscribed resource changes.
type ResourceUpdatedNotification struct {
	URI string `json:"uri"`
}

// SubscriptionManager tracks resource subscriptions.
type SubscriptionManager struct {
	mu            sync.RWMutex
	subscriptions map[string]map[string]struct{} // URI -> set of client IDs
	perClient     map[string]int                 // client ID -> subscription count
	maxPerClient  int
}

// SubscriptionManagerOption configures a SubscriptionManager.
type SubscriptionManagerOption func(*SubscriptionManager)

// WithMaxSubscriptionsPerClient sets the per-client subscription cap. Values
// <= 0 keep the default.
func WithMaxSubscriptionsPerClient(n int) SubscriptionManagerOption {
	return func(m *SubscriptionManager) {
		if n > 0 {
			m.maxPerClient = n
		}
	}
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager(opts ...SubscriptionManagerOption) *SubscriptionManager {
	m := &SubscriptionManager{
		subscriptions: make(map[string]map[string]struct{}),
		perClient:     make(map[string]int),
		maxPerClient:  defaultMaxSubscriptionsPerClient,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Subscribe adds a client subscription for a resource URI. It returns false
// (rejecting the subscription) when the client is already at its per-client
// cap. Re-subscribing to an existing URI is idempotent and always accepted.
func (m *SubscriptionManager) Subscribe(clientID, uri string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if clients := m.subscriptions[uri]; clients != nil {
		if _, ok := clients[clientID]; ok {
			return true // already subscribed — idempotent
		}
	}

	if m.perClient[clientID] >= m.maxPerClient {
		return false
	}

	if m.subscriptions[uri] == nil {
		m.subscriptions[uri] = make(map[string]struct{})
	}
	m.subscriptions[uri][clientID] = struct{}{}
	m.perClient[clientID]++
	return true
}

// Unsubscribe removes a client subscription for a resource URI.
func (m *SubscriptionManager) Unsubscribe(clientID, uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if clients, ok := m.subscriptions[uri]; ok {
		if _, subscribed := clients[clientID]; subscribed {
			delete(clients, clientID)
			m.decClientLocked(clientID)
		}
		if len(clients) == 0 {
			delete(m.subscriptions, uri)
		}
	}
}

// UnsubscribeAll removes all subscriptions for a client.
func (m *SubscriptionManager) UnsubscribeAll(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for uri, clients := range m.subscriptions {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(m.subscriptions, uri)
		}
	}
	delete(m.perClient, clientID)
}

// decClientLocked decrements a client's subscription count, dropping the entry
// at zero. Caller must hold m.mu.
func (m *SubscriptionManager) decClientLocked(clientID string) {
	if m.perClient[clientID] <= 1 {
		delete(m.perClient, clientID)
		return
	}
	m.perClient[clientID]--
}

// Subscribers returns the client IDs subscribed to a resource URI.
func (m *SubscriptionManager) Subscribers(uri string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients, ok := m.subscriptions[uri]
	if !ok {
		return nil
	}

	result := make([]string, 0, len(clients))
	for clientID := range clients {
		result = append(result, clientID)
	}
	return result
}

// HasSubscribers returns true if the resource URI has any subscribers.
func (m *SubscriptionManager) HasSubscribers(uri string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subscriptions[uri]) > 0
}

// IsSubscribed returns true if the client is subscribed to the resource URI.
func (m *SubscriptionManager) IsSubscribed(clientID, uri string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if clients, ok := m.subscriptions[uri]; ok {
		_, subscribed := clients[clientID]
		return subscribed
	}
	return false
}

// SubscriptionCount returns the total number of subscriptions.
func (m *SubscriptionManager) SubscriptionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, clients := range m.subscriptions {
		count += len(clients)
	}
	return count
}
