package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// streamableTestHandler is a minimal MCP handler exercising the streamable
// transport: initialize returns a result, "notify" emits a server->client
// notification before replying, and any other method echoes its params back.
func streamableTestHandler() Handler {
	return HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		switch req.Method {
		case protocol.MethodInitialize:
			return protocol.NewResponse(req.ID, map[string]any{"protocolVersion": "2025-03-26"}), nil
		case "notify":
			if sender := NotificationSenderFromContext(ctx); sender != nil {
				_ = sender.SendNotification("notifications/message", map[string]any{"text": "hello"})
			}
			return protocol.NewResponse(req.ID, map[string]any{"ok": true}), nil
		default:
			return protocol.NewResponse(req.ID, map[string]any{"echo": req.Method}), nil
		}
	})
}

func newStreamableServer(t *testing.T, opts ...HTTPOption) (*HTTP, *httptest.Server) {
	t.Helper()
	h := NewHTTP("127.0.0.1:0", append([]HTTPOption{WithStreamable()}, opts...)...)
	ts := httptest.NewServer(h.createHandler(streamableTestHandler()))
	t.Cleanup(ts.Close)
	return h, ts
}

func postMCP(t *testing.T, ts *httptest.Server, accept, sessionID string, req protocol.Request) *http.Response {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if accept != "" {
		httpReq.Header.Set("Accept", accept)
	}
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := ts.Client().Do(httpReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func initSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
	})
	defer func() { _ = resp.Body.Close() }()
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("initialize did not mint an Mcp-Session-Id")
	}
	return sid
}

func TestStreamableHTTP_POSTReturnsJSON(t *testing.T) {
	_, ts := newStreamableServer(t)

	resp := postMCP(t, ts, "application/json, text/event-stream", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if resp.Header.Get("Mcp-Session-Id") == "" {
		t.Fatal("initialize response missing Mcp-Session-Id")
	}

	var out protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", out.Error)
	}
}

func TestStreamableHTTP_POSTReturnsSSE(t *testing.T) {
	_, ts := newStreamableServer(t)
	sid := initSession(t, ts)

	// Accept only text/event-stream forces the SSE-framed reply path.
	resp := postMCP(t, ts, "text/event-stream", sid, protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  "notify",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// The stream carries the in-handler notification followed by the final
	// JSON-RPC response, both as SSE data frames.
	frames := sseDataFrames(string(body))
	if len(frames) < 2 {
		t.Fatalf("expected >=2 SSE data frames (notification + response), got %d: %q", len(frames), body)
	}

	var sawNotification, sawResponse bool
	for _, f := range frames {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal([]byte(f), &msg); err != nil {
			t.Fatalf("frame not JSON: %q", f)
		}
		if _, hasID := msg["id"]; hasID {
			sawResponse = true
		} else if m, ok := msg["method"]; ok && strings.Contains(string(m), "notifications/message") {
			sawNotification = true
		}
	}
	if !sawNotification {
		t.Errorf("did not observe streamed notification in SSE frames: %q", body)
	}
	if !sawResponse {
		t.Errorf("did not observe final JSON-RPC response in SSE frames: %q", body)
	}
}

func TestStreamableHTTP_SessionMintingAndEcho(t *testing.T) {
	_, ts := newStreamableServer(t)
	sid := initSession(t, ts)

	// A follow-up request without the session header is rejected.
	respMissing := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  "ping",
	})
	_ = respMissing.Body.Close()
	if respMissing.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing-session status = %d, want 400", respMissing.StatusCode)
	}

	// An unknown session id is rejected as expired.
	respUnknown := postMCP(t, ts, "application/json", "deadbeefdeadbeef", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`3`),
		Method:  "ping",
	})
	_ = respUnknown.Body.Close()
	if respUnknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown-session status = %d, want 404", respUnknown.StatusCode)
	}

	// The minted session id is accepted and echoed back.
	respOK := postMCP(t, ts, "application/json", sid, protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`4`),
		Method:  "ping",
	})
	defer func() { _ = respOK.Body.Close() }()
	if respOK.StatusCode != http.StatusOK {
		t.Fatalf("valid-session status = %d, want 200", respOK.StatusCode)
	}
	if got := respOK.Header.Get("Mcp-Session-Id"); got != sid {
		t.Fatalf("echoed session id = %q, want %q", got, sid)
	}
}

func TestStreamableHTTP_GETOpensStream(t *testing.T) {
	h, ts := newStreamableServer(t)
	sid := initSession(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", sid)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("GET Content-Type = %q, want text/event-stream", ct)
	}

	// Push a server->client notification once the stream is registered.
	pushed := make(chan struct{})
	go func() {
		defer close(pushed)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if err := h.NotifyClient(sid, "notifications/message", map[string]any{"text": "push"}); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Errorf("failed to push notification to standing stream")
	}()

	frame, err := readOneSSEData(resp.Body)
	if err != nil {
		t.Fatalf("read SSE frame: %v", err)
	}
	<-pushed
	if !strings.Contains(frame, "notifications/message") {
		t.Fatalf("unexpected pushed frame: %q", frame)
	}
}

func TestStreamableHTTP_GETRequiresSession(t *testing.T) {
	_, ts := newStreamableServer(t)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET without session status = %d, want 400", resp.StatusCode)
	}
}

func TestStreamableHTTP_DeleteSession(t *testing.T) {
	_, ts := newStreamableServer(t)
	sid := initSession(t, ts)

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("build DELETE: %v", err)
	}
	req.Header.Set("Mcp-Session-Id", sid)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do DELETE: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	// The session is gone: a follow-up request is now rejected as expired.
	after := postMCP(t, ts, "application/json", sid, protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`9`),
		Method:  "ping",
	})
	_ = after.Body.Close()
	if after.StatusCode != http.StatusNotFound {
		t.Fatalf("post after delete status = %d, want 404", after.StatusCode)
	}
}

func TestStreamableHTTP_OriginEnforced(t *testing.T) {
	_, ts := newStreamableServer(t, WithAllowedOrigins("https://trusted.example"))

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed origin status = %d, want 403", resp.StatusCode)
	}
}

func TestStreamableHTTP_AuthorizeGate(t *testing.T) {
	_, ts := newStreamableServer(t, WithAuthorize(func(r *http.Request) error {
		if r.Header.Get("X-Api-Key") != "secret" {
			return errors.New("denied")
		}
		return nil
	}))

	resp := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d, want 403", resp.StatusCode)
	}
}

// TestStreamableHTTP_LegacyDefaultUnchanged confirms that without WithStreamable
// the /mcp endpoint remains POST-only (GET is rejected), preserving the legacy
// behavior.
func TestStreamableHTTP_LegacyDefaultUnchanged(t *testing.T) {
	h := NewHTTP("127.0.0.1:0")
	ts := httptest.NewServer(h.createHandler(streamableTestHandler()))
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("legacy GET /mcp status = %d, want 405", resp.StatusCode)
	}
}

// sseDataFrames extracts the payloads of all "data:" frames from an SSE body.
func sseDataFrames(body string) []string {
	var frames []string
	var cur strings.Builder
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "data:"):
			cur.WriteString(strings.TrimSpace(line[len("data:"):]))
		case line == "":
			if cur.Len() > 0 {
				frames = append(frames, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		frames = append(frames, cur.String())
	}
	return frames
}

// readOneSSEData reads until the first complete "data:" frame from a live SSE
// stream and returns its payload.
func readOneSSEData(r io.Reader) (string, error) {
	buf := make([]byte, 1)
	var line strings.Builder
	var data strings.Builder
	for {
		n, err := r.Read(buf)
		if n > 0 {
			c := buf[0]
			if c == '\n' {
				l := strings.TrimRight(line.String(), "\r")
				line.Reset()
				if payload, ok := strings.CutPrefix(l, "data:"); ok {
					data.WriteString(strings.TrimSpace(payload))
				} else if l == "" && data.Len() > 0 {
					return data.String(), nil
				}
			} else {
				line.WriteByte(c)
			}
		}
		if err != nil {
			if data.Len() > 0 {
				return data.String(), nil
			}
			return "", err
		}
	}
}
