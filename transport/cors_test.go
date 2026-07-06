package transport_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.klarlabs.de/mcp/transport"
)

func TestCORSHandler(t *testing.T) {
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	t.Run("allows all origins with wildcard", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins: []string{"*"},
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("expected Access-Control-Allow-Origin '*', got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("allows specific origin", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins: []string{"http://allowed.com", "http://also-allowed.com"},
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://allowed.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "http://allowed.com" {
			t.Errorf("expected Access-Control-Allow-Origin 'http://allowed.com', got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("blocks disallowed origin", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins: []string{"http://allowed.com"},
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://notallowed.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("expected no Access-Control-Allow-Origin header, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("handles preflight request", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "DELETE"},
			AllowHeaders: []string{"Content-Type", "X-Custom-Header"},
			MaxAge:       3600,
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", rec.Code)
		}

		methods := rec.Header().Get("Access-Control-Allow-Methods")
		if methods != "GET, POST, DELETE" {
			t.Errorf("expected methods 'GET, POST, DELETE', got %q", methods)
		}

		headers := rec.Header().Get("Access-Control-Allow-Headers")
		if headers != "Content-Type, X-Custom-Header" {
			t.Errorf("expected headers 'Content-Type, X-Custom-Header', got %q", headers)
		}

		maxAge := rec.Header().Get("Access-Control-Max-Age")
		if maxAge != "3600" {
			t.Errorf("expected max-age '3600', got %q", maxAge)
		}
	})

	t.Run("allows credentials", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins:     []string{"http://example.com"},
			AllowCredentials: true,
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("expected Access-Control-Allow-Credentials 'true'")
		}
	})

	t.Run("exposes headers", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins:  []string{"*"},
			ExposeHeaders: []string{"X-Custom-Response", "X-Request-ID"},
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		exposed := rec.Header().Get("Access-Control-Expose-Headers")
		if exposed != "X-Custom-Response, X-Request-ID" {
			t.Errorf("expected expose headers 'X-Custom-Response, X-Request-ID', got %q", exposed)
		}
	})

	t.Run("uses default values", func(t *testing.T) {
		config := transport.CORSConfig{
			AllowOrigins: []string{"*"},
		}
		handler := transport.CORSHandler(config, echoHandler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Should have default methods
		methods := rec.Header().Get("Access-Control-Allow-Methods")
		if methods != "GET, POST, OPTIONS" {
			t.Errorf("expected default methods, got %q", methods)
		}

		// Should have default max age
		maxAge := rec.Header().Get("Access-Control-Max-Age")
		if maxAge != "86400" {
			t.Errorf("expected default max-age '86400', got %q", maxAge)
		}
	})
}

func TestCORSHandler_WildcardWithCredentialsNormalized(t *testing.T) {
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := transport.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
	}
	handler := transport.CORSHandler(config, echoHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// "*" + credentials is invalid; credentials must be dropped.
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("wildcard origin must not be combined with credentials, got %q",
			rec.Header().Get("Access-Control-Allow-Credentials"))
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected wildcard ACAO, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestDefaultCORSConfig(t *testing.T) {
	config := transport.DefaultCORSConfig()

	if len(config.AllowOrigins) != 1 || config.AllowOrigins[0] != "*" {
		t.Error("expected AllowOrigins to be ['*']")
	}

	if len(config.AllowMethods) != 3 {
		t.Error("expected 3 default methods")
	}

	if config.MaxAge != 86400 {
		t.Errorf("expected MaxAge 86400, got %d", config.MaxAge)
	}
}
