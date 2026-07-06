package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// templateParamRegex matches {param} placeholders in URI templates.
var templateParamRegex = regexp.MustCompile(`\{([^}]+)\}`)

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

// URITemplate returns the URI template the resource was registered
// under (e.g. "data://info" or "users://{id}").
func (r *Resource) URITemplate() string { return r.uriTemplate }

// Name returns the resource's human-readable name. Empty when the
// builder never set one.
func (r *Resource) Name() string { return r.name }

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
	// Extract parameter names using pre-compiled regex
	matches := templateParamRegex.FindAllStringSubmatch(r.uriTemplate, -1)

	r.paramNames = make([]string, 0, len(matches))
	for _, match := range matches {
		r.paramNames = append(r.paramNames, match[1])
	}

	// Escape special regex characters and replace {param} with capture groups
	pattern := regexp.QuoteMeta(r.uriTemplate)
	pattern = strings.ReplaceAll(pattern, `\{`, "{")
	pattern = strings.ReplaceAll(pattern, `\}`, "}")
	pattern = templateParamRegex.ReplaceAllString(pattern, `([^/]+)`)
	pattern = "^" + pattern + "$"

	var err error
	r.uriRegex, err = regexp.Compile(pattern)
	return err
}

// Read executes the resource handler for the given URI.
func (r *Resource) Read(ctx context.Context, uri string) (*ResourceContent, error) {
	params, ok := r.matchURI(uri)
	if !ok {
		return nil, fmt.Errorf("URI %q does not match template %q", uri, r.uriTemplate)
	}

	return r.handler(ctx, uri, params)
}

// matchURITemplate matches a URI against a template string.
// This compiles the template regex on each call and is intended for infrequent
// operations like completion where we don't have a pre-compiled Resource.
func matchURITemplate(template, uri string) (map[string]string, bool) {
	// Extract parameter names
	matches := templateParamRegex.FindAllStringSubmatch(template, -1)
	paramNames := make([]string, 0, len(matches))
	for _, match := range matches {
		paramNames = append(paramNames, match[1])
	}

	// Build regex pattern
	pattern := regexp.QuoteMeta(template)
	pattern = strings.ReplaceAll(pattern, `\{`, "{")
	pattern = strings.ReplaceAll(pattern, `\}`, "}")
	pattern = templateParamRegex.ReplaceAllString(pattern, `([^/]+)`)
	pattern = "^" + pattern + "$"

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false
	}

	uriMatches := re.FindStringSubmatch(uri)
	if uriMatches == nil {
		return nil, false
	}

	params := make(map[string]string)
	for i, name := range paramNames {
		if i+1 < len(uriMatches) {
			params[name] = uriMatches[i+1]
		}
	}

	return params, true
}

// matchURI matches a URI against the pre-compiled template regex and extracts parameters.
func (r *Resource) matchURI(uri string) (map[string]string, bool) {
	if r.uriRegex == nil {
		return nil, false
	}

	matches := r.uriRegex.FindStringSubmatch(uri)
	if matches == nil {
		return nil, false
	}

	params := make(map[string]string)
	for i, name := range r.paramNames {
		if i+1 < len(matches) {
			params[name] = matches[i+1]
		}
	}

	return params, true
}

// countTemplateParams returns the number of {param} placeholders in a template.
func countTemplateParams(tmpl string) int {
	return len(templateParamRegex.FindAllStringIndex(tmpl, -1))
}

// literalPrefixLen returns the length of the literal prefix of a template —
// the run of characters before the first {param} placeholder. A concrete URI
// with no parameters has a prefix equal to its whole length.
func literalPrefixLen(tmpl string) int {
	if i := strings.IndexByte(tmpl, '{'); i >= 0 {
		return i
	}
	return len(tmpl)
}

// literalLen returns the number of literal (non-placeholder) characters in a
// template, used as a tiebreak when two templates share the same parameter
// count and literal prefix.
func literalLen(tmpl string) int {
	return len(templateParamRegex.ReplaceAllString(tmpl, ""))
}

// moreSpecific reports whether template a is strictly more specific than
// template b, giving a total, deterministic ordering for most-specific-wins
// dispatch. An exact/literal template (no parameters) always beats a
// parameterized one; among templates the tie-break order is: fewer parameters,
// then longer literal prefix, then more literal characters, then the lexically
// smaller template. The final lexical tie-break guarantees a stable result
// regardless of registration or map-iteration order.
func moreSpecific(a, b string) bool {
	if pa, pb := countTemplateParams(a), countTemplateParams(b); pa != pb {
		return pa < pb
	}
	if la, lb := literalPrefixLen(a), literalPrefixLen(b); la != lb {
		return la > lb
	}
	if la, lb := literalLen(a), literalLen(b); la != lb {
		return la > lb
	}
	return a < b
}

// selectResource returns the most specific resource whose template matches uri,
// deterministically. It iterates a sorted copy of the template keys (never the
// randomized map) and keeps the most specific match per moreSpecific, so
// overlapping templates always resolve to the same resource.
func selectResource(resources map[string]*Resource, uri string) (*Resource, bool) {
	keys := make([]string, 0, len(resources))
	for k := range resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var best *Resource
	for _, k := range keys {
		r := resources[k]
		if _, ok := r.matchURI(uri); !ok {
			continue
		}
		if best == nil || moreSpecific(r.uriTemplate, best.uriTemplate) {
			best = r
		}
	}
	return best, best != nil
}

// mostSpecificMatchingTemplate returns the most specific template from
// templates that matches uri, deterministically (see moreSpecific). It is used
// where only template strings are available (e.g. completion handlers keyed by
// template) rather than pre-compiled resources.
func mostSpecificMatchingTemplate(templates []string, uri string) (string, bool) {
	sorted := make([]string, len(templates))
	copy(sorted, templates)
	sort.Strings(sorted)

	best := ""
	found := false
	for _, t := range sorted {
		if _, ok := matchURITemplate(t, uri); !ok {
			continue
		}
		if !found || moreSpecific(t, best) {
			best = t
			found = true
		}
	}
	return best, found
}

// ContainedPath safely resolves an untrusted resource parameter to a filesystem
// path guaranteed to stay within root, for file-style resources whose {param}
// maps onto a path under a base directory.
//
// IMPORTANT: resource parameters are untrusted and arrive URL-undecoded. A
// {param} capture group forbids "/", but a value of ".." — or a percent-encoded
// or otherwise crafted segment that a handler decodes — can still escape the
// base directory. Handlers that turn a parameter into a path MUST route it
// through this helper (or an equivalent check) before touching the filesystem;
// the framework does not do so automatically.
//
// The parameter must be relative (an absolute value is rejected). The joined
// path is cleaned and checked for lexical containment under root, then symlinks
// are resolved and containment is re-checked so a symlink inside root cannot
// point outside it. root must exist. On success it returns the cleaned,
// symlink-resolved absolute path; on any escape it returns a non-nil error.
func ContainedPath(root, param string) (string, error) {
	if param == "" {
		return "", fmt.Errorf("empty path parameter")
	}
	if filepath.IsAbs(param) {
		return "", fmt.Errorf("path parameter %q must be relative to root %q", param, root)
	}

	cleaned := filepath.Clean(filepath.Join(root, param))
	if !withinRoot(root, cleaned) {
		return "", fmt.Errorf("path parameter %q escapes root %q", param, root)
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", root, err)
	}

	realPath, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		// A not-yet-existing target has nothing to resolve; the lexical
		// containment check above already guarantees it is under root.
		if os.IsNotExist(err) {
			return cleaned, nil
		}
		return "", fmt.Errorf("resolve path %q: %w", cleaned, err)
	}

	if !withinRoot(realRoot, realPath) {
		return "", fmt.Errorf("path parameter %q escapes root %q via symlink", param, root)
	}
	return realPath, nil
}

// withinRoot reports whether path is root itself or lies beneath it, using a
// lexical relative-path check (no filesystem access).
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
