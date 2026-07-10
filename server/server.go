// Package server provides the core MCP server implementation.
package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.klarlabs.de/mcp/protocol"
)

// Icon represents an icon for UI display. A single Icon value serializes for
// both MCP icon shapes so callers do not need era-specific types:
//
//   - the legacy shape (2025-11-25, SEP-973) uses uri/mimeType/size;
//   - the modern shape (2026-07-28) uses src/sizes/theme.
//
// The two eras describe the same concepts under different names:
//
//	legacy URI  ≈ modern Src   (image URL or data URI)
//	legacy Size ≈ modern Sizes (48 ≈ "48x48"; modern also allows "any" and
//	                            multiple space-separated descriptors)
//	MimeType is shared by both eras.
//
// Populate the legacy fields, the modern fields, or both. NewIcon builds a
// modern icon; Normalize fills each era's fields from the other so a value
// authored for one era serializes sensibly for both. Legacy struct literals
// keep working unchanged.
type Icon struct {
	// URI is the legacy (2025-11-25) image source: a data URI or https URL.
	// Corresponds to the modern Src.
	URI string `json:"uri"`
	// MimeType is the image media type (image/png, image/svg+xml, ...). Shared
	// by both the legacy and modern shapes.
	MimeType string `json:"mimeType,omitempty"`
	// Size is the legacy square pixel size (icons are square). Corresponds to
	// the modern Sizes.
	Size int `json:"size,omitempty"`

	// Src is the modern (2026-07-28) image source: a data URI or https URL.
	// Corresponds to the legacy URI.
	Src string `json:"src,omitempty"`
	// Sizes is the modern space-separated size descriptor string, e.g.
	// "48x48 96x96" or "any". Corresponds to the legacy Size.
	Sizes string `json:"sizes,omitempty"`
	// Theme selects a light/dark variant of the icon: "light" or "dark". Modern
	// only; empty means the icon applies to any theme.
	Theme string `json:"theme,omitempty"`
}

// NewIcon constructs a modern (2026-07-28) icon with the given source URL or
// data URI. Chain WithMimeType, WithSizes, and WithTheme to add metadata, and
// call Normalize to also populate the legacy fields for back-compat.
func NewIcon(src string) Icon {
	return Icon{Src: src}
}

// WithMimeType returns a copy of the icon with the image media type set. The
// media type is shared by both the legacy and modern shapes.
func (i Icon) WithMimeType(mimeType string) Icon {
	i.MimeType = mimeType
	return i
}

// WithSizes returns a copy of the icon with the modern space-separated size
// descriptor string set, e.g. "48x48 96x96" or "any".
func (i Icon) WithSizes(sizes string) Icon {
	i.Sizes = sizes
	return i
}

// WithTheme returns a copy of the icon carrying a light/dark theme variant
// ("light" or "dark"). Icons may exist in per-theme variants; attach one theme
// per Icon value. Modern only.
func (i Icon) WithTheme(theme string) Icon {
	i.Theme = theme
	return i
}

// Normalize returns a copy of the icon with empty fields filled from their
// cross-era counterparts: URI and Src copy across verbatim, and Size and Sizes
// convert (Size 48 ≈ Sizes "48x48"; the first square "NxN" descriptor in Sizes
// ≈ Size N; "any" yields no pixel Size). It never overwrites a field that is
// already set, so an icon authored for one era serializes for both.
func (i Icon) Normalize() Icon {
	// Source URL: legacy URI <-> modern Src.
	if i.Src == "" {
		i.Src = i.URI
	}
	if i.URI == "" {
		i.URI = i.Src
	}
	// Size: legacy Size (pixels) <-> modern Sizes (descriptor string).
	if i.Sizes == "" && i.Size > 0 {
		i.Sizes = fmt.Sprintf("%dx%d", i.Size, i.Size)
	}
	if i.Size == 0 && i.Sizes != "" {
		i.Size = parseSquareSize(i.Sizes)
	}
	return i
}

// parseSquareSize extracts the pixel size from a modern size descriptor string
// such as "48x48" or "32x32 64x64", returning the width of the first square
// descriptor. It returns 0 for "any" or any value it cannot parse.
func parseSquareSize(sizes string) int {
	for _, field := range strings.Fields(sizes) {
		w, h, ok := strings.Cut(field, "x")
		if !ok {
			continue
		}
		width, err := strconv.Atoi(w)
		if err != nil {
			continue
		}
		height, err := strconv.Atoi(h)
		if err != nil || width != height {
			continue
		}
		return width
	}
	return 0
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
	// Logging advertises the logging capability: the server accepts
	// logging/setLevel and may emit notifications/message. Off by default so a
	// bare server advertises no capabilities.
	Logging bool
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
	Icons        []Icon
	TaskSupport  TaskSupport
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
	augTasks     *augTaskRegistry
	resourceSubs *resourceSubscriptions
	resultCache  *resultCacheConfig

	// regErrs accumulates registration collisions. The fluent builder API
	// returns the builder rather than an error, so a duplicate tool/resource/
	// prompt name (which is rejected, not silently overwritten) is recorded
	// here and surfaced via Err().
	regErrs []error
}

// New creates a new MCP server with the given info and options.
func New(info Info, opts ...Option) *Server {
	s := &Server{
		info:         info,
		tools:        make(map[string]*Tool),
		resources:    make(map[string]*Resource),
		prompts:      make(map[string]*Prompt),
		resourceSubs: newResourceSubscriptions(),
		augTasks:     newAugTaskRegistry(),
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

// Middleware returns a copy of the middleware registered via Use. The serve
// path composes this with any serve-scoped middleware (WithMiddleware). It is
// exported so the handler builder can read it; previously s.middleware was
// never consulted, silently dropping everything passed to Use.
func (s *Server) Middleware() []Middleware {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Middleware, len(s.middleware))
	copy(out, s.middleware)
	return out
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
			Icons:        t.icons,
			TaskSupport:  t.taskSupport,
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

// registerTool adds a tool to the server. A duplicate name is rejected (the
// first registration wins) and recorded on the server rather than silently
// overwriting the earlier tool; check Err() to surface the collision. To
// intentionally replace a tool, call RemoveTool first, then re-register.
func (s *Server) registerTool(t *Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tools[t.name]; exists {
		s.regErrs = append(s.regErrs,
			fmt.Errorf("duplicate tool registration: %q is already registered", t.name))
		return
	}
	s.tools[t.name] = t
}

// Err returns any errors accumulated while wiring up the server, joined into a
// single error (nil when there were none). Because the fluent builder API
// returns the builder — not an error — registration collisions (duplicate
// tool, resource, or prompt names) are recorded and reported here. Check it
// once after registering everything: if err := srv.Err(); err != nil { ... }.
func (s *Server) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return errors.Join(s.regErrs...)
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
			Title:       r.title,
			Description: r.description,
			MimeType:    r.mimeType,
			Annotations: r.annotations,
			Icons:       r.icons,
		})
	}
	return result
}

// registerResource adds a resource to the server. A duplicate URI template is
// rejected (the first registration wins) and recorded rather than silently
// overwriting the earlier resource; check Err() to surface the collision. To
// intentionally replace a resource, call RemoveResource first, then
// re-register.
func (s *Server) registerResource(r *Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.resources[r.uriTemplate]; exists {
		s.regErrs = append(s.regErrs,
			fmt.Errorf("duplicate resource registration: URI template %q is already registered", r.uriTemplate))
		return
	}
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

// FindResourceForURI finds the resource that matches the given URI.
//
// When several registered templates match the same concrete URI (e.g. the
// specific "config://database" and the catch-all "config://{key}"), selection
// is deterministic and most-specific-wins: an exact literal template beats any
// template with parameters, and among templates the one with the fewest
// parameters (then the longest literal prefix, then lexically smallest) is
// chosen. It never depends on Go's randomized map iteration order.
func (s *Server) FindResourceForURI(uri string) (*Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return selectResource(s.resources, uri)
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
			Title:       p.title,
			Description: p.description,
			Arguments:   p.arguments,
			Annotations: p.annotations,
			Icons:       p.icons,
		})
	}
	return result
}

// registerPrompt adds a prompt to the server. A duplicate name is rejected
// (the first registration wins) and recorded rather than silently overwriting
// the earlier prompt; check Err() to surface the collision. To intentionally
// replace a prompt, call RemovePrompt first, then re-register.
func (s *Server) registerPrompt(p *Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.prompts[p.name]; exists {
		s.regErrs = append(s.regErrs,
			fmt.Errorf("duplicate prompt registration: %q is already registered", p.name))
		return
	}
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

// AugTasks returns the task-augmented-request registry (MCP 2025-11-25). It is
// the store the dispatcher uses for tools/call task augmentation and the
// tasks/get|result|cancel|list operations.
func (s *Server) AugTasks() *augTaskRegistry { return s.augTasks }

// HasTaskTools reports whether any registered tool opts into task augmentation
// (TaskSupport optional or required), used to auto-advertise the server's tasks
// capability.
func (s *Server) HasTaskTools() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tools {
		if t.taskSupport == TaskSupportOptional || t.taskSupport == TaskSupportRequired {
			return true
		}
	}
	return false
}

// resultCacheConfig holds the cache hint stamped on cacheable results
// (tools/list, resources/list, resources/read, …) for modern clients.
type resultCacheConfig struct {
	ttlMs int64
	scope string
}

// WithResultCache sets the cache hint (ttlMs and cacheScope, "public" or
// "private") advertised on cacheable list/read results to modern (2026-07-28)
// clients via CacheableResult. Legacy responses are unaffected.
func WithResultCache(ttlMs int64, scope string) Option {
	return func(s *Server) {
		s.resultCache = &resultCacheConfig{ttlMs: ttlMs, scope: scope}
	}
}

// ResultCache returns the configured cache hint, or ok=false when none is set.
func (s *Server) ResultCache() (ttlMs int64, scope string, ok bool) {
	if s.resultCache == nil {
		return 0, "", false
	}
	return s.resultCache.ttlMs, s.resultCache.scope, true
}

// HasCompletions reports whether any prompt/resource completion handler has
// been registered, so the server can auto-advertise the completions capability
// even when the Capabilities.Completions flag was not set explicitly.
func (s *Server) HasCompletions() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.completions != nil
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
				Title:       r.title,
				Description: r.description,
				MimeType:    r.mimeType,
				Annotations: r.annotations,
				Icons:       r.icons,
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
