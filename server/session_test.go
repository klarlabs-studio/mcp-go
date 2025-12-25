package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// mockRequestSender is a mock implementation of RequestSender for testing.
type mockRequestSender struct {
	mu        sync.Mutex
	requests  []*protocol.Request
	responses []*protocol.Response
	errors    []error
	index     int
}

func (m *mockRequestSender) SendRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, req)

	if m.index < len(m.errors) && m.errors[m.index] != nil {
		err := m.errors[m.index]
		m.index++
		return nil, err
	}

	if m.index < len(m.responses) {
		resp := m.responses[m.index]
		m.index++
		return resp, nil
	}

	return nil, errors.New("no response configured")
}

// mockNotificationSender is a mock implementation of NotificationSender for testing.
type mockNotificationSender struct {
	mu            sync.Mutex
	notifications []struct {
		method string
		params any
	}
}

func (m *mockNotificationSender) SendNotification(method string, params any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = append(m.notifications, struct {
		method string
		params any
	}{method, params})
	return nil
}

func TestNewSession(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)

	if session.ID() != "session-1" {
		t.Errorf("expected ID 'session-1', got %q", session.ID())
	}
	if session.LogLevel() != LogLevelInfo {
		t.Errorf("expected default log level 'info', got %q", session.LogLevel())
	}
}

func TestSessionWithClientCapabilities(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	caps := ClientCapabilities{
		Sampling: true,
		Roots:    &RootsCapability{ListChanged: true},
	}

	session := NewSession("session-1", sender, notifier, WithClientCapabilities(caps))

	if !session.SupportsFeature("sampling") {
		t.Error("expected sampling to be supported")
	}
	if !session.SupportsFeature("roots") {
		t.Error("expected roots to be supported")
	}
	if !session.SupportsFeature("roots.listChanged") {
		t.Error("expected roots.listChanged to be supported")
	}
	if session.SupportsFeature("unknown") {
		t.Error("expected unknown feature to not be supported")
	}
}

func TestSessionSetClientCapabilities(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)

	if session.SupportsFeature("sampling") {
		t.Error("sampling should not be supported initially")
	}

	session.SetClientCapabilities(ClientCapabilities{Sampling: true})

	if !session.SupportsFeature("sampling") {
		t.Error("sampling should be supported after setting capabilities")
	}
}

func TestSessionCreateMessage(t *testing.T) {
	sender := &mockRequestSender{
		responses: []*protocol.Response{
			{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Result: map[string]any{
					"role": "assistant",
					"content": map[string]any{
						"type": "text",
						"text": "4",
					},
					"model":      "claude-3",
					"stopReason": "endTurn",
				},
			},
		},
	}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Sampling: true}))

	req := &CreateMessageRequest{
		Messages: []SamplingMessage{
			{Role: RoleUser, Content: NewTextContent("What is 2+2?")},
		},
		MaxTokens: 100,
	}

	result, err := session.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Role != RoleAssistant {
		t.Errorf("expected role 'assistant', got %q", result.Role)
	}
	if result.Content.Text != "4" {
		t.Errorf("expected text '4', got %q", result.Content.Text)
	}
	if result.Model != "claude-3" {
		t.Errorf("expected model 'claude-3', got %q", result.Model)
	}
}

func TestSessionCreateMessageNoCapability(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier) // No sampling capability

	req := &CreateMessageRequest{
		Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("test")}},
		MaxTokens: 100,
	}

	_, err := session.CreateMessage(context.Background(), req)
	if err == nil {
		t.Error("expected error when sampling not supported")
	}
	if err.Error() != "client does not support sampling" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSessionListRoots(t *testing.T) {
	sender := &mockRequestSender{
		responses: []*protocol.Response{
			{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Result: map[string]any{
					"roots": []any{
						map[string]any{"uri": "file:///project", "name": "Project"},
					},
				},
			},
		},
	}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Roots: &RootsCapability{}}))

	result, err := session.ListRoots(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(result.Roots))
	}
	if result.Roots[0].URI != "file:///project" {
		t.Errorf("expected URI 'file:///project', got %q", result.Roots[0].URI)
	}

	// Check cached roots
	cachedRoots := session.Roots()
	if len(cachedRoots) != 1 {
		t.Errorf("expected 1 cached root, got %d", len(cachedRoots))
	}
}

func TestSessionListRootsNoCapability(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier) // No roots capability

	_, err := session.ListRoots(context.Background())
	if err == nil {
		t.Error("expected error when roots not supported")
	}
}

func TestSessionHandleRootsChanged(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	var callbackRoots []Root
	session := NewSession("session-1", sender, notifier,
		WithRootsChangeCallback(func(roots []Root) {
			callbackRoots = roots
		}))

	newRoots := []Root{
		{URI: "file:///new-project", Name: "New Project"},
	}

	session.HandleRootsChanged(newRoots)

	// Check cached roots
	cachedRoots := session.Roots()
	if len(cachedRoots) != 1 {
		t.Errorf("expected 1 cached root, got %d", len(cachedRoots))
	}

	// Check callback was called
	if len(callbackRoots) != 1 {
		t.Errorf("expected callback with 1 root, got %d", len(callbackRoots))
	}
}

func TestSessionLogging(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)
	session.SetLogLevel(LogLevelDebug)

	session.Debug("app", "debug message")
	session.Info("app", "info message")
	session.Warning("app", "warning message")
	session.Error("app", "error message")

	if len(notifier.notifications) != 4 {
		t.Errorf("expected 4 notifications, got %d", len(notifier.notifications))
	}

	for _, n := range notifier.notifications {
		if n.method != protocol.MethodLoggingMessage {
			t.Errorf("expected method %q, got %q", protocol.MethodLoggingMessage, n.method)
		}
	}
}

func TestSessionLoggingFiltering(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)
	session.SetLogLevel(LogLevelWarning) // Only warning and above

	session.Debug("app", "debug message")  // Should be filtered
	session.Info("app", "info message")    // Should be filtered
	session.Warning("app", "warning message")
	session.Error("app", "error message")

	if len(notifier.notifications) != 2 {
		t.Errorf("expected 2 notifications (warning and error), got %d", len(notifier.notifications))
	}
}

func TestSessionCancel(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)

	requestID := json.RawMessage(`123`)
	err := session.Cancel(requestID, "user cancelled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifier.notifications) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notifier.notifications))
	}
	if notifier.notifications[0].method != protocol.MethodCancelled {
		t.Errorf("expected method %q, got %q", protocol.MethodCancelled, notifier.notifications[0].method)
	}
}

func TestSessionSubscriptions(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)

	session.Subscribe("file:///config.json")

	manager := session.SubscriptionManager()
	if !manager.IsSubscribed("session-1", "file:///config.json") {
		t.Error("expected session to be subscribed")
	}

	session.Unsubscribe("file:///config.json")

	if manager.IsSubscribed("session-1", "file:///config.json") {
		t.Error("expected session to be unsubscribed")
	}
}

func TestSessionNotifyResourceUpdated(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)

	err := session.NotifyResourceUpdated("file:///config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifier.notifications) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notifier.notifications))
	}
	if notifier.notifications[0].method != protocol.MethodResourceUpdated {
		t.Errorf("expected method %q, got %q", protocol.MethodResourceUpdated, notifier.notifications[0].method)
	}
}

func TestSessionNotifyListChanged(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)

	session.NotifyResourceListChanged()
	session.NotifyToolListChanged()
	session.NotifyPromptListChanged()

	if len(notifier.notifications) != 3 {
		t.Errorf("expected 3 notifications, got %d", len(notifier.notifications))
	}

	methods := []string{
		protocol.MethodResourceListChanged,
		protocol.MethodToolListChanged,
		protocol.MethodPromptListChanged,
	}

	for i, m := range methods {
		if notifier.notifications[i].method != m {
			t.Errorf("notification %d: expected method %q, got %q", i, m, notifier.notifications[i].method)
		}
	}
}

func TestContextWithSession(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)
	ctx := ContextWithSession(context.Background(), session)

	retrieved := SessionFromContext(ctx)
	if retrieved != session {
		t.Error("expected to retrieve the same session from context")
	}
}

func TestSessionFromContextNil(t *testing.T) {
	ctx := context.Background()

	retrieved := SessionFromContext(ctx)
	if retrieved != nil {
		t.Error("expected nil session from context without session")
	}
}

func TestSessionAllLogLevels(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("session-1", sender, notifier)
	session.SetLogLevel(LogLevelDebug)

	session.Debug("app", "debug")
	session.Info("app", "info")
	session.Notice("app", "notice")
	session.Warning("app", "warning")
	session.Error("app", "error")
	session.Critical("app", "critical")
	session.Alert("app", "alert")
	session.Emergency("app", "emergency")

	if len(notifier.notifications) != 8 {
		t.Errorf("expected 8 notifications, got %d", len(notifier.notifications))
	}
}
