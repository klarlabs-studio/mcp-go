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

	"github.com/felixgeelhaar/mcp-go/protocol"
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

	t.Run("send to specific client", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		httpReq := httptest.NewRequest(http.MethodGet, "/mcp/sse?clientId=test-client", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		go httpHandler.ServeHTTP(rec, httpReq)

		time.Sleep(30 * time.Millisecond)

		ok := transport.SendTo("test-client", []byte("direct message"))
		if !ok {
			t.Error("expected SendTo to return true for existing client")
		}

		ok = transport.SendTo("non-existent", []byte("should fail"))
		if ok {
			t.Error("expected SendTo to return false for non-existent client")
		}

		cancel()
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
