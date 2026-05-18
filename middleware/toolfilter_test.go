package middleware

import (
	"context"
	"testing"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// stubToolsListHandler returns a fixed tools/list result so the
// middleware can be tested without booting a real server.
func stubToolsListHandler(toolNames []string) HandlerFunc {
	return func(_ context.Context, _ *protocol.Request) (*protocol.Response, error) {
		tools := make([]map[string]any, 0, len(toolNames))
		for _, n := range toolNames {
			tools = append(tools, map[string]any{
				"name":        n,
				"description": "stub for " + n,
			})
		}
		return &protocol.Response{Result: map[string]any{"tools": tools}}, nil
	}
}

func toolNamesFrom(t *testing.T, resp *protocol.Response) []string {
	t.Helper()
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result not a map: %+v", resp.Result)
	}
	tools, ok := m["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools not []map[string]any: %+v", m["tools"])
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		n, _ := tool["name"].(string)
		names = append(names, n)
	}
	return names
}

func TestToolFilterDropsRejectedTools(t *testing.T) {
	allow := func(_ context.Context, name string) bool {
		return name == "schema_search" || name == "list_servers"
	}
	mw := ToolFilter(allow)
	h := mw(stubToolsListHandler([]string{
		"schema_search", "list_servers", "query_execute", "auth_login",
	}))
	resp, err := h(context.Background(), &protocol.Request{Method: protocol.MethodToolsList})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := toolNamesFrom(t, resp)
	want := map[string]bool{"schema_search": true, "list_servers": true}
	if len(got) != len(want) {
		t.Errorf("got %d tools, want %d (%v)", len(got), len(want), got)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected tool %q passed the filter", n)
		}
	}
}

func TestToolFilterPassesThroughForOtherMethods(t *testing.T) {
	allowNone := func(_ context.Context, _ string) bool { return false }
	mw := ToolFilter(allowNone)
	called := false
	inner := HandlerFunc(func(_ context.Context, _ *protocol.Request) (*protocol.Response, error) {
		called = true
		return &protocol.Response{Result: map[string]any{"hello": "world"}}, nil
	})
	h := mw(inner)
	resp, err := h(context.Background(), &protocol.Request{Method: protocol.MethodToolsCall})
	if err != nil || !called {
		t.Fatalf("non-list method should pass through unchanged; called=%v err=%v", called, err)
	}
	if resp.Result.(map[string]any)["hello"] != "world" {
		t.Errorf("response body mutated for non-list method")
	}
}

func TestToolFilterNilPredicateFailsOpen(t *testing.T) {
	mw := ToolFilter(nil)
	h := mw(stubToolsListHandler([]string{"a", "b", "c"}))
	resp, err := h(context.Background(), &protocol.Request{Method: protocol.MethodToolsList})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got := toolNamesFrom(t, resp); len(got) != 3 {
		t.Errorf("nil predicate should fail open (no filtering), got %v", got)
	}
}

func TestToolFilterTolerantToUnknownShapes(t *testing.T) {
	// Future mcp-go schema changes might rewrite the result shape.
	// The filter must NOT panic or silently empty the response in
	// that case — leave it untouched so the rest of the response
	// keeps working until the middleware is updated.
	allowNone := func(_ context.Context, _ string) bool { return false }
	mw := ToolFilter(allowNone)
	inner := HandlerFunc(func(_ context.Context, _ *protocol.Request) (*protocol.Response, error) {
		return &protocol.Response{Result: "unexpected string result"}, nil
	})
	h := mw(inner)
	resp, err := h(context.Background(), &protocol.Request{Method: protocol.MethodToolsList})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.Result != "unexpected string result" {
		t.Errorf("non-map result mutated: %+v", resp.Result)
	}
}
