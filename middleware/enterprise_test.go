package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

func TestOAuth2Authenticator(t *testing.T) {
	t.Run("validates JWT token", func(t *testing.T) {
		claims := map[string]any{
			"sub":   "user-123",
			"iss":   "https://auth.example.com",
			"exp":   float64(time.Now().Add(time.Hour).Unix()),
			"iat":   float64(time.Now().Unix()),
			"scope": "read write",
		}
		claimsJSON, _ := json.Marshal(claims)
		encoded := base64.StdEncoding.EncodeToString(claimsJSON)
		token := "header." + encoded + ".signature"

		validator := NewJWTValidator("secret")
		got, err := validator.ValidateToken(context.Background(), token)

		if err != nil {
			t.Fatalf("ValidateToken() error = %v", err)
		}

		if got.Subject != "user-123" {
			t.Errorf("Subject = %q, want user-123", got.Subject)
		}
		if got.Issuer != "https://auth.example.com" {
			t.Errorf("Issuer = %q, want https://auth.example.com", got.Issuer)
		}
	})

	t.Run("rejects expired token", func(t *testing.T) {
		claims := map[string]any{
			"sub": "user-123",
			"exp": float64(time.Now().Add(-time.Hour).Unix()),
		}
		claimsJSON, _ := json.Marshal(claims)
		encoded := base64.StdEncoding.EncodeToString(claimsJSON)
		token := "header." + encoded + ".signature"

		auth := NewOAuth2Authenticator(OAuth2Config{}, NewJWTValidator("secret"))

		ctx := protocol.ContextWithRequestMeta(context.Background(), protocol.RequestMeta{"Authorization": "Bearer " + token})
		_, err := auth.Authenticate(ctx, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		})

		if err == nil {
			t.Error("expected error for expired token")
		}
	})

	t.Run("checks required scopes", func(t *testing.T) {
		claims := map[string]any{
			"sub":   "user-123",
			"scope": "read",
		}
		claimsJSON, _ := json.Marshal(claims)
		encoded := base64.StdEncoding.EncodeToString(claimsJSON)
		token := "header." + encoded + ".signature"

		auth := NewOAuth2Authenticator(OAuth2Config{
			RequiredScopes: []string{"read", "write"},
		}, NewJWTValidator("secret"))

		ctx := protocol.ContextWithRequestMeta(context.Background(), protocol.RequestMeta{"Authorization": "Bearer " + token})
		_, err := auth.Authenticate(ctx, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		})

		if err == nil {
			t.Error("expected error for missing scope")
		}
	})
}

func TestJWTValidator(t *testing.T) {
	t.Run("rejects invalid token format", func(t *testing.T) {
		validator := NewJWTValidator("secret")
		_, err := validator.ValidateToken(context.Background(), "invalid-token")
		if err == nil {
			t.Error("expected error for invalid token format")
		}
	})

	t.Run("parses audience as string", func(t *testing.T) {
		claims := map[string]any{
			"sub": "user-123",
			"aud": "my-client",
			"exp": float64(time.Now().Add(time.Hour).Unix()),
		}
		claimsJSON, _ := json.Marshal(claims)
		encoded := base64.StdEncoding.EncodeToString(claimsJSON)
		token := "header." + encoded + ".signature"

		validator := NewJWTValidator("secret")
		got, _ := validator.ValidateToken(context.Background(), token)

		if len(got.Audience) != 1 || got.Audience[0] != "my-client" {
			t.Errorf("Audience = %v, want [my-client]", got.Audience)
		}
	})

	t.Run("parses audience as array", func(t *testing.T) {
		claims := map[string]any{
			"sub": "user-123",
			"aud": []any{"client1", "client2"},
			"exp": float64(time.Now().Add(time.Hour).Unix()),
		}
		claimsJSON, _ := json.Marshal(claims)
		encoded := base64.StdEncoding.EncodeToString(claimsJSON)
		token := "header." + encoded + ".signature"

		validator := NewJWTValidator("secret")
		got, _ := validator.ValidateToken(context.Background(), token)

		if len(got.Audience) != 2 {
			t.Errorf("Audience = %v, want 2 items", got.Audience)
		}
	})
}

func TestTracingMiddleware(t *testing.T) {
	t.Run("generates tracing IDs", func(t *testing.T) {
		mw := Tracing()
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			corrID := CorrelationIDFromContext(ctx)
			traceID := TraceIDFromContext(ctx)
			if corrID == "" {
				t.Error("expected correlation ID")
			}
			if traceID == "" {
				t.Error("expected trace ID")
			}
			return protocol.NewResponse(req.ID, nil), nil
		})

		handler(context.Background(), &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
	})

	t.Run("preserves existing correlation ID", func(t *testing.T) {
		mw := Tracing()
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			corrID := CorrelationIDFromContext(ctx)
			if corrID != "existing-corr-id" {
				t.Errorf("CorrelationID = %q, want existing-corr-id", corrID)
			}
			return protocol.NewResponse(req.ID, nil), nil
		})

		ctx := ContextWithCorrelationID(context.Background(), "existing-corr-id")
		handler(ctx, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
	})
}

func TestFormatTracingHeaders(t *testing.T) {
	t.Run("formats headers", func(t *testing.T) {
		ctx := ContextWithCorrelationID(context.Background(), "corr-123")
		ctx = ContextWithTracing(ctx, "trace-456")

		headers := FormatTracingHeaders(ctx)

		if headers[CorrelationIDHeader] != "corr-123" {
			t.Errorf("CorrelationIDHeader = %q, want corr-123", headers[CorrelationIDHeader])
		}
		if headers[TraceIDHeader] != "trace-456" {
			t.Errorf("TraceIDHeader = %q, want trace-456", headers[TraceIDHeader])
		}
	})
}

func TestParseTracingHeaders(t *testing.T) {
	t.Run("parses headers case-insensitive", func(t *testing.T) {
		corrID, traceID := ParseTracingHeaders(map[string]string{
			"x-correlation-id": "corr-123",
			"X-TRACE-ID":       "trace-456",
		})

		if corrID != "corr-123" {
			t.Errorf("correlationID = %q, want corr-123", corrID)
		}
		if traceID != "trace-456" {
			t.Errorf("traceID = %q, want trace-456", traceID)
		}
	})
}
