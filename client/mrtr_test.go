package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/client"
	"go.klarlabs.de/mcp/protocol"
)

func inputRequiredResponse(result map[string]any) protocol.Response {
	return protocol.Response{JSONRPC: "2.0", ID: json.RawMessage(`1`), Result: result}
}

func TestCallTool_InputRequiredSurfacesAsError(t *testing.T) {
	tr := &mockTransport{responses: []protocol.Response{inputRequiredResponse(map[string]any{
		"resultType": "input_required",
		"inputRequests": map[string]any{
			"github_login": map[string]any{
				"method": "elicitation/create",
				"params": map[string]any{"message": "name?", "requestedSchema": map[string]any{"type": "object"}},
			},
		},
		"requestState": "opaque-1",
	})}}
	c := client.New(tr)

	_, err := c.CallTool(context.Background(), "ask", nil)
	if !errors.Is(err, client.ErrInputRequired) {
		t.Fatalf("err = %v, want client.ErrInputRequired", err)
	}
	var ir *client.InputRequiredError
	if !errors.As(err, &ir) {
		t.Fatalf("err = %T, want *client.InputRequiredError", err)
	}
	req, ok := ir.InputRequests["github_login"]
	if !ok || req.Method != protocol.MethodElicitationCreate {
		t.Fatalf("inputRequests = %+v, want github_login elicitation/create", ir.InputRequests)
	}
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Message != "name?" {
		t.Fatalf("params = %s (%v)", req.Params, err)
	}
	if ir.RequestState != "opaque-1" {
		t.Errorf("requestState = %q, want opaque-1", ir.RequestState)
	}
	if got := ir.Error(); got != "server requires input (MRTR): github_login=elicitation/create" {
		t.Errorf("Error() = %q", got)
	}
}

func TestCallTool_InputRequiredWithoutRequestsOrStateIsInvalid(t *testing.T) {
	tr := &mockTransport{responses: []protocol.Response{inputRequiredResponse(map[string]any{
		"resultType": "input_required",
	})}}
	_, err := client.New(tr).CallTool(context.Background(), "ask", nil)
	if !errors.Is(err, client.ErrInvalidResult) || errors.Is(err, client.ErrInputRequired) {
		t.Fatalf("err = %v, want client.ErrInvalidResult", err)
	}
}

func TestCallTool_InputRequiredStateOnly(t *testing.T) {
	tr := &mockTransport{responses: []protocol.Response{inputRequiredResponse(map[string]any{
		"resultType":   "input_required",
		"requestState": "s",
	})}}
	_, err := client.New(tr).CallTool(context.Background(), "ask", nil)
	var ir *client.InputRequiredError
	if !errors.As(err, &ir) || ir.RequestState != "s" || len(ir.InputRequests) != 0 {
		t.Fatalf("err = %v, want a state-only *client.InputRequiredError", err)
	}
	if got := ir.Error(); got != "server requires input (MRTR): retry with requestState" {
		t.Errorf("Error() = %q", got)
	}
}

// TestRetry_PutsInputResponsesInParams: the retry carries inputResponses and
// requestState as top-level params (InputResponseRequestParams), not in _meta.
func TestRetry_PutsInputResponsesInParams(t *testing.T) {
	tr := &mockTransport{responses: []protocol.Response{
		{JSONRPC: "2.0", ID: json.RawMessage(`1`), Result: map[string]any{
			"resultType": "complete", "content": []any{map[string]any{"type": "text", "text": "ok"}},
		}},
	}}
	c := client.New(tr, client.WithCapabilities(map[string]any{"elicitation": map[string]any{}}))
	ir := &client.InputRequiredError{RequestState: "opaque-1"}
	ctx := ir.Retry(context.Background(), map[string]any{
		"github_login": map[string]any{"action": "accept", "content": map[string]any{"name": "octocat"}},
	})
	res, err := c.CallTool(ctx, "ask", map[string]any{"x": 1})
	if err != nil || len(res.Content) != 1 || res.Content[0].Text != "ok" {
		t.Fatalf("CallTool = %+v, %v", res, err)
	}

	var sent struct {
		Name           string                     `json:"name"`
		Arguments      map[string]any             `json:"arguments"`
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
		RequestState   any                        `json:"requestState"`
		Meta           map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(tr.requests[0].Params, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Name != "ask" || sent.Arguments["x"] != float64(1) {
		t.Errorf("name/arguments lost: %s", tr.requests[0].Params)
	}
	if string(sent.InputResponses["github_login"]) != `{"action":"accept","content":{"name":"octocat"}}` {
		t.Errorf("inputResponses = %s", tr.requests[0].Params)
	}
	if sent.RequestState != "opaque-1" {
		t.Errorf("requestState = %#v, want the string opaque-1", sent.RequestState)
	}
	if _, inMeta := sent.Meta[protocol.MetaKeyInputResponses]; inMeta {
		t.Error("inputResponses must not travel in _meta")
	}
	if string(sent.Meta[protocol.MetaKeyClientCapabilities]) != `{"elicitation":{}}` {
		t.Errorf("clientCapabilities = %s, want the declared capabilities", sent.Meta[protocol.MetaKeyClientCapabilities])
	}
}

func TestRetry_NoStateOmitsField(t *testing.T) {
	tr := &mockTransport{responses: []protocol.Response{
		{JSONRPC: "2.0", ID: json.RawMessage(`1`), Result: map[string]any{"resultType": "complete", "messages": []any{}}},
	}}
	ctx := client.WithInputResponses(context.Background(), map[string]any{"a": map[string]any{"roots": []any{}}}, "")
	if _, err := client.New(tr).GetPrompt(ctx, "p", nil); err != nil {
		t.Fatal(err)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(tr.requests[0].Params, &sent); err != nil {
		t.Fatal(err)
	}
	if _, ok := sent["requestState"]; ok {
		t.Error("requestState must be omitted when the server sent none")
	}
	if _, ok := sent["inputResponses"]; !ok {
		t.Error("prompts/get retry must carry inputResponses")
	}
}

func TestRetry_IgnoredByOtherMethods(t *testing.T) {
	tr := &mockTransport{responses: []protocol.Response{
		{JSONRPC: "2.0", ID: json.RawMessage(`1`), Result: map[string]any{"resultType": "complete", "tools": []any{}}},
	}}
	ctx := client.WithInputResponses(context.Background(), map[string]any{"a": map[string]any{}}, "s")
	if _, err := client.New(tr).ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(tr.requests[0].Params, &sent); err != nil {
		t.Fatal(err)
	}
	if _, ok := sent["inputResponses"]; ok {
		t.Error("tools/list must not carry inputResponses")
	}
}
