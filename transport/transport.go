// Package transport provides MCP transport implementations.
package transport

import (
	"context"
	"encoding/json"

	"go.klarlabs.de/mcp/protocol"
)

// JSONRPCVersion is the JSON-RPC version emitted in every transport-level
// notification or response. MCP is JSON-RPC 2.0.
const JSONRPCVersion = "2.0"

// Handler processes incoming MCP requests.
type Handler interface {
	HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error)
}

// HandlerFunc is an adapter to allow ordinary functions as handlers.
type HandlerFunc func(ctx context.Context, req *protocol.Request) (*protocol.Response, error)

// HandleRequest calls f(ctx, req).
func (f HandlerFunc) HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	return f(ctx, req)
}

// Transport defines the communication layer interface.
type Transport interface {
	// Serve starts the transport, blocking until ctx is canceled or an error occurs.
	Serve(ctx context.Context, handler Handler) error

	// Addr returns the transport's address description.
	Addr() string
}

// NotificationSender can send JSON-RPC notifications to clients.
type NotificationSender interface {
	SendNotification(method string, params any) error
}

// notificationSenderKey is the context key for the notification sender.
type notificationSenderKey struct{}

// ContextWithNotificationSender returns a context with the notification sender attached.
func ContextWithNotificationSender(ctx context.Context, sender NotificationSender) context.Context {
	return context.WithValue(ctx, notificationSenderKey{}, sender)
}

// NotificationSenderFromContext returns the notification sender from context, or nil if none.
func NotificationSenderFromContext(ctx context.Context) NotificationSender {
	sender, _ := ctx.Value(notificationSenderKey{}).(NotificationSender)
	return sender
}

// Notification represents a JSON-RPC notification (no ID, no response expected).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
