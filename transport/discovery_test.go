package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

func TestServerDiscovery(t *testing.T) {
	t.Run("creates discovery from manifest", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			Version:         "1.0.0",
			ProtocolVersion: "2025-11-25",
			Capabilities: server.Capabilities{
				Tools:     true,
				Resources: true,
				Prompts:   true,
			},
		}

		discovery := NewServerDiscovery(manifest)

		if discovery.MCPPVersion != "2025-11-25" {
			t.Errorf("MCPPVersion = %q, want 2025-11-25", discovery.MCPPVersion)
		}
		if discovery.Server.Name != "test-server" {
			t.Errorf("Server.Name = %q, want test-server", discovery.Server.Name)
		}
		if !discovery.Capabilities.Tools {
			t.Error("expected Capabilities.Tools to be true")
		}
		if !discovery.Capabilities.Resources {
			t.Error("expected Capabilities.Resources to be true")
		}
	})

	t.Run("serves discovery JSON", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			Version:         "1.0.0",
			ProtocolVersion: "2025-11-25",
			Capabilities: server.Capabilities{
				Tools: true,
			},
		}

		discovery := NewServerDiscovery(manifest)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil)
		rec := httptest.NewRecorder()

		discovery.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", contentType)
		}

		cacheControl := rec.Header().Get("Cache-Control")
		if !strings.Contains(cacheControl, "max-age") {
			t.Errorf("Cache-Control = %q, want to contain max-age", cacheControl)
		}

		var result ServerDiscovery
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if result.Server.Name != "test-server" {
			t.Errorf("Server.Name = %q, want test-server", result.Server.Name)
		}
	})

	t.Run("adds endpoints via option", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		endpoints := ServerEndpoint{
			StreamableHTTP: "https://example.com/mcp",
			SSE:            "https://example.com/mcp/sse",
			WebSocket:      "wss://example.com/ws",
		}

		discovery := NewServerDiscovery(manifest, WithDiscoveryEndpoints(endpoints))

		if discovery.Endpoints.StreamableHTTP != "https://example.com/mcp" {
			t.Errorf("Endpoints.StreamableHTTP = %q, want https://example.com/mcp", discovery.Endpoints.StreamableHTTP)
		}
	})

	t.Run("adds auth via option", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		auth := ServerAuth{
			Required:            true,
			Methods:             []AuthMethod{AuthOAuth2, AuthAPIKey},
			AuthorizationHeader: "Bearer",
		}

		discovery := NewServerDiscovery(manifest, WithDiscoveryAuth(auth))

		if discovery.Authentication == nil {
			t.Fatal("expected Authentication to be set")
		}
		if !discovery.Authentication.Required {
			t.Error("expected Authentication.Required to be true")
		}
		if len(discovery.Authentication.Methods) != 2 {
			t.Errorf("len(Methods) = %d, want 2", len(discovery.Authentication.Methods))
		}
	})

	t.Run("advertises OAuth/OIDC metadata via auth struct", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		auth := ServerAuth{
			Required:                  true,
			Methods:                   []AuthMethod{AuthOAuth2},
			AuthorizationServers:      []string{"https://auth.example.com"},
			ProtectedResourceMetadata: "https://mcp.example.com/.well-known/oauth-protected-resource",
			ResourceIndicator:         "https://mcp.example.com",
			ScopesSupported:           []string{"mcp:read", "mcp:write"},
			OIDCConfiguration:         "https://auth.example.com/.well-known/openid-configuration",
		}

		discovery := NewServerDiscovery(manifest, WithDiscoveryAuth(auth))

		rec := httptest.NewRecorder()
		discovery.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil))

		body := rec.Body.String()
		wantSubstrings := []string{
			`"authorizationServers":["https://auth.example.com"]`,
			`"protectedResourceMetadata":"https://mcp.example.com/.well-known/oauth-protected-resource"`,
			`"resourceIndicator":"https://mcp.example.com"`,
			`"scopesSupported":["mcp:read","mcp:write"]`,
			`"oidcConfiguration":"https://auth.example.com/.well-known/openid-configuration"`,
		}
		for _, want := range wantSubstrings {
			if !strings.Contains(body, want) {
				t.Errorf("expected body to contain %s, got %q", want, body)
			}
		}

		// Round-trip to confirm the fields deserialize correctly.
		var result ServerDiscovery
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if result.Authentication == nil {
			t.Fatal("expected Authentication to be set")
		}
		if len(result.Authentication.AuthorizationServers) != 1 {
			t.Errorf("len(AuthorizationServers) = %d, want 1", len(result.Authentication.AuthorizationServers))
		}
		if result.Authentication.ResourceIndicator != "https://mcp.example.com" {
			t.Errorf("ResourceIndicator = %q, want https://mcp.example.com", result.Authentication.ResourceIndicator)
		}
	})

	t.Run("advertises OAuth/OIDC metadata via option creating auth section", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		meta := OAuthMetadata{
			AuthorizationServers:      []string{"https://auth.example.com"},
			ProtectedResourceMetadata: "https://mcp.example.com/.well-known/oauth-protected-resource",
			ResourceIndicator:         "https://mcp.example.com",
			ScopesSupported:           []string{"mcp:read"},
			OIDCConfiguration:         "https://auth.example.com/.well-known/openid-configuration",
		}

		discovery := NewServerDiscovery(manifest, WithDiscoveryOAuthMetadata(meta))

		if discovery.Authentication == nil {
			t.Fatal("expected Authentication to be created by option")
		}
		if len(discovery.Authentication.Methods) != 1 || discovery.Authentication.Methods[0] != AuthOAuth2 {
			t.Errorf("Methods = %v, want [oauth2]", discovery.Authentication.Methods)
		}
		if discovery.Authentication.OIDCConfiguration != meta.OIDCConfiguration {
			t.Errorf("OIDCConfiguration = %q, want %q", discovery.Authentication.OIDCConfiguration, meta.OIDCConfiguration)
		}
	})

	t.Run("option enriches existing auth section", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		auth := ServerAuth{
			Required:            true,
			Methods:             []AuthMethod{AuthBearer},
			AuthorizationHeader: "Bearer",
		}
		meta := OAuthMetadata{
			ResourceIndicator: "https://mcp.example.com",
		}

		discovery := NewServerDiscovery(manifest,
			WithDiscoveryAuth(auth),
			WithDiscoveryOAuthMetadata(meta),
		)

		if discovery.Authentication == nil {
			t.Fatal("expected Authentication to be set")
		}
		// Existing fields preserved.
		if discovery.Authentication.AuthorizationHeader != "Bearer" {
			t.Errorf("AuthorizationHeader = %q, want Bearer", discovery.Authentication.AuthorizationHeader)
		}
		if len(discovery.Authentication.Methods) != 1 || discovery.Authentication.Methods[0] != AuthBearer {
			t.Errorf("Methods = %v, want [bearer]", discovery.Authentication.Methods)
		}
		// New field added.
		if discovery.Authentication.ResourceIndicator != "https://mcp.example.com" {
			t.Errorf("ResourceIndicator = %q, want https://mcp.example.com", discovery.Authentication.ResourceIndicator)
		}
	})

	t.Run("omits OAuth/OIDC fields when unset", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		auth := ServerAuth{
			Required: true,
			Methods:  []AuthMethod{AuthAPIKey},
		}

		discovery := NewServerDiscovery(manifest, WithDiscoveryAuth(auth))

		rec := httptest.NewRecorder()
		discovery.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil))

		body := rec.Body.String()
		unwantedKeys := []string{
			"authorizationServers",
			"protectedResourceMetadata",
			"resourceIndicator",
			"scopesSupported",
			"oidcConfiguration",
		}
		for _, key := range unwantedKeys {
			if strings.Contains(body, key) {
				t.Errorf("expected key %q to be omitted, got %q", key, body)
			}
		}
	})

	t.Run("includes optional fields", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			Version:         "2.0.0",
			Title:           "Test Server",
			Description:     "A test MCP server",
			WebsiteURL:      "https://example.com",
			ProtocolVersion: "2025-11-25",
			Capabilities:    server.Capabilities{},
		}

		discovery := NewServerDiscovery(manifest)

		if discovery.Server.Title != "Test Server" {
			t.Errorf("Server.Title = %q, want Test Server", discovery.Server.Title)
		}
		if discovery.Server.Description != "A test MCP server" {
			t.Errorf("Server.Description = %q, want A test MCP server", discovery.Server.Description)
		}
		if discovery.Server.WebsiteURL != "https://example.com" {
			t.Errorf("Server.WebsiteURL = %q, want https://example.com", discovery.Server.WebsiteURL)
		}
	})
}

func TestHTTP_Discovery(t *testing.T) {
	t.Run("serves .well-known/mcp when configured", func(t *testing.T) {
		manifest := &server.Manifest{
			Name:            "test-server",
			Version:         "1.0.0",
			ProtocolVersion: "2025-11-25",
			Capabilities: server.Capabilities{
				Tools: true,
			},
		}

		discovery := NewServerDiscovery(manifest)
		transport := NewHTTP(":0", WithDiscovery(discovery))

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, nil
		})
		httpHandler := transport.createHandler(handler)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil)
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `"name":"test-server"`) {
			t.Errorf("expected server name in response, got %q", body)
		}
		if !strings.Contains(body, `"tools":true`) {
			t.Errorf("expected tools capability in response, got %q", body)
		}
	})

	t.Run("does not serve .well-known/mcp when not configured", func(t *testing.T) {
		transport := NewHTTP(":0")

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, nil
		})
		httpHandler := transport.createHandler(handler)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil)
		rec := httptest.NewRecorder()

		httpHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}
