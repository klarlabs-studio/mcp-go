package transport

import (
	"encoding/json"
	"net/http"

	"github.com/felixgeelhaar/mcp-go/server"
)

type ServerInfo struct {
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	WebsiteURL  string            `json:"websiteUrl,omitempty"`
	Icons       []server.Icon     `json:"icons,omitempty"`
	BuildInfo   *server.BuildInfo `json:"buildInfo,omitempty"`
}

type ServerCapabilities struct {
	Tools     bool `json:"tools,omitempty"`
	Resources bool `json:"resources,omitempty"`
	Prompts   bool `json:"prompts,omitempty"`
	Sampling  bool `json:"sampling,omitempty"`
	Roots     bool `json:"roots,omitempty"`
}

type ServerEndpoint struct {
	StreamableHTTP string `json:"streamableHttp,omitempty"`
	SSE            string `json:"sse,omitempty"`
	WebSocket      string `json:"websocket,omitempty"`
}

type AuthMethod string

const (
	AuthNone   AuthMethod = "none"
	AuthAPIKey AuthMethod = "api_key"
	AuthOAuth2 AuthMethod = "oauth2"
	AuthBearer AuthMethod = "bearer"
	AuthMTLS   AuthMethod = "mtls"
)

type ServerAuth struct {
	Required            bool         `json:"required,omitempty"`
	Methods             []AuthMethod `json:"methods,omitempty"`
	AuthorizationHeader string       `json:"authorizationHeader,omitempty"`
}

type ServerDiscovery struct {
	MCPPVersion    string             `json:"mcpVersion"`
	Server         ServerInfo         `json:"server"`
	Capabilities   ServerCapabilities `json:"capabilities"`
	Endpoints      ServerEndpoint     `json:"endpoints,omitempty"`
	Authentication *ServerAuth        `json:"authentication,omitempty"`
}

type DiscoveryOption func(*ServerDiscovery)

func WithDiscoveryEndpoints(endpoints ServerEndpoint) DiscoveryOption {
	return func(d *ServerDiscovery) {
		d.Endpoints = endpoints
	}
}

func WithDiscoveryAuth(auth ServerAuth) DiscoveryOption {
	return func(d *ServerDiscovery) {
		d.Authentication = &auth
	}
}

func NewServerDiscovery(manifest *server.Manifest, opts ...DiscoveryOption) *ServerDiscovery {
	d := &ServerDiscovery{
		MCPPVersion: manifest.ProtocolVersion,
		Server: ServerInfo{
			Name:        manifest.Name,
			Version:     manifest.Version,
			Title:       manifest.Title,
			Description: manifest.Description,
			WebsiteURL:  manifest.WebsiteURL,
			Icons:       manifest.Icons,
			BuildInfo:   manifest.BuildInfo,
		},
		Capabilities: ServerCapabilities{
			Tools:     manifest.Capabilities.Tools,
			Resources: manifest.Capabilities.Resources,
			Prompts:   manifest.Capabilities.Prompts,
		},
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

func (d *ServerDiscovery) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(d)
}
