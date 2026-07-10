package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// modernParams wraps method params with the required modern per-request _meta.
func modernParams(t *testing.T, version string, extra map[string]any) json.RawMessage {
	t.Helper()
	meta := map[string]any{
		protocol.MetaKeyClientInfo:         map[string]any{"name": "c", "version": "1"},
		protocol.MetaKeyClientCapabilities: map[string]any{},
	}
	if version != "" {
		meta[protocol.MetaKeyProtocolVersion] = version
	}
	p := map[string]any{"_meta": meta}
	maps.Copy(p, extra)
	return mustParams(t, p)
}

// TestModern_ResultTypeStamped verifies that a modern request (carrying the
// per-request _meta) gets resultType:"complete" stamped on its result.
func TestModern_ResultTypeStamped(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("t").Handler(func(_ struct{}) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList,
		Params: modernParams(t, protocol.DraftVersion, nil),
	}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("tools/list (modern): %v", err)
	}
	if resp.Result.(map[string]any)["resultType"] != protocol.ResultTypeComplete {
		t.Errorf("expected resultType complete, got %v", resp.Result)
	}
}

// TestModern_UnsupportedVersion verifies a modern request for a version the
// server does not serve gets -32022 (except server/discover).
func TestModern_UnsupportedVersion(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("t").Handler(func(_ struct{}) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList,
		Params: modernParams(t, "1900-01-01", nil),
	}
	_, err := handler.HandleRequest(context.Background(), req)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeUnsupportedProtocolVersion {
		t.Fatalf("expected -32022, got %v", err)
	}

	// server/discover is exempt from the version check.
	dreq := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodServerDiscover,
		Params: modernParams(t, "1900-01-01", nil),
	}
	if _, err := handler.HandleRequest(context.Background(), dreq); err != nil {
		t.Errorf("server/discover should be exempt from version check, got %v", err)
	}
}

// TestModern_MissingRequiredMeta verifies a modern request missing a required
// _meta field is rejected with -32602.
func TestModern_MissingRequiredMeta(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("t").Handler(func(_ struct{}) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	// protocolVersion present (marks it modern) but clientInfo/caps omitted.
	params := mustParams(t, map[string]any{
		"_meta": map[string]any{protocol.MetaKeyProtocolVersion: protocol.DraftVersion},
	})
	req := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList, Params: params}
	_, err := handler.HandleRequest(context.Background(), req)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("expected -32602 for missing required _meta, got %v", err)
	}
}

// TestLegacy_NoResultType confirms a legacy request (no modern _meta) is served
// unchanged — no resultType is stamped.
func TestLegacy_NoResultType(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("t").Handler(func(_ struct{}) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	req := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList, Params: json.RawMessage(`{}`)}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("tools/list (legacy): %v", err)
	}
	if _, present := resp.Result.(map[string]any)["resultType"]; present {
		t.Errorf("legacy result must not carry resultType, got %v", resp.Result)
	}
}
