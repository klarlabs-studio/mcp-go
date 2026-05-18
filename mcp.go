// Package mcp provides a framework for building MCP (Model Context Protocol) servers.
//
// mcp-go aims to be the "Gin framework" for MCP servers, providing typed handlers
// with automatic JSON Schema generation, Gin-style middleware chains, pluggable
// transports (stdio, HTTP+SSE, WebSocket), and production-ready defaults.
//
// # Handler Signatures
//
// Tool handlers: func(input T) (R, error) or func(ctx, input T) (R, error)
//
// Resource handlers receive URI and template params (e.g., "users://{id}"):
//
//	func(ctx, uri string, params map[string]string) (*ResourceContent, error)
//
// Prompt handlers: func(ctx, args map[string]string) (*PromptResult, error)
//
// # Progress Reporting
//
// Use ProgressFromContext in long-running handlers. Report is thread-safe.
// Errors are non-fatal and typically ignored.
//
// # Error Handling
//
// Return errors from handlers. Use protocol.NewInvalidParams, protocol.NewNotFound,
// etc. for specific MCP error codes.
//
// See examples/basic for a complete working example with tools, resources, and prompts.
package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mcp-go/middleware"
	"github.com/felixgeelhaar/mcp-go/protocol"
	"github.com/felixgeelhaar/mcp-go/server"
	"github.com/felixgeelhaar/mcp-go/transport"
	grpctransport "github.com/felixgeelhaar/mcp-go/transport/grpc"
)

// JSON field names used in MCP protocol payloads. Extracted as constants
// so map-literal builders share a single source of truth (and silence
// goconst).
const (
	fieldName            = "name"
	fieldVersion         = "version"
	fieldProtocolVersion = "protocolVersion"
	fieldListChanged     = "listChanged"
	fieldText            = "text"
)

// Re-export core types for convenience

// ServerInfo contains server metadata exposed to clients.
type ServerInfo = server.Info

// Capabilities declares what features the server supports.
type Capabilities = server.Capabilities

// Server is the MCP server instance.
type Server = server.Server

// Option configures a Server.
type Option = server.Option

// Resource types
type ResourceContent = server.ResourceContent
type ResourceInfo = server.ResourceInfo

// Prompt types
type PromptResult = server.PromptResult
type PromptMessage = server.PromptMessage
type PromptArgument = server.PromptArgument
type PromptInfo = server.PromptInfo
type TextContent = server.TextContent
type ImageContent = server.ImageContent

// ServerInfo metadata types
type Icon = server.Icon
type BuildInfo = server.BuildInfo

// Progress types for streaming tool responses
type ProgressToken = server.ProgressToken
type Progress = server.Progress
type ProgressReporter = server.ProgressReporter

// Annotation types for tools, resources, and prompts
type ToolAnnotations = server.ToolAnnotations
type ResourceAnnotations = server.ResourceAnnotations
type PromptAnnotations = server.PromptAnnotations

// Helper functions for annotation values
var (
	Bool  = server.Bool
	Float = server.Float
)

// Sampling types for server-initiated LLM completions
type SamplingMessage = server.SamplingMessage
type Role = server.Role
type Content = server.Content
type CreateMessageRequest = server.CreateMessageRequest
type CreateMessageResult = server.CreateMessageResult
type ModelPreferences = server.ModelPreferences
type ModelHint = server.ModelHint

// Role constants
const (
	RoleUser      = server.RoleUser
	RoleAssistant = server.RoleAssistant
)

// Content constructors
var (
	NewTextContent  = server.NewTextContent
	NewImageContent = server.NewImageContent
)

// Roots types for workspace awareness
type Root = server.Root
type ListRootsResult = server.ListRootsResult

// Logging types for MCP log messages
type LogLevel = server.LogLevel
type LoggingMessage = server.LoggingMessage
type SetLevelRequest = server.SetLevelRequest

// LogLevel constants
const (
	LogLevelDebug     = server.LogLevelDebug
	LogLevelInfo      = server.LogLevelInfo
	LogLevelNotice    = server.LogLevelNotice
	LogLevelWarning   = server.LogLevelWarning
	LogLevelError     = server.LogLevelError
	LogLevelCritical  = server.LogLevelCritical
	LogLevelAlert     = server.LogLevelAlert
	LogLevelEmergency = server.LogLevelEmergency
)

// ShouldLog returns true if a message at the given level should be logged
var ShouldLog = server.ShouldLog

// Cancellation types
type CancelledNotification = server.CancelledNotification
type CancellationManager = server.CancellationManager

var NewCancellationManager = server.NewCancellationManager

// Context utilities for cancellation
var (
	ContextWithCancellationManager = server.ContextWithCancellationManager
	CancellationManagerFromContext = server.CancellationManagerFromContext
)

// Subscription types for resource change notifications
type SubscribeRequest = server.SubscribeRequest
type UnsubscribeRequest = server.UnsubscribeRequest
type ResourceUpdatedNotification = server.ResourceUpdatedNotification
type SubscriptionManager = server.SubscriptionManager

var NewSubscriptionManager = server.NewSubscriptionManager

// Structured result types for tool responses
type StructuredResult = server.StructuredResult

// Elicitation types for interactive user prompts
type ElicitRequest = server.ElicitRequest
type ElicitResult = server.ElicitResult
type Elicitor = server.Elicitor

var (
	NewElicitor       = server.NewElicitor
	ElicitFromContext = server.ElicitFromContext
)

// Channel types for server-initiated push messages
type ChannelMessage = server.ChannelMessage
type ChannelSender = server.ChannelSender

var (
	NewChannelSender   = server.NewChannelSender
	ChannelFromContext = server.ChannelFromContext
)

// Completion types for autocomplete support
type CompletionRef = server.CompletionRef
type CompletionArgument = server.CompletionArgument
type CompletionResult = server.CompletionResult
type CompletionHandler = server.CompletionHandler

// Resource template types
type ResourceTemplateInfo = server.ResourceTemplateInfo

// Session types for bidirectional MCP communication
type Session = server.Session
type SessionOption = server.SessionOption
type RequestSender = server.RequestSender
type NotificationSender = server.NotificationSender
type ClientCapabilities = server.ClientCapabilities
type RootsCapability = server.RootsCapability

var (
	NewSession              = server.NewSession
	WithClientCapabilities  = server.WithClientCapabilities
	WithRootsChangeCallback = server.WithRootsChangeCallback
	ContextWithSession      = server.ContextWithSession
	SessionFromContext      = server.SessionFromContext
)

// ExtractParams extracts URI template parameters into a typed struct.
// Use this in resource handlers for type-safe parameter extraction.
//
// Example:
//
//	type UserParams struct {
//	    ID   string `uri:"id"`
//	    Page int    `uri:"page"`
//	}
//
//	srv.Resource("users://{id}/page/{page}").Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
//	    p, err := mcp.ExtractParams[UserParams](params)
//	    if err != nil {
//	        return nil, err
//	    }
//	    // Use p.ID (string) and p.Page (int)
//	})
func ExtractParams[T any](params map[string]string) (T, error) {
	return server.ExtractParams[T](params)
}

// ProgressFromContext returns the progress reporter from context.
// Use this in tool handlers to report progress for long-running operations.
//
// Example:
//
//	srv.Tool("process").Handler(func(ctx context.Context, input ProcessInput) (string, error) {
//	    progress := mcp.ProgressFromContext(ctx)
//	    total := 100.0
//	    for i := 0; i < 100; i++ {
//	        progress.Report(float64(i), &total)
//	        // do work...
//	    }
//	    return "done", nil
//	})
var ProgressFromContext = server.ProgressFromContext

// Middleware types
type Middleware = middleware.Middleware
type MiddlewareHandlerFunc = middleware.HandlerFunc
type Logger = middleware.Logger
type LogField = middleware.Field
type RateLimitOption = middleware.RateLimitOption

// RateLimit re-exports for convenience.
var (
	RateLimit            = middleware.RateLimit
	RateLimitByMethod    = middleware.RateLimitByMethod
	RateLimitByClient    = middleware.RateLimitByClient
	WithRateLimitKeyFunc = middleware.WithRateLimitKeyFunc
	WithRateLimitLogger  = middleware.WithRateLimitLogger
)

// SizeLimit re-exports for convenience.
type SizeLimitOption = middleware.SizeLimitOption

var (
	SizeLimit           = middleware.SizeLimit
	WithSizeLimitLogger = middleware.WithSizeLimitLogger
)

// Size limit presets.
const (
	KB = middleware.KB
	MB = middleware.MB
)

// Auth re-exports for convenience.
type Identity = middleware.Identity
type AuthOption = middleware.AuthOption
type Authenticator = middleware.Authenticator

var (
	Auth                     = middleware.Auth
	WithAuthLogger           = middleware.WithAuthLogger
	WithAuthSkipMethods      = middleware.WithAuthSkipMethods
	WithAuthRealm            = middleware.WithAuthRealm
	WithAuthErrorMessage     = middleware.WithAuthErrorMessage
	APIKeyAuthenticator      = middleware.APIKeyAuthenticator
	BearerTokenAuthenticator = middleware.BearerTokenAuthenticator
	StaticAPIKeys            = middleware.StaticAPIKeys
	StaticTokens             = middleware.StaticTokens
	ChainAuthenticators      = middleware.ChainAuthenticators
	IdentityFromContext      = middleware.IdentityFromContext
	ContextWithIdentity      = middleware.ContextWithIdentity
)

// BearerAuth returns a Middleware that requires clients to present one
// of the given tokens as a Bearer credential in the Authorization
// header.
//
// The map's keys are the secret tokens to accept; the values become
// the Identity.ID + Identity.Name surfaced in the request context via
// IdentityFromContext. Pass a friendly client name as the value.
//
// Handshake methods ("initialize", "notifications/initialized", and
// "ping") are exempted from authentication automatically — clients
// can't present a token until the handshake completes. Additional
// opts (WithAuthRealm, WithAuthSkipMethods, etc.) compose with the
// defaults.
//
// Use the full Auth + BearerTokenAuthenticator + StaticTokens API
// directly if you need per-token metadata, scopes, or multi-tenant
// identity routing.
func BearerAuth(tokens map[string]string, opts ...AuthOption) middleware.Middleware {
	identities := make(map[string]*Identity, len(tokens))
	for token, name := range tokens {
		identities[token] = &Identity{ID: name, Name: name}
	}
	authOpts := append([]AuthOption{
		WithAuthSkipMethods(protocol.MethodInitialized),
	}, opts...)
	return Auth(BearerTokenAuthenticator(StaticTokens(identities)), authOpts...)
}

// HTTPOption configures the HTTP transport.
type HTTPOption = transport.HTTPOption

// CORS configuration for HTTP transports.
type CORSConfig = transport.CORSConfig

var (
	DefaultCORSConfig = transport.DefaultCORSConfig
	WithCORS          = transport.WithCORS
	WithDefaultCORS   = transport.WithDefaultCORS
)

// Shutdown configuration for HTTP transports.
type ShutdownConfig = transport.ShutdownConfig
type ShutdownManager = transport.ShutdownManager

var (
	DefaultShutdownConfig  = transport.DefaultShutdownConfig
	NewShutdownManager     = transport.NewShutdownManager
	WithShutdownTimeout    = transport.WithShutdownTimeout
	WithShutdownDrainDelay = transport.WithShutdownDrainDelay
)

// ServeOption configures how the server is run.
type ServeOption func(*serveOptions)

type serveOptions struct {
	middleware     []Middleware
	logger         Logger
	toolFilter     ToolFilterFunc
	resourceFilter ResourceFilterFunc
	promptFilter   PromptFilterFunc
}

// WithMiddleware adds middleware to the request handling chain.
func WithMiddleware(m ...Middleware) ServeOption {
	return func(o *serveOptions) {
		o.middleware = append(o.middleware, m...)
	}
}

// WithLogger sets the logger for the default middleware stack.
func WithLogger(l Logger) ServeOption {
	return func(o *serveOptions) {
		o.logger = l
	}
}

// ToolFilterFunc decides whether a tool should be visible to (and
// callable by) the caller represented by ctx. Returning false hides
// the tool from tools/list AND causes tools/call to fail with a
// not-found error, so the filter is the authoritative contract
// rather than a display-only layer.
//
// Use IdentityFromContext(ctx) to read the authenticated caller when
// the predicate needs to vary by client.
type ToolFilterFunc func(ctx context.Context, name string) bool

// ResourceFilterFunc decides whether a resource should be visible to
// the caller represented by ctx. The predicate receives both the URI
// template and the human-readable name so authorisation can route on
// either. Returning false hides the resource from resources/list AND
// blocks resources/read for that URI.
type ResourceFilterFunc func(ctx context.Context, uri, name string) bool

// PromptFilterFunc decides whether a prompt should be visible to the
// caller. Returning false hides the prompt from prompts/list AND
// blocks prompts/get.
type PromptFilterFunc func(ctx context.Context, name string) bool

// WithToolFilter installs a predicate that gates tools/list visibility
// and tools/call execution. The predicate runs once per list entry
// (and once per call) with the request context, so identity-aware
// filtering composes naturally with the auth middleware.
//
// Without this option, every registered tool is visible to every
// caller. Multiple WithToolFilter calls replace previous ones — there
// is no chaining; if you need multiple predicates, compose them in a
// single function.
func WithToolFilter(filter ToolFilterFunc) ServeOption {
	return func(o *serveOptions) {
		o.toolFilter = filter
	}
}

// WithResourceFilter is the resources/list + resources/read counterpart
// to WithToolFilter. See ResourceFilterFunc for predicate semantics.
func WithResourceFilter(filter ResourceFilterFunc) ServeOption {
	return func(o *serveOptions) {
		o.resourceFilter = filter
	}
}

// WithPromptFilter is the prompts/list + prompts/get counterpart to
// WithToolFilter. See PromptFilterFunc for predicate semantics.
func WithPromptFilter(filter PromptFilterFunc) ServeOption {
	return func(o *serveOptions) {
		o.promptFilter = filter
	}
}

// NewServer creates a new MCP server with the given info and options.
func NewServer(info ServerInfo, opts ...Option) *Server {
	return server.New(info, opts...)
}

// WithInstructions sets the server instructions that provide context to AI models
// about how to use this server effectively.
var WithInstructions = server.WithInstructions

// ServerInfo option functions
var (
	// WithTitle sets a human-readable display name for the server.
	WithTitle = server.WithTitle
	// WithDescription sets a description of what the server does.
	WithDescription = server.WithDescription
	// WithWebsiteURL sets the server's documentation or homepage URL.
	WithWebsiteURL = server.WithWebsiteURL
	// WithIcons sets the icons for UI display.
	WithIcons = server.WithIcons
	// WithBuildInfo sets build metadata for debugging and version verification.
	WithBuildInfo = server.WithBuildInfo
)

// ServeStdio runs the server using stdio transport.
// This blocks until the context is canceled or an error occurs.
func ServeStdio(ctx context.Context, srv *Server, opts ...ServeOption) error {
	t := transport.NewStdio()
	handler := newRequestHandler(srv, opts...)
	return t.Serve(ctx, handler)
}

// ServeHTTP runs the server using HTTP transport with SSE support.
// This blocks until the context is canceled or an error occurs.
func ServeHTTP(ctx context.Context, srv *Server, addr string, opts ...HTTPOption) error {
	t := transport.NewHTTP(addr, opts...)
	handler := newRequestHandler(srv)
	return t.Serve(ctx, handler)
}

// ServeHTTPWithMiddleware runs the server using HTTP transport with middleware support.
func ServeHTTPWithMiddleware(ctx context.Context, srv *Server, addr string, httpOpts []HTTPOption, serveOpts ...ServeOption) error {
	t := transport.NewHTTP(addr, httpOpts...)
	handler := newRequestHandler(srv, serveOpts...)
	return t.Serve(ctx, handler)
}

// WithReadTimeout sets the read timeout for HTTP requests.
func WithReadTimeout(d time.Duration) HTTPOption {
	return transport.WithReadTimeout(d)
}

// WithWriteTimeout sets the write timeout for HTTP responses.
func WithWriteTimeout(d time.Duration) HTTPOption {
	return transport.WithWriteTimeout(d)
}

// WithDiscovery sets the server discovery metadata for the /.well-known/mcp endpoint.
func WithDiscovery(discovery *transport.ServerDiscovery) HTTPOption {
	return transport.WithDiscovery(discovery)
}

// WithTLSConfig enables HTTPS termination on the HTTP transport. The
// supplied *tls.Config is used verbatim — bring your own certificate
// loading + rotation strategy (LoadX509KeyPair, autocert, SPIFFE,
// etc.). Set ClientCAs + ClientAuth for mTLS.
func WithTLSConfig(cfg *tls.Config) HTTPOption {
	return transport.WithTLSConfig(cfg)
}

// WebSocketOption configures the WebSocket transport.
type WebSocketOption = transport.WebSocketOption

// ServeWebSocket runs the server using WebSocket transport.
// This blocks until the context is canceled or an error occurs.
func ServeWebSocket(ctx context.Context, srv *Server, addr string, opts ...WebSocketOption) error {
	t := transport.NewWebSocket(addr, opts...)
	handler := newRequestHandler(srv)
	return t.Serve(ctx, handler)
}

// ServeWebSocketWithMiddleware runs the server using WebSocket transport with middleware support.
func ServeWebSocketWithMiddleware(ctx context.Context, srv *Server, addr string, wsOpts []WebSocketOption, serveOpts ...ServeOption) error {
	t := transport.NewWebSocket(addr, wsOpts...)
	handler := newRequestHandler(srv, serveOpts...)
	return t.Serve(ctx, handler)
}

// WithWebSocketReadTimeout sets the read timeout for WebSocket messages.
func WithWebSocketReadTimeout(d time.Duration) WebSocketOption {
	return transport.WithWebSocketReadTimeout(d)
}

// WithWebSocketWriteTimeout sets the write timeout for WebSocket messages.
func WithWebSocketWriteTimeout(d time.Duration) WebSocketOption {
	return transport.WithWebSocketWriteTimeout(d)
}

// WithWebSocketTLSConfig enables wss:// on the WebSocket transport.
// Same caller-owned cert strategy as WithTLSConfig.
func WithWebSocketTLSConfig(cfg *tls.Config) WebSocketOption {
	return transport.WithWebSocketTLSConfig(cfg)
}

// GRPCOption configures the gRPC transport.
type GRPCOption = grpctransport.Option

// ServeGRPC runs the server using gRPC transport with bidirectional streaming.
// This enables MCP communication over gRPC, providing benefits like binary
// encoding, built-in flow control, and native support for enterprise infrastructure.
// This blocks until the context is canceled or an error occurs.
func ServeGRPC(ctx context.Context, srv *Server, addr string, opts ...GRPCOption) error {
	t := grpctransport.NewGRPC(addr, opts...)
	handler := newRequestHandler(srv)
	return t.Serve(ctx, handler)
}

// ServeGRPCWithMiddleware runs the server using gRPC transport with middleware support.
func ServeGRPCWithMiddleware(ctx context.Context, srv *Server, addr string, grpcOpts []GRPCOption, serveOpts ...ServeOption) error {
	t := grpctransport.NewGRPC(addr, grpcOpts...)
	handler := newRequestHandler(srv, serveOpts...)
	return t.Serve(ctx, handler)
}

// WithGRPCShutdownTimeout sets the maximum time to wait for graceful shutdown.
func WithGRPCShutdownTimeout(d time.Duration) GRPCOption {
	return grpctransport.WithShutdownTimeout(d)
}

// WithGRPCDrainDelay sets the delay before starting connection draining.
// This allows load balancers to remove the server from rotation.
func WithGRPCDrainDelay(d time.Duration) GRPCOption {
	return grpctransport.WithDrainDelay(d)
}

// WithGRPCTLSConfig is a shorthand for embedded TLS on the gRPC
// transport. Equivalent to passing grpc.Creds(credentials.NewTLS(cfg))
// through grpctransport.WithServerOptions. Use the underlying
// grpctransport.WithServerOptions when you need credentials that
// aren't a static *tls.Config (SPIFFE, ALTS, etc.).
func WithGRPCTLSConfig(cfg *tls.Config) GRPCOption {
	return grpctransport.WithTLSConfig(cfg)
}

// Middleware re-exports

// Chain composes multiple middleware into a single middleware.
func Chain(middlewares ...Middleware) Middleware {
	return middleware.Chain(middlewares...)
}

// Recover returns middleware that catches panics and converts them to internal errors.
func Recover() Middleware {
	return middleware.Recover()
}

// RecoverWithHandler returns middleware that catches panics and calls the provided handler.
func RecoverWithHandler(handler func(ctx context.Context, req *protocol.Request, panicVal any) (*protocol.Response, error)) Middleware {
	return middleware.RecoverWithHandler(handler)
}

// Timeout returns middleware that enforces a request deadline.
func Timeout(d time.Duration) Middleware {
	return middleware.Timeout(d)
}

// RequestID returns middleware that injects a unique request ID into the context.
func RequestID() Middleware {
	return middleware.RequestID()
}

// RequestIDFromContext returns the request ID from the context, or empty string if not set.
func RequestIDFromContext(ctx context.Context) string {
	return middleware.RequestIDFromContext(ctx)
}

// Logging returns middleware that logs request details.
func Logging(logger Logger) Middleware {
	return middleware.Logging(logger)
}

// DefaultMiddleware returns the recommended production middleware stack.
func DefaultMiddleware(logger Logger) []Middleware {
	return middleware.DefaultStack(logger)
}

// DefaultMiddlewareWithTimeout returns the default stack with a timeout middleware.
func DefaultMiddlewareWithTimeout(logger Logger, timeout time.Duration) []Middleware {
	return middleware.DefaultStackWithTimeout(logger, timeout)
}

// LogF creates a new log field with the given key and value.
func LogF(key string, value any) LogField {
	return middleware.NewField(key, value)
}

// OpenTelemetry re-exports for convenience.
type OTelOption = middleware.OTelOption

var (
	OTel                = middleware.OTel
	WithTracerProvider  = middleware.WithTracerProvider
	WithMeterProvider   = middleware.WithMeterProvider
	WithOTelServiceName = middleware.WithOTelServiceName
	WithOTelSkipMethods = middleware.WithOTelSkipMethods
	SpanFromContext     = middleware.SpanFromContext
	AddSpanEvent        = middleware.AddSpanEvent
	SetSpanAttribute    = middleware.SetSpanAttribute
)

// requestHandler adapts Server to transport.Handler
type requestHandler struct {
	srv            *Server
	handleFunc     middleware.HandlerFunc
	toolFilter     ToolFilterFunc
	resourceFilter ResourceFilterFunc
	promptFilter   PromptFilterFunc
}

func newRequestHandler(srv *Server, opts ...ServeOption) *requestHandler {
	options := &serveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	h := &requestHandler{
		srv:            srv,
		toolFilter:     options.toolFilter,
		resourceFilter: options.resourceFilter,
		promptFilter:   options.promptFilter,
	}

	// Build the handler function
	baseHandler := middleware.HandlerFunc(h.handle)

	// Apply middleware if any
	if len(options.middleware) > 0 {
		h.handleFunc = middleware.Chain(options.middleware...)(baseHandler)
	} else {
		h.handleFunc = baseHandler
	}

	return h
}

func (h *requestHandler) HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	return h.handleFunc(ctx, req)
}

// methodHandlers maps MCP methods to their handlers.
// All handlers use the same signature (ctx, req) for uniform dispatch.
func (h *requestHandler) methodHandlers() map[string]func(context.Context, *protocol.Request) (*protocol.Response, error) {
	return map[string]func(context.Context, *protocol.Request) (*protocol.Response, error){
		protocol.MethodInitialize:    h.handleInitialize,
		protocol.MethodToolsList:     h.handleToolsList,
		protocol.MethodToolsCall:     h.handleToolsCall,
		protocol.MethodResourcesList: h.handleResourcesList,
		protocol.MethodResourcesRead: h.handleResourcesRead,
		protocol.MethodPromptsList:   h.handlePromptsList,
		protocol.MethodPromptsGet:    h.handlePromptsGet,
		protocol.MethodPing:          h.handlePing,
	}
}

func (h *requestHandler) handle(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if handler, ok := h.methodHandlers()[req.Method]; ok {
		return handler(ctx, req)
	}
	return nil, protocol.NewMethodNotFound(req.Method)
}

func (h *requestHandler) handleInitialize(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	manifest := h.srv.Manifest()

	// Build capabilities based on explicit flags OR registered handlers
	// This ensures capabilities are advertised even if users don't set flags explicitly
	capabilities := make(map[string]any)

	if manifest.Capabilities.Tools || len(h.srv.Tools()) > 0 {
		capabilities["tools"] = map[string]any{fieldListChanged: true}
	}
	if manifest.Capabilities.Resources || len(h.srv.Resources()) > 0 {
		capabilities["resources"] = map[string]any{fieldListChanged: true}
	}
	if manifest.Capabilities.Prompts || len(h.srv.Prompts()) > 0 {
		capabilities["prompts"] = map[string]any{fieldListChanged: true}
	}

	// Build serverInfo with required fields
	serverInfo := map[string]any{
		fieldName:    manifest.Name,
		fieldVersion: manifest.Version,
	}

	// Add optional MCP spec fields if set
	if manifest.Title != "" {
		serverInfo["title"] = manifest.Title
	}
	if manifest.Description != "" {
		serverInfo["description"] = manifest.Description
	}
	if manifest.WebsiteURL != "" {
		serverInfo["websiteUrl"] = manifest.WebsiteURL
	}
	if len(manifest.Icons) > 0 {
		serverInfo["icons"] = manifest.Icons
	}
	// Add extension field (not in MCP spec)
	if manifest.BuildInfo != nil {
		serverInfo["buildInfo"] = manifest.BuildInfo
	}

	result := map[string]any{
		fieldProtocolVersion: manifest.ProtocolVersion,
		"serverInfo":         serverInfo,
		"capabilities":       capabilities,
	}

	// Include instructions if set
	if instructions := h.srv.Instructions(); instructions != "" {
		result["instructions"] = instructions
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handleToolsList(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	tools := h.srv.Tools()

	toolList := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		if h.toolFilter != nil && !h.toolFilter(ctx, t.Name) {
			continue
		}
		item := map[string]any{
			fieldName:     t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
		if t.OutputSchema != nil {
			item["outputSchema"] = t.OutputSchema
		}
		if t.Annotations != nil {
			item["annotations"] = t.Annotations
		}
		if t.Meta != nil {
			item["_meta"] = t.Meta
		}
		toolList = append(toolList, item)
	}

	result := map[string]any{
		"tools": toolList,
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handleToolsCall(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	// Parse params
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	// Get tool. A filter that hides the tool from tools/list must also
	// block tools/call — otherwise the filter is a display layer, not
	// the authorisation contract callers rely on.
	tool, ok := h.srv.GetTool(params.Name)
	if !ok {
		return nil, protocol.NewNotFound("tool not found: " + params.Name)
	}
	if h.toolFilter != nil && !h.toolFilter(ctx, params.Name) {
		return nil, protocol.NewNotFound("tool not found: " + params.Name)
	}

	// Set up progress reporting if token is present
	progressToken := server.ExtractProgressToken(req.Params)
	if progressToken != "" {
		if sender := transport.NotificationSenderFromContext(ctx); sender != nil {
			// Adapt transport.NotificationSender to server.NotificationSender
			reporter := server.NewProgressReporter(progressToken, &notificationAdapter{sender})
			ctx = server.ContextWithProgress(ctx, reporter)
		}
	}

	// Inject elicitor and channel sender if session is available
	if session := server.SessionFromContext(ctx); session != nil {
		if session.SupportsFeature("elicitation") {
			ctx = server.ContextWithElicitor(ctx, server.NewElicitor(session))
		}
		if session.SupportsFeature("channels") {
			ctx = server.ContextWithChannel(ctx, server.NewChannelSender(session))
		}
	}

	// Execute tool
	result, err := tool.Execute(ctx, params.Arguments)
	if err != nil {
		// Check if it's already an MCP error
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, protocol.NewInternalError(err.Error())
	}

	// Format result based on type
	response, err := buildToolCallResponse(tool, result)
	if err != nil {
		return nil, err
	}

	if tool.Meta() != nil {
		response["_meta"] = tool.Meta()
	}

	return protocol.NewResponse(req.ID, response), nil
}

// buildToolCallResponse converts a handler's return value into the MCP
// tools/call response body. Three shapes are accepted:
//
//   - server.StructuredResult / *server.StructuredResult: explicit
//     content + structuredContent + optional isError flag.
//   - string: legacy single text content block.
//   - any other typed value: serialized to a text content block, and
//     (when the tool declares an outputSchema) promoted to
//     structuredContent so strict MCP clients accept the response.
//
// Extracted from handleToolsCall to keep that function under the
// project's cyclomatic-complexity ceiling.
func buildToolCallResponse(tool *server.Tool, result any) (map[string]any, error) {
	response := make(map[string]any)

	switch v := result.(type) {
	case server.StructuredResult:
		applyStructuredResult(response, &v)
		return response, nil
	case *server.StructuredResult:
		if v != nil {
			applyStructuredResult(response, v)
		}
		return response, nil
	}

	// Legacy: serialize to a single text content block. Typed structs
	// also feed structuredContent when an outputSchema is declared so
	// the response satisfies strict MCP clients.
	var textContent string
	var marshaled []byte
	switch tv := result.(type) {
	case string:
		textContent = tv
	default:
		data, err := json.Marshal(tv)
		if err != nil {
			return nil, protocol.NewInternalError(fmt.Sprintf("failed to serialize tool result: %v", err))
		}
		textContent = string(data)
		marshaled = data
	}
	response["content"] = []map[string]any{
		{
			"type":    fieldText,
			fieldText: textContent,
		},
	}
	if tool.OutputSchema() != nil && marshaled != nil {
		var structured map[string]any
		if err := json.Unmarshal(marshaled, &structured); err == nil && structured != nil {
			response["structuredContent"] = structured
		}
	}
	return response, nil
}

// applyStructuredResult writes a StructuredResult into the response map.
// Empty Content is normalized to [] (per MCP spec) so clients never see
// a missing content field.
func applyStructuredResult(response map[string]any, v *server.StructuredResult) {
	if len(v.Content) > 0 {
		response["content"] = v.Content
	} else {
		response["content"] = []map[string]any{}
	}
	response["structuredContent"] = v.StructuredContent
	if v.IsError {
		response["isError"] = true
	}
}

func (h *requestHandler) handleResourcesList(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	resources := h.srv.Resources()

	resourceList := make([]map[string]any, 0, len(resources))
	for _, r := range resources {
		if h.resourceFilter != nil && !h.resourceFilter(ctx, r.URITemplate, r.Name) {
			continue
		}
		item := map[string]any{
			"uri":     r.URITemplate,
			fieldName: r.Name,
		}
		if r.Description != "" {
			item["description"] = r.Description
		}
		if r.MimeType != "" {
			item["mimeType"] = r.MimeType
		}
		if r.Annotations != nil {
			item["annotations"] = r.Annotations
		}
		resourceList = append(resourceList, item)
	}

	result := map[string]any{
		"resources": resourceList,
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handleResourcesRead(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	// Parse params
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	// Find resource that matches the URI. Hidden resources stay
	// hidden — resources/read on a filtered resource looks identical
	// to one that was never registered.
	resource, ok := h.srv.FindResourceForURI(params.URI)
	if !ok {
		return nil, protocol.NewNotFound("resource not found: " + params.URI)
	}
	if h.resourceFilter != nil && !h.resourceFilter(ctx, resource.URITemplate(), resource.Name()) {
		return nil, protocol.NewNotFound("resource not found: " + params.URI)
	}

	// Read resource
	content, err := resource.Read(ctx, params.URI)
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, protocol.NewInternalError(err.Error())
	}

	result := map[string]any{
		"contents": []map[string]any{
			{
				"uri":      content.URI,
				"mimeType": content.MimeType,
				fieldText:  content.Text,
			},
		},
	}

	// Include blob if present
	if content.Blob != "" {
		result["contents"].([]map[string]any)[0]["blob"] = content.Blob
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handlePromptsList(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	prompts := h.srv.Prompts()

	promptList := make([]map[string]any, 0, len(prompts))
	for _, p := range prompts {
		if h.promptFilter != nil && !h.promptFilter(ctx, p.Name) {
			continue
		}
		item := map[string]any{
			fieldName: p.Name,
		}
		if p.Description != "" {
			item["description"] = p.Description
		}
		if len(p.Arguments) > 0 {
			args := make([]map[string]any, 0, len(p.Arguments))
			for _, arg := range p.Arguments {
				argItem := map[string]any{
					fieldName:  arg.Name,
					"required": arg.Required,
				}
				if arg.Description != "" {
					argItem["description"] = arg.Description
				}
				args = append(args, argItem)
			}
			item["arguments"] = args
		}
		if p.Annotations != nil {
			item["annotations"] = p.Annotations
		}
		promptList = append(promptList, item)
	}

	result := map[string]any{
		"prompts": promptList,
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handlePromptsGet(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	// Parse params
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	// Get prompt. Filter blocks hidden prompts from prompts/get the
	// same way it blocks tools/call — the contract is "if you can't
	// see it, you can't reach it".
	prompt, ok := h.srv.GetPrompt(params.Name)
	if !ok {
		return nil, protocol.NewNotFound("prompt not found: " + params.Name)
	}
	if h.promptFilter != nil && !h.promptFilter(ctx, params.Name) {
		return nil, protocol.NewNotFound("prompt not found: " + params.Name)
	}

	// Execute prompt
	result, err := prompt.Get(ctx, params.Arguments)
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, protocol.NewInvalidParams(err.Error())
	}

	response := map[string]any{
		"messages": result.Messages,
	}
	if result.Description != "" {
		response["description"] = result.Description
	}

	return protocol.NewResponse(req.ID, response), nil
}

func (h *requestHandler) handlePing(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	return protocol.NewResponse(req.ID, map[string]any{}), nil
}

// notificationAdapter adapts transport.NotificationSender to server.NotificationSender.
type notificationAdapter struct {
	sender transport.NotificationSender
}

func (a *notificationAdapter) SendNotification(method string, params any) error {
	return a.sender.SendNotification(method, params)
}
