package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

func TestStdioHTTP_RoundTrip(t *testing.T) {
	var out bytes.Buffer
	inR, inW := io.Pipe()
	tr := NewStdioHTTP(
		WithStdioHTTPStdin(inR),
		WithStdioHTTPStdout(&out),
		WithStdioHTTPStderr(io.Discard),
	)

	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		if sender := NotificationSenderFromContext(ctx); sender != nil {
			_ = sender.SendNotification(protocol.MethodProgress, map[string]any{"progress": 1})
		}
		return protocol.NewResponse(req.ID, map[string]any{"ok": true}), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tr.Serve(ctx, handler) }()

	body, _ := json.Marshal(protocol.Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodPing,
	})
	frame, _ := json.Marshal(HTTPFrame{
		Type:   "request",
		ID:     "req-1",
		Method: "POST",
		Path:   "/mcp",
		Headers: map[string]string{
			"Mcp-Method": protocol.MethodPing,
			"Accept":     "application/json",
		},
		Body: body,
	})
	if _, err := inW.Write(append(frame, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = inW.Close()

	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected notification + response, got %d lines: %q", len(lines), out.String())
	}
	var notif, resp HTTPFrame
	if err := json.Unmarshal([]byte(lines[0]), &notif); err != nil {
		t.Fatalf("notif: %v", err)
	}
	if notif.Type != "notification" || notif.ID != "req-1" {
		t.Fatalf("notif = %#v", notif)
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &resp); err != nil {
		t.Fatalf("resp: %v", err)
	}
	if resp.Type != "response" || resp.Status != 200 || resp.ID != "req-1" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestStdioHTTP_HeaderMismatch(t *testing.T) {
	var out bytes.Buffer
	inR, inW := io.Pipe()
	tr := NewStdioHTTP(WithStdioHTTPStdin(inR), WithStdioHTTPStdout(&out), WithStdioHTTPStderr(io.Discard))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- tr.Serve(ctx, HandlerFunc(func(context.Context, *protocol.Request) (*protocol.Response, error) {
			return nil, protocol.NewInternalError("handler should not run")
		}))
	}()

	body, _ := json.Marshal(protocol.Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodToolsList,
	})
	frame, _ := json.Marshal(HTTPFrame{
		Type:    "request",
		ID:      "x",
		Method:  "POST",
		Headers: map[string]string{"Mcp-Method": "tools/call"},
		Body:    body,
	})
	_, _ = inW.Write(append(frame, '\n'))
	_ = inW.Close()
	<-done

	var resp HTTPFrame
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode: %v body=%q", err, out.String())
	}
	if resp.Status != 400 {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
}

func TestWebhookNotifier_SendNotification(t *testing.T) {
	var (
		mu  sync.Mutex
		got []byte
		hdr http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hdr = r.Header.Clone()
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	n.Headers = http.Header{"X-Hook": []string{"1"}}
	if err := n.SendNotification(protocol.MethodChannelMessage, map[string]any{
		"channel": "status",
		"message": map[string]any{"type": "text", "text": "hi"},
	}); err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hdr.Get("Mcp-Method") != protocol.MethodChannelMessage {
		t.Fatalf("Mcp-Method = %q", hdr.Get("Mcp-Method"))
	}
	if hdr.Get("X-Hook") != "1" {
		t.Fatalf("X-Hook = %q", hdr.Get("X-Hook"))
	}
	var msg Notification
	if err := json.Unmarshal(got, &msg); err != nil {
		t.Fatalf("body: %v", err)
	}
	if msg.Method != protocol.MethodChannelMessage {
		t.Fatalf("method = %q", msg.Method)
	}
}

func TestMultiNotifier(t *testing.T) {
	var a, b int
	m := &MultiNotifier{Notifiers: []NotificationSender{
		testNotifier(func(string, any) error { a++; return nil }),
		testNotifier(func(string, any) error { b++; return nil }),
	}}
	if err := m.SendNotification("n", nil); err != nil {
		t.Fatal(err)
	}
	if a != 1 || b != 1 {
		t.Fatalf("a=%d b=%d", a, b)
	}
}

type testNotifier func(string, any) error

func (f testNotifier) SendNotification(method string, params any) error {
	return f(method, params)
}

func TestDiscoveryAuthExtensions(t *testing.T) {
	d := NewServerDiscovery(&server.Manifest{
		Name:            "s",
		Version:         "1",
		ProtocolVersion: protocol.MCPVersion,
	}, WithDiscoveryAuthExtensions(AuthExtensions{
		DPoP:                       true,
		TokenExchange:              true,
		IDJAG:                      "https://as.example/id-jag",
		WorkloadIdentityFederation: "https://wif.example",
	}))
	if d.Authentication == nil || d.Authentication.Extensions == nil {
		t.Fatal("expected auth extensions")
	}
	if !d.Authentication.Extensions.DPoP || d.Authentication.Extensions.IDJAG == "" {
		t.Fatalf("extensions = %#v", d.Authentication.Extensions)
	}

	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil))
	if !strings.Contains(rec.Body.String(), `"dpop":true`) {
		t.Fatalf("body missing dpop: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"idJag"`) {
		t.Fatalf("body missing idJag: %s", rec.Body.String())
	}
}
