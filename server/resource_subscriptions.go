package server

import (
	"errors"
	"sync"

	"go.klarlabs.de/mcp/protocol"
)

// ResourceNotifier delivers a server-initiated notification to one connected
// client. Transports that support server push (HTTP+SSE, WebSocket) implement
// it so the Server can deliver resource-updated notifications to exactly the
// clients that subscribed — never broadcast to all (Secure By Default).
type ResourceNotifier interface {
	// NotifyClient sends a JSON-RPC notification to the given client.
	NotifyClient(clientID, method string, params any) error
}

// resourceSubscriptions is the server-wide registry of resource subscriptions:
// which client is subscribed to which URI. It is the bridge between the
// resources/subscribe request handlers and an out-of-band watcher that calls
// NotifyResourceUpdated.
type resourceSubscriptions struct {
	mu       sync.RWMutex
	byURI    map[string]map[string]struct{} // uri -> set of client ids
	notifier ResourceNotifier
}

func newResourceSubscriptions() *resourceSubscriptions {
	return &resourceSubscriptions{byURI: make(map[string]map[string]struct{})}
}

func (r *resourceSubscriptions) setNotifier(n ResourceNotifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifier = n
}

func (r *resourceSubscriptions) subscribe(clientID, uri string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	set := r.byURI[uri]
	if set == nil {
		set = make(map[string]struct{})
		r.byURI[uri] = set
	}
	set[clientID] = struct{}{}
}

func (r *resourceSubscriptions) unsubscribe(clientID, uri string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if set := r.byURI[uri]; set != nil {
		delete(set, clientID)
		if len(set) == 0 {
			delete(r.byURI, uri)
		}
	}
}

// removeClient drops every subscription a client held — called when its
// connection closes so the registry never leaks dead clients.
func (r *resourceSubscriptions) removeClient(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for uri, set := range r.byURI {
		delete(set, clientID)
		if len(set) == 0 {
			delete(r.byURI, uri)
		}
	}
}

func (r *resourceSubscriptions) subscribers(uri string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := r.byURI[uri]
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}

// notifyUpdated pushes a resources/updated notification to every client
// subscribed to uri. With no notifier wired (e.g. a transport without server
// push) it is a no-op. Per-client delivery errors are joined and returned so
// one dead client does not stop the rest.
func (r *resourceSubscriptions) notifyUpdated(uri string) error {
	r.mu.RLock()
	notifier := r.notifier
	clients := make([]string, 0, len(r.byURI[uri]))
	for id := range r.byURI[uri] {
		clients = append(clients, id)
	}
	r.mu.RUnlock()

	if notifier == nil {
		return nil
	}
	var errs []error
	for _, id := range clients {
		if err := notifier.NotifyClient(id, protocol.MethodResourceUpdated, ResourceUpdatedNotification{URI: uri}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
