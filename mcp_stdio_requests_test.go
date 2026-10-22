package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
	"go.klarlabs.de/mcp/transport"
)

// stdioPeer is a hand-rolled MCP client over in-memory pipes, driving the real
// request handler through transport.Stdio. (The module's stdio client cannot
// answer server→client requests, so the client side is scripted here.)
type stdioPeer struct {
	t    *testing.T
	w    *transport.NewlineFramer
	msgs chan map[string]json.RawMessage
}

func startStdioPeer(t *testing.T, srv *Server) *stdioPeer {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	tr := transport.NewStdio(transport.WithStdin(inR), transport.WithStdout(outW), transport.WithStderr(io.Discard))
	p := &stdioPeer{t: t, w: transport.NewNewlineFramer(nil, inW), msgs: make(chan map[string]json.RawMessage, 64)}

	go func() {
		r := transport.NewNewlineFramer(outR, nil)
		for {
			line, err := r.ReadMessage()
			if err != nil {
				close(p.msgs)
				return
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(line, &m); err != nil {
				t.Errorf("invalid server frame %q: %v", line, err)
				continue
			}
			p.msgs <- m
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = tr.Serve(ctx, newRequestHandler(srv)) }()
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = outW.Close()
		_ = outR.Close()
	})
	return p
}

func (p *stdioPeer) send(v any) {
	p.t.Helper()
	if err := p.w.WriteMessage(v); err != nil {
		p.t.Fatalf("write: %v", err)
	}
}

func (p *stdioPeer) next() map[string]json.RawMessage {
	p.t.Helper()
	select {
	case m, ok := <-p.msgs:
		if !ok {
			p.t.Fatal("server output closed")
		}
		return m
	case <-time.After(5 * time.Second):
		p.t.Fatal("timed out waiting for the server")
	}
	return nil
}

// initialize runs the initialize-era handshake declaring caps.
func (p *stdioPeer) initialize(caps map[string]any) {
	p.t.Helper()
	type initParams struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ClientInfo      map[string]any `json:"clientInfo"`
	}
	p.send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": initParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    caps,
		ClientInfo:      map[string]any{"name": "peer", "version": "1"},
	}})
	if m := p.next(); string(m["id"]) != "1" || m["error"] != nil {
		p.t.Fatalf("initialize failed: %v", m)
	}
	p.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func askServer() *Server {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		el := server.ElicitFromContext(ctx)
		if el == nil {
			return "", fmt.Errorf("no elicitor")
		}
		res, err := el.Elicit(ctx, &server.ElicitRequest{Message: "your name?"})
		if err != nil {
			return "", err
		}
		return "hi " + res.Content["name"].(string), nil
	})
	return srv
}

func toolText(t *testing.T, m map[string]json.RawMessage) (string, bool) {
	t.Helper()
	var res struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(m["result"], &res); err != nil || len(res.Content) == 0 {
		t.Fatalf("unexpected tools/call response: %v (%v)", m, err)
	}
	return res.Content[0].Text, res.IsError
}

// TestServeStdio_ElicitationRoundTrip: over stdio, a tool's elicitation reaches
// an initialize-era client that declared the capability, and its answer
// completes the tool call.
func TestServeStdio_ElicitationRoundTrip(t *testing.T) {
	p := startStdioPeer(t, askServer())
	p.initialize(map[string]any{"elicitation": map[string]any{}})
	p.send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "ask", "arguments": map[string]any{}}})

	req := p.next()
	var method string
	_ = json.Unmarshal(req["method"], &method)
	if method != protocol.MethodElicitationCreate || req["id"] == nil {
		t.Fatalf("want an elicitation/create request, got %v", req)
	}
	p.send(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{
		"action": "accept", "content": map[string]any{"name": "Ada"},
	}})

	resp := p.next()
	if string(resp["id"]) != "2" {
		t.Fatalf("want the tools/call response, got %v", resp)
	}
	if text, isErr := toolText(t, resp); isErr || text != "hi Ada" {
		t.Fatalf("tool result = %q (isError=%v), want %q", text, isErr, "hi Ada")
	}
}

// TestServeStdio_ElicitationWithoutCapability: a client that did not declare
// elicitation gets no server request; the handler sees no elicitor, and the
// session's direct call still reports the capability error.
func TestServeStdio_ElicitationWithoutCapability(t *testing.T) {
	// Report outcomes as tool text: plain handler errors are redacted to
	// "internal error" on the wire.
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		if server.ElicitFromContext(ctx) != nil {
			return "elicitor injected", nil
		}
		_, err := server.NewElicitor(server.SessionFromContext(ctx)).Elicit(ctx, &server.ElicitRequest{Message: "?"})
		return fmt.Sprint(err), nil
	})
	srv.Tool("sample").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		//nolint:staticcheck // exercises the deprecated sampling path's capability gate.
		_, err := server.SessionFromContext(ctx).CreateMessage(ctx, &server.CreateMessageRequest{MaxTokens: 1})
		return fmt.Sprint(err), nil
	})
	p := startStdioPeer(t, srv)
	p.initialize(map[string]any{})

	for i, tc := range []struct{ tool, want string }{
		{"ask", "client does not support elicitation"},
		{"sample", "client does not support sampling"},
	} {
		id := i + 2
		p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": tc.tool, "arguments": map[string]any{}}})
		resp := p.next() // the reply, not a server→client request
		if string(resp["id"]) != fmt.Sprint(id) {
			t.Fatalf("%s: want the tools/call response, got %v", tc.tool, resp)
		}
		if text, _ := toolText(t, resp); text != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.tool, text, tc.want)
		}
	}
}

// TestServeStdio_ModernRequestStaysStateless: a 2026-07-28 request over stdio
// uses the MRTR path — an input_required result, never a server→client
// request on the wire — even though the stdio session can now send requests.
func TestServeStdio_ModernRequestStaysStateless(t *testing.T) {
	p := startStdioPeer(t, askServer())
	p.send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": modernMRTRParams(t, map[string]any{"elicitation": map[string]any{}}, nil)})

	resp := p.next()
	if string(resp["id"]) != "1" || resp["method"] != nil {
		t.Fatalf("want the tools/call response first, got %v", resp)
	}
	var res struct {
		ResultType    string `json:"resultType"`
		InputRequests map[string]struct {
			Method string `json:"method"`
		} `json:"inputRequests"`
	}
	if err := json.Unmarshal(resp["result"], &res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.ResultType != protocol.ResultTypeInputRequired || len(res.InputRequests) != 1 {
		t.Fatalf("result = %s, want input_required with one input request", resp["result"])
	}
	for _, r := range res.InputRequests {
		if r.Method != protocol.MethodElicitationCreate {
			t.Fatalf("input request method = %q, want %q", r.Method, protocol.MethodElicitationCreate)
		}
	}
}
