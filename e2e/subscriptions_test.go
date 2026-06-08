package e2e

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/client"
)

// freePort returns a currently-unused localhost address.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// waitForHealth blocks until the server's /health endpoint answers.
func waitForHealth(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not become healthy")
}

func TestResourceSubscriptionPushE2E(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:         "push-test",
		Version:      "1.0.0",
		Capabilities: mcp.Capabilities{Resources: true, ResourceSubscribe: true},
	})
	srv.Resource("email://inbox").
		Description("inbox").
		Handler(func(_ context.Context, uri string, _ map[string]string) (*mcp.ResourceContent, error) {
			return &mcp.ResourceContent{URI: uri, MimeType: "application/json", Text: "{}"}, nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freePort(t)
	go func() { _ = mcp.ServeHTTP(ctx, srv, addr) }()
	waitForHealth(t, addr)

	tr, err := client.NewHTTPTransport("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	c := client.New(tr)
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	updates := make(chan string, 4)
	c.OnResourceUpdated(func(uri string) { updates <- uri })
	go func() { _ = c.StartNotifications(ctx) }()

	// Let the SSE stream connect and register on the server.
	time.Sleep(250 * time.Millisecond)

	if err := c.Subscribe(ctx, "email://inbox"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Let the subscription register before pushing.
	time.Sleep(100 * time.Millisecond)

	if err := srv.NotifyResourceUpdated("email://inbox"); err != nil {
		t.Fatalf("notify: %v", err)
	}

	select {
	case uri := <-updates:
		if uri != "email://inbox" {
			t.Errorf("updated uri = %q, want email://inbox", uri)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not receive the resource-updated push")
	}
}

func TestHTTPTransportIsStreaming(t *testing.T) {
	// The HTTP transport must expose the server-push channel so the client can
	// receive notifications; a transport without it returns
	// ErrNotificationsUnsupported from StartNotifications.
	tr, err := client.NewHTTPTransport("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(tr).(client.StreamingTransport); !ok {
		t.Fatal("HTTPTransport should implement client.StreamingTransport")
	}
}
