package server

import (
	"errors"
	"sync"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// captureNotifier records every NotifyClient call for assertions.
type captureNotifier struct {
	mu    sync.Mutex
	calls []struct {
		clientID, method, uri string
	}
	failClient string
}

func (c *captureNotifier) NotifyClient(clientID, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	uri := ""
	if n, ok := params.(ResourceUpdatedNotification); ok {
		uri = n.URI
	}
	c.calls = append(c.calls, struct{ clientID, method, uri string }{clientID, method, uri})
	if clientID == c.failClient {
		return errors.New("delivery failed")
	}
	return nil
}

func TestResourceSubscriptionsNotifyTargetsOnlySubscribers(t *testing.T) {
	r := newResourceSubscriptions()
	notifier := &captureNotifier{}
	r.setNotifier(notifier)

	r.subscribe("client-a", "email://inbox")
	r.subscribe("client-b", "email://inbox")
	r.subscribe("client-c", "email://outbox") // different URI, must not be notified

	if err := r.notifyUpdated("email://inbox"); err != nil {
		t.Fatalf("notifyUpdated: %v", err)
	}

	if len(notifier.calls) != 2 {
		t.Fatalf("got %d notifications, want 2 (only inbox subscribers)", len(notifier.calls))
	}
	for _, c := range notifier.calls {
		if c.method != protocol.MethodResourceUpdated {
			t.Errorf("method = %q, want %q", c.method, protocol.MethodResourceUpdated)
		}
		if c.uri != "email://inbox" {
			t.Errorf("uri = %q, want email://inbox", c.uri)
		}
		if c.clientID == "client-c" {
			t.Error("non-subscriber client-c was notified")
		}
	}
}

func TestResourceSubscriptionsUnsubscribe(t *testing.T) {
	r := newResourceSubscriptions()
	notifier := &captureNotifier{}
	r.setNotifier(notifier)

	r.subscribe("client-a", "email://inbox")
	r.unsubscribe("client-a", "email://inbox")

	if err := r.notifyUpdated("email://inbox"); err != nil {
		t.Fatalf("notifyUpdated: %v", err)
	}
	if len(notifier.calls) != 0 {
		t.Errorf("unsubscribed client still notified: %d calls", len(notifier.calls))
	}
}

func TestResourceSubscriptionsRemoveClient(t *testing.T) {
	r := newResourceSubscriptions()
	notifier := &captureNotifier{}
	r.setNotifier(notifier)

	r.subscribe("client-a", "email://inbox")
	r.subscribe("client-a", "email://outbox")
	r.subscribe("client-b", "email://inbox")

	r.removeClient("client-a") // connection closed

	if got := r.subscribers("email://inbox"); len(got) != 1 || got[0] != "client-b" {
		t.Errorf("inbox subscribers after removeClient = %v, want [client-b]", got)
	}
	if got := r.subscribers("email://outbox"); len(got) != 0 {
		t.Errorf("outbox subscribers after removeClient = %v, want empty", got)
	}
}

func TestResourceSubscriptionsNoNotifierIsNoOp(t *testing.T) {
	r := newResourceSubscriptions()
	r.subscribe("client-a", "email://inbox")
	if err := r.notifyUpdated("email://inbox"); err != nil {
		t.Errorf("notifyUpdated with no notifier should be a no-op, got %v", err)
	}
}

func TestResourceSubscriptionsJoinsDeliveryErrors(t *testing.T) {
	r := newResourceSubscriptions()
	notifier := &captureNotifier{failClient: "client-bad"}
	r.setNotifier(notifier)

	r.subscribe("client-good", "email://inbox")
	r.subscribe("client-bad", "email://inbox")

	err := r.notifyUpdated("email://inbox")
	if err == nil {
		t.Fatal("expected a delivery error from the failing client")
	}
	// The good client must still have been notified despite the bad one.
	if len(notifier.calls) != 2 {
		t.Errorf("got %d notifications, want 2 (both attempted)", len(notifier.calls))
	}
}
