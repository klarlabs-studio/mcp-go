package e2e

import (
	"context"
	"testing"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/client"
)

// The legacy handshake over HTTP, with nothing but defaults on either side.
//
// client.New defaults to the stateless 2026-07-28 revision and stamps it on the
// HTTP transport's MCP-Protocol-Version header. Initialize negotiates at the
// initialize era (MCPVersion, 2025-11-25) and sends that in the body. If the
// header is not brought along, ServeHTTP rejects the request with -32020
// ("header does not match body version") and the default client cannot talk
// to the default server. That shipped in 1.28.0 and broke every caller using
// New + Initialize over HTTP — nox's `attack mcp --http` and roady's SDK among
// them.
func TestInitializeOverHTTPWithDefaults(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "init-test", Version: "1.0.0"})
	srv.Tool("echo").
		Description("echo").
		Handler(func(_ context.Context, _ struct{}) (string, error) { return "ok", nil })

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

	info, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize with a default client against a default server: %v", err)
	}
	if info.Name != "init-test" {
		t.Errorf("server name = %q", info.Name)
	}
	// The call after the handshake carries the negotiated version too.
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools after Initialize: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("tools = %d, want 1", len(tools))
	}
}
