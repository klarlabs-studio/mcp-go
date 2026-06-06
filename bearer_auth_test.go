package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	mcp "go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/middleware"
	"go.klarlabs.de/mcp/protocol"
)

// callWith invokes the middleware with the given request + bearer token.
func callWith(t *testing.T, mw middleware.Middleware, method, token string, captured **mcp.Identity) (*protocol.Response, error) {
	t.Helper()
	handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		if captured != nil {
			*captured = mcp.IdentityFromContext(ctx)
		}
		return protocol.NewResponse(req.ID, "ok"), nil
	})
	ctx := context.Background()
	if token != "" {
		ctx = protocol.ContextWithRequestMeta(ctx, protocol.RequestMeta{
			"Authorization": "Bearer " + token,
		})
	}
	return handler(ctx, &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  method,
	})
}

func TestBearerAuth_AcceptsKnownToken(t *testing.T) {
	mw := mcp.BearerAuth(map[string]string{
		"secret-1": "scry-client",
	})
	var got *mcp.Identity
	resp, err := callWith(t, mw, "tools/call", "secret-1", &got)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if got == nil {
		t.Fatal("expected identity in context")
	}
	if got.ID != "scry-client" || got.Name != "scry-client" {
		t.Errorf("identity = %+v, want ID/Name = scry-client", got)
	}
}

func TestBearerAuth_RejectsUnknownToken(t *testing.T) {
	mw := mcp.BearerAuth(map[string]string{
		"secret-1": "scry-client",
	})
	_, err := callWith(t, mw, "tools/call", "wrong-token", nil)
	if err == nil {
		t.Fatal("expected auth error")
	}
}

func TestBearerAuth_RejectsMissingToken(t *testing.T) {
	mw := mcp.BearerAuth(map[string]string{
		"secret-1": "scry-client",
	})
	_, err := callWith(t, mw, "tools/call", "", nil)
	if err == nil {
		t.Fatal("expected auth error when no token presented")
	}
}

func TestBearerAuth_SkipsHandshakeMethods(t *testing.T) {
	// Handshake methods must pass unauthenticated — the client can't
	// present a token until it knows the server's capabilities.
	mw := mcp.BearerAuth(map[string]string{
		"secret-1": "scry-client",
	})
	for _, method := range []string{
		protocol.MethodInitialize,
		protocol.MethodInitialized,
		protocol.MethodPing,
	} {
		t.Run(method, func(t *testing.T) {
			resp, err := callWith(t, mw, method, "", nil)
			if err != nil {
				t.Errorf("handshake method %q must be allowed without auth, got err: %v", method, err)
			}
			if resp == nil {
				t.Errorf("handshake method %q must produce a response", method)
			}
		})
	}
}

func TestBearerAuth_MultipleTokens(t *testing.T) {
	mw := mcp.BearerAuth(map[string]string{
		"alice-token": "alice",
		"bob-token":   "bob",
	})
	for token, want := range map[string]string{
		"alice-token": "alice",
		"bob-token":   "bob",
	} {
		var got *mcp.Identity
		resp, err := callWith(t, mw, "tools/call", token, &got)
		if err != nil {
			t.Fatalf("token %q: %v", token, err)
		}
		if resp == nil {
			t.Fatalf("token %q: no response", token)
		}
		if got == nil || got.ID != want {
			t.Errorf("token %q surfaced identity %+v, want ID=%q", token, got, want)
		}
	}
}

func TestBearerAuth_AcceptsExtraAuthOptions(t *testing.T) {
	// User passes their own WithAuthSkipMethods — should compose with
	// the built-in handshake exemption.
	mw := mcp.BearerAuth(
		map[string]string{"secret": "client"},
		mcp.WithAuthSkipMethods("custom/method"),
	)
	// Custom method bypasses auth.
	if _, err := callWith(t, mw, "custom/method", "", nil); err != nil {
		t.Errorf("custom skip method should bypass auth, got: %v", err)
	}
	// Handshake still bypasses (the built-in skip stays).
	if _, err := callWith(t, mw, protocol.MethodInitialize, "", nil); err != nil {
		t.Errorf("handshake should still bypass, got: %v", err)
	}
	// Regular method still requires auth.
	if _, err := callWith(t, mw, "tools/call", "", nil); err == nil {
		t.Error("regular method should require auth")
	}
}
