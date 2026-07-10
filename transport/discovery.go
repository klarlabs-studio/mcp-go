package transport

import (
	"encoding/json"
	"net/http"

	"go.klarlabs.de/mcp/server"
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

// ServerAuth advertises how clients should authenticate with this MCP server.
//
// ADVERTISEMENT-ONLY: The OAuth 2.0 / OpenID Connect fields below are
// discovery pointers only. This library never handles tokens, performs
// OAuth flows, or validates credentials. It merely publishes metadata so
// spec-compliant clients (MCP spec 2025-06-18 / 2025-11-25) can locate the
// authorization server and its metadata documents. Token acquisition,
// validation, and enforcement remain entirely the responsibility of the
// server author and their identity provider.
type ServerAuth struct {
	Required            bool         `json:"required,omitempty"`
	Methods             []AuthMethod `json:"methods,omitempty"`
	AuthorizationHeader string       `json:"authorizationHeader,omitempty"`

	// AuthorizationServers lists issuer URLs of the OAuth 2.0 authorization
	// server(s) that can issue tokens for this resource, per RFC 9728
	// (OAuth 2.0 Protected Resource Metadata). Advertisement only.
	AuthorizationServers []string `json:"authorizationServers,omitempty"`

	// ProtectedResourceMetadata is the URL of this server's
	// /.well-known/oauth-protected-resource document (RFC 9728). Clients
	// fetch it to discover authorization servers and supported scopes.
	// Advertisement only; this library does not serve or validate it.
	ProtectedResourceMetadata string `json:"protectedResourceMetadata,omitempty"`

	// ResourceIndicator is the canonical resource identifier for this MCP
	// server, used by clients as the RFC 8707 `resource` parameter when
	// requesting tokens scoped to this server. Advertisement only.
	ResourceIndicator string `json:"resourceIndicator,omitempty"`

	// ScopesSupported lists the OAuth 2.0 scopes this server recognizes,
	// per RFC 9728. Advertisement only; scopes are not enforced here.
	ScopesSupported []string `json:"scopesSupported,omitempty"`

	// OIDCConfiguration is the URL of the authorization server's
	// .well-known/openid-configuration document (OpenID Connect Discovery).
	// Advertisement only; this library performs no token validation.
	OIDCConfiguration string `json:"oidcConfiguration,omitempty"`
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

// OAuthMetadata carries the OAuth 2.0 (RFC 9728) and OpenID Connect discovery
// pointers a server can advertise. This is advertisement-only metadata: the
// library never handles tokens, runs OAuth flows, or validates credentials.
type OAuthMetadata struct {
	AuthorizationServers      []string
	ProtectedResourceMetadata string
	ResourceIndicator         string
	ScopesSupported           []string
	OIDCConfiguration         string
}

// WithDiscoveryOAuthMetadata advertises OAuth 2.0 Protected Resource Metadata
// (RFC 9728) and OpenID Connect Discovery pointers on the authentication
// section. If no authentication section exists yet it is created with the
// oauth2 method; otherwise the existing section is enriched in place.
//
// ADVERTISEMENT-ONLY: these fields are discovery hints for spec-compliant
// clients. The library performs no token validation or OAuth flow handling.
func WithDiscoveryOAuthMetadata(meta OAuthMetadata) DiscoveryOption {
	return func(d *ServerDiscovery) {
		if d.Authentication == nil {
			d.Authentication = &ServerAuth{
				Required: true,
				Methods:  []AuthMethod{AuthOAuth2},
			}
		}
		d.Authentication.AuthorizationServers = meta.AuthorizationServers
		d.Authentication.ProtectedResourceMetadata = meta.ProtectedResourceMetadata
		d.Authentication.ResourceIndicator = meta.ResourceIndicator
		d.Authentication.ScopesSupported = meta.ScopesSupported
		d.Authentication.OIDCConfiguration = meta.OIDCConfiguration
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
