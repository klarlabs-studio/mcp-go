package server

import (
	"context"
)

// CompletionHandler handles completion requests for prompts or resources.
type CompletionHandler func(ctx context.Context, ref CompletionRef, argument CompletionArgument) (*CompletionResult, error)

// CompletionRef represents a reference to a prompt or resource for completion.
type CompletionRef struct {
	Type string `json:"type"` // "ref/prompt" or "ref/resource"
	Name string `json:"name,omitempty"` // For prompt references
	URI  string `json:"uri,omitempty"`  // For resource references
}

// CompletionArgument represents the argument being completed.
type CompletionArgument struct {
	Name  string `json:"name"`  // Argument name
	Value string `json:"value"` // Current partial value
}

// CompletionResult contains completion suggestions.
type CompletionResult struct {
	Values  []string `json:"values"`           // Suggested completions (max 100)
	Total   int      `json:"total,omitempty"`  // Total available matches
	HasMore bool     `json:"hasMore,omitempty"` // Whether more results exist
}

// CompletionRequest is the request for completion/complete.
type CompletionRequest struct {
	Ref      CompletionRef      `json:"ref"`
	Argument CompletionArgument `json:"argument"`
}

// CompletionResponse is the response for completion/complete.
type CompletionResponse struct {
	Completion CompletionResult `json:"completion"`
}

// completionRegistry stores completion handlers.
type completionRegistry struct {
	promptHandlers   map[string]CompletionHandler // keyed by prompt name
	resourceHandlers map[string]CompletionHandler // keyed by URI template
	defaultHandler   CompletionHandler
}

// newCompletionRegistry creates a new completion registry.
func newCompletionRegistry() *completionRegistry {
	return &completionRegistry{
		promptHandlers:   make(map[string]CompletionHandler),
		resourceHandlers: make(map[string]CompletionHandler),
	}
}

// RegisterPromptCompletion registers a completion handler for a prompt.
func (r *completionRegistry) RegisterPromptCompletion(name string, handler CompletionHandler) {
	r.promptHandlers[name] = handler
}

// RegisterResourceCompletion registers a completion handler for a resource.
func (r *completionRegistry) RegisterResourceCompletion(uriTemplate string, handler CompletionHandler) {
	r.resourceHandlers[uriTemplate] = handler
}

// SetDefaultHandler sets a default handler for unmatched completions.
func (r *completionRegistry) SetDefaultHandler(handler CompletionHandler) {
	r.defaultHandler = handler
}

// Handle processes a completion request.
func (r *completionRegistry) Handle(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
	var handler CompletionHandler

	switch ref.Type {
	case "ref/prompt":
		handler = r.promptHandlers[ref.Name]
	case "ref/resource":
		handler = r.resourceHandlers[ref.URI]
		// Try to match by URI template pattern if exact match fails
		if handler == nil {
			for template, h := range r.resourceHandlers {
				if _, ok := matchURI(template, ref.URI); ok {
					handler = h
					break
				}
			}
		}
	}

	if handler == nil {
		handler = r.defaultHandler
	}

	if handler == nil {
		// Return empty result if no handler
		return &CompletionResult{
			Values:  []string{},
			Total:   0,
			HasMore: false,
		}, nil
	}

	result, err := handler(ctx, ref, arg)
	if err != nil {
		return nil, err
	}

	// Enforce max 100 values per MCP spec
	if len(result.Values) > 100 {
		result.Values = result.Values[:100]
		result.HasMore = true
	}

	return result, nil
}

// PromptCompletionBuilder builds completion handlers for prompts.
type PromptCompletionBuilder struct {
	name    string
	server  *Server
	handler CompletionHandler
}

// Handler sets the completion handler.
func (b *PromptCompletionBuilder) Handler(fn CompletionHandler) {
	b.handler = fn
	if b.server.completions == nil {
		b.server.completions = newCompletionRegistry()
	}
	b.server.completions.RegisterPromptCompletion(b.name, fn)
}

// ResourceCompletionBuilder builds completion handlers for resources.
type ResourceCompletionBuilder struct {
	uriTemplate string
	server      *Server
	handler     CompletionHandler
}

// Handler sets the completion handler.
func (b *ResourceCompletionBuilder) Handler(fn CompletionHandler) {
	b.handler = fn
	if b.server.completions == nil {
		b.server.completions = newCompletionRegistry()
	}
	b.server.completions.RegisterResourceCompletion(b.uriTemplate, fn)
}
