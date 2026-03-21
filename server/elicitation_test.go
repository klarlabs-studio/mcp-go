package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

func TestNewElicitor(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Elicitation: true}))

	elicitor := NewElicitor(session)
	if elicitor == nil {
		t.Fatal("expected non-nil elicitor")
	}
}

func TestElicitor_Elicit(t *testing.T) {
	sender := &mockRequestSender{
		responses: []*protocol.Response{
			{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Result: map[string]any{
					"action": "accept",
					"content": map[string]any{
						"field": "First Name",
					},
				},
			},
		},
	}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Elicitation: true}))

	elicitor := NewElicitor(session)

	req := &ElicitRequest{
		Message: "Which field?",
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"field": map[string]any{
					"type": "string",
					"enum": []string{"First Name", "Last Name"},
				},
			},
		},
	}

	result, err := elicitor.Elicit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action != "accept" {
		t.Errorf("expected action 'accept', got %q", result.Action)
	}
	if result.Content["field"] != "First Name" {
		t.Errorf("expected field 'First Name', got %v", result.Content["field"])
	}

	// Verify the request was sent with correct method
	if len(sender.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(sender.requests))
	}
	if sender.requests[0].Method != "elicitation/create" {
		t.Errorf("expected method 'elicitation/create', got %q", sender.requests[0].Method)
	}
}

func TestElicitor_ElicitNoCapability(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier) // No elicitation capability

	elicitor := NewElicitor(session)

	_, err := elicitor.Elicit(context.Background(), &ElicitRequest{Message: "test"})
	if err == nil {
		t.Error("expected error when elicitation not supported")
	}
	if err.Error() != "client does not support elicitation" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestElicitor_ElicitNilElicitor(t *testing.T) {
	var elicitor *Elicitor

	_, err := elicitor.Elicit(context.Background(), &ElicitRequest{Message: "test"})
	if err == nil {
		t.Error("expected error for nil elicitor")
	}
}

func TestElicitor_ElicitDecline(t *testing.T) {
	sender := &mockRequestSender{
		responses: []*protocol.Response{
			{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Result: map[string]any{
					"action": "decline",
				},
			},
		},
	}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Elicitation: true}))

	elicitor := NewElicitor(session)
	result, err := elicitor.Elicit(context.Background(), &ElicitRequest{Message: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "decline" {
		t.Errorf("expected action 'decline', got %q", result.Action)
	}
}

func TestContextWithElicitor(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}
	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Elicitation: true}))

	elicitor := NewElicitor(session)
	ctx := ContextWithElicitor(context.Background(), elicitor)

	retrieved := ElicitFromContext(ctx)
	if retrieved != elicitor {
		t.Error("expected to retrieve the same elicitor from context")
	}
}

func TestElicitFromContextNil(t *testing.T) {
	retrieved := ElicitFromContext(context.Background())
	if retrieved != nil {
		t.Error("expected nil elicitor from context without elicitor")
	}
}

func TestSessionSupportsElicitation(t *testing.T) {
	sender := &mockRequestSender{}
	notifier := &mockNotificationSender{}

	session := NewSession("s1", sender, notifier,
		WithClientCapabilities(ClientCapabilities{Elicitation: true}))

	if !session.SupportsFeature("elicitation") {
		t.Error("expected elicitation to be supported")
	}

	session2 := NewSession("s2", sender, notifier)
	if session2.SupportsFeature("elicitation") {
		t.Error("expected elicitation to not be supported")
	}
}
