package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// Server Card media types and schema URI (SEP-2127 / experimental-ext-server-card).
const (
	ServerCardSchemaURI  = "https://static.modelcontextprotocol.io/schemas/v1/server-card.schema.json"
	ServerCardMediaType  = "application/mcp-server-card+json"
	AICatalogMediaType   = "application/ai-catalog+json"
	AICatalogSpecVersion = "1.0"
	serverCardPathSuffix = "/server-card"
	aiCatalogWellKnown   = "/.well-known/ai-catalog.json"
	mcpCatalogWellKnown  = "/.well-known/mcp/catalog.json"
	remoteStreamableHTTP = "streamable-http"
	remoteSSE            = "sse"
)

// ServerCard is a SEP-2127 static metadata document describing a remote MCP
// server for pre-connection discovery. It is advisory: clients must reconcile
// it against server/discover after connecting.
type ServerCard struct {
	Schema      string         `json:"$schema"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Title       string         `json:"title,omitempty"`
	WebsiteURL  string         `json:"websiteUrl,omitempty"`
	Repository  *Repository    `json:"repository,omitempty"`
	Icons       []server.Icon  `json:"icons,omitempty"`
	Remotes     []Remote       `json:"remotes,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`

	// catalogIdentifier is the AI Catalog entry identifier (urn:air:...).
	// Empty means derive one from Name at serve time.
	catalogIdentifier string
}

// Repository metadata for the MCP server source code.
type Repository struct {
	URL       string `json:"url"`
	Source    string `json:"source"`
	Subfolder string `json:"subfolder,omitempty"`
	ID        string `json:"id,omitempty"`
}

// Remote describes an HTTP-based MCP endpoint on a Server Card.
type Remote struct {
	Type                      string                 `json:"type"`
	URL                       string                 `json:"url"`
	Headers                   []KeyValueInput        `json:"headers,omitempty"`
	Variables                 map[string]RemoteInput `json:"variables,omitempty"`
	SupportedProtocolVersions []string               `json:"supportedProtocolVersions,omitempty"`
}

// RemoteInput describes a configuration variable or header value.
type RemoteInput struct {
	Description string   `json:"description,omitempty"`
	IsRequired  bool     `json:"isRequired,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty"`
	Format      string   `json:"format,omitempty"`
	Default     string   `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Value       string   `json:"value,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

// KeyValueInput is a named RemoteInput (typically an HTTP header).
type KeyValueInput struct {
	Name string `json:"name"`
	RemoteInput
	Variables map[string]RemoteInput `json:"variables,omitempty"`
}

// ServerCardOption configures a ServerCard.
type ServerCardOption func(*ServerCard)

// WithServerCardName sets the reverse-DNS card name (namespace/name).
func WithServerCardName(name string) ServerCardOption {
	return func(c *ServerCard) { c.Name = name }
}

// WithServerCardRemote appends a remote endpoint.
func WithServerCardRemote(remote Remote) ServerCardOption {
	return func(c *ServerCard) { c.Remotes = append(c.Remotes, remote) }
}

// WithServerCardRepository sets repository metadata.
func WithServerCardRepository(repo Repository) ServerCardOption {
	return func(c *ServerCard) { c.Repository = &repo }
}

// WithServerCardMeta sets namespaced _meta on the card.
func WithServerCardMeta(meta map[string]any) ServerCardOption {
	return func(c *ServerCard) { c.Meta = meta }
}

// WithServerCardCatalogIdentifier sets the AI Catalog entry identifier.
func WithServerCardCatalogIdentifier(id string) ServerCardOption {
	return func(c *ServerCard) { c.catalogIdentifier = id }
}

// NewServerCard builds a SEP-2127 Server Card from a server Manifest.
// If Name is not reverse-DNS (namespace/name), it is rewritten as local/{name}.
func NewServerCard(manifest *server.Manifest, opts ...ServerCardOption) *ServerCard {
	desc := manifest.Description
	if desc == "" {
		desc = manifest.Name
	}
	if len(desc) > 100 {
		desc = desc[:100]
	}
	c := &ServerCard{
		Schema:      ServerCardSchemaURI,
		Name:        cardName(manifest.Name),
		Version:     manifest.Version,
		Description: desc,
		Title:       manifest.Title,
		WebsiteURL:  manifest.WebsiteURL,
		Icons:       manifest.Icons,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.Name != "" {
		c.Name = cardName(c.Name)
	}
	return c
}

// NewServerCardFromDiscovery builds a Server Card from an existing
// ServerDiscovery document, including remotes derived from Endpoints.
func NewServerCardFromDiscovery(d *ServerDiscovery, opts ...ServerCardOption) *ServerCard {
	if d == nil {
		return NewServerCard(&server.Manifest{Name: "local/unknown", Version: "0.0.0"}, opts...)
	}
	manifest := &server.Manifest{
		Name:        d.Server.Name,
		Version:     d.Server.Version,
		Title:       d.Server.Title,
		Description: d.Server.Description,
		WebsiteURL:  d.Server.WebsiteURL,
		Icons:       d.Server.Icons,
	}
	cardOpts := make([]ServerCardOption, 0, len(opts)+4)
	versions := supportedVersionsForCard(d.MCPPVersion)
	if ep := d.Endpoints.StreamableHTTP; ep != "" {
		cardOpts = append(cardOpts, WithServerCardRemote(Remote{
			Type:                      remoteStreamableHTTP,
			URL:                       ep,
			SupportedProtocolVersions: versions,
		}))
	}
	if ep := d.Endpoints.SSE; ep != "" {
		cardOpts = append(cardOpts, WithServerCardRemote(Remote{
			Type:                      remoteSSE,
			URL:                       ep,
			SupportedProtocolVersions: versions,
		}))
	}
	cardOpts = append(cardOpts, opts...)
	return NewServerCard(manifest, cardOpts...)
}

func supportedVersionsForCard(primary string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(protocol.SupportedVersions)+1)
	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	add(primary)
	for _, v := range protocol.SupportedVersions {
		add(v)
	}
	return out
}

func cardName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "local/unnamed"
	}
	if strings.Contains(name, "/") {
		return name
	}
	return "local/" + name
}

// ServeHTTP serves the Server Card document with the SEP-2127 media type,
// CORS headers for browser clients, and Cache-Control / ETag validators.
func (c *ServerCard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, etag, ok := c.encodeCached()
	if !ok {
		http.Error(w, "server card unavailable", http.StatusInternalServerError)
		return
	}
	setCardCORS(w)
	w.Header().Set("Content-Type", ServerCardMediaType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// CatalogEntry is one AI Catalog entry pointing at (or embedding) a Server Card.
type CatalogEntry struct {
	Identifier string          `json:"identifier"`
	Type       string          `json:"type"`
	URL        string          `json:"url,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

// AICatalog is the domain-level discovery document that lists Server Cards.
type AICatalog struct {
	SpecVersion string         `json:"specVersion"`
	Entries     []CatalogEntry `json:"entries"`
}

// ServeCatalog writes an AI Catalog that points at this card's URL.
// cardURL should be absolute when known; relative paths are accepted.
func (c *ServerCard) ServeCatalog(w http.ResponseWriter, r *http.Request, cardURL string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cardURL == "" {
		cardURL = absoluteURL(r, "/mcp"+serverCardPathSuffix)
	}
	id := c.catalogIdentifier
	if id == "" {
		id = catalogIdentifierFor(c.Name, r.Host)
	}
	doc := AICatalog{
		SpecVersion: AICatalogSpecVersion,
		Entries: []CatalogEntry{{
			Identifier: id,
			Type:       ServerCardMediaType,
			URL:        cardURL,
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		http.Error(w, "catalog unavailable", http.StatusInternalServerError)
		return
	}
	etag := etagOf(body)
	setCardCORS(w)
	w.Header().Set("Content-Type", AICatalogMediaType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func (c *ServerCard) encodeCached() ([]byte, string, bool) {
	body, err := json.Marshal(c)
	if err != nil {
		return nil, "", false
	}
	return body, etagOf(body), true
}

func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func setCardCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match")
	w.Header().Set("Access-Control-Expose-Headers", "ETag")
}

func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if host == "" {
		return path
	}
	return scheme + "://" + host + path
}

func catalogIdentifierFor(cardName, host string) string {
	ns, name, ok := strings.Cut(cardName, "/")
	if !ok {
		ns, name = "local", cardName
	}
	publisher := host
	if publisher == "" {
		publisher = ns
	}
	publisher = strings.TrimPrefix(publisher, "www.")
	return "urn:air:" + publisher + ":mcp:" + name
}
