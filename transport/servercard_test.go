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

func TestServerCard_ServeHTTP(t *testing.T) {
	manifest := &server.Manifest{
		Name:        "com.example/weather",
		Version:     "1.2.3",
		Description: "Weather forecasts",
		Title:       "Weather",
		WebsiteURL:  "https://example.com",
	}
	card := NewServerCard(manifest, WithServerCardRemote(Remote{
		Type:                      "streamable-http",
		URL:                       "https://example.com/mcp",
		SupportedProtocolVersions: []string{protocol.ModernVersion},
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp/server-card", nil)
	rec := httptest.NewRecorder()
	card.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ServerCardMediaType {
		t.Fatalf("Content-Type = %q, want %q", ct, ServerCardMediaType)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("missing CORS Allow-Origin")
	}
	if etag := rec.Header().Get("ETag"); etag == "" {
		t.Fatal("missing ETag")
	}

	var got ServerCard
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Schema != ServerCardSchemaURI {
		t.Errorf("$schema = %q", got.Schema)
	}
	if got.Name != "com.example/weather" {
		t.Errorf("name = %q", got.Name)
	}
	if len(got.Remotes) != 1 || got.Remotes[0].Type != "streamable-http" {
		t.Fatalf("remotes = %#v", got.Remotes)
	}

	// If-None-Match → 304
	req2 := httptest.NewRequest(http.MethodGet, "/mcp/server-card", nil)
	req2.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec2 := httptest.NewRecorder()
	card.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match status = %d, want 304", rec2.Code)
	}
}

func TestServerCard_LocalNameRewritten(t *testing.T) {
	card := NewServerCard(&server.Manifest{Name: "weather", Version: "1.0.0", Description: "x"})
	if card.Name != "local/weather" {
		t.Fatalf("name = %q, want local/weather", card.Name)
	}
}

func TestServerCard_FromDiscovery(t *testing.T) {
	d := NewServerDiscovery(&server.Manifest{
		Name:            "com.example/demo",
		Version:         "2.0.0",
		Description:     "Demo",
		ProtocolVersion: protocol.ModernVersion,
	}, WithDiscoveryEndpoints(ServerEndpoint{
		StreamableHTTP: "https://example.com/mcp",
		SSE:            "https://example.com/mcp/sse",
	}))
	card := NewServerCardFromDiscovery(d)
	if len(card.Remotes) != 2 {
		t.Fatalf("remotes = %d, want 2", len(card.Remotes))
	}
}

func TestServerCard_CatalogAndHTTPMount(t *testing.T) {
	card := NewServerCard(&server.Manifest{
		Name:        "com.example/demo",
		Version:     "1.0.0",
		Description: "Demo server",
	}, WithServerCardRemote(Remote{
		Type: "streamable-http",
		URL:  "https://example.com/mcp",
	}))

	h := NewHTTP(":0", WithStreamable(), WithServerCard(card))
	handler := h.createHandler(HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, map[string]any{}), nil
	}))

	t.Run("server-card", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp/server-card", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"$schema"`) {
			t.Fatalf("body missing $schema: %s", rec.Body.String())
		}
	})

	for _, path := range []string{aiCatalogWellKnown, mcpCatalogWellKnown} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != AICatalogMediaType {
				t.Fatalf("Content-Type = %q", ct)
			}
			var cat AICatalog
			if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if cat.SpecVersion != AICatalogSpecVersion || len(cat.Entries) != 1 {
				t.Fatalf("catalog = %#v", cat)
			}
			if cat.Entries[0].Type != ServerCardMediaType {
				t.Fatalf("entry type = %q", cat.Entries[0].Type)
			}
			if !strings.Contains(cat.Entries[0].URL, "/mcp/server-card") {
				t.Fatalf("entry url = %q", cat.Entries[0].URL)
			}
		})
	}
}
