package middleware

import (
	"context"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// ToolPredicate decides whether a tool should be visible to a caller.
// Returns true to keep the tool in the tools/list response, false to
// hide it.
//
// The predicate sees the request context — use IdentityFromContext
// (when paired with the Auth middleware) to make authz-aware
// decisions. The name argument is the tool's `name` field exactly
// as the server registered it.
type ToolPredicate func(ctx context.Context, name string) bool

// ToolFilter returns a Middleware that wraps the tools/list handler
// and removes entries the predicate rejects. Other methods pass
// through untouched.
//
// Typical use: hide destructive tools (query_execute, auth_login,
// etc.) from clients whose transport credential grants only
// read-only scope. The agent's planner picks from the advertised
// catalog, so filtering at list time avoids the "tool exists but
// call returns permission_denied" round trip.
//
// Resources/Prompts get sibling functions (ResourceFilter,
// PromptFilter — TODO) when consumers ask for them; ToolFilter
// covers the common case.
func ToolFilter(allow ToolPredicate) Middleware {
	if allow == nil {
		// Nil predicate is a programming mistake. Refuse-to-filter
		// — every tool passes through — so a forgotten wiring step
		// fails open rather than silently hiding everything.
		return func(next HandlerFunc) HandlerFunc { return next }
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			resp, err := next(ctx, req)
			if err != nil || resp == nil || resp.Result == nil {
				return resp, err
			}
			if req.Method != protocol.MethodToolsList {
				return resp, err
			}
			// Result shape from handleToolsList:
			// map[string]any{"tools": []map[string]any{...}}.
			// Defensive: if either type assertion fails we leave
			// the response untouched so a future schema tweak
			// doesn't silently re-leak the full catalog.
			m, ok := resp.Result.(map[string]any)
			if !ok {
				return resp, err
			}
			rawTools, ok := m["tools"].([]map[string]any)
			if !ok {
				return resp, err
			}
			filtered := make([]map[string]any, 0, len(rawTools))
			for _, t := range rawTools {
				name, _ := t["name"].(string)
				if allow(ctx, name) {
					filtered = append(filtered, t)
				}
			}
			m["tools"] = filtered
			return resp, err
		}
	}
}
