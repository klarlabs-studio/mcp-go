package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// This file is the per-revision MCP conformance harness. It drives a fully
// featured server through every method a given spec revision defines and
// asserts the response shape. As each phase of docs/revisions-roadmap.md is
// certified, its new methods/versions are added as cases with a higher
// minVersion — the harness is the gate that a revision is honored.

// conformanceCase is one method exercised against the reference server.
type conformanceCase struct {
	name string
	// minVersion is the earliest protocol revision this case applies to.
	minVersion string
	method     string
	params     any
	// validate asserts on the successful result map. A nil validate only
	// asserts that the call returned a non-error result.
	validate func(t *testing.T, result map[string]any)
}

// referenceServer builds a server exercising every 2024-11-05 primitive:
// a tool, a static resource, a templated resource, a prompt, and a
// prompt-completion handler.
func referenceServer() *Server {
	srv := NewServer(ServerInfo{
		Name:    "conformance",
		Version: "1.0.0",
		Capabilities: Capabilities{
			ResourceSubscribe: true,
			Logging:           true,
		},
	})

	type echoIn struct {
		Text string `json:"text"`
	}
	srv.Tool("echo").
		Description("echoes text").
		Handler(func(in echoIn) (string, error) { return in.Text, nil })

	srv.Resource("static://info").
		Name("info").
		Description("static info").
		Handler(func(_ context.Context, uri string, _ map[string]string) (*ResourceContent, error) {
			return &ResourceContent{URI: uri, Text: "info"}, nil
		})

	srv.Resource("users://{id}").
		Name("user").
		Description("a user").
		Handler(func(_ context.Context, uri string, params map[string]string) (*ResourceContent, error) {
			return &ResourceContent{URI: uri, Text: "user " + params["id"]}, nil
		})

	srv.Prompt("greet").
		Description("greeting prompt").
		Handler(func(_ context.Context, _ map[string]string) (*PromptResult, error) {
			return &PromptResult{Messages: []PromptMessage{{Role: "user", Content: NewTextContent("hi")}}}, nil
		})

	srv.PromptCompletion("greet").
		Handler(func(_ context.Context, _ CompletionRef, _ CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{Values: []string{"world"}}, nil
		})

	return srv
}

func conformanceCases() []conformanceCase {
	hasKey := func(k string) func(*testing.T, map[string]any) {
		return func(t *testing.T, result map[string]any) {
			t.Helper()
			if _, ok := result[k]; !ok {
				t.Errorf("expected key %q in result, got %v", k, result)
			}
		}
	}
	return []conformanceCase{
		{
			name: "initialize", minVersion: "2024-11-05", method: protocol.MethodInitialize,
			params: map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
			validate: func(t *testing.T, r map[string]any) {
				if r["protocolVersion"] != "2024-11-05" {
					t.Errorf("protocolVersion = %v", r["protocolVersion"])
				}
				if _, ok := r["capabilities"]; !ok {
					t.Error("missing capabilities")
				}
			},
		},
		{name: "ping", minVersion: "2024-11-05", method: protocol.MethodPing, params: map[string]any{}},
		{name: "tools/list", minVersion: "2024-11-05", method: protocol.MethodToolsList, params: map[string]any{}, validate: hasKey("tools")},
		{
			name: "tools/call", minVersion: "2024-11-05", method: protocol.MethodToolsCall,
			params:   map[string]any{"name": "echo", "arguments": map[string]any{"text": "hi"}},
			validate: hasKey("content"),
		},
		{name: "resources/list", minVersion: "2024-11-05", method: protocol.MethodResourcesList, params: map[string]any{}, validate: hasKey("resources")},
		{
			name: "resources/read", minVersion: "2024-11-05", method: protocol.MethodResourcesRead,
			params:   map[string]any{"uri": "static://info"},
			validate: hasKey("contents"),
		},
		{
			name: "resources/templates/list", minVersion: "2024-11-05", method: protocol.MethodResourcesTemplatesList,
			params: map[string]any{}, validate: hasKey("resourceTemplates"),
		},
		{name: "prompts/list", minVersion: "2024-11-05", method: protocol.MethodPromptsList, params: map[string]any{}, validate: hasKey("prompts")},
		{
			name: "prompts/get", minVersion: "2024-11-05", method: protocol.MethodPromptsGet,
			params:   map[string]any{"name": "greet"},
			validate: hasKey("messages"),
		},
		{
			name: "completion/complete", minVersion: "2024-11-05", method: protocol.MethodCompletionComplete,
			params: map[string]any{
				"ref":      map[string]any{"type": "ref/prompt", "name": "greet"},
				"argument": map[string]any{"name": "x", "value": ""},
			},
			validate: hasKey("completion"),
		},
		{
			name: "logging/setLevel", minVersion: "2024-11-05", method: protocol.MethodLoggingSetLevel,
			params: map[string]any{"level": "info"},
		},
	}
}

func TestConformance_2024_11_05(t *testing.T) {
	const rev = "2024-11-05"
	srv := referenceServer()
	handler := newRequestHandler(srv)

	for _, tc := range conformanceCases() {
		if tc.minVersion > rev {
			continue // not part of this revision yet
		}
		t.Run(tc.name, func(t *testing.T) {
			req := &protocol.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  tc.method,
				Params:  mustParams(t, tc.params),
			}
			resp, err := handler.HandleRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.method, err)
			}
			if resp == nil {
				t.Fatalf("%s: nil response", tc.method)
			}
			if resp.Error != nil {
				t.Fatalf("%s: error response: %+v", tc.method, resp.Error)
			}
			if tc.validate != nil {
				result, ok := resp.Result.(map[string]any)
				if !ok {
					t.Fatalf("%s: result not a map: %T", tc.method, resp.Result)
				}
				tc.validate(t, result)
			}
		})
	}
}
