package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// Session represents a bidirectional MCP session with a client.
// It allows the server to send requests to the client (for sampling, roots)
// and receive notifications.
type Session struct {
	id          string
	mu          sync.RWMutex
	sender      RequestSender
	notifier    NotificationSender
	requestID   atomic.Int64
	logLevel    LogLevel
	roots       []Root
	rootsChange func([]Root)

	// Cancellation tracking
	cancellation *CancellationManager

	// Resource subscriptions
	subscriptions *SubscriptionManager

	// Client capabilities (what the client supports)
	clientCaps ClientCapabilities
}

// ClientCapabilities describes what features the client supports.
type ClientCapabilities struct {
	Sampling bool             `json:"sampling,omitempty"`
	Roots    *RootsCapability `json:"roots,omitempty"`
}

// RootsCapability describes the client's roots support.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// RequestSender can send requests to the client and receive responses.
type RequestSender interface {
	SendRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error)
}

// Note: NotificationSender is defined in progress.go

// SessionOption configures a Session.
type SessionOption func(*Session)

// WithClientCapabilities sets the client capabilities.
func WithClientCapabilities(caps ClientCapabilities) SessionOption {
	return func(s *Session) {
		s.clientCaps = caps
	}
}

// WithRootsChangeCallback sets a callback for when roots change.
func WithRootsChangeCallback(callback func([]Root)) SessionOption {
	return func(s *Session) {
		s.rootsChange = callback
	}
}

// NewSession creates a new session with the given ID and options.
func NewSession(id string, sender RequestSender, notifier NotificationSender, opts ...SessionOption) *Session {
	s := &Session{
		id:            id,
		sender:        sender,
		notifier:      notifier,
		logLevel:      LogLevelInfo,
		cancellation:  NewCancellationManager(),
		subscriptions: NewSubscriptionManager(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ID returns the session ID.
func (s *Session) ID() string {
	return s.id
}

// ClientCapabilities returns the client's capabilities.
func (s *Session) ClientCapabilities() ClientCapabilities {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clientCaps
}

// SetClientCapabilities updates the client's capabilities.
func (s *Session) SetClientCapabilities(caps ClientCapabilities) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientCaps = caps
}

// SupportsFeature returns true if the client supports the given feature.
func (s *Session) SupportsFeature(feature string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch feature {
	case "sampling":
		return s.clientCaps.Sampling
	case "roots":
		return s.clientCaps.Roots != nil
	case "roots.listChanged":
		return s.clientCaps.Roots != nil && s.clientCaps.Roots.ListChanged
	default:
		return false
	}
}

// CreateMessage sends a sampling request to the client.
// Returns an error if the client doesn't support sampling.
func (s *Session) CreateMessage(ctx context.Context, req *CreateMessageRequest) (*CreateMessageResult, error) {
	if !s.SupportsFeature("sampling") {
		return nil, fmt.Errorf("client does not support sampling")
	}

	params, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	idRaw, err := json.Marshal(s.requestID.Add(1))
	if err != nil {
		return nil, fmt.Errorf("marshal request ID: %w", err)
	}

	rpcReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      idRaw,
		Method:  protocol.MethodSamplingCreateMessage,
		Params:  params,
	}

	resp, err := s.sender.SendRequest(ctx, rpcReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	var result CreateMessageResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// ListRoots requests the list of roots from the client.
// Returns an error if the client doesn't support roots.
func (s *Session) ListRoots(ctx context.Context) (*ListRootsResult, error) {
	if !s.SupportsFeature("roots") {
		return nil, fmt.Errorf("client does not support roots")
	}

	idRaw, err := json.Marshal(s.requestID.Add(1))
	if err != nil {
		return nil, fmt.Errorf("marshal request ID: %w", err)
	}

	rpcReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      idRaw,
		Method:  protocol.MethodRootsList,
		Params:  nil,
	}

	resp, err := s.sender.SendRequest(ctx, rpcReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	var result ListRootsResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	// Cache the roots
	s.mu.Lock()
	s.roots = result.Roots
	s.mu.Unlock()

	return &result, nil
}

// Roots returns the cached roots. Call ListRoots first to populate.
func (s *Session) Roots() []Root {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roots
}

// HandleRootsChanged is called when the client sends a roots/list_changed notification.
func (s *Session) HandleRootsChanged(roots []Root) {
	s.mu.Lock()
	s.roots = roots
	callback := s.rootsChange
	s.mu.Unlock()

	if callback != nil {
		callback(roots)
	}
}

// Log sends a log message at the specified level.
func (s *Session) Log(level LogLevel, logger string, data any) {
	s.mu.RLock()
	minLevel := s.logLevel
	s.mu.RUnlock()

	if !ShouldLog(level, minLevel) {
		return
	}

	msg := LoggingMessage{
		Level:  level,
		Logger: logger,
		Data:   data,
	}

	_ = s.notifier.SendNotification(protocol.MethodLoggingMessage, msg)
}

// Debug logs a debug message.
func (s *Session) Debug(logger string, data any) {
	s.Log(LogLevelDebug, logger, data)
}

// Info logs an info message.
func (s *Session) Info(logger string, data any) {
	s.Log(LogLevelInfo, logger, data)
}

// Notice logs a notice message.
func (s *Session) Notice(logger string, data any) {
	s.Log(LogLevelNotice, logger, data)
}

// Warning logs a warning message.
func (s *Session) Warning(logger string, data any) {
	s.Log(LogLevelWarning, logger, data)
}

// Error logs an error message.
func (s *Session) Error(logger string, data any) {
	s.Log(LogLevelError, logger, data)
}

// Critical logs a critical message.
func (s *Session) Critical(logger string, data any) {
	s.Log(LogLevelCritical, logger, data)
}

// Alert logs an alert message.
func (s *Session) Alert(logger string, data any) {
	s.Log(LogLevelAlert, logger, data)
}

// Emergency logs an emergency message.
func (s *Session) Emergency(logger string, data any) {
	s.Log(LogLevelEmergency, logger, data)
}

// SetLogLevel sets the minimum log level.
func (s *Session) SetLogLevel(level LogLevel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logLevel = level
}

// LogLevel returns the current minimum log level.
func (s *Session) LogLevel() LogLevel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logLevel
}

// Cancel sends a cancellation notification for a request.
func (s *Session) Cancel(requestID json.RawMessage, reason string) error {
	notification := CancelledNotification{
		RequestID: requestID,
		Reason:    reason,
	}
	return s.notifier.SendNotification(protocol.MethodCancelled, notification)
}

// CancellationManager returns the session's cancellation manager.
func (s *Session) CancellationManager() *CancellationManager {
	return s.cancellation
}

// Subscribe adds a subscription for a resource URI.
func (s *Session) Subscribe(uri string) {
	s.subscriptions.Subscribe(s.id, uri)
}

// Unsubscribe removes a subscription for a resource URI.
func (s *Session) Unsubscribe(uri string) {
	s.subscriptions.Unsubscribe(s.id, uri)
}

// SubscriptionManager returns the session's subscription manager.
func (s *Session) SubscriptionManager() *SubscriptionManager {
	return s.subscriptions
}

// NotifyResourceUpdated sends a resource updated notification.
func (s *Session) NotifyResourceUpdated(uri string) error {
	notification := ResourceUpdatedNotification{URI: uri}
	return s.notifier.SendNotification(protocol.MethodResourceUpdated, notification)
}

// NotifyResourceListChanged sends a resource list changed notification.
func (s *Session) NotifyResourceListChanged() error {
	return s.notifier.SendNotification(protocol.MethodResourceListChanged, nil)
}

// NotifyToolListChanged sends a tool list changed notification.
func (s *Session) NotifyToolListChanged() error {
	return s.notifier.SendNotification(protocol.MethodToolListChanged, nil)
}

// NotifyPromptListChanged sends a prompt list changed notification.
func (s *Session) NotifyPromptListChanged() error {
	return s.notifier.SendNotification(protocol.MethodPromptListChanged, nil)
}

// sessionKey is the context key for the session.
type sessionKey struct{}

// ContextWithSession returns a context with the session attached.
func ContextWithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// SessionFromContext returns the session from context, or nil if none.
func SessionFromContext(ctx context.Context) *Session {
	session, _ := ctx.Value(sessionKey{}).(*Session)
	return session
}
