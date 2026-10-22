package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/client"
	"go.klarlabs.de/mcp/protocol"
)

// TestMRTRRoundTripOverHTTP drives an elicitation through the stateless MRTR
// flow with the library's own client and server: the first tools/call returns
// input_required with a spec-shaped inputRequests map, the client answers by
// key, and the retry completes.
func TestMRTRRoundTripOverHTTP(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "mrtr", Version: "1.0.0"})
	srv.Tool("greet").
		Description("asks for a name").
		Handler(func(ctx context.Context, _ struct{}) (string, error) {
			res, err := mcp.ElicitFromContext(ctx).Elicit(ctx, &mcp.ElicitRequest{
				Message: "Your name?",
				RequestedSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				},
			})
			if err != nil {
				return "", err
			}
			return "hello " + res.Content["name"].(string), nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freePort(t)
	go func() { _ = mcp.ServeHTTP(ctx, srv, addr) }()
	waitForHealth(t, addr)

	tr, err := client.NewHTTPTransport("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	c := client.New(tr, client.WithCapabilities(map[string]any{"elicitation": map[string]any{}}))
	if _, err := c.Discover(ctx); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	_, err = c.CallTool(ctx, "greet", map[string]any{})
	var ir *client.InputRequiredError
	if !errors.As(err, &ir) {
		t.Fatalf("first call: err = %v, want *client.InputRequiredError", err)
	}
	if len(ir.InputRequests) != 1 {
		t.Fatalf("inputRequests = %+v, want one", ir.InputRequests)
	}
	responses := map[string]any{}
	for id, req := range ir.InputRequests {
		if req.Method != protocol.MethodElicitationCreate {
			t.Fatalf("inputRequests[%q].method = %q, want elicitation/create", id, req.Method)
		}
		var params struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Message != "Your name?" {
			t.Fatalf("params = %s (%v)", req.Params, err)
		}
		responses[id] = map[string]any{"action": "accept", "content": map[string]any{"name": "Ada"}}
	}

	res, err := c.CallTool(ir.Retry(ctx, responses), "greet", map[string]any{})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "hello Ada" {
		t.Fatalf("retry result = %+v, want hello Ada", res.Content)
	}
}
