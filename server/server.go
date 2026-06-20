// Package server provides the core MCP server implementation.
package server

import (
	"context"
	"sync"

	"go.klarlabs.de/mcp/protocol"
)

// Icon represents an icon for UI display.
type Icon struct {
	URI      string `json:"uri"`                // data URI or https URL
	MimeType string `json:"mimeType,omitempty"` // image/png, image/svg+xml, etc.
	Size     int    `json:"size,omitempty"`     // pixel size (icons are square)
}

// BuildInfo contains build metadata for debugging and version verification.
// This is an extension beyond the MCP spec.
type BuildInfo struct {
	Commit    string `json:"commit,omitempty"`    // Git commit SHA
	BuildDate string `json:"buildDate,omitempty"` // Build timestamp
}

// Info contains server metadata exposed to clients.
type Info struct {
	Name         string       // required - programmatic identifier
	Version      string       // required - semantic version
	Title        string       // optional - human-readable display name
	Description  string       // optional - what this server does
	WebsiteURL   string       // optional - docs/homepage link
	Icons        []Icon       // optional - for UI display
	BuildInfo    *BuildInfo   // optional - build metadata (extension)
	Capabilities Capabilities // declares what features the server supports
}

// Capabilities declares what features the server supports.
type Capabilities struct {
	Tools       bool
	Resources   bool
	Prompts     bool
	Completions bool
	// ResourceSubscribe advertises resources.subscribe: clients may subscribe
	// to a resource URI and receive notifications/resources/updated when it
	// changes. Requires a transport with server push (HTTP+SSE).
	ResourceSubscribe bool
}

// Manifest represents the server manifest returned to clients.
type Manifest struct {
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Title           string       `json:"title,omitempty"`
	Description     string       `json:"description,omitempty"`
	WebsiteURL      string       `json:"websiteUrl,omitempty"`
	Icons           []Icon       `json:"icons,omitempty"`
	BuildInfo       *BuildInfo   `json:"buildInfo,omitempty"`
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
}

// ToolInfo represents metadata about a registered tool.
type ToolInfo struct {
	Name         string
	Description  string
	InputSchema  any
	OutputSchema any
	Annotations  *ToolAnnotations
	Meta         map[string]any
}

// Option configures a Server.
type Option func(*Server)

// Server is the MCP server instance.
type Server struct {
	mu sync.RWMutex

	info         Info
	instructions string
	tools        map[string]*Tool
	resources    map[string]*Resource
	prompts      map[string]*Prompt
	middleware   []Middleware
	completions  *completionRegistry
	tasks        *TaskManager
	resourceSubs *resourceSubscriptions
}

// New creates a new MCP server with the given info and options.
func New(info Info, opts ...Option) *Server {
	s := &Server{
		info:         info,
		tools:        make(map[string]*Tool),
		resources:    make(map[string]*Resource),
		prompts:      make(map[string]*Prompt),
		resourceSubs: newResourceSubscriptions(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ResourceSubscriptionsEnabled reports whether the server advertises and
// handles resource subscriptions.
func (s *Server) ResourceSubscriptionsEnabled() bool {
	return s.info.Capabilities.ResourceSubscribe
}

// SetResourceNotifier wires the transport's server-push mechanism so the
// server can deliver resources/updated notifications to subscribed clients.
// Transports with server push (HTTP+SSE) call this during Serve.
func (s *Server) SetResourceNotifier(n ResourceNotifier) {
	s.resourceSubs.setNotifier(n)
}

// SubscribeResource records that a client is interested in a resource URI.
func (s *Server) SubscribeResource(clientID, uri string) {
	s.resourceSubs.subscribe(clientID, uri)
}

// UnsubscribeResource drops a client's interest in a resource URI.
func (s *Server) UnsubscribeResource(clientID, uri string) {
	s.resourceSubs.unsubscribe(clientID, uri)
}

// RemoveClientSubscriptions drops every subscription a client held — call it
// when the client's connection closes.
func (s *Server) RemoveClientSubscriptions(clientID string) {
	s.resourceSubs.removeClient(clientID)
}

// NotifyResourceUpdated pushes a notifications/resources/updated to every
// client subscribed to uri. Safe to call from any goroutine (e.g. a file
// watcher). A no-op when no notifier is wired.
func (s *Server) NotifyResourceUpdated(uri string) error {
	return s.resourceSubs.notifyUpdated(uri)
}

// WithInstructions sets the server instructions that provide context to AI models
// about how to use this server effectively.
func WithInstructions(instructions string) Option {
	return func(s *Server) {
		s.instructions = instructions
	}
}

// WithTitle sets a human-readable display name for the server.
func WithTitle(title string) Option {
	return func(s *Server) {
		s.info.Title = title
	}
}

// WithDescription sets a description of what the server does.
func WithDescription(description string) Option {
	return func(s *Server) {
		s.info.Description = description
	}
}

// WithWebsiteURL sets the server's documentation or homepage URL.
func WithWebsiteURL(url string) Option {
	return func(s *Server) {
		s.info.WebsiteURL = url
	}
}

// WithIcons sets the icons for UI display.
func WithIcons(icons ...Icon) Option {
	return func(s *Server) {
		s.info.Icons = icons
	}
}

// WithBuildInfo sets build metadata for debugging and version verification.
func WithBuildInfo(commit, buildDate string) Option {
	return func(s *Server) {
		s.info.BuildInfo = &BuildInfo{
			Commit:    commit,
			BuildDate: buildDate,
		}
	}
}

// Instructions returns the server instructions.
func (s *Server) Instructions() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instructions
}

// Info returns the server info.
func (s *Server) Info() Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

// Use registers middleware to be executed on every request.
func (s *Server) Use(middleware ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middleware = append(s.middleware, middleware...)
}

// Tool starts building a new tool with the given name.
func (s *Server) Tool(name string) *ToolBuilder {
	return &ToolBuilder{
		tool: &Tool{
			name: name,
		},
		server: s,
	}
}

// Tools returns info about all registered tools.
func (s *Server) Tools() []ToolInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ToolInfo, 0, len(s.tools))
	for _, t := range s.tools {
		result = append(result, ToolInfo{
			Name:         t.name,
			Description:  t.description,
			InputSchema:  t.inputSchema,
			OutputSchema: t.outputSchema,
			Annotations:  t.annotations,
			Meta:         t.meta,
		})
	}
	return result
}

// ListTools returns info about all registered tools. It is an alias of Tools
// matching the MCP introspection name used by the client side.
func (s *Server) ListTools() []ToolInfo {
	return s.Tools()
}

// Manifest returns the server manifest for MCP initialization.
func (s *Server) Manifest() Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Manifest{
		Name:            s.info.Name,
		Version:         s.info.Version,
		Title:           s.info.Title,
		Description:     s.info.Description,
		WebsiteURL:      s.info.WebsiteURL,
		Icons:           s.info.Icons,
		BuildInfo:       s.info.BuildInfo,
		ProtocolVersion: protocol.MCPVersion,
		Capabilities:    s.info.Capabilities,
	}
}

// registerTool adds a tool to the server.
func (s *Server) registerTool(t *Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.name] = t
}

// GetTool retrieves a tool by name.
func (s *Server) GetTool(name string) (*Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

// RemoveTool removes a tool by name. Returns true if the tool existed.
func (s *Server) RemoveTool(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tools[name]
	if ok {
		delete(s.tools, name)
	}
	return ok
}

// Resource starts building a new resource with the given URI template.
func (s *Server) Resource(uriTemplate string) *ResourceBuilder {
	return &ResourceBuilder{
		resource: &Resource{
			uriTemplate: uriTemplate,
		},
		server: s,
	}
}

// Resources returns info about all registered resources.
func (s *Server) Resources() []ResourceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ResourceInfo, 0, len(s.resources))
	for _, r := range s.resources {
		result = append(result, ResourceInfo{
			URITemplate: r.uriTemplate,
			Name:        r.name,
			Description: r.description,
			MimeType:    r.mimeType,
			Annotations: r.annotations,
		})
	}
	return result
}

// registerResource adds a resource to the server.
func (s *Server) registerResource(r *Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[r.uriTemplate] = r
}

// GetResource retrieves a resource by URI template.
func (s *Server) GetResource(uriTemplate string) (*Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.resources[uriTemplate]
	return r, ok
}

// RemoveResource removes a resource by URI template. Returns true if it existed.
func (s *Server) RemoveResource(uriTemplate string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.resources[uriTemplate]
	if ok {
		delete(s.resources, uriTemplate)
	}
	return ok
}

// FindResourceForURI finds a resource that matches the given URI.
func (s *Server) FindResourceForURI(uri string) (*Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.resources {
		if _, ok := r.matchURI(uri); ok {
			return r, true
		}
	}
	return nil, false
}

// Prompt starts building a new prompt with the given name.
func (s *Server) Prompt(name string) *PromptBuilder {
	return &PromptBuilder{
		prompt: &Prompt{
			name: name,
		},
		server: s,
	}
}

// Prompts returns info about all registered prompts.
func (s *Server) Prompts() []PromptInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]PromptInfo, 0, len(s.prompts))
	for _, p := range s.prompts {
		result = append(result, PromptInfo{
			Name:        p.name,
			Description: p.description,
			Arguments:   p.arguments,
			Annotations: p.annotations,
		})
	}
	return result
}

// registerPrompt adds a prompt to the server.
func (s *Server) registerPrompt(p *Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[p.name] = p
}

// GetPrompt retrieves a prompt by name.
func (s *Server) GetPrompt(name string) (*Prompt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prompts[name]
	return p, ok
}

// RemovePrompt removes a prompt by name. Returns true if it existed.
func (s *Server) RemovePrompt(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.prompts[name]
	if ok {
		delete(s.prompts, name)
	}
	return ok
}

// PromptCompletion starts building a completion handler for a prompt.
func (s *Server) PromptCompletion(name string) *PromptCompletionBuilder {
	return &PromptCompletionBuilder{
		name:   name,
		server: s,
	}
}

// ResourceCompletion starts building a completion handler for a resource.
func (s *Server) ResourceCompletion(uriTemplate string) *ResourceCompletionBuilder {
	return &ResourceCompletionBuilder{
		uriTemplate: uriTemplate,
		server:      s,
	}
}

// HandleCompletion processes a completion request.
func (s *Server) HandleCompletion(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
	s.mu.RLock()
	completions := s.completions
	s.mu.RUnlock()

	if completions == nil {
		return &CompletionResult{
			Values:  []string{},
			Total:   0,
			HasMore: false,
		}, nil
	}

	return completions.Handle(ctx, ref, arg)
}

// ResourceTemplates returns info about all registered resource templates.
func (s *Server) ResourceTemplates() []ResourceTemplateInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ResourceTemplateInfo, 0, len(s.resources))
	for _, r := range s.resources {
		// Only include resources with URI templates (containing {})
		if isTemplate(r.uriTemplate) {
			result = append(result, ResourceTemplateInfo{
				URITemplate: r.uriTemplate,
				Name:        r.name,
				Description: r.description,
				MimeType:    r.mimeType,
				Annotations: r.annotations,
			})
		}
	}
	return result
}

// isTemplate checks if a URI contains template parameters.
func isTemplate(uri string) bool {
	for i := 0; i < len(uri); i++ {
		if uri[i] == '{' {
			return true
		}
	}
	return false
}
