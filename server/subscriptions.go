package server

import (
	"sync"
)

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
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subscriptions: make(map[string]map[string]struct{}),
	}
}

// Subscribe adds a client subscription for a resource URI.
func (m *SubscriptionManager) Subscribe(clientID, uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.subscriptions[uri] == nil {
		m.subscriptions[uri] = make(map[string]struct{})
	}
	m.subscriptions[uri][clientID] = struct{}{}
}

// Unsubscribe removes a client subscription for a resource URI.
func (m *SubscriptionManager) Unsubscribe(clientID, uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if clients, ok := m.subscriptions[uri]; ok {
		delete(clients, clientID)
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

// subscriptionManagerKey is the context key for the subscription manager.
type subscriptionManagerKey struct{}
