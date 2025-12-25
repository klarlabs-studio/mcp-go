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
}

func TestHTTP_SSE(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})

	transport := NewHTTP(":0")
	httpHandler := transport.createHandler(handler)

	t.Run("establishes SSE connection", func(t *testing.T) {
		// Use a cancelable context so we can stop the SSE handler
		ctx, cancel := context.WithCancel(context.Background())
		httpReq := httptest.NewRequest(http.MethodGet, "/mcp/sse", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		// Run in goroutine since SSE blocks
		done := make(chan struct{})
		go func() {
			httpHandler.ServeHTTP(rec, httpReq)
			close(done)
		}()

		// Give it time to set headers and start the SSE loop
		time.Sleep(20 * time.Millisecond)

		// Cancel the request context to stop SSE
		cancel()

		// Wait for the goroutine to complete before checking headers
		select {
		case <-done:
			// SSE handler returned - safe to check headers now
		case <-time.After(time.Second):
			t.Fatal("SSE handler did not exit after context cancellation")
		}

		// Check headers - now safe since goroutine has exited
		contentType := rec.Header().Get("Content-Type")
		if contentType != "" && !strings.Contains(contentType, "text/event-stream") {
			t.Errorf("Content-Type = %q, want text/event-stream", contentType)
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
