package server

import (
	"context"
	"errors"
	"testing"
)

// TestSession_RequestMethods_NoSender verifies that server→client request
// methods return ErrNoRequestSender (rather than panicking on a nil sender)
// when the session has only a notifier. This is what makes it safe for a
// transport to inject a notifier-only session — one-way features keep working
// while sampling/elicitation/roots degrade gracefully.
func TestSession_RequestMethods_NoSender(t *testing.T) {
	sess := NewSession("t", nil, nil, WithClientCapabilities(ClientCapabilities{
		Sampling:    true,
		Elicitation: true,
		Roots:       &RootsCapability{},
	}))

	if _, err := sess.CreateMessage(context.Background(), &CreateMessageRequest{}); !errors.Is(err, ErrNoRequestSender) {
		t.Errorf("CreateMessage: got %v, want ErrNoRequestSender", err)
	}
	if _, err := sess.ListRoots(context.Background()); !errors.Is(err, ErrNoRequestSender) {
		t.Errorf("ListRoots: got %v, want ErrNoRequestSender", err)
	}
	if _, err := NewElicitor(sess).Elicit(context.Background(), &ElicitRequest{}); !errors.Is(err, ErrNoRequestSender) {
		t.Errorf("Elicit: got %v, want ErrNoRequestSender", err)
	}
}

// TestSession_Channels_WorkWithNotifierOnly confirms the counterpart: a
// notifier-only session CAN push channel messages, because channels are a
// one-way notification and never touch the request sender.
func TestSession_Channels_WorkWithNotifierOnly(t *testing.T) {
	var sent []string
	notifier := notifierFunc(func(method string, _ any) error {
		sent = append(sent, method)
		return nil
	})
	sess := NewSession("t", nil, notifier, WithClientCapabilities(ClientCapabilities{Channels: true}))

	if err := NewChannelSender(sess).SendText("status", "hi"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected 1 notification sent, got %d", len(sent))
	}
}

// notifierFunc adapts a func to the NotificationSender interface.
type notifierFunc func(method string, params any) error

func (f notifierFunc) SendNotification(method string, params any) error {
	return f(method, params)
}
