package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// listTestServer builds a server pre-seeded with two tools, two
// resources, and two prompts so the filter predicates have something
// non-trivial to act on.
func listTestServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer(ServerInfo{Name: "filter-test", Version: "1.0.0"})

	type Input struct{}
	srv.Tool("public_search").
		Description("Anyone can call").
		Handler(func(_ Input) (string, error) { return "ok", nil })
	srv.Tool("admin_drop_table").
		Description("Admin only").
		Handler(func(_ Input) (string, error) { return "ok", nil })

	srv.Resource("data://public").
		Name("Public").
		Handler(func(_ context.Context, uri string, _ map[string]string) (*ResourceContent, error) {
			return &ResourceContent{URI: uri, Text: "public"}, nil
		})
	srv.Resource("data://internal/secrets").
		Name("InternalSecrets").
		Handler(func(_ context.Context, uri string, _ map[string]string) (*ResourceContent, error) {
			return &ResourceContent{URI: uri, Text: "secret"}, nil
		})

	srv.Prompt("public_greeting").
		Description("Public").
		Handler(func(_ context.Context, _ map[string]string) (*PromptResult, error) {
			return &PromptResult{}, nil
		})
	srv.Prompt("admin_diag").
		Description("Admin only").
		Handler(func(_ context.Context, _ map[string]string) (*PromptResult, error) {
			return &PromptResult{}, nil
		})

	return srv
}

// listToolNames issues tools/list against the supplied handler and
// returns the names that came back so tests can compare without
// caring about the rest of the envelope.
func listToolNames(t *testing.T, srv *Server, opts ...ServeOption) []string {
	t.Helper()
	handler := newRequestHandler(srv, opts...)
	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodToolsList,
	})
	if err != nil {
		t.Fatalf("tools/list returned error: %v", err)
	}
	return extractNames(t, resp, "tools")
}

func listResourceNames(t *testing.T, srv *Server, opts ...ServeOption) []string {
	t.Helper()
	handler := newRequestHandler(srv, opts...)
	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodResourcesList,
	})
	if err != nil {
		t.Fatalf("resources/list returned error: %v", err)
	}
	return extractNames(t, resp, "resources")
}

func listPromptNames(t *testing.T, srv *Server, opts ...ServeOption) []string {
	t.Helper()
	handler := newRequestHandler(srv, opts...)
	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodPromptsList,
	})
	if err != nil {
		t.Fatalf("prompts/list returned error: %v", err)
	}
	return extractNames(t, resp, "prompts")
}

func extractNames(t *testing.T, resp *protocol.Response, key string) []string {
	t.Helper()
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	items, ok := result[key].([]map[string]any)
	if !ok {
		t.Fatalf("result[%q] is not []map[string]any: %T", key, result[key])
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name, ok := item["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}

// ── tools ────────────────────────────────────────────────────────────

func TestWithToolFilter_NoFilter_ListsAll(t *testing.T) {
	srv := listTestServer(t)
	names := listToolNames(t, srv)
	if len(names) != 2 {
		t.Fatalf("expected both tools without a filter, got %v", names)
	}
}

func TestWithToolFilter_HidesRejected(t *testing.T) {
	srv := listTestServer(t)
	names := listToolNames(t, srv, WithToolFilter(func(_ context.Context, name string) bool {
		return name == "public_search"
	}))
	if len(names) != 1 || names[0] != "public_search" {
		t.Fatalf("expected only public_search, got %v", names)
	}
	if contains(names, "admin_drop_table") {
		t.Fatalf("admin_drop_table should not be visible: %v", names)
	}
}

// callerKey is a caller-defined context key. mcp-go no longer ships an
// Identity type; callers attach whatever they need (resolved from their own
// http.Client auth transport, mTLS peer cert, etc.) to the request context and
// read it back in filter predicates.
type callerKey struct{}

func TestWithToolFilter_SeesContext(t *testing.T) {
	srv := listTestServer(t)
	handler := newRequestHandler(srv, WithToolFilter(func(ctx context.Context, name string) bool {
		// Predicate reads a caller-attached value from ctx — the pattern
		// callers use after resolving identity in their own transport.
		if caller, _ := ctx.Value(callerKey{}).(string); caller == "admin" {
			return true
		}
		return name == "public_search"
	}))

	// Anonymous caller — only the public tool comes back.
	respAnon, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList,
	})
	if err != nil {
		t.Fatalf("anon list error: %v", err)
	}
	anon := extractNames(t, respAnon, "tools")
	if len(anon) != 1 || anon[0] != "public_search" {
		t.Fatalf("anon caller should see only public_search, got %v", anon)
	}

	// Admin caller — both tools come back.
	adminCtx := context.WithValue(context.Background(), callerKey{}, "admin")
	respAdmin, err := handler.HandleRequest(adminCtx, &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodToolsList,
	})
	if err != nil {
		t.Fatalf("admin list error: %v", err)
	}
	admin := extractNames(t, respAdmin, "tools")
	if len(admin) != 2 {
		t.Fatalf("admin caller should see both tools, got %v", admin)
	}
}

// ── resources ───────────────────────────────────────────────────────

func TestWithResourceFilter_HidesRejected(t *testing.T) {
	srv := listTestServer(t)
	names := listResourceNames(t, srv, WithResourceFilter(func(_ context.Context, uri, _ string) bool {
		return uri == "data://public"
	}))
	if len(names) != 1 || names[0] != "Public" {
		t.Fatalf("expected only Public resource, got %v", names)
	}
}

// ── prompts ─────────────────────────────────────────────────────────

func TestWithPromptFilter_HidesRejected(t *testing.T) {
	srv := listTestServer(t)
	names := listPromptNames(t, srv, WithPromptFilter(func(_ context.Context, name string) bool {
		return name == "public_greeting"
	}))
	if len(names) != 1 || names[0] != "public_greeting" {
		t.Fatalf("expected only public_greeting, got %v", names)
	}
}

// ── tools/call still rejects filtered tools ─────────────────────────

// A hidden tool must NOT be callable either — the filter is the
// contract, not just a display layer. The author flagged "see-but-
// not-call" as the leaky state to avoid; here we verify a filter that
// hides a tool also blocks the call.
func TestWithToolFilter_AlsoBlocksCall(t *testing.T) {
	srv := listTestServer(t)
	handler := newRequestHandler(srv, WithToolFilter(func(_ context.Context, name string) bool {
		return name != "admin_drop_table"
	}))

	callReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodToolsCall,
		Params:  json.RawMessage(`{"name":"admin_drop_table","arguments":{}}`),
	}
	_, err := handler.HandleRequest(context.Background(), callReq)
	if err == nil {
		t.Fatalf("expected hidden tool to be uncallable")
	}
}
