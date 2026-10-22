package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/transport"
)

// The MCP schema (2025-11-25 and 2026-07-28) declares CallToolRequestParams
// and GetPromptRequestParams `arguments` optional. These tests drive a
// tools/call and prompts/get with the field absent, explicitly null, and as {}
// over every transport, so a regression in the shared dispatch shows up
// wherever a client reaches it.

const (
	optArgsToolEmpty    = "empty"
	optArgsToolOptional = "optional"
	optArgsToolRequired = "required"
	optArgsPromptOpt    = "greet"
	optArgsPromptReq    = "summarize"
)

// argsForm is how a request spells (or omits) its arguments field.
type argsForm struct {
	name  string
	apply func(params map[string]any)
}

var optArgsForms = []argsForm{
	{"absent", func(map[string]any) {}},
	{"null", func(p map[string]any) { p["arguments"] = nil }},
	{"empty object", func(p map[string]any) { p["arguments"] = map[string]any{} }},
}

func optionalArgsServer() *Server {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool(optArgsToolEmpty).Handler(func(_ struct{}) (string, error) { return "ok", nil })
	srv.Tool(optArgsToolOptional).Handler(func(in struct {
		Limit int    `json:"limit,omitempty"`
		Query string `json:"query,omitempty"`
	}) (string, error) {
		return fmt.Sprintf("limit=%d query=%q", in.Limit, in.Query), nil
	})
	srv.Tool(optArgsToolRequired).Handler(func(in struct {
		Query string `json:"query" jsonschema:"required"`
	}) (string, error) {
		return "ran with query=" + in.Query, nil
	})
	srv.Prompt(optArgsPromptOpt).
		Argument("name", "who to greet", false).
		Handler(func(_ context.Context, args map[string]string) (*PromptResult, error) {
			return &PromptResult{Messages: []PromptMessage{{Role: "user", Content: TextContent{Type: "text", Text: "hi " + args["name"]}}}}, nil
		})
	srv.Prompt(optArgsPromptReq).
		Argument("topic", "what to summarize", true).
		Handler(func(_ context.Context, args map[string]string) (*PromptResult, error) {
			return &PromptResult{Messages: []PromptMessage{{Role: "user", Content: TextContent{Type: "text", Text: args["topic"]}}}}, nil
		})
	return srv
}

// rpcCaller sends one request with the given method and params and returns the
// raw JSON-RPC response frame.
type rpcCaller func(t *testing.T, method string, params map[string]any) map[string]json.RawMessage

func TestOptionalArguments_AllTransports(t *testing.T) {
	transports := []struct {
		name  string
		start func(t *testing.T, srv *Server) rpcCaller
	}{
		{"dispatch legacy", startDispatchCaller(false)},
		{"dispatch modern", startDispatchCaller(true)},
		{"stdio", startStdioCaller},
		{"http streamable stateless", startHTTPCaller(true)},
		{"http legacy", startHTTPCaller(false)},
		{"websocket", startWebSocketCaller},
	}
	for _, tr := range transports {
		t.Run(tr.name, func(t *testing.T) {
			call := tr.start(t, optionalArgsServer())
			for _, form := range optArgsForms {
				t.Run(form.name, func(t *testing.T) {
					assertOptionalArgsBehaviour(t, call, form)
				})
			}
		})
	}
}

func assertOptionalArgsBehaviour(t *testing.T, call rpcCaller, form argsForm) {
	t.Helper()
	params := func(name string) map[string]any {
		p := map[string]any{"name": name}
		form.apply(p)
		return p
	}

	text, isErr := toolText(t, mustResult(t, call(t, protocol.MethodToolsCall, params(optArgsToolEmpty))))
	if isErr || text != "ok" {
		t.Errorf("struct{} tool: got (%q, isError=%v), want (\"ok\", false)", text, isErr)
	}

	text, isErr = toolText(t, mustResult(t, call(t, protocol.MethodToolsCall, params(optArgsToolOptional))))
	if isErr || text != `limit=0 query=""` {
		t.Errorf("all-optional tool: got (%q, isError=%v), want zero values", text, isErr)
	}

	// A required field must still be enforced: the handler must not run with
	// zero values, and the model gets an isError result naming the field.
	text, isErr = toolText(t, mustResult(t, call(t, protocol.MethodToolsCall, params(optArgsToolRequired))))
	if !isErr {
		t.Errorf("required tool without arguments ran: %q", text)
	}
	if !strings.Contains(text, "query") || !strings.Contains(text, "required field is missing") {
		t.Errorf("required tool error = %q, want it to name the missing field", text)
	}
	if strings.Contains(text, "invalid JSON") {
		t.Errorf("required tool error = %q, must not blame the JSON", text)
	}

	resp := mustResult(t, call(t, protocol.MethodPromptsGet, params(optArgsPromptOpt)))
	if got := promptText(t, resp); got != "hi " {
		t.Errorf("all-optional prompt: got %q, want %q", got, "hi ")
	}

	resp = call(t, protocol.MethodPromptsGet, params(optArgsPromptReq))
	if resp["error"] == nil {
		t.Fatalf("required prompt without arguments succeeded: %s", resp["result"])
	}
	if !strings.Contains(string(resp["error"]), "topic") {
		t.Errorf("required prompt error = %s, want it to name the missing argument", resp["error"])
	}
}

func mustResult(t *testing.T, m map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	if m["error"] != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", m["error"])
	}
	return m
}

func promptText(t *testing.T, m map[string]json.RawMessage) string {
	t.Helper()
	var res struct {
		Messages []struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(m["result"], &res); err != nil || len(res.Messages) == 0 {
		t.Fatalf("unexpected prompts/get response: %v (%v)", m, err)
	}
	return res.Messages[0].Content.Text
}

// withModernMeta adds the 2026-07-28 per-request _meta to a copy of params.
func withModernMeta(params map[string]any) map[string]any {
	out := map[string]any{"_meta": map[string]any{
		protocol.MetaKeyProtocolVersion:    protocol.ModernVersion,
		protocol.MetaKeyClientInfo:         map[string]any{"name": "c", "version": "1"},
		protocol.MetaKeyClientCapabilities: map[string]any{},
	}}
	for k, v := range params {
		out[k] = v
	}
	return out
}

var optArgsNextID atomic.Int64

func rpcFrame(method string, params map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": optArgsNextID.Add(1) + 100, "method": method, "params": params}
}

func decodeFrame(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid response frame %q: %v", data, err)
	}
	return m
}

func startDispatchCaller(modern bool) func(t *testing.T, srv *Server) rpcCaller {
	return func(_ *testing.T, srv *Server) rpcCaller {
		handler := newRequestHandler(srv)
		return func(t *testing.T, method string, params map[string]any) map[string]json.RawMessage {
			t.Helper()
			if modern {
				params = withModernMeta(params)
			}
			raw, err := json.Marshal(rpcFrame(method, params))
			if err != nil {
				t.Fatal(err)
			}
			var req protocol.Request
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Fatal(err)
			}
			resp, err := handler.HandleRequest(context.Background(), &req)
			if err != nil {
				var mcpErr *protocol.Error
				if !errors.As(err, &mcpErr) {
					t.Fatalf("handler returned a non-protocol error: %v", err)
				}
				resp = protocol.NewErrorResponse(req.ID, mcpErr)
			}
			out, err := json.Marshal(resp)
			if err != nil {
				t.Fatal(err)
			}
			return decodeFrame(t, out)
		}
	}
}

func startStdioCaller(t *testing.T, srv *Server) rpcCaller {
	p := startStdioPeer(t, srv)
	p.initialize(map[string]any{})
	return func(t *testing.T, method string, params map[string]any) map[string]json.RawMessage {
		t.Helper()
		p.t = t
		p.send(rpcFrame(method, params))
		return p.next()
	}
}

func startHTTPCaller(streamable bool) func(t *testing.T, srv *Server) rpcCaller {
	return func(t *testing.T, srv *Server) rpcCaller {
		opt := transport.WithLegacyHTTP()
		if streamable {
			opt = transport.WithStreamable()
		}
		tr := transport.NewHTTP("127.0.0.1:0", opt)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go func() { _ = tr.Serve(ctx, newRequestHandler(srv)) }()
		addr := waitForAddr(t, tr.ListenAddr)
		url := "http://" + addr + "/mcp"

		return func(t *testing.T, method string, params map[string]any) map[string]json.RawMessage {
			t.Helper()
			if streamable {
				params = withModernMeta(params)
			}
			body, err := json.Marshal(rpcFrame(method, params))
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			if streamable {
				req.Header.Set(protocol.HeaderProtocolVersion, protocol.ModernVersion)
				req.Header.Set("Mcp-Method", method)
				req.Header.Set(protocol.HeaderName, params["name"].(string))
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST %s: %v", method, err)
			}
			defer func() { _ = resp.Body.Close() }()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(resp.Body); err != nil {
				t.Fatal(err)
			}
			return decodeFrame(t, buf.Bytes())
		}
	}
}

func startWebSocketCaller(t *testing.T, srv *Server) rpcCaller {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	tr := transport.NewWebSocket(addr)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = tr.Serve(ctx, newRequestHandler(srv)) }()

	var conn *websocket.Conn
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, resp, err := websocket.DefaultDialer.Dial("ws://"+addr+"/", nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			conn = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial websocket: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return func(t *testing.T, method string, params map[string]any) map[string]json.RawMessage {
		t.Helper()
		if err := conn.WriteJSON(rpcFrame(method, params)); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return decodeFrame(t, data)
	}
}

func waitForAddr(t *testing.T, addr func() string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for addr() == "" {
		if time.Now().After(deadline) {
			t.Fatal("server did not start listening")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return addr()
}
