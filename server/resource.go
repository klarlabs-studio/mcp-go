package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ResourceContent represents the content returned by a resource read.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // Base64 encoded binary data
}

// ResourceHandler is the function signature for resource handlers.
type ResourceHandler func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error)

// Resource represents a readable resource exposed via MCP.
type Resource struct {
	uriTemplate string
	name        string
	description string
	mimeType    string
	handler     ResourceHandler
	annotations *ResourceAnnotations

	// Compiled regex for URI matching
	uriRegex   *regexp.Regexp
	paramNames []string
}

// ResourceInfo represents metadata about a registered resource.
type ResourceInfo struct {
	URITemplate string
	Name        string
	Description string
	MimeType    string
	Annotations *ResourceAnnotations
}

// ResourceTemplateInfo represents metadata about a resource template.
type ResourceTemplateInfo struct {
	URITemplate string
	Name        string
	Description string
	MimeType    string
	Annotations *ResourceAnnotations
}

// ResourceBuilder provides a fluent API for building resources.
type ResourceBuilder struct {
	resource *Resource
	server   *Server
	err      error
}

// Name sets an optional human-readable name for the resource.
func (b *ResourceBuilder) Name(name string) *ResourceBuilder {
	if b.err != nil {
		return b
	}
	b.resource.name = name
	return b
}

// Description sets the resource description.
func (b *ResourceBuilder) Description(desc string) *ResourceBuilder {
	if b.err != nil {
		return b
	}
	b.resource.description = desc
	return b
}

// MimeType sets the MIME type of the resource content.
func (b *ResourceBuilder) MimeType(mimeType string) *ResourceBuilder {
	if b.err != nil {
		return b
	}
	b.resource.mimeType = mimeType
	return b
}

// Handler sets the resource handler function.
func (b *ResourceBuilder) Handler(fn ResourceHandler) *ResourceBuilder {
	if b.err != nil {
		return b
	}

	b.resource.handler = fn

	// Compile URI template to regex
	if err := b.resource.compileTemplate(); err != nil {
		b.err = err
		return b
	}

	b.server.registerResource(b.resource)
	return b
}

// compileTemplate converts a URI template to a regex for matching.
func (r *Resource) compileTemplate() error {
	// Extract parameter names and build regex
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(r.uriTemplate, -1)

	r.paramNames = make([]string, 0, len(matches))
	for _, match := range matches {
		r.paramNames = append(r.paramNames, match[1])
	}

	// Escape special regex characters and replace {param} with capture groups
	pattern := regexp.QuoteMeta(r.uriTemplate)
	pattern = strings.ReplaceAll(pattern, `\{`, "{")
	pattern = strings.ReplaceAll(pattern, `\}`, "}")
	pattern = paramRegex.ReplaceAllString(pattern, `([^/]+)`)
	pattern = "^" + pattern + "$"

	var err error
	r.uriRegex, err = regexp.Compile(pattern)
	return err
}

// Read executes the resource handler for the given URI.
func (r *Resource) Read(ctx context.Context, uri string) (*ResourceContent, error) {
	params, ok := matchURI(r.uriTemplate, uri)
	if !ok {
		return nil, fmt.Errorf("URI %q does not match template %q", uri, r.uriTemplate)
	}

	return r.handler(ctx, uri, params)
}

// matchURI matches a URI against a template and extracts parameters.
func matchURI(template, uri string) (map[string]string, bool) {
	// Extract parameter names
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(template, -1)

	paramNames := make([]string, 0, len(matches))
	for _, match := range matches {
		paramNames = append(paramNames, match[1])
	}

	// Build regex pattern
	pattern := regexp.QuoteMeta(template)
	pattern = strings.ReplaceAll(pattern, `\{`, "{")
	pattern = strings.ReplaceAll(pattern, `\}`, "}")
	pattern = paramRegex.ReplaceAllString(pattern, `([^/]+)`)
	pattern = "^" + pattern + "$"

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false
	}

	// Match URI
	uriMatches := re.FindStringSubmatch(uri)
	if uriMatches == nil {
		return nil, false
	}

	// Extract parameter values
	params := make(map[string]string)
	for i, name := range paramNames {
		if i+1 < len(uriMatches) {
			params[name] = uriMatches[i+1]
		}
	}

	return params, true
}
