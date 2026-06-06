package server

import (
	"context"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestNewChannelSender(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Channels: true}))

	cs := NewChannelSender(session)
	if cs == nil {
		t.Fatal("expected non-nil channel sender")
	}
}

func TestChannelSender_Send(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Channels: true}))

	cs := NewChannelSender(session)

	msg := &ChannelMessage{
		Channel:  "dom-events",
		Content:  NewTextContent("New modal appeared"),
		Priority: "normal",
	}

	err := cs.Send(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifier.notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.notifications))
	}
	if notifier.notifications[0].method != protocol.MethodChannelMessage {
		t.Errorf("expected method %q, got %q", protocol.MethodChannelMessage, notifier.notifications[0].method)
	}
}

func TestChannelSender_SendText(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Channels: true}))

	cs := NewChannelSender(session)

	err := cs.SendText("navigation", "URL changed to /dashboard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifier.notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.notifications))
	}

	msg, ok := notifier.notifications[0].params.(*ChannelMessage)
	if !ok {
		t.Fatalf("expected *ChannelMessage, got %T", notifier.notifications[0].params)
	}
	if msg.Channel != "navigation" {
		t.Errorf("expected channel 'navigation', got %q", msg.Channel)
	}
	if msg.Content.Text != "URL changed to /dashboard" {
		t.Errorf("expected text 'URL changed to /dashboard', got %q", msg.Content.Text)
	}
}

func TestChannelSender_SendNoCapability(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier) // No channels capability

	cs := NewChannelSender(session)

	err := cs.Send(&ChannelMessage{Channel: "test", Content: NewTextContent("hello")})
	if err == nil {
		t.Error("expected error when channels not supported")
	}
	if err.Error() != "client does not support channels" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChannelSender_SendNilSender(t *testing.T) {
	var cs *ChannelSender

	err := cs.Send(&ChannelMessage{Channel: "test", Content: NewTextContent("hello")})
	if err == nil {
		t.Error("expected error for nil channel sender")
	}
}

func TestContextWithChannel(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Channels: true}))

	cs := NewChannelSender(session)
	ctx := ContextWithChannel(context.Background(), cs)

	retrieved := ChannelFromContext(ctx)
	if retrieved != cs {
		t.Error("expected to retrieve the same channel sender from context")
	}
}

func TestChannelFromContextNil(t *testing.T) {
	retrieved := ChannelFromContext(context.Background())
	if retrieved != nil {
		t.Error("expected nil channel sender from context without channel")
	}
}

func TestSessionSupportsChannels(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Channels: true}))

	if !session.SupportsFeature("channels") {
		t.Error("expected channels to be supported")
	}

	session2 := NewSession("s2", sender, notifier)
	if session2.SupportsFeature("channels") {
		t.Error("expected channels to not be supported")
	}
}

func TestChannelSender_SendWithData(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Channels: true}))

	cs := NewChannelSender(session)

	msg := &ChannelMessage{
		Channel:  "network",
		Content:  NewTextContent("API response received"),
		Data:     map[string]any{"status": 200, "url": "/api/users"},
		Priority: "high",
	}

	err := cs.Send(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sent := notifier.notifications[0].params.(*ChannelMessage)
	if sent.Data["status"] != 200 {
		t.Errorf("expected status 200, got %v", sent.Data["status"])
	}
	if sent.Priority != "high" {
		t.Errorf("expected priority 'high', got %q", sent.Priority)
	}
}
