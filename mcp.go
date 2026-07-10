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
	"log"
	"time"

	"go.klarlabs.de/mcp/middleware"
	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
	"go.klarlabs.de/mcp/transport"
	grpctransport "go.klarlabs.de/mcp/transport/grpc"
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
	fieldType            = "type"
	fieldContent         = "content"
	fieldURI             = "uri"
	fieldTask            = "task"
	fieldTaskID          = "taskId"
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

// Sampling-with-tools types (MCP 2025-11-25): a sampling request may offer
// tools to the model and receive tool-use results.
type SamplingTool = server.SamplingTool
type SamplingToolChoice = server.SamplingToolChoice
type SamplingToolCall = server.SamplingToolCall

// ContentBlock is the canonical MCP content-block union (alias of Content),
// covering text, image, audio, resource_link, and embedded resource blocks.
type ContentBlock = server.ContentBlock
type EmbeddedResource = server.EmbeddedResource
type ContentAnnotations = server.ContentAnnotations

// Role constants
const (
	RoleUser      = server.RoleUser
	RoleAssistant = server.RoleAssistant
)

// Content constructors
var (
	NewTextContent      = server.NewTextContent
	NewImageContent     = server.NewImageContent
	NewAudioContent     = server.NewAudioContent
	NewResourceLink     = server.NewResourceLink
	NewEmbeddedResource = server.NewEmbeddedResource
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

// ToolInputError is returned by a tool when input fails to parse/validate;
// the dispatcher surfaces it as an isError result (SEP-1303).
type ToolInputError = server.ToolInputError

// Elicitation types for interactive user prompts
type ElicitRequest = server.ElicitRequest
type ElicitResult = server.ElicitResult
type Elicitor = server.Elicitor

var (
	NewElicitor       = server.NewElicitor
	ElicitFromContext = server.ElicitFromContext
)

// Elicitation modes (MCP 2025-11-25): form (default, in-band structured data)
// and url (out-of-band navigation for sensitive interactions).
const (
	ElicitModeForm = server.ElicitModeForm
	ElicitModeURL  = server.ElicitModeURL
)

// MRTR types (MCP 2026-07-28): the stateless Multi Round-Trip Request model that
// replaces server-initiated sampling, elicitation, and roots. A stateless
// handler's input calls are fulfilled from client-supplied InputResponses or, on
// the first round, returned as an InputRequiredResult for the client to fulfill
// and retry.
type InputRequest = server.InputRequest
type InputResponse = server.InputResponse
type InputRequiredResult = server.InputRequiredResult

// ErrInputRequired is the sentinel a stateless handler receives from an input
// call (CreateMessage / Elicit / ListRoots) when the client has not yet supplied
// that input; propagate it unchanged.
var ErrInputRequired = server.ErrInputRequired

// Input request kinds carried in InputRequest.Kind.
const (
	InputKindSampling    = server.InputKindSampling
	InputKindElicitation = server.InputKindElicitation
	InputKindRoots       = server.InputKindRoots
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

// Task-augmented request types (MCP 2025-11-25). Declare a tool's task support
// with .TaskSupport(mcp.TaskSupportOptional); clients augment tools/call with a
// task and poll tasks/get / tasks/result.
type AugTask = server.AugTask
type TaskSupport = server.TaskSupport

const (
	TaskSupportForbidden = server.TaskSupportForbidden
	TaskSupportOptional  = server.TaskSupportOptional
	TaskSupportRequired  = server.TaskSupportRequired
)

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

// mcp-go does not provide authentication. By design, the library never handles
// tokens, OAuth flows, or credentials. On the client side, inject auth via the
// caller-supplied http.Client transport (see mcp.WithHTTPClient). On the server
// side, terminate auth at the transport/proxy layer or in caller-provided
// middleware; mcp-go ships none. To vary behavior by caller in a filter
// predicate, attach your own value to the request context and read it back —
// the library no longer ships an Identity type.

// HTTPOption configures the HTTP transport.
type HTTPOption = transport.HTTPOption

// CORS configuration for HTTP transports.
type CORSConfig = transport.CORSConfig

var (
	DefaultCORSConfig = transport.DefaultCORSConfig
	WithCORS          = transport.WithCORS
	WithDefaultCORS   = transport.WithDefaultCORS
	// WithStreamable enables the modern Streamable HTTP transport (MCP
	// 2025-03-26): a single /mcp endpoint with Mcp-Session-Id and GET SSE.
	WithStreamable = transport.WithStreamable
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
// When the predicate needs to vary by client, attach a caller value to the
// request context in your own middleware/transport and read it back here —
// mcp-go ships no Identity type and never handles auth.
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
	// WithResultCache sets the cache hint (ttlMs, cacheScope) advertised on
	// cacheable list/read results to modern (2026-07-28) clients.
	WithResultCache = server.WithResultCache
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
	wireResourceSubscriptions(srv, t)
	handler := newRequestHandler(srv)
	return t.Serve(ctx, handler)
}

// ServeHTTPWithMiddleware runs the server using HTTP transport with middleware support.
func ServeHTTPWithMiddleware(ctx context.Context, srv *Server, addr string, httpOpts []HTTPOption, serveOpts ...ServeOption) error {
	t := transport.NewHTTP(addr, httpOpts...)
	wireResourceSubscriptions(srv, t)
	handler := newRequestHandler(srv, serveOpts...)
	return t.Serve(ctx, handler)
}

// wireResourceSubscriptions connects the HTTP transport's server-push and
// disconnect lifecycle to the server's subscription registry, so resource
// updates reach subscribers and closed connections release their state.
func wireResourceSubscriptions(srv *Server, t *transport.HTTP) {
	if !srv.ResourceSubscriptionsEnabled() {
		return
	}
	srv.SetResourceNotifier(t)
	t.SetDisconnectHook(srv.RemoveClientSubscriptions)
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
	// cancellations tracks in-flight requests so an incoming
	// notifications/cancelled can actually cancel a running handler's context.
	cancellations *server.CancellationManager
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
		cancellations:  server.NewCancellationManager(),
	}

	// Build the handler function
	baseHandler := middleware.HandlerFunc(h.handle)

	// Assemble the middleware chain, outermost first:
	//   Recover (always on) -> Server.Use() middleware -> WithMiddleware() -> handler
	//
	// Two correctness fixes live here:
	//   1. Server.Use() middleware is now actually applied. Previously only
	//      options.middleware (from WithMiddleware) was read, so everything
	//      registered via srv.Use(...) — including Recover/SizeLimit — was
	//      silently dropped, leaving servers unprotected while appearing hardened.
	//   2. Recover is forced OUTERMOST and on by default, so a panic in any
	//      middleware or handler is converted to an error instead of unwinding
	//      the transport read-loop and crashing the whole server process. A
	//      caller that adds its own Recover (e.g. RecoverWithHandler) still runs
	//      inner-first, so custom panic handling is preserved.
	serverMW := srv.Middleware()
	chain := make([]Middleware, 0, len(serverMW)+len(options.middleware)+1)
	chain = append(chain, middleware.Recover())
	chain = append(chain, serverMW...)
	chain = append(chain, options.middleware...)
	h.handleFunc = middleware.Chain(chain...)(baseHandler)

	return h
}

func (h *requestHandler) HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	return h.handleFunc(ctx, req)
}

// methodHandlers maps MCP methods to their handlers.
// All handlers use the same signature (ctx, req) for uniform dispatch.
func (h *requestHandler) methodHandlers() map[string]func(context.Context, *protocol.Request) (*protocol.Response, error) {
	return map[string]func(context.Context, *protocol.Request) (*protocol.Response, error){
		protocol.MethodInitialize:             h.handleInitialize,
		protocol.MethodToolsList:              h.handleToolsList,
		protocol.MethodToolsCall:              h.handleToolsCall,
		protocol.MethodResourcesList:          h.handleResourcesList,
		protocol.MethodResourcesRead:          h.handleResourcesRead,
		protocol.MethodResourcesSubscribe:     h.handleResourcesSubscribe,
		protocol.MethodResourcesUnsubscribe:   h.handleResourcesUnsubscribe,
		protocol.MethodPromptsList:            h.handlePromptsList,
		protocol.MethodPromptsGet:             h.handlePromptsGet,
		protocol.MethodPing:                   h.handlePing,
		protocol.MethodCancelled:              h.handleCancelled,
		protocol.MethodInitialized:            h.handleInitialized,
		protocol.MethodCompletionComplete:     h.handleCompletion,
		protocol.MethodLoggingSetLevel:        h.handleLoggingSetLevel,
		protocol.MethodResourcesTemplatesList: h.handleResourcesTemplatesList,
		protocol.MethodTasksGet:               h.handleTasksGet,
		protocol.MethodTasksResult:            h.handleTasksResult,
		protocol.MethodTasksCancel:            h.handleTasksCancel,
		protocol.MethodTasksList:              h.handleTasksList,
		protocol.MethodTasksUpdate:            h.handleTasksUpdate,
		protocol.MethodServerDiscover:         h.handleServerDiscover,
		protocol.MethodSubscriptionsListen:    h.handleSubscriptionsListen,
	}
}

func (h *requestHandler) handle(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	// Track in-flight requests (those with an ID) so a notifications/cancelled
	// for this ID cancels the derived context mid-flight. The deferred cancel
	// also untracks on normal completion. Notifications have no ID and are not
	// tracked (a cancelled notification must not try to cancel itself).
	if !req.IsNotification() {
		var cancel context.CancelFunc
		ctx, cancel = h.cancellations.Track(ctx, string(req.ID))
		defer cancel()
	}

	// Modern (stateless) path: a request carrying the per-request protocol
	// _meta is validated and served with a request-scoped session built from
	// its declared capabilities. Legacy requests fall through unchanged.
	meta, modern, err := parseModernMeta(req.Params)
	if err != nil {
		return nil, h.publicError(req, err)
	}
	if modern {
		ctx, err = h.applyModern(ctx, req.Method, meta)
		if err != nil {
			return nil, h.publicError(req, err)
		}
		// tasks/list is retired in the modern (2026-07-28) tasks extension, which
		// favors direct task handles over listing. The legacy method stays for
		// negotiated 2025-11-25 sessions; a modern caller gets MethodNotFound.
		if req.Method == protocol.MethodTasksList {
			return nil, h.publicError(req, protocol.NewMethodNotFound(req.Method))
		}
	}

	resp, err := h.dispatch(ctx, req)
	// MRTR (MCP 2026-07-28): a stateless handler that called sampling/elicitation/
	// roots without a supplied response is paused, not failed — surface the
	// recorded inputRequests as an input_required result for the client to
	// fulfill and retry.
	if modern {
		if r, ok := inputRequiredResponse(ctx, req); ok {
			return r, nil
		}
	}
	if err != nil {
		if modern {
			err = modernizeError(err)
		}
		return nil, h.publicError(req, err)
	}
	if modern {
		withResultType(resp)
		h.applyCacheHint(req.Method, resp)
	}
	return resp, nil
}

func (h *requestHandler) dispatch(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if handler, ok := h.methodHandlers()[req.Method]; ok {
		return handler(ctx, req)
	}
	return nil, protocol.NewMethodNotFound(req.Method)
}

// publicError is the single chokepoint that decides what error detail reaches
// the peer. Deliberate protocol errors (InvalidParams, MethodNotFound,
// NotFound, …) carry an intended-public message and pass through verbatim. Any
// other error is an internal failure whose message may embed paths, state, or
// secret-adjacent detail: it is logged server-side (stderr — never stdout,
// which would corrupt stdio framing) and replaced with a generic -32603 so
// nothing leaks. Because this sits at the shared handler boundary it covers
// every transport uniformly.
func (h *requestHandler) publicError(req *protocol.Request, err error) error {
	var mcpErr *protocol.Error
	if errors.As(err, &mcpErr) {
		return mcpErr
	}
	method := ""
	if req != nil {
		method = req.Method
	}
	log.Printf("mcp: internal error handling %q: %v", method, err)
	return protocol.NewInternalError("internal error")
}

// handleCancelled processes a notifications/cancelled by cancelling the tracked
// in-flight request with the given id. It is a notification, so it returns no
// response body.
func (h *requestHandler) handleCancelled(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	var notif server.CancelledNotification
	if err := json.Unmarshal(req.Params, &notif); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	h.cancellations.Cancel(string(notif.RequestID))
	return nil, nil
}

// serverCapabilities builds the capabilities map advertised in the initialize
// response. Capabilities are advertised from explicit flags OR from registered
// handlers, so a server that registers tools/resources/prompts/completions
// advertises them even without setting the flags. Extracted from
// handleInitialize to keep that handler's complexity in check.
func (h *requestHandler) serverCapabilities(manifest server.Manifest) map[string]any {
	capabilities := make(map[string]any)

	if manifest.Capabilities.Tools || len(h.srv.Tools()) > 0 {
		capabilities["tools"] = map[string]any{fieldListChanged: true}
	}
	if manifest.Capabilities.Resources || len(h.srv.Resources()) > 0 {
		resourceCaps := map[string]any{fieldListChanged: true}
		if manifest.Capabilities.ResourceSubscribe {
			resourceCaps["subscribe"] = true
		}
		capabilities["resources"] = resourceCaps
	}
	if manifest.Capabilities.Prompts || len(h.srv.Prompts()) > 0 {
		capabilities["prompts"] = map[string]any{fieldListChanged: true}
	}
	// Completions and logging are advertised now that both methods are wired
	// into the dispatcher. Completions auto-advertises when a handler is
	// registered (mirroring tools/resources/prompts); logging is opt-in via the
	// Logging capability flag so a bare server advertises nothing.
	if manifest.Capabilities.Completions || h.srv.HasCompletions() {
		capabilities["completions"] = map[string]any{}
	}
	if manifest.Capabilities.Logging {
		capabilities["logging"] = map[string]any{}
	}
	// Tasks (MCP 2025-11-25): auto-advertised when any tool opts into task
	// augmentation. list/cancel are always supported; tools/call is the only
	// task-augmentable server request.
	if h.srv.HasTaskTools() {
		capabilities["tasks"] = map[string]any{
			"list":   map[string]any{},
			"cancel": map[string]any{},
			"requests": map[string]any{
				"tools": map[string]any{"call": map[string]any{}},
			},
		}
	}
	return capabilities
}

func (h *requestHandler) handleInitialize(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	manifest := h.srv.Manifest()

	// Parse the client's initialize params. Older code ignored these entirely,
	// which meant the server never negotiated the protocol version and never
	// recorded the client's capabilities. Both are parsed here now.
	var params struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
		ClientInfo      map[string]any  `json:"clientInfo"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, protocol.NewInvalidParams(err.Error())
		}
	}

	// Negotiate: echo the client's version when we support it, otherwise reply
	// with our preferred version and let the client decide whether to proceed.
	negotiatedVersion := protocol.NegotiateVersion(params.ProtocolVersion)

	// Record the client's advertised capabilities on the session (when a
	// transport has attached one) so feature gating (sampling, elicitation)
	// can consult them later in the connection.
	if session := server.SessionFromContext(ctx); session != nil && len(params.Capabilities) > 0 {
		session.SetClientCapabilitiesJSON(params.Capabilities)
	}

	capabilities := h.serverCapabilities(manifest)

	result := map[string]any{
		fieldProtocolVersion: negotiatedVersion,
		"serverInfo":         serverInfoMap(manifest),
		"capabilities":       capabilities,
	}

	// Include instructions if set
	if instructions := h.srv.Instructions(); instructions != "" {
		result["instructions"] = instructions
	}

	return protocol.NewResponse(req.ID, result), nil
}

// serverInfoMap builds the serverInfo/implementation object shared by
// initialize and server/discover.
func serverInfoMap(manifest server.Manifest) map[string]any {
	serverInfo := map[string]any{
		fieldName:    manifest.Name,
		fieldVersion: manifest.Version,
	}
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
	if manifest.BuildInfo != nil {
		serverInfo["buildInfo"] = manifest.BuildInfo // extension field, not in MCP spec
	}
	return serverInfo
}

// extensionsMap advertises the reverse-DNS extensions the server supports in
// capabilities.extensions (MCP 2026-07-28). MCP Apps is always offered (mcp-go
// serves ui:// resources); Tasks when any tool opts into augmentation.
func (h *requestHandler) extensionsMap() map[string]any {
	ext := map[string]any{
		protocol.ExtensionUI: map[string]any{},
	}
	if h.srv.HasTaskTools() {
		ext[protocol.ExtensionTasks] = map[string]any{}
	}
	return ext
}

// handleServerDiscover serves server/discover (MCP 2026-07-28): the stateless
// replacement for the initialize handshake. It reports the server's supported
// protocol versions, capabilities (including the extensions map), and identity
// in a single cacheable result.
func (h *requestHandler) handleServerDiscover(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	manifest := h.srv.Manifest()
	capabilities := h.serverCapabilities(manifest)
	capabilities["extensions"] = h.extensionsMap()

	supported := make([]string, 0, len(protocol.SupportedVersions)+1)
	supported = append(supported, protocol.DraftVersion)
	supported = append(supported, protocol.SupportedVersions...)

	result := map[string]any{
		"resultType":        protocol.ResultTypeComplete,
		"supportedVersions": supported,
		"capabilities":      capabilities,
		"serverInfo":        serverInfoMap(manifest),
	}
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
		// Top-level title (MCP 2025-06-18); tools carry it inside annotations.
		if t.Annotations != nil && t.Annotations.Title != "" {
			item["title"] = t.Annotations.Title
		}
		if t.OutputSchema != nil {
			item["outputSchema"] = t.OutputSchema
		}
		if t.Annotations != nil {
			item["annotations"] = t.Annotations
		}
		if len(t.Icons) > 0 {
			item["icons"] = t.Icons
		}
		// execution.taskSupport (MCP 2025-11-25): advertise only when the tool
		// opts in, so a plain tool's listing is unchanged.
		if t.TaskSupport == server.TaskSupportOptional || t.TaskSupport == server.TaskSupportRequired {
			item["execution"] = map[string]any{"taskSupport": string(t.TaskSupport)}
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

// toolExecutionError builds an isError CallToolResult carrying a message — used
// to surface input-validation failures to the model per SEP-1303.
func toolExecutionError(msg string) map[string]any {
	return map[string]any{
		fieldContent: []map[string]any{{fieldType: fieldText, fieldText: msg}},
		"isError":    true,
	}
}

// reconcileTaskSupport enforces a tool's execution.taskSupport against whether
// the caller augmented the request with a task: a task on a forbidden/unset
// tool, or a plain call on a required-task tool, is -32601 (Method not found)
// per MCP 2025-11-25.
func reconcileTaskSupport(mode server.TaskSupport, augmented bool, name string) error {
	switch mode {
	case server.TaskSupportRequired:
		if !augmented {
			return protocol.NewMethodNotFound("tool requires task augmentation: " + name)
		}
	case server.TaskSupportOptional:
		// either form is allowed
	default: // forbidden / unset
		if augmented {
			return protocol.NewMethodNotFound("tool does not support task augmentation: " + name)
		}
	}
	return nil
}

// startAugmentedToolCall runs a tools/call as a background task and returns the
// CreateTaskResult immediately. The closure runs the tool and builds its normal
// response so tasks/result returns exactly what a plain call would have.
func (h *requestHandler) startAugmentedToolCall(ctx context.Context, req *protocol.Request, tool *server.Tool, args json.RawMessage, ttl *int64) (*protocol.Response, error) {
	task, err := h.srv.StartAugmentedCall(ctx, ttl, func(runCtx context.Context) (any, bool, error) {
		res, execErr := tool.Execute(runCtx, args)
		if execErr != nil {
			// SEP-1303: an input error becomes a failed task carrying an
			// isError result (not a protocol error).
			var inputErr *server.ToolInputError
			if errors.As(execErr, &inputErr) {
				return toolExecutionError(inputErr.Message), true, nil
			}
			return nil, false, execErr
		}
		resp, berr := buildToolCallResponse(tool, res)
		if berr != nil {
			return nil, false, berr
		}
		if tool.Meta() != nil {
			resp["_meta"] = tool.Meta()
		}
		isErr, _ := resp["isError"].(bool)
		return resp, isErr, nil
	})
	if err != nil {
		return nil, err
	}
	return protocol.NewResponse(req.ID, map[string]any{fieldTask: task}), nil
}

func (h *requestHandler) handleToolsCall(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	// Parse params
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Task      *struct {
			TTL *int64 `json:"ttl"`
		} `json:"task"`
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

	// Task augmentation (MCP 2025-11-25): reconcile the request against the
	// tool's execution.taskSupport before executing.
	augmented := params.Task != nil
	if err := reconcileTaskSupport(tool.TaskSupport(), augmented, params.Name); err != nil {
		return nil, err
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

	// Task-augmented call: accept immediately, run in the background, and return
	// a CreateTaskResult. The requestor then polls tasks/get and fetches the
	// outcome via tasks/result.
	if augmented {
		var ttl *int64
		if params.Task != nil {
			ttl = params.Task.TTL
		}
		return h.startAugmentedToolCall(ctx, req, tool, params.Arguments, ttl)
	}

	// Execute tool. A deliberate protocol error passes through verbatim; any
	// other error is returned raw so publicError can sanitize it (no leaking
	// internal detail to the peer).
	result, err := tool.Execute(ctx, params.Arguments)
	if err != nil {
		// SEP-1303: input problems are tool execution errors, not protocol
		// errors, so the model can self-correct.
		var inputErr *server.ToolInputError
		if errors.As(err, &inputErr) {
			return protocol.NewResponse(req.ID, toolExecutionError(inputErr.Message)), nil
		}
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, err
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
	response[fieldContent] = []map[string]any{
		{
			fieldType: fieldText,
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
		response[fieldContent] = v.Content
	} else {
		response[fieldContent] = []map[string]any{}
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
			fieldURI:  r.URITemplate,
			fieldName: r.Name,
		}
		if r.Title != "" {
			item["title"] = r.Title
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
		if len(r.Icons) > 0 {
			item["icons"] = r.Icons
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

	// Read resource. Protocol errors pass through; other errors are returned
	// raw for publicError to sanitize.
	content, err := resource.Read(ctx, params.URI)
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, err
	}

	result := map[string]any{
		"contents": []map[string]any{
			{
				fieldURI:   content.URI,
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

func (h *requestHandler) handleResourcesSubscribe(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if !h.srv.ResourceSubscriptionsEnabled() {
		return nil, protocol.NewMethodNotFound(req.Method)
	}
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	if params.URI == "" {
		return nil, protocol.NewInvalidParams("subscribe requires a uri")
	}
	clientID := transport.ClientIDFromContext(ctx)
	if clientID == "" {
		return nil, protocol.NewInvalidParams("resources/subscribe requires a client stream (connect to /mcp/sse first)")
	}
	h.srv.SubscribeResource(clientID, params.URI)
	return protocol.NewResponse(req.ID, map[string]any{}), nil
}

func (h *requestHandler) handleResourcesUnsubscribe(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if !h.srv.ResourceSubscriptionsEnabled() {
		return nil, protocol.NewMethodNotFound(req.Method)
	}
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	clientID := transport.ClientIDFromContext(ctx)
	if clientID != "" {
		h.srv.UnsubscribeResource(clientID, params.URI)
	}
	return protocol.NewResponse(req.ID, map[string]any{}), nil
}

// handleSubscriptionsListen serves subscriptions/listen (MCP 2026-07-28, SEP):
// the stateless replacement for the GET SSE stream plus resources/subscribe and
// resources/unsubscribe. A modern client declares the notification methods it
// wants delivered (e.g. notifications/resources/updated) and, optionally, the
// resource URIs it cares about. The requested URIs are registered on the
// request-scoped session's SubscriptionManager — the same machinery
// resources/subscribe drives — and the handler returns a stable, non-empty
// subscriptionId. Subsequent notifications carry that id under
// _meta[io.modelcontextprotocol/subscriptionId] so the client can correlate
// them with this listen call.
//
// This increment covers protocol negotiation and server-side registration only.
// Realizing the long-lived POST-response stream over which the tagged
// notifications actually flow is a deferred transport follow-up; nothing under
// transport/ is wired to it yet.
func (h *requestHandler) handleSubscriptionsListen(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	// subscriptions/listen is a modern (stateless) method: the request-scoped
	// session is built from the per-request _meta. Its absence means the caller
	// did not send a modern request, so there is nothing to register against.
	session := server.SessionFromContext(ctx)
	if session == nil {
		return nil, protocol.NewInvalidParams("subscriptions/listen requires a modern request (per-request _meta)")
	}

	var params struct {
		Notifications []string `json:"notifications"`
		URIs          []string `json:"uris"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, protocol.NewInvalidParams(err.Error())
		}
	}

	// Register each requested resource URI on the session's subscription
	// manager. An empty URI is rejected rather than silently registered.
	for _, uri := range params.URIs {
		if uri == "" {
			return nil, protocol.NewInvalidParams("subscriptions/listen uris must be non-empty")
		}
		session.Subscribe(uri)
	}

	id, err := newSubscriptionID()
	if err != nil {
		return nil, err
	}
	return protocol.NewResponse(req.ID, map[string]any{"subscriptionId": id}), nil
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
		if p.Title != "" {
			item["title"] = p.Title
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
		if len(p.Icons) > 0 {
			item["icons"] = p.Icons
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

	// Execute prompt. Protocol errors pass through; other errors are returned
	// raw for publicError to sanitize (a handler failure is internal, not a
	// client params error).
	result, err := prompt.Get(ctx, params.Arguments)
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, err
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

// handleInitialized acknowledges the client's notifications/initialized. It is a
// notification (no id), so it produces no response — but wiring it explicitly
// stops it from falling through to MethodNotFound.
func (h *requestHandler) handleInitialized(_ context.Context, _ *protocol.Request) (*protocol.Response, error) {
	return nil, nil
}

// handleCompletion serves completion/complete, delegating to the server's
// registered prompt/resource completion handlers. Previously the registry
// existed but was never reachable over the wire.
func (h *requestHandler) handleCompletion(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		Ref      server.CompletionRef      `json:"ref"`
		Argument server.CompletionArgument `json:"argument"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	result, err := h.srv.HandleCompletion(ctx, params.Ref, params.Argument)
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			return nil, mcpErr
		}
		return nil, err
	}

	// MCP nests the payload under "completion".
	return protocol.NewResponse(req.ID, map[string]any{
		"completion": map[string]any{
			"values":  result.Values,
			"total":   result.Total,
			"hasMore": result.HasMore,
		},
	}), nil
}

// handleLoggingSetLevel serves logging/setLevel. The level is validated and, when
// a session is attached to the request context, applied to that session so
// subsequent notifications/message are filtered accordingly.
func (h *requestHandler) handleLoggingSetLevel(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params server.SetLevelRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	if !server.IsValidLogLevel(params.Level) {
		return nil, protocol.NewInvalidParams("unknown log level: " + string(params.Level))
	}
	if session := server.SessionFromContext(ctx); session != nil {
		session.SetLogLevel(params.Level)
	}
	return protocol.NewResponse(req.ID, map[string]any{}), nil
}

// handleResourcesTemplatesList serves resources/templates/list, exposing the
// URI-template resources the server registered.
func (h *requestHandler) handleResourcesTemplatesList(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	templates := h.srv.ResourceTemplates()
	list := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		if h.resourceFilter != nil && !h.resourceFilter(ctx, t.URITemplate, t.Name) {
			continue
		}
		item := map[string]any{
			"uriTemplate": t.URITemplate,
			fieldName:     t.Name,
		}
		if t.Title != "" {
			item["title"] = t.Title
		}
		if t.Description != "" {
			item["description"] = t.Description
		}
		if t.MimeType != "" {
			item["mimeType"] = t.MimeType
		}
		if t.Annotations != nil {
			item["annotations"] = t.Annotations
		}
		if len(t.Icons) > 0 {
			item["icons"] = t.Icons
		}
		list = append(list, item)
	}
	return protocol.NewResponse(req.ID, map[string]any{"resourceTemplates": list}), nil
}

// relatedTaskMeta is the _meta key that associates a message with its task.
const relatedTaskMetaKey = "io.modelcontextprotocol/related-task"

// handleTasksGet serves tasks/get: return the task's current state.
func (h *requestHandler) handleTasksGet(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	task, ok := h.srv.GetAugTask(params.TaskID)
	if !ok {
		return nil, protocol.NewInvalidParams("task not found: " + params.TaskID)
	}
	return protocol.NewResponse(req.ID, task), nil
}

// handleTasksResult serves tasks/result: block until the task is terminal, then
// return exactly what the underlying request would have returned (with the
// related-task _meta association).
func (h *requestHandler) handleTasksResult(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	result, execErr, err := h.srv.AwaitAugTaskResult(ctx, params.TaskID)
	if err != nil {
		if errors.Is(err, server.ErrAugTaskNotFound) {
			return nil, protocol.NewInvalidParams("task not found: " + params.TaskID)
		}
		return nil, err // ctx cancelled, etc.
	}
	if execErr != nil {
		// The underlying request produced a JSON-RPC error; tasks/result must
		// return that same error.
		var mcpErr *protocol.Error
		if errors.As(execErr, &mcpErr) {
			return nil, mcpErr
		}
		return nil, execErr
	}
	resp, ok := result.(map[string]any)
	if !ok || resp == nil {
		// Cancelled (or resultless) task: surface as a tool execution error.
		resp = map[string]any{
			fieldContent: []map[string]any{{fieldType: fieldText, fieldText: "task did not produce a result"}},
			"isError":    true,
		}
	}
	attachRelatedTask(resp, params.TaskID)
	return protocol.NewResponse(req.ID, resp), nil
}

// handleTasksCancel serves tasks/cancel.
func (h *requestHandler) handleTasksCancel(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	task, err := h.srv.CancelAugTask(params.TaskID)
	if err != nil {
		// Both unknown-task and already-terminal map to -32602 per spec.
		return nil, protocol.NewInvalidParams(err.Error())
	}
	return protocol.NewResponse(req.ID, task), nil
}

// handleTasksUpdate serves tasks/update (MCP 2026-07-28 tasks extension): refresh
// a non-terminal task's ttl so a slow task is not evicted before it finishes. A
// null ttl clears the deadline. Unknown/terminal tasks map to -32602.
func (h *requestHandler) handleTasksUpdate(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		TaskID string `json:"taskId"`
		TTL    *int64 `json:"ttl"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	task, err := h.srv.UpdateAugTask(params.TaskID, params.TTL)
	if err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}
	return protocol.NewResponse(req.ID, task), nil
}

// handleTasksList serves tasks/list with cursor pagination.
func (h *requestHandler) handleTasksList(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		Cursor string `json:"cursor"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, protocol.NewInvalidParams(err.Error())
		}
	}
	tasks, next := h.srv.ListAugTasks(params.Cursor, 0)
	result := map[string]any{"tasks": tasks}
	if next != "" {
		result["nextCursor"] = next
	}
	return protocol.NewResponse(req.ID, result), nil
}

// attachRelatedTask records the io.modelcontextprotocol/related-task association
// in a result's _meta.
func attachRelatedTask(resp map[string]any, taskID string) {
	meta, _ := resp["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[relatedTaskMetaKey] = map[string]any{fieldTaskID: taskID}
	resp["_meta"] = meta
}

// notificationAdapter adapts transport.NotificationSender to server.NotificationSender.
type notificationAdapter struct {
	sender transport.NotificationSender
}

func (a *notificationAdapter) SendNotification(method string, params any) error {
	return a.sender.SendNotification(method, params)
}
