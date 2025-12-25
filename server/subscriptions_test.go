package server

import (
	"sort"
	"testing"
)

func TestSubscriptionManager(t *testing.T) {
	manager := NewSubscriptionManager()

	// Initial state
	if manager.SubscriptionCount() != 0 {
		t.Errorf("expected 0 subscriptions, got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerSubscribe(t *testing.T) {
	manager := NewSubscriptionManager()

	manager.Subscribe("client-1", "file:///config.json")

	if !manager.IsSubscribed("client-1", "file:///config.json") {
		t.Error("expected client-1 to be subscribed to file:///config.json")
	}
	if manager.SubscriptionCount() != 1 {
		t.Errorf("expected 1 subscription, got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerMultipleSubscribers(t *testing.T) {
	manager := NewSubscriptionManager()

	manager.Subscribe("client-1", "file:///config.json")
	manager.Subscribe("client-2", "file:///config.json")
	manager.Subscribe("client-3", "file:///data.json")

	subscribers := manager.Subscribers("file:///config.json")
	if len(subscribers) != 2 {
		t.Errorf("expected 2 subscribers for config.json, got %d", len(subscribers))
	}

	// Sort for consistent comparison
	sort.Strings(subscribers)
	if subscribers[0] != "client-1" || subscribers[1] != "client-2" {
		t.Errorf("unexpected subscribers: %v", subscribers)
	}

	if manager.SubscriptionCount() != 3 {
		t.Errorf("expected 3 total subscriptions, got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerUnsubscribe(t *testing.T) {
	manager := NewSubscriptionManager()

	manager.Subscribe("client-1", "file:///config.json")
	manager.Subscribe("client-2", "file:///config.json")

	manager.Unsubscribe("client-1", "file:///config.json")

	if manager.IsSubscribed("client-1", "file:///config.json") {
		t.Error("client-1 should not be subscribed after unsubscribe")
	}
	if !manager.IsSubscribed("client-2", "file:///config.json") {
		t.Error("client-2 should still be subscribed")
	}
	if manager.SubscriptionCount() != 1 {
		t.Errorf("expected 1 subscription, got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerUnsubscribeLastClient(t *testing.T) {
	manager := NewSubscriptionManager()

	manager.Subscribe("client-1", "file:///config.json")
	manager.Unsubscribe("client-1", "file:///config.json")

	if manager.HasSubscribers("file:///config.json") {
		t.Error("should have no subscribers after last client unsubscribes")
	}
	if manager.SubscriptionCount() != 0 {
		t.Errorf("expected 0 subscriptions, got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerUnsubscribeAll(t *testing.T) {
	manager := NewSubscriptionManager()

	manager.Subscribe("client-1", "file:///a.json")
	manager.Subscribe("client-1", "file:///b.json")
	manager.Subscribe("client-1", "file:///c.json")
	manager.Subscribe("client-2", "file:///a.json")

	manager.UnsubscribeAll("client-1")

	if manager.IsSubscribed("client-1", "file:///a.json") {
		t.Error("client-1 should not be subscribed to a.json")
	}
	if manager.IsSubscribed("client-1", "file:///b.json") {
		t.Error("client-1 should not be subscribed to b.json")
	}
	if !manager.IsSubscribed("client-2", "file:///a.json") {
		t.Error("client-2 should still be subscribed to a.json")
	}
	if manager.SubscriptionCount() != 1 {
		t.Errorf("expected 1 subscription, got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerHasSubscribers(t *testing.T) {
	manager := NewSubscriptionManager()

	if manager.HasSubscribers("file:///config.json") {
		t.Error("should have no subscribers initially")
	}

	manager.Subscribe("client-1", "file:///config.json")

	if !manager.HasSubscribers("file:///config.json") {
		t.Error("should have subscribers after subscribe")
	}
}

func TestSubscriptionManagerSubscribersEmpty(t *testing.T) {
	manager := NewSubscriptionManager()

	subscribers := manager.Subscribers("file:///nonexistent")
	if subscribers != nil {
		t.Errorf("expected nil subscribers for nonexistent URI, got %v", subscribers)
	}
}

func TestSubscriptionManagerDuplicateSubscription(t *testing.T) {
	manager := NewSubscriptionManager()

	manager.Subscribe("client-1", "file:///config.json")
	manager.Subscribe("client-1", "file:///config.json") // Duplicate

	// Should still only count as 1
	if manager.SubscriptionCount() != 1 {
		t.Errorf("expected 1 subscription (no duplicates), got %d", manager.SubscriptionCount())
	}
}

func TestSubscriptionManagerUnsubscribeNonexistent(t *testing.T) {
	manager := NewSubscriptionManager()

	// Should not panic
	manager.Unsubscribe("client-1", "file:///nonexistent")

	if manager.SubscriptionCount() != 0 {
		t.Errorf("expected 0 subscriptions, got %d", manager.SubscriptionCount())
	}
}

func TestSubscribeRequest(t *testing.T) {
	req := SubscribeRequest{
		URI: "file:///config.json",
	}

	if req.URI != "file:///config.json" {
		t.Errorf("expected URI 'file:///config.json', got %q", req.URI)
	}
}

func TestUnsubscribeRequest(t *testing.T) {
	req := UnsubscribeRequest{
		URI: "file:///config.json",
	}

	if req.URI != "file:///config.json" {
		t.Errorf("expected URI 'file:///config.json', got %q", req.URI)
	}
}

func TestResourceUpdatedNotification(t *testing.T) {
	notification := ResourceUpdatedNotification{
		URI: "file:///config.json",
	}

	if notification.URI != "file:///config.json" {
		t.Errorf("expected URI 'file:///config.json', got %q", notification.URI)
	}
}
