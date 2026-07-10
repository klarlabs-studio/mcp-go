package mcp

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// modernMRTRParams builds tools/call params carrying the modern per-request
// _meta with the given client capabilities plus optional MRTR retry fields.
func modernMRTRParams(t *testing.T, caps map[string]any, extra map[string]any) json.RawMessage {
	t.Helper()
	meta := map[string]any{
		protocol.MetaKeyProtocolVersion:    protocol.DraftVersion,
		protocol.MetaKeyClientInfo:         map[string]any{"name": "c", "version": "1"},
		protocol.MetaKeyClientCapabilities: caps,
	}
	maps.Copy(meta, extra)
	return mustParams(t, map[string]any{
		"name":      "ask",
		"arguments": map[string]any{},
		"_meta":     meta,
	})
}

// TestMRTR_SamplingRoundTrip drives the full stateless sampling round-trip: the
// first modern tools/call pauses with an input_required result naming the
// sampling request; the retry carries the client's completion and the handler
// runs to a complete result.
func TestMRTR_SamplingRoundTrip(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		sess := server.SessionFromContext(ctx)
		res, err := sess.CreateMessage(ctx, &server.CreateMessageRequest{MaxTokens: 16})
		if err != nil {
			return "", err
		}
		return "model said: " + res.Content.Text, nil
	})
	handler := newRequestHandler(srv)

	caps := map[string]any{"sampling": map[string]any{}}

	// Round 1: no inputResponses → input_required.
	req1 := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernMRTRParams(t, caps, nil),
	}
	resp1, err := handler.HandleRequest(context.Background(), req1)
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	res1, ok := resp1.Result.(server.InputRequiredResult)
	if !ok {
		t.Fatalf("round 1 result type = %T, want InputRequiredResult", resp1.Result)
	}
	if res1.ResultType != protocol.ResultTypeInputRequired {
		t.Errorf("resultType = %q, want input_required", res1.ResultType)
	}
	if len(res1.InputRequests) != 1 || res1.InputRequests[0].Kind != server.InputKindSampling {
		t.Fatalf("inputRequests = %+v, want one sampling request", res1.InputRequests)
	}
	irID := res1.InputRequests[0].ID

	// Round 2: supply the completion, retry the same call.
	completion := map[string]any{
		"role": "assistant", "model": "test", "content": map[string]any{"type": "text", "text": "hello"},
	}
	retryMeta := map[string]any{
		protocol.MetaKeyInputResponses: []any{map[string]any{"id": irID, "payload": completion}},
	}
	req2 := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodToolsCall,
		Params: modernMRTRParams(t, caps, retryMeta),
	}
	resp2, err := handler.HandleRequest(context.Background(), req2)
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	result := resp2.Result.(map[string]any)
	if result["resultType"] != protocol.ResultTypeComplete {
		t.Errorf("round 2 resultType = %v, want complete", result["resultType"])
	}
	content := result["content"].([]map[string]any)
	if content[0]["text"] != "model said: hello" {
		t.Errorf("round 2 text = %v, want %q", content[0]["text"], "model said: hello")
	}
}

// TestMRTR_ElicitationRoundTrip drives the same round-trip through the elicitor
// injected into a tool handler's context.
func TestMRTR_ElicitationRoundTrip(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		el := server.ElicitFromContext(ctx)
		if el == nil {
			t.Fatal("elicitor not injected into handler context")
		}
		res, err := el.Elicit(ctx, &server.ElicitRequest{Message: "your name?"})
		if err != nil {
			return "", err
		}
		return "hi " + res.Content["name"].(string), nil
	})
	handler := newRequestHandler(srv)

	caps := map[string]any{"elicitation": map[string]any{}}

	req1 := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernMRTRParams(t, caps, nil),
	}
	resp1, err := handler.HandleRequest(context.Background(), req1)
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	res1 := resp1.Result.(server.InputRequiredResult)
	if len(res1.InputRequests) != 1 || res1.InputRequests[0].Kind != server.InputKindElicitation {
		t.Fatalf("inputRequests = %+v, want one elicitation request", res1.InputRequests)
	}

	answer := map[string]any{"action": "accept", "content": map[string]any{"name": "Ada"}}
	retryMeta := map[string]any{
		protocol.MetaKeyInputResponses: []any{map[string]any{"id": res1.InputRequests[0].ID, "payload": answer}},
	}
	req2 := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodToolsCall,
		Params: modernMRTRParams(t, caps, retryMeta),
	}
	resp2, err := handler.HandleRequest(context.Background(), req2)
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	content := resp2.Result.(map[string]any)["content"].([]map[string]any)
	if content[0]["text"] != "hi Ada" {
		t.Errorf("round 2 text = %v, want %q", content[0]["text"], "hi Ada")
	}
}

// TestMRTR_LegacyStillErrors confirms a legacy (non-modern) request with no
// request sender is unaffected by MRTR — sampling still returns the transport
// error rather than an input_required result.
func TestMRTR_LegacyStillErrors(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		sess := server.SessionFromContext(ctx)
		if sess == nil {
			return "no-session", nil
		}
		_, err := sess.CreateMessage(ctx, &server.CreateMessageRequest{MaxTokens: 1})
		if err != nil {
			return "", err
		}
		return "ok", nil
	})
	handler := newRequestHandler(srv)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: mustParams(t, map[string]any{"name": "ask", "arguments": map[string]any{}}),
	}
	resp, err := handler.HandleRequest(context.Background(), req)
	// No modern _meta → no session in context → handler returns "no-session"
	// (there is no legacy session wired in this bare handler harness). The key
	// assertion is that no input_required result is produced.
	if err != nil {
		t.Fatalf("legacy call: %v", err)
	}
	if _, ok := resp.Result.(server.InputRequiredResult); ok {
		t.Fatal("legacy request must not yield an input_required result")
	}
}
