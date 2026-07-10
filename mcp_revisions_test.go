package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// mustParams marshals v into json.RawMessage for a protocol.Request.
func mustParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return b
}

// initResult drives an initialize request through the handler and returns the
// decoded result map.
func initResult(t *testing.T, srv *Server, protocolVersion string) map[string]any {
	t.Helper()
	handler := newRequestHandler(srv)
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
		Params: mustParams(t, map[string]any{
			"protocolVersion": protocolVersion,
			"clientInfo":      map[string]any{"name": "c", "version": "1"},
		}),
	}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	res, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("initialize result type: %T", resp.Result)
	}
	return res
}

func TestNegotiateVersion(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{"supported echoes back", "2024-11-05", "2024-11-05"},
		{"empty falls back to default", "", protocol.MCPVersion},
		{"unknown falls back to default", "2030-01-01", protocol.MCPVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protocol.NegotiateVersion(tt.requested); got != tt.want {
				t.Errorf("NegotiateVersion(%q) = %q, want %q", tt.requested, got, tt.want)
			}
		})
	}
}

func TestInitialize_NegotiatesRequestedVersion(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})

	// A supported version is echoed verbatim (previously the server always
	// returned its own version regardless of the request).
	res := initResult(t, srv, "2024-11-05")
	if res["protocolVersion"] != "2024-11-05" {
		t.Errorf("supported: got protocolVersion %v, want 2024-11-05", res["protocolVersion"])
	}

	// An unsupported version negotiates down to the server's default.
	res = initResult(t, srv, "2999-01-01")
	if res["protocolVersion"] != protocol.MCPVersion {
		t.Errorf("unsupported: got protocolVersion %v, want %v", res["protocolVersion"], protocol.MCPVersion)
	}
}

func TestInitialize_AdvertisesCompletionsWhenRegistered(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	// Registering a prompt completion handler must auto-advertise the
	// completions capability, mirroring tools/resources/prompts.
	srv.PromptCompletion("greet").
		Handler(func(_ context.Context, _ CompletionRef, _ CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{Values: []string{"world"}}, nil
		})

	res := initResult(t, srv, "2024-11-05")
	caps, ok := res["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities type: %T", res["capabilities"])
	}
	if _, ok := caps["completions"]; !ok {
		t.Errorf("expected completions capability advertised, got %v", caps)
	}
}

// TestWiredMethods_NoLongerMethodNotFound guards the regression where these
// methods were implemented but never registered in the dispatcher, so every
// call returned -32601 MethodNotFound.
func TestWiredMethods_NoLongerMethodNotFound(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	cases := []struct {
		method string
		params any
	}{
		{protocol.MethodCompletionComplete, map[string]any{
			"ref":      map[string]any{"type": "ref/prompt", "name": "x"},
			"argument": map[string]any{"name": "a", "value": ""},
		}},
		{protocol.MethodLoggingSetLevel, map[string]any{"level": "info"}},
		{protocol.MethodResourcesTemplatesList, map[string]any{}},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			req := &protocol.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  tc.method,
				Params:  mustParams(t, tc.params),
			}
			resp, err := handler.HandleRequest(context.Background(), req)
			if err != nil {
				var mcpErr *protocol.Error
				if errors.As(err, &mcpErr) && mcpErr.Code == protocol.CodeMethodNotFound {
					t.Fatalf("%s returned MethodNotFound — not wired into dispatcher", tc.method)
				}
				t.Fatalf("%s: unexpected error: %v", tc.method, err)
			}
			if resp == nil || resp.Result == nil {
				t.Fatalf("%s: expected a result, got %+v", tc.method, resp)
			}
		})
	}
}

func TestLoggingSetLevel_RejectsUnknownLevel(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodLoggingSetLevel,
		Params:  mustParams(t, map[string]any{"level": "bogus"}),
	}
	_, err := handler.HandleRequest(context.Background(), req)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("expected InvalidParams for unknown level, got %v", err)
	}
}

func TestResourcesTemplatesList_ReturnsTemplates(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Resource("users://{id}").
		Name("user").
		Description("a user").
		Handler(func(_ context.Context, uri string, _ map[string]string) (*ResourceContent, error) {
			return &ResourceContent{URI: uri, Text: "x"}, nil
		})

	handler := newRequestHandler(srv)
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodResourcesTemplatesList,
		Params:  json.RawMessage(`{}`),
	}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("templates/list: %v", err)
	}
	res := resp.Result.(map[string]any)
	list, ok := res["resourceTemplates"].([]map[string]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected 1 template, got %#v", res["resourceTemplates"])
	}
	if list[0]["uriTemplate"] != "users://{id}" {
		t.Errorf("uriTemplate = %v, want users://{id}", list[0]["uriTemplate"])
	}
}

// TestToolsList_AdvertisesIcons verifies the integration wiring: icons set on a
// tool builder surface in the tools/list response (MCP 2025-11-25, SEP-973).
func TestToolsList_AdvertisesIcons(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	type in struct {
		X string `json:"x"`
	}
	srv.Tool("iconic").
		Description("has an icon").
		Icons(Icon{URI: "https://example.com/i.png", MimeType: "image/png", Size: 48}).
		Handler(func(in in) (string, error) { return in.X, nil })

	handler := newRequestHandler(srv)
	req := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsList, Params: json.RawMessage(`{}`)}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	tools := resp.Result.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if _, ok := tools[0]["icons"]; !ok {
		t.Errorf("expected icons advertised in tools/list, got %v", tools[0])
	}
}

// TestToolResult_CarriesUnionContent verifies audio/resource_link content blocks
// returned from a tool handler flow through tools/call unchanged — the payoff of
// the ContentBlock union (Phases 1–2 content types) needing no dispatcher change.
func TestToolResult_CarriesUnionContent(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	type in struct {
		X string `json:"x"`
	}
	srv.Tool("media").
		Description("returns audio + link").
		Handler(func(_ in) (StructuredResult, error) {
			return StructuredResult{Content: []ContentBlock{
				NewAudioContent("audio/wav", "aGk="),
				NewResourceLink("file://x", "x"),
			}}, nil
		})

	handler := newRequestHandler(srv)
	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: mustParams(t, map[string]any{"name": "media", "arguments": map[string]any{"x": "y"}}),
	}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	// Round-trip the response to JSON and assert the block types survived.
	raw, _ := json.Marshal(resp.Result)
	s := string(raw)
	if !strings.Contains(s, `"audio"`) || !strings.Contains(s, `"resource_link"`) {
		t.Errorf("expected audio + resource_link blocks in tool result, got %s", s)
	}
}

func TestSession_SetClientCapabilitiesJSON(t *testing.T) {
	sess := NewSession("id", nil, nil)
	sess.SetClientCapabilitiesJSON(json.RawMessage(`{
		"sampling": {},
		"elicitation": {},
		"roots": {"listChanged": true}
	}`))
	caps := sess.ClientCapabilities()
	if !caps.Sampling {
		t.Error("expected Sampling=true")
	}
	if !caps.Elicitation {
		t.Error("expected Elicitation=true")
	}
	if caps.Roots == nil || !caps.Roots.ListChanged {
		t.Errorf("expected Roots.ListChanged=true, got %+v", caps.Roots)
	}
}
