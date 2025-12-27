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
	"encoding/json"
	"errors"
	"time"

	"github.com/felixgeelhaar/mcp-go/middleware"
	"github.com/felixgeelhaar/mcp-go/protocol"
	"github.com/felixgeelhaar/mcp-go/server"
	"github.com/felixgeelhaar/mcp-go/transport"
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
	middleware []Middleware
	logger     Logger
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

// NewServer creates a new MCP server with the given info and options.
func NewServer(info ServerInfo, opts ...Option) *Server {
	return server.New(info, opts...)
}

// WithInstructions sets the server instructions that provide context to AI models
// about how to use this server effectively.
var WithInstructions = server.WithInstructions

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
	return middleware.F(key, value)
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
	srv        *Server
	handleFunc middleware.HandlerFunc
}

func newRequestHandler(srv *Server, opts ...ServeOption) *requestHandler {
	options := &serveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	h := &requestHandler{srv: srv}

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

func (h *requestHandler) handle(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	switch req.Method {
	case protocol.MethodInitialize:
		return h.handleInitialize(req)
	case protocol.MethodToolsList:
		return h.handleToolsList(req)
	case protocol.MethodToolsCall:
		return h.handleToolsCall(ctx, req)
	case protocol.MethodResourcesList:
		return h.handleResourcesList(req)
	case protocol.MethodResourcesRead:
		return h.handleResourcesRead(ctx, req)
	case protocol.MethodPromptsList:
		return h.handlePromptsList(req)
	case protocol.MethodPromptsGet:
		return h.handlePromptsGet(ctx, req)
	case protocol.MethodPing:
		return h.handlePing(req)
	default:
		return nil, protocol.NewMethodNotFound(req.Method)
	}
}

func (h *requestHandler) handleInitialize(req *protocol.Request) (*protocol.Response, error) {
	manifest := h.srv.Manifest()

	// Build capabilities based on what's registered
	capabilities := make(map[string]any)

	if manifest.Capabilities.Tools {
		capabilities["tools"] = map[string]any{}
	}
	if manifest.Capabilities.Resources {
		capabilities["resources"] = map[string]any{}
	}
	if manifest.Capabilities.Prompts {
		capabilities["prompts"] = map[string]any{}
	}

	result := map[string]any{
		"protocolVersion": manifest.ProtocolVersion,
		"serverInfo": map[string]any{
			"name":    manifest.Name,
			"version": manifest.Version,
		},
		"capabilities": capabilities,
	}

	// Include instructions if set
	if instructions := h.srv.Instructions(); instructions != "" {
		result["instructions"] = instructions
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handleToolsList(req *protocol.Request) (*protocol.Response, error) {
	tools := h.srv.Tools()

	toolList := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		item := map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
		if t.Annotations != nil {
			item["annotations"] = t.Annotations
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

	// Get tool
	tool, ok := h.srv.GetTool(params.Name)
	if !ok {
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

	// Format result
	response := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": result,
			},
		},
	}

	return protocol.NewResponse(req.ID, response), nil
}

func (h *requestHandler) handleResourcesList(req *protocol.Request) (*protocol.Response, error) {
	resources := h.srv.Resources()

	resourceList := make([]map[string]any, 0, len(resources))
	for _, r := range resources {
		item := map[string]any{
			"uri":  r.URITemplate,
			"name": r.Name,
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

	// Find resource that matches the URI
	resource, ok := h.srv.FindResourceForURI(params.URI)
	if !ok {
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
				"text":     content.Text,
			},
		},
	}

	// Include blob if present
	if content.Blob != "" {
		result["contents"].([]map[string]any)[0]["blob"] = content.Blob
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handlePromptsList(req *protocol.Request) (*protocol.Response, error) {
	prompts := h.srv.Prompts()

	promptList := make([]map[string]any, 0, len(prompts))
	for _, p := range prompts {
		item := map[string]any{
			"name": p.Name,
		}
		if p.Description != "" {
			item["description"] = p.Description
		}
		if len(p.Arguments) > 0 {
			args := make([]map[string]any, 0, len(p.Arguments))
			for _, arg := range p.Arguments {
				argItem := map[string]any{
					"name":     arg.Name,
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

	// Get prompt
	prompt, ok := h.srv.GetPrompt(params.Name)
	if !ok {
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

func (h *requestHandler) handlePing(req *protocol.Request) (*protocol.Response, error) {
	return protocol.NewResponse(req.ID, map[string]any{}), nil
}

// notificationAdapter adapts transport.NotificationSender to server.NotificationSender.
type notificationAdapter struct {
	sender transport.NotificationSender
}

func (a *notificationAdapter) SendNotification(method string, params any) error {
	return a.sender.SendNotification(method, params)
}
