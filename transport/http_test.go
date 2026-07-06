package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

func TestNewHTTP(t *testing.T) {
	t.Run("creates http transport with address", func(t *testing.T) {
		transport := NewHTTP(":8080")

		if transport == nil {
			t.Fatal("expected transport to be created")
		}

		if transport.Addr() != ":8080" {
			t.Errorf("Addr() = %q, want %q", transport.Addr(), ":8080")
		}
	})

	t.Run("creates http transport with options", func(t *testing.T) {
		transport := NewHTTP(":8080",
			WithReadTimeout(5*time.Second),
			WithWriteTimeout(10*time.Second),
		)

		if transport.readTimeout != 5*time.Second {
			t.Errorf("readTimeout = %v, want %v", transport.readTimeout, 5*time.Second)
		}
		if transport.writeTimeout != 10*time.Second {
			t.Errorf("writeTimeout = %v, want %v", transport.writeTimeout, 10*time.Second)
		}
	})

	t.Run("creates http transport with session store", func(t *testing.T) {
		store := NewInMemoryStore()
		transport := NewHTTP(":8080", WithSessionStore(store))

		if transport.sessionStore == nil {
			t.Fatal("expected session store to be set")
		}
	})
}

func TestHTTP_Handler(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, map[string]string{"status": "ok"}), nil
	})

	transport := NewHTTP(":0")
	httpHandler := transport.createHandler(handler)

	t.Run("handles POST /mcp requests", func(t *testing.T) {
		req := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test/method",
		}
		reqBytes, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, httpReq)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `"result"`) {
			t.Errorf("expected result in response, got %q", body)
		}
		if !strings.Contains(body, `"status":"ok"`) {
			t.Errorf("expected status in response, got %q", body)
		}
	})

	t.Run("returns 405 for non-POST to /mcp", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, httpReq)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("returns 400 for invalid JSON", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{invalid}"))
		httpReq.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, httpReq)

		if rec.Code != http.StatusOK { // JSON-RPC errors return 200 with error in body
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `"error"`) {
			t.Errorf("expected error in response, got %q", body)
		}
	})

	t.Run("handles /health endpoint", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, httpReq)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `"status":"ok"`) {
			t.Errorf("expected status ok in response, got %q", body)
		}
	})

	t.Run("handles /healthz endpoint with default store", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, httpReq)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `"status":"ok"`) {
			t.Errorf("expected status ok in response, got %q", body)
		}
		if !strings.Contains(body, `"ready":true`) {
			t.Errorf("expected ready true in response, got %q", body)
		}
	})
}

func TestHTTP_SSE(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})

	transport := NewHTTP(":0")
	httpHandler := transport.createHandler(handler)

	t.Run("establishes SSE connection", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		httpReq := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			httpHandler.ServeHTTP(rec, httpReq)
			close(done)
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("SSE handler did not exit after context cancellation")
		}

		contentType := rec.Header().Get("Content-Type")
		if contentType != "" && !strings.Contains(contentType, "text/event-stream") {
			t.Errorf("Content-Type = %q, want text/event-stream", contentType)
		}
	})

	t.Run("SSE with custom client ID", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		httpReq := httptest.NewRequest(http.MethodGet, "/mcp/sse?clientId=custom-id", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			httpHandler.ServeHTTP(rec, httpReq)
			close(done)
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("SSE handler did not exit")
		}
	})

	t.Run("SSE includes Link header with session store", func(t *testing.T) {
		transportWithStore := NewHTTP(":0", WithSessionStore(NewInMemoryStore()))
		httpHandlerWithStore := transportWithStore.createHandler(handler)

		ctx, cancel := context.WithCancel(context.Background())
		httpReq := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			httpHandlerWithStore.ServeHTTP(rec, httpReq)
			close(done)
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("SSE handler did not exit")
		}

		linkHeader := rec.Header().Get("Link")
		if linkHeader == "" {
			t.Error("expected Link header when session store is configured")
		}
	})
}

func TestHTTP_Broadcast(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})

	transport := NewHTTP(":0")
	httpHandler := transport.createHandler(handler)

	t.Run("broadcast to SSE clients", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		httpReq1 := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
		rec1 := httptest.NewRecorder()

		httpReq2 := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
		rec2 := httptest.NewRecorder()

		go httpHandler.ServeHTTP(rec1, httpReq1)
		go httpHandler.ServeHTTP(rec2, httpReq2)

		time.Sleep(30 * time.Millisecond)

		transport.Broadcast([]byte("test message"))

		time.Sleep(10 * time.Millisecond)

		cancel()
	})
}

func TestHTTP_SendTo(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})

	transport := NewHTTP(":0")
	httpHandler := transport.createHandler(handler)

	// Use a real server so the SSE stream is read over a synchronized network
	// connection rather than racing on an httptest.Recorder.
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	// connectSSE opens an SSE stream and returns the connection plus the
	// clientId echoed in the "connected" event. Registration happens before
	// that event is written, so the channel is present once it returns.
	connectSSE := func(t *testing.T, ctx context.Context, query string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/mcp/sse"+query, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("connect SSE: %v", err)
		}
		data, err := NewSSEReader(resp.Body).ReadData()
		if err != nil {
			t.Fatalf("read connected event: %v", err)
		}
		var connected struct {
			ClientID string `json:"clientId"`
		}
		if err := json.Unmarshal(data, &connected); err != nil {
			t.Fatalf("parse connected event %q: %v", data, err)
		}
		return resp, connected.ClientID
	}

	t.Run("send to specific client", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		resp, id := connectSSE(t, ctx, "?clientId=test-client")
		defer func() { _ = resp.Body.Close() }()
		if id != "test-client" {
			t.Fatalf("client-supplied id should be honored for correlation, got %q", id)
		}

		if !transport.SendTo("test-client", []byte("direct message")) {
			t.Error("expected SendTo to return true for existing client")
		}
		if transport.SendTo("non-existent", []byte("should fail")) {
			t.Error("expected SendTo to return false for non-existent client")
		}
	})

	t.Run("empty clientId is minted server-side, not a timestamp", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		resp, id := connectSSE(t, ctx, "")
		defer func() { _ = resp.Body.Close() }()
		// crypto/rand 16 bytes hex-encoded == 32 chars; a UnixNano timestamp
		// would be ~19 numeric digits.
		if len(id) != 32 {
			t.Fatalf("expected 32-char random id, got %q (len %d)", id, len(id))
		}
	})

	t.Run("duplicate clientId cannot steal a live channel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Victim connects with a chosen id.
		victim, id := connectSSE(t, ctx, "?clientId=victim")
		defer func() { _ = victim.Body.Close() }()
		if id != "victim" {
			t.Fatalf("unexpected id %q", id)
		}

		// Attacker tries to register the same id: must be refused, not allowed
		// to overwrite/steal the victim's channel.
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/mcp/sse?clientId=victim", nil)
		attacker, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("attacker connect: %v", err)
		}
		defer func() { _ = attacker.Body.Close() }()
		if attacker.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("duplicate id status = %d, want %d", attacker.StatusCode, http.StatusServiceUnavailable)
		}

		// The victim's channel is intact and still addressable.
		if !transport.SendTo("victim", []byte("still mine")) {
			t.Error("victim channel must survive a hijack attempt")
		}
	})
}

func TestHTTP_Serve(t *testing.T) {
	t.Run("starts and stops server", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		transport := NewHTTP(":0") // Random available port

		ctx, cancel := context.WithCancel(context.Background())

		errCh := make(chan error, 1)
		go func() {
			errCh <- transport.Serve(ctx, handler)
		}()

		// Give server time to start
		time.Sleep(50 * time.Millisecond)

		// Cancel to stop server
		cancel()

		select {
		case err := <-errCh:
			if err != nil && err != context.Canceled && err != http.ErrServerClosed {
				t.Errorf("unexpected error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("server did not stop in time")
		}
	})

	t.Run("accepts requests while running", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, map[string]string{"method": req.Method}), nil
		})

		transport := NewHTTP("127.0.0.1:0")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- transport.Serve(ctx, handler)
		}()

		// Give server time to start and get actual address
		time.Sleep(50 * time.Millisecond)

		addr := transport.ListenAddr()
		if addr == "" {
			t.Skip("could not get listen address")
		}

		// Make a request
		reqBody := `{"jsonrpc":"2.0","id":1,"method":"test"}`
		resp, err := http.Post("http://"+addr+"/mcp", "application/json", strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"method":"test"`) {
			t.Errorf("unexpected response: %s", body)
		}

		cancel()
	})
}

func okHandler() Handler {
	return HandlerFunc(func(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, map[string]string{"status": "ok"}), nil
	})
}

func TestHTTP_SSE_CrossOriginRejected(t *testing.T) {
	h := NewHTTP(":0").createHandler(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Errorf("SSE must not emit Access-Control-Allow-Origin: *, got %q", got)
	}
}

func TestHTTP_SSE_SameOriginAllowed(t *testing.T) {
	h := NewHTTP(":0").createHandler(okHandler())

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not exit")
	}

	if rec.Code == http.StatusForbidden {
		t.Fatalf("same-origin request rejected: %d", rec.Code)
	}
}

func TestHTTP_SSE_AllowlistedOriginAllowed(t *testing.T) {
	h := NewHTTP(":0", WithAllowedOrigins("https://app.example")).createHandler(okHandler())

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not exit")
	}

	if rec.Code == http.StatusForbidden {
		t.Fatalf("allowlisted origin rejected: %d", rec.Code)
	}
}

func TestHTTP_MCP_CrossOriginRejected(t *testing.T) {
	h := NewHTTP(":0").createHandler(okHandler())

	body, _ := json.Marshal(protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "test"})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHTTP_MCP_OversizeBodyRejected(t *testing.T) {
	h := NewHTTP(":0", WithMaxRequestBytes(256)).createHandler(okHandler())

	big := strings.Repeat("a", 4096)
	body := `{"jsonrpc":"2.0","id":1,"method":"test","params":{"x":"` + big + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHTTP_MCP_NotificationReturns202(t *testing.T) {
	h := NewHTTP(":0").createHandler(okHandler())

	// No id => notification.
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notify"}`))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if strings.Contains(rec.Body.String(), `"result"`) {
		t.Errorf("notification must not receive a response body, got %q", rec.Body.String())
	}
}

func TestHTTP_Authorize_RejectsBothPaths(t *testing.T) {
	deny := WithAuthorize(func(_ *http.Request) error { return context.Canceled })
	h := NewHTTP(":0", deny).createHandler(okHandler())

	post := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"t"}`))
	postRec := httptest.NewRecorder()
	h.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusForbidden {
		t.Errorf("POST /mcp status = %d, want 403", postRec.Code)
	}

	sse := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)
	sseRec := httptest.NewRecorder()
	h.ServeHTTP(sseRec, sse)
	if sseRec.Code != http.StatusForbidden {
		t.Errorf("GET /mcp/sse status = %d, want 403", sseRec.Code)
	}
}

func TestHTTP_SSE_MaxConnections(t *testing.T) {
	h := NewHTTP(":0", WithMaxSSEConnections(1)).createHandler(okHandler())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
	firstRec := httptest.NewRecorder()
	go func() { h.ServeHTTP(firstRec, first) }()
	time.Sleep(20 * time.Millisecond)

	second := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil)
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, second)

	if secondRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("second connection status = %d, want %d", secondRec.Code, http.StatusServiceUnavailable)
	}
}

func TestHTTP_RequestContextFn(t *testing.T) {
	type ctxKey string
	const subjectKey ctxKey = "subject"

	var seen string
	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		if v, ok := ctx.Value(subjectKey).(string); ok {
			seen = v
		}
		return protocol.NewResponse(req.ID, map[string]string{"status": "ok"}), nil
	})

	transport := NewHTTP(":0", WithRequestContextFn(func(ctx context.Context, r *http.Request) context.Context {
		return context.WithValue(ctx, subjectKey, r.Header.Get("X-Cert-Subject"))
	}))
	httpHandler := transport.createHandler(handler)

	req := protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "test/method"}
	reqBytes, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqBytes))
	httpReq.Header.Set("X-Cert-Subject", "CN=svc-a")
	rec := httptest.NewRecorder()

	httpHandler.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seen != "CN=svc-a" {
		t.Errorf("handler ctx subject = %q, want %q", seen, "CN=svc-a")
	}
}
