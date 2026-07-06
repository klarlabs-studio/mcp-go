package client_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/mcp/client"
	"go.klarlabs.de/mcp/protocol"
)

// echoServer accepts a single JSON-RPC POST and replies with a fixed
// result. It captures the last received headers and body for
// assertion.
type echoServer struct {
	mu       sync.Mutex
	path     string
	headers  http.Header
	body     []byte
	response any
	status   int
	wait     time.Duration
}

func (e *echoServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		expected := e.path
		if expected == "" {
			expected = "/mcp"
		}
		if r.URL.Path != expected {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		e.mu.Lock()
		e.headers = r.Header.Clone()
		e.body = body
		e.mu.Unlock()
		if e.wait > 0 {
			select {
			case <-time.After(e.wait):
			case <-r.Context().Done():
				return
			}
		}
		if e.status == 0 {
			e.status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.status)
		if e.response != nil {
			_ = json.NewEncoder(w).Encode(e.response)
		}
	}
}

func mkRequest(t *testing.T, id int64, method string) *protocol.Request {
	t.Helper()
	idRaw, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal id: %v", err)
	}
	return &protocol.Request{JSONRPC: "2.0", ID: idRaw, Method: method}
}

func TestHTTPTransport_RoundTrip(t *testing.T) {
	srv := &echoServer{
		response: map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"echoed": true},
		},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()

	resp, err := tr.Send(context.Background(), mkRequest(t, 1, "tools/list"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok || result["echoed"] != true {
		t.Errorf("Result=%v want echoed=true", resp.Result)
	}
}

// bearerTransport injects an Authorization header. mcp-go ships no auth; this
// is the caller-owned transport pattern the library mandates — auth lives
// entirely in the supplied http.Client.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func TestHTTPTransport_AuthViaInjectedHTTPClient(t *testing.T) {
	srv := &echoServer{
		response: map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": map[string]any{},
		},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	hc := &http.Client{Transport: &bearerTransport{token: "s3cret"}}
	tr, err := client.NewHTTPTransport(ts.URL, client.WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()

	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "ping")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := srv.headers.Get("Authorization"); got != "Bearer s3cret" {
		t.Errorf("Authorization=%q want Bearer s3cret", got)
	}
}

func TestHTTPTransport_CustomHeaderForwarded(t *testing.T) {
	srv := &echoServer{

		response: map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": map[string]any{},
		},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL,
		client.WithHTTPHeader("X-Org-Id", "org-1"),
		client.WithHTTPHeader("X-Trace", "abc"),
	)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "ping")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := srv.headers.Get("X-Org-Id"); got != "org-1" {
		t.Errorf("X-Org-Id=%q", got)
	}
	if got := srv.headers.Get("X-Trace"); got != "abc" {
		t.Errorf("X-Trace=%q", got)
	}
	if got := srv.headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type=%q", got)
	}
}

func TestHTTPTransport_NonSuccessStatus(t *testing.T) {
	srv := &echoServer{

		status: http.StatusUnauthorized,
		response: map[string]any{
			"error": "missing token",
		},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	_, err = tr.Send(context.Background(), mkRequest(t, 1, "ping"))
	var statusErr *client.HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err=%v want HTTPStatusError", err)
	}
	if statusErr.Status != http.StatusUnauthorized {
		t.Errorf("Status=%d want 401", statusErr.Status)
	}
	if !strings.Contains(statusErr.Body, "missing token") {
		t.Errorf("Body=%q want contains 'missing token'", statusErr.Body)
	}
}

func TestHTTPTransport_ContextCancellation(t *testing.T) {
	srv := &echoServer{

		wait: 200 * time.Millisecond,
		response: map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": map[string]any{},
		},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := tr.Send(ctx, mkRequest(t, 1, "ping")); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestHTTPTransport_TLSWithCustomCAPool(t *testing.T) {
	srv := &echoServer{

		response: map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tls": true},
		},
	}
	ts := httptest.NewTLSServer(srv.handler(t))
	defer ts.Close()

	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())

	tr, err := client.NewHTTPTransport(ts.URL, client.WithCABundle(pool))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	resp, err := tr.Send(context.Background(), mkRequest(t, 1, "ping"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if m, ok := resp.Result.(map[string]any); !ok || m["tls"] != true {
		t.Errorf("Result=%v", resp.Result)
	}
}

func TestHTTPTransport_TLSDefaultPoolRejectsSelfSigned(t *testing.T) {
	srv := &echoServer{

		response: map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}},
	}
	ts := httptest.NewTLSServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "ping")); err == nil {
		t.Fatal("expected TLS verification failure")
	}
}

func TestHTTPTransport_InsecureSkipVerify(t *testing.T) {
	srv := &echoServer{

		response: map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}},
	}
	ts := httptest.NewTLSServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL, client.WithInsecureSkipVerify())
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "ping")); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestHTTPTransport_CustomHTTPClientWins(t *testing.T) {
	srv := &echoServer{

		response: map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}},
	}
	ts := httptest.NewTLSServer(srv.handler(t))
	defer ts.Close()

	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	custom := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	tr, err := client.NewHTTPTransport(ts.URL,
		client.WithHTTPClient(custom),
		client.WithCABundle(nil), // ignored when WithHTTPClient is set
	)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "ping")); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestHTTPTransport_RejectsBadURL(t *testing.T) {
	cases := []struct{ url, want string }{
		{"", "base URL"},
		{"://broken", "parse"},
		{"ftp://example.com", "scheme"},
		{"https://", "host"},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			_, err := client.NewHTTPTransport(tc.url)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("NewHTTPTransport(%q) err=%v want contains %q", tc.url, err, tc.want)
			}
		})
	}
}

func TestHTTPTransport_EndpointPathOverride(t *testing.T) {
	srv := &echoServer{
		path:     "/api/mcp",
		response: map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL, client.WithEndpointPath("/api/mcp"))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "ping")); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

// A malicious server must not be able to OOM the client by streaming an
// unbounded body: the transport caps how many bytes it reads and errors past it.
func TestHTTPTransport_OversizedResponseCapped(t *testing.T) {
	big := strings.Repeat("a", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"data":%q}}`, big)
	}))
	defer srv.Close()

	tr, err := client.NewHTTPTransport(srv.URL, client.WithMaxResponseBytes(256))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()

	_, err = tr.Send(context.Background(), mkRequest(t, 1, "x"))
	if err == nil {
		t.Fatal("expected oversized response to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want a size-limit error", err)
	}
}

// A response at or below the cap still succeeds (boundary not over-rejected).
func TestHTTPTransport_ResponseWithinCapSucceeds(t *testing.T) {
	srv := &echoServer{
		response: map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"ok": true}},
	}
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL, client.WithMaxResponseBytes(64*1024))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()

	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "x")); err != nil {
		t.Fatalf("Send within cap: %v", err)
	}
}

// A server-supplied redirect to another host must not forward the caller's
// custom auth header (WithHTTPHeader is the auth hook; Go only strips
// Authorization/Cookie cross-host). The transport refuses the cross-host hop.
func TestHTTPTransport_RefusesCrossHostRedirect(t *testing.T) {
	var mu sync.Mutex
	attackerCalled := false
	leakedKey := ""

	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attackerCalled = true
		leakedKey = r.Header.Get("X-API-Key")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer attacker.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/mcp", http.StatusFound)
	}))
	defer origin.Close()

	tr, err := client.NewHTTPTransport(origin.URL, client.WithHTTPHeader("X-API-Key", "s3cret"))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()

	_, err = tr.Send(context.Background(), mkRequest(t, 1, "x"))
	if err == nil {
		t.Fatal("expected cross-host redirect to be refused, got nil")
	}

	mu.Lock()
	defer mu.Unlock()
	if attackerCalled {
		t.Fatalf("attacker host received the request; custom auth header leaked: %q", leakedKey)
	}
}

// A same-host redirect (e.g. path change) is still followed, and the custom
// header is forwarded to the same host as expected.
func TestHTTPTransport_AllowsSameHostRedirect(t *testing.T) {
	var mu sync.Mutex
	gotKeyAtFinal := ""

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotKeyAtFinal = r.Header.Get("X-API-Key")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tr, err := client.NewHTTPTransport(ts.URL, client.WithHTTPHeader("X-API-Key", "s3cret"))
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer func() { _ = tr.Close() }()

	if _, err := tr.Send(context.Background(), mkRequest(t, 1, "x")); err != nil {
		t.Fatalf("same-host redirect Send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotKeyAtFinal != "s3cret" {
		t.Fatalf("same-host redirect target key = %q, want s3cret", gotKeyAtFinal)
	}
}
