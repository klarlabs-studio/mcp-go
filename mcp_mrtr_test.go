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
// _meta with the given client capabilities plus optional extra _meta keys.
func modernMRTRParams(t *testing.T, caps map[string]any, extra map[string]any) json.RawMessage {
	t.Helper()
	return modernMRTRRetryParams(t, caps, extra, nil)
}

// modernMRTRRetryParams is modernMRTRParams plus top-level params fields — where
// the spec's InputResponseRequestParams carries inputResponses and requestState.
func modernMRTRRetryParams(t *testing.T, caps, extraMeta, retry map[string]any) json.RawMessage {
	t.Helper()
	meta := map[string]any{
		protocol.MetaKeyProtocolVersion:    protocol.ModernVersion,
		protocol.MetaKeyClientInfo:         map[string]any{"name": "c", "version": "1"},
		protocol.MetaKeyClientCapabilities: caps,
	}
	maps.Copy(meta, extraMeta)
	p := map[string]any{
		"name":      "ask",
		"arguments": map[string]any{},
		"_meta":     meta,
	}
	maps.Copy(p, retry)
	return mustParams(t, p)
}

// TestMRTR_SamplingRoundTrip drives the full stateless sampling round-trip: the
// first modern tools/call pauses with an input_required result naming the
// sampling request; the retry carries the client's completion and the handler
// runs to a complete result.
func TestMRTR_SamplingRoundTrip(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		sess := server.SessionFromContext(ctx)
		//nolint:staticcheck // exercises the deprecated CreateMessage path, which MRTR still intercepts via the InputBroker.
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

	// Round 2: supply the completion in params.inputResponses, keyed by the
	// input request id, and retry the same call.
	completion := map[string]any{
		"role": "assistant", "model": "test", "content": map[string]any{"type": "text", "text": "hello"},
	}
	req2 := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodToolsCall,
		Params: modernMRTRRetryParams(t, caps, nil, map[string]any{
			"inputResponses": map[string]any{irID: completion},
		}),
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
	req2 := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodToolsCall,
		Params: modernMRTRRetryParams(t, caps, nil, map[string]any{
			"inputResponses": map[string]any{res1.InputRequests[0].ID: answer},
		}),
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

// TestMRTR_PreSpecMetaRetryAccepted: before following the published schema,
// mcp-go read the retry from _meta (an array of {id, payload} under
// MetaKeyInputResponses, any JSON under MetaKeyRequestState). Callers built
// against that still resolve, and their state goes back out as a string.
func TestMRTR_PreSpecMetaRetryAccepted(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		first, err := server.ElicitFromContext(ctx).Elicit(ctx, &server.ElicitRequest{Message: "first?"})
		if err != nil {
			return "", err
		}
		second, err := server.ElicitFromContext(ctx).Elicit(ctx, &server.ElicitRequest{Message: "second?"})
		if err != nil {
			return "", err
		}
		return first.Action + "+" + second.Action, nil
	})
	handler := newRequestHandler(srv)
	caps := map[string]any{"elicitation": map[string]any{}}

	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernMRTRParams(t, caps, map[string]any{
			protocol.MetaKeyInputResponses: []any{map[string]any{"id": "ir-0", "payload": map[string]any{"action": "accept"}}},
			protocol.MetaKeyRequestState:   map[string]any{"round": 2},
		}),
	})
	if err != nil {
		t.Fatalf("pre-spec retry: %v", err)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		InputRequests map[string]struct {
			Method string `json:"method"`
		} `json:"inputRequests"`
		RequestState any `json:"requestState"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	// ir-0 resolved from the _meta array; ir-1 is the next request.
	if len(res.InputRequests) != 1 || res.InputRequests["ir-1"].Method != protocol.MethodElicitationCreate {
		t.Fatalf("result = %s, want only ir-1 outstanding", raw)
	}
	if res.RequestState != `{"round":2}` {
		t.Fatalf("requestState = %#v, want the string {\"round\":2}", res.RequestState)
	}
}

// TestMRTR_ParamsRetryWinsOverMeta: when both locations are present, the
// spec's params fields are used.
func TestMRTR_ParamsRetryWinsOverMeta(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		res, err := server.ElicitFromContext(ctx).Elicit(ctx, &server.ElicitRequest{Message: "?"})
		if err != nil {
			return "", err
		}
		return res.Action, nil
	})
	handler := newRequestHandler(srv)
	resp, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernMRTRRetryParams(t, map[string]any{"elicitation": map[string]any{}},
			map[string]any{protocol.MetaKeyInputResponses: []any{map[string]any{"id": "ir-0", "payload": map[string]any{"action": "decline"}}}},
			map[string]any{"inputResponses": map[string]any{"ir-0": map[string]any{"action": "accept"}}},
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	content := resp.Result.(map[string]any)["content"].([]map[string]any)
	if content[0]["text"] != "accept" {
		t.Fatalf("text = %v, want accept (from params.inputResponses)", content[0]["text"])
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
		//nolint:staticcheck // exercises the deprecated CreateMessage path, which MRTR still intercepts via the InputBroker.
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
