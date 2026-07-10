package transport

import (
	"bytes"
	"context"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// TestStdioInjectsSession guards the Phase 0 fix: the stdio transport now
// attaches a per-connection session to the request context, so handlers that
// rely on SessionFromContext (logging, channels, resource-updated) are
// reachable. Previously SessionFromContext(ctx) was always nil over stdio.
func TestStdioInjectsSession(t *testing.T) {
	var got *server.Session
	h := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		got = server.SessionFromContext(ctx)
		return protocol.NewResponse(req.ID, map[string]any{}), nil
	})

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	out := &bytes.Buffer{}
	tr := NewStdio(WithStdin(in), WithStdout(out))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = tr.Serve(ctx, h)

	if got == nil {
		t.Fatal("expected stdio to inject a session into the request context, got nil")
	}
}
