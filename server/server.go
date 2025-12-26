// Package server provides the core MCP server implementation.
package server

import (
	"context"
	"sync"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// Info contains server metadata exposed to clients.
type Info struct {
	Name         string
	Version      string
	Capabilities Capabilities
}

// Capabilities declares what features the server supports.
type Capabilities struct {
	Tools       bool
	Resources   bool
	Prompts     bool
	Completions bool
}

// Manifest represents the server manifest returned to clients.
type Manifest struct {
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
}

// ToolInfo represents metadata about a registered tool.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema any
	Annotations *ToolAnnotations
}

// Option configures a Server.
type Option func(*Server)

// Server is the MCP server instance.
type Server struct {
	mu sync.RWMutex

	info        Info
	tools       map[string]*Tool
	resources   map[string]*Resource
	prompts     map[string]*Prompt
	middleware  []Middleware
	completions *completionRegistry
}

// New creates a new MCP server with the given info and options.
func New(info Info, opts ...Option) *Server {
	s := &Server{
		info:      info,
		tools:     make(map[string]*Tool),
		resources: make(map[string]*Resource),
		prompts:   make(map[string]*Prompt),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
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
			Name:        t.name,
			Description: t.description,
			InputSchema: t.inputSchema,
			Annotations: t.annotations,
		})
	}
	return result
}

// Manifest returns the server manifest for MCP initialization.
func (s *Server) Manifest() Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Manifest{
		Name:            s.info.Name,
		Version:         s.info.Version,
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

// getTool retrieves a tool by name (internal).
func (s *Server) getTool(name string) (*Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

// GetTool retrieves a tool by name (public).
func (s *Server) GetTool(name string) (*Tool, bool) {
	return s.getTool(name)
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

// getResource retrieves a resource by URI template.
func (s *Server) getResource(uriTemplate string) (*Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.resources[uriTemplate]
	return r, ok
}

// GetResource retrieves a resource by URI template (public).
func (s *Server) GetResource(uriTemplate string) (*Resource, bool) {
	return s.getResource(uriTemplate)
}

// FindResourceForURI finds a resource that matches the given URI.
func (s *Server) FindResourceForURI(uri string) (*Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.resources {
		if _, ok := matchURI(r.uriTemplate, uri); ok {
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

// getPrompt retrieves a prompt by name.
func (s *Server) getPrompt(name string) (*Prompt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prompts[name]
	return p, ok
}

// GetPrompt retrieves a prompt by name (public).
func (s *Server) GetPrompt(name string) (*Prompt, bool) {
	return s.getPrompt(name)
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
