package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
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

// TestModern_ParseTraceContext verifies the W3C Trace Context keys are lifted
// out of a modern request's _meta into modernMeta.
func TestModern_ParseTraceContext(t *testing.T) {
	const (
		traceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
		tracestate  = "vendor=abc"
		baggage     = "userId=alice"
	)
	params := mustParams(t, map[string]any{
		"_meta": map[string]any{
			protocol.MetaKeyProtocolVersion:    protocol.DraftVersion,
			protocol.MetaKeyClientInfo:         map[string]any{"name": "c", "version": "1"},
			protocol.MetaKeyClientCapabilities: map[string]any{},
			protocol.MetaKeyTraceparent:        traceparent,
			protocol.MetaKeyTracestate:         tracestate,
			protocol.MetaKeyBaggage:            baggage,
		},
	})

	m, modern, err := parseModernMeta(params)
	if err != nil || !modern {
		t.Fatalf("parseModernMeta: modern=%v err=%v", modern, err)
	}
	if m.traceparent != traceparent || m.tracestate != tracestate || m.baggage != baggage {
		t.Fatalf("trace fields mismatch: %+v", m)
	}
}

// TestModern_ApplyTraceContext verifies applyModern joins the caller's trace:
// with a TraceContext propagator installed, the returned context carries the
// incoming remote span context so handler spans parent onto it.
func TestModern_ApplyTraceContext(t *testing.T) {
	const incomingTraceID = "0af7651916cd43dd8448eb211c80319c"
	traceparent := "00-" + incomingTraceID + "-b7ad6b7169203331-01"

	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTextMapPropagator(prev)

	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	h := newRequestHandler(srv)
	m := &modernMeta{
		protocolVersion: protocol.DraftVersion,
		clientInfo:      json.RawMessage(`{"name":"c","version":"1"}`),
		clientCaps:      json.RawMessage(`{}`),
		traceparent:     traceparent,
	}

	ctx, err := h.applyModern(context.Background(), protocol.MethodToolsList, m)
	if err != nil {
		t.Fatalf("applyModern: %v", err)
	}
	if server.SessionFromContext(ctx) == nil {
		t.Fatal("expected request-scoped session on context")
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsRemote() {
		t.Fatalf("expected valid remote span context, got %+v", sc)
	}
	if sc.TraceID().String() != incomingTraceID {
		t.Errorf("expected trace id %q, got %q", incomingTraceID, sc.TraceID().String())
	}
}

// TestModern_ApplyNoTraceContext verifies applyModern leaves the context free of
// a remote span context when no trace context is supplied.
func TestModern_ApplyNoTraceContext(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTextMapPropagator(prev)

	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	h := newRequestHandler(srv)
	m := &modernMeta{
		protocolVersion: protocol.DraftVersion,
		clientInfo:      json.RawMessage(`{"name":"c","version":"1"}`),
		clientCaps:      json.RawMessage(`{}`),
	}

	ctx, err := h.applyModern(context.Background(), protocol.MethodToolsList, m)
	if err != nil {
		t.Fatalf("applyModern: %v", err)
	}
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Error("expected no span context when trace context absent")
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

// TestModern_CacheHintStamped verifies WithResultCache stamps ttlMs/cacheScope
// on a cacheable modern result, and only for modern requests.
func TestModern_CacheHintStamped(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"}, WithResultCache(60000, "public"))
	srv.Tool("t").Handler(func(_ struct{}) (string, error) { return "ok", nil })
	handler := newRequestHandler(srv)

	// Modern tools/list → cache hint present.
	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList,
		Params: modernParams(t, protocol.DraftVersion, nil),
	}
	resp, _ := handler.HandleRequest(context.Background(), req)
	res := resp.Result.(map[string]any)
	if res["ttlMs"] != int64(60000) || res["cacheScope"] != "public" {
		t.Errorf("expected cache hint on modern result, got %v", res)
	}

	// Legacy tools/list → no cache hint.
	lreq := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodToolsList, Params: json.RawMessage(`{}`)}
	lresp, _ := handler.HandleRequest(context.Background(), lreq)
	if _, present := lresp.Result.(map[string]any)["ttlMs"]; present {
		t.Errorf("legacy result must not carry a cache hint")
	}
}

// TestModern_ResourceNotFoundRenumbered verifies a resource-not-found error is
// -32602 on the modern path (vs -32001 on legacy).
func TestModern_ResourceNotFoundRenumbered(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	modReq := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodResourcesRead,
		Params: modernParams(t, protocol.DraftVersion, map[string]any{"uri": "missing://x"}),
	}
	_, err := handler.HandleRequest(context.Background(), modReq)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("modern resource-not-found: expected -32602, got %v", err)
	}

	legReq := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodResourcesRead,
		Params: mustParams(t, map[string]any{"uri": "missing://x"}),
	}
	_, err = handler.HandleRequest(context.Background(), legReq)
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeNotFound {
		t.Fatalf("legacy resource-not-found: expected -32001, got %v", err)
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
