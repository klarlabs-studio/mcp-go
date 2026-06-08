package client

import (
	"context"
	"encoding/json"
	"errors"

	"go.klarlabs.de/mcp/protocol"
)

// ErrNotificationsUnsupported is returned by StartNotifications when the
// transport has no server-push channel (e.g. plain stdio or an HTTP transport
// against a server without SSE).
var ErrNotificationsUnsupported = errors.New("mcp-go/client: transport does not support notifications")

// uriParam is the JSON key for a resource URI in subscribe/unsubscribe params.
const uriParam = "uri"

// Subscribe registers interest in a resource URI. The server pushes
// notifications/resources/updated when the resource changes; call
// StartNotifications (HTTP+SSE) and register OnResourceUpdated to receive them.
func (c *Client) Subscribe(ctx context.Context, uri string) error {
	_, err := c.call(ctx, protocol.MethodResourcesSubscribe, map[string]string{uriParam: uri})
	return err
}

// Unsubscribe stops the server pushing updates for a resource URI.
func (c *Client) Unsubscribe(ctx context.Context, uri string) error {
	_, err := c.call(ctx, protocol.MethodResourcesUnsubscribe, map[string]string{uriParam: uri})
	return err
}

// OnResourceUpdated registers a callback invoked for each
// notifications/resources/updated the client receives. Register before
// StartNotifications. Safe for concurrent use.
func (c *Client) OnResourceUpdated(handler func(uri string)) {
	c.mu.Lock()
	c.resourceUpdatedHandlers = append(c.resourceUpdatedHandlers, handler)
	c.mu.Unlock()
}

// StartNotifications opens the transport's server-push channel and dispatches
// inbound notifications to registered handlers until ctx is cancelled. It
// blocks; run it in a goroutine. Returns ErrNotificationsUnsupported when the
// transport cannot receive server-initiated messages.
func (c *Client) StartNotifications(ctx context.Context) error {
	st, ok := c.transport.(StreamingTransport)
	if !ok {
		return ErrNotificationsUnsupported
	}
	return st.Stream(ctx, c.dispatchNotification)
}

// dispatchNotification routes one inbound notification to the relevant
// handlers. Currently only resources/updated is surfaced.
func (c *Client) dispatchNotification(method string, params json.RawMessage) {
	if method != protocol.MethodResourceUpdated {
		return
	}
	var payload struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &payload); err != nil || payload.URI == "" {
		return
	}
	c.mu.RLock()
	handlers := make([]func(string), len(c.resourceUpdatedHandlers))
	copy(handlers, c.resourceUpdatedHandlers)
	c.mu.RUnlock()
	for _, h := range handlers {
		h(payload.URI)
	}
}
