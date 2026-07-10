package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// defaultMaxRequestBytes bounds a single POST /mcp body. JSON-RPC requests are
// small; the cap stops a client from exhausting memory with an unbounded body.
const defaultMaxRequestBytes = 4 << 20 // 4 MiB

// defaultMaxSSEConnections caps concurrently registered SSE push channels so a
// flood of connections cannot exhaust server memory.
const defaultMaxSSEConnections = 1024

// healthStatusField is the JSON field name used in /health and /healthz
// payloads.
const healthStatusField = "status"

type HTTP struct {
	addr             string
	readTimeout      time.Duration
	writeTimeout     time.Duration
	shutdownTimeout  time.Duration
	drainDelay       time.Duration
	corsConfig       *CORSConfig
	sessionStore     SessionStore
	discovery        *ServerDiscovery
	tlsConfig        *tls.Config
	requestContextFn func(context.Context, *http.Request) context.Context
	authorizeFn      func(*http.Request) error

	allowedOrigins  []string
	allowAllOrigins bool
	maxRequestBytes int64
	maxSSEClients   int

	// streamable enables the modern Streamable HTTP transport (MCP 2025-03-26):
	// a single /mcp endpoint that accepts POST (JSON or SSE-framed replies), GET
	// (a standing server->client SSE stream), and DELETE (session teardown),
	// with an Mcp-Session-Id header minted on initialize and echoed thereafter.
	streamable bool

	// stateless switches the streamable POST path to the MCP 2026-07-28 model:
	// the Mcp-Session-Id lifecycle is dropped (no minting, no per-request header
	// requirement) and the Mcp-Method routing header is hard-required (a POST that
	// omits it is rejected with -32020) rather than merely validated-when-present.
	// Implies streamable.
	stateless bool

	mu         sync.RWMutex
	listenAddr string
	server     *http.Server

	sseClients   map[string]chan []byte
	sseClientsMu sync.RWMutex

	onDisconnect func(clientID string)
}

// SetDisconnectHook registers a callback invoked when an SSE client
// disconnects, so the server can release per-client state (e.g. resource
// subscriptions). Safe to call before Serve.
func (h *HTTP) SetDisconnectHook(fn func(clientID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onDisconnect = fn
}

// NotifyClient delivers a JSON-RPC notification to one connected SSE client.
// It satisfies the server's ResourceNotifier contract structurally, letting
// the server push resources/updated to subscribers. Returns an error if the
// client is not connected or its buffer is full.
func (h *HTTP) NotifyClient(clientID, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("notify %s: marshal params: %w", clientID, err)
	}
	msg, err := json.Marshal(protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  raw,
	})
	if err != nil {
		return fmt.Errorf("notify %s: marshal notification: %w", clientID, err)
	}
	if !h.SendTo(clientID, msg) {
		return fmt.Errorf("notify %s: client not connected or buffer full", clientID)
	}
	return nil
}

type HTTPOption func(*HTTP)

func WithReadTimeout(d time.Duration) HTTPOption {
	return func(h *HTTP) {
		h.readTimeout = d
	}
}

func WithWriteTimeout(d time.Duration) HTTPOption {
	return func(h *HTTP) {
		h.writeTimeout = d
	}
}

func WithSessionStore(store SessionStore) HTTPOption {
	return func(h *HTTP) {
		h.sessionStore = store
	}
}

func WithDiscovery(discovery *ServerDiscovery) HTTPOption {
	return func(h *HTTP) {
		h.discovery = discovery
	}
}

// WithTLSConfig enables embedded TLS termination. When set, the
// transport serves over HTTPS using the supplied *tls.Config — bring
// your own certificate loading, rotation, and verification strategy
// (crypto/tls.LoadX509KeyPair, autocert, SPIFFE workload API, etc.).
//
// Set Certificates or GetCertificate on the config for plain HTTPS;
// add ClientCAs and ClientAuth for mTLS. mcp-go does not validate the
// config — pass tls.Config.Clone() if you want to keep mutating the
// original after handing it off.
//
// When TLS is enabled, the WebSocket upgrade path inherits HTTPS
// automatically since both share the same *http.Server.
func WithTLSConfig(cfg *tls.Config) HTTPOption {
	return func(h *HTTP) {
		h.tlsConfig = cfg
	}
}

// WithRequestContextFn registers a function that runs once per HTTP
// request, before mcp-go unwraps the JSON-RPC payload into a
// protocol.Request. The returned context propagates through the
// handler and middleware chain via normal context semantics.
//
// This is the place to derive request-scoped identity from transport
// details that middleware (operating on the unwrapped protocol.Request)
// cannot see. The canonical use is mTLS: read the verified client
// certificate and stash a caller-defined value for downstream authz.
// mcp-go ships no Identity type and never handles auth — use your own
// context key.
//
//	type callerKey struct{}
//
//	NewHTTP(addr,
//	    WithTLSConfig(mtlsCfg),
//	    WithRequestContextFn(func(ctx context.Context, r *http.Request) context.Context {
//	        if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
//	            cn := r.TLS.PeerCertificates[0].Subject.CommonName
//	            ctx = context.WithValue(ctx, callerKey{}, cn)
//	        }
//	        return ctx
//	    }),
//	)
//
// The function must return a non-nil context derived from the one
// passed in; returning a context unrelated to the request breaks
// cancellation and deadlines.
func WithRequestContextFn(fn func(context.Context, *http.Request) context.Context) HTTPOption {
	return func(h *HTTP) {
		h.requestContextFn = fn
	}
}

// WithAuthorize registers an authorization gate that runs on both POST /mcp
// and the SSE push stream (GET /mcp/sse) before any work is done. Returning a
// non-nil error rejects the request with 403 Forbidden, so the same policy
// protects the request/response path and the server-push stream — the SSE
// stream is otherwise unauthenticated by construction. The function sees the
// raw *http.Request (headers, TLS peer certs, etc.); mcp-go ships no identity
// type and performs no auth of its own.
func WithAuthorize(fn func(*http.Request) error) HTTPOption {
	return func(h *HTTP) {
		h.authorizeFn = fn
	}
}

// WithAllowedOrigins restricts which browser Origins may reach POST /mcp and
// the SSE stream. Pass exact origins (scheme://host[:port]). Requests without
// an Origin header (non-browser clients) are always allowed; browser requests
// from a listed origin, a same-origin Host, or a loopback address are allowed
// by default. This is the primary defense against DNS-rebinding and cross-site
// access to a localhost server.
func WithAllowedOrigins(origins ...string) HTTPOption {
	return func(h *HTTP) {
		h.allowedOrigins = origins
	}
}

// WithInsecureAllowAllOrigins disables Origin enforcement entirely, restoring
// the pre-hardening behavior where any website could reach the server. Named
// "insecure" deliberately: only use it behind a trusted gateway that performs
// its own origin/CSRF checks.
func WithInsecureAllowAllOrigins() HTTPOption {
	return func(h *HTTP) {
		h.allowAllOrigins = true
	}
}

// WithMaxRequestBytes caps the POST /mcp request body. A non-positive value
// disables the limit. Default: 4 MiB.
func WithMaxRequestBytes(n int64) HTTPOption {
	return func(h *HTTP) {
		h.maxRequestBytes = n
	}
}

// WithMaxSSEConnections caps the number of concurrent SSE push connections;
// once reached, new connections receive 503. A non-positive value disables the
// cap. Default: 1024.
func WithMaxSSEConnections(n int) HTTPOption {
	return func(h *HTTP) {
		h.maxSSEClients = n
	}
}

// WithStreamable enables the modern Streamable HTTP transport (MCP 2025-03-26)
// on the /mcp endpoint, in addition to the legacy POST /mcp + GET /mcp/sse
// endpoints, which remain available for backward compatibility.
//
// In streamable mode the single /mcp endpoint:
//   - POST accepts a JSON-RPC request and replies either with a single JSON
//     object (Content-Type: application/json) or an SSE stream
//     (Content-Type: text/event-stream), negotiated via the request's Accept
//     header. Notifications sent by handlers during an SSE-framed reply are
//     streamed as SSE data frames ahead of the final response frame.
//   - GET opens a standing server->client SSE stream, keyed by Mcp-Session-Id,
//     for notifications delivered outside a request/response exchange.
//   - DELETE tears down the session named by Mcp-Session-Id.
//
// The server mints an Mcp-Session-Id on the initialize response and requires it
// to be echoed on every subsequent request. All existing security controls
// (origin checks, max body size, authorize hook, request-context hook) apply to
// the streamable paths unchanged.
func WithStreamable() HTTPOption {
	return func(h *HTTP) {
		h.streamable = true
	}
}

// WithStreamableStateless enables the modern Streamable HTTP transport in the
// stateless (MCP 2026-07-28) model. It implies WithStreamable and additionally:
//
//   - drops the Mcp-Session-Id lifecycle — no session id is minted on initialize
//     and none is required on subsequent POSTs (every request self-describes via
//     its `_meta`, so no server-side session correlation is needed);
//   - hard-requires the Mcp-Method routing header on every POST (absent → -32020),
//     rather than the default validate-when-present behavior.
//
// It is opt-in and does not affect the legacy (session-negotiated) streamable
// path when left off.
func WithStreamableStateless() HTTPOption {
	return func(h *HTTP) {
		h.streamable = true
		h.stateless = true
	}
}

func NewHTTP(addr string, opts ...HTTPOption) *HTTP {
	h := &HTTP{
		addr:            addr,
		readTimeout:     30 * time.Second,
		writeTimeout:    30 * time.Second,
		shutdownTimeout: 30 * time.Second,
		maxRequestBytes: defaultMaxRequestBytes,
		maxSSEClients:   defaultMaxSSEConnections,
		sseClients:      make(map[string]chan []byte),
		sessionStore:    NewInMemoryStore(),
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

func (h *HTTP) Addr() string {
	return h.addr
}

func (h *HTTP) ListenAddr() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.listenAddr
}

func (h *HTTP) Serve(ctx context.Context, handler Handler) error {
	httpHandler := h.createHandler(handler)

	listener, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	h.mu.Lock()
	h.listenAddr = listener.Addr().String()
	h.server = &http.Server{
		Handler:      httpHandler,
		ReadTimeout:  h.readTimeout,
		WriteTimeout: h.writeTimeout,
		TLSConfig:    h.tlsConfig,
	}
	tlsEnabled := h.tlsConfig != nil
	h.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		var srvErr error
		if tlsEnabled {
			// ServeTLS reads cert/key from h.server.TLSConfig.Certificates
			// (or GetCertificate) — the empty filename args are
			// intentional and required by the stdlib API.
			srvErr = h.server.ServeTLS(listener, "", "")
		} else {
			srvErr = h.server.Serve(listener)
		}
		if srvErr != nil && srvErr != http.ErrServerClosed {
			errCh <- srvErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		if h.drainDelay > 0 {
			time.Sleep(h.drainDelay)
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), h.shutdownTimeout)
		defer cancel()
		if err := h.server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (h *HTTP) createHandler(handler Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{healthStatusField: "ok"})
	})

	mux.HandleFunc("/healthz", h.handleHealthz)

	if h.discovery != nil {
		mux.Handle("/.well-known/mcp", h.discovery)
	}

	mux.HandleFunc("/mcp/sse", func(w http.ResponseWriter, r *http.Request) {
		h.handleSSE(w, r)
	})

	if h.streamable {
		// Streamable HTTP (MCP 2025-03-26): one endpoint multiplexes POST
		// (request/reply), GET (standing server-push stream), and DELETE
		// (session teardown). The legacy /mcp/sse endpoint above stays wired for
		// backward compatibility.
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
			h.handleStreamable(w, r, handler)
		})
	} else {
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
			h.handleMCP(w, r, handler)
		})
	}

	if h.corsConfig != nil {
		return CORSHandler(*h.corsConfig, mux)
	}

	return mux
}

func (h *HTTP) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := "ok"
	ready := true

	if h.sessionStore != nil {
		if _, err := h.sessionStore.ListSessions(r.Context()); err != nil {
			status = "degraded"
			ready = false
		}
	}

	httpStatus := http.StatusOK
	if !ready {
		httpStatus = http.StatusServiceUnavailable
	}

	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(map[string]any{
		healthStatusField: status,
		"ready":           ready,
	})
}

func (h *HTTP) handleMCP(w http.ResponseWriter, r *http.Request, handler Handler) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !originAllowed(r, h.allowedOrigins, h.allowAllOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if h.authorizeFn != nil {
		if err := h.authorizeFn(r); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	if h.requestContextFn != nil {
		ctx = h.requestContextFn(ctx, r)
	}
	// Correlate this request with the client's server-push stream so handlers
	// like resources/subscribe can target it. The client echoes the clientId
	// it received on its SSE connection.
	//
	// Unlike stdio/websocket, HTTP does not inject a per-connection
	// server.Session here: POST requests are stateless, so a per-request
	// session would not carry the client capabilities recorded at initialize
	// across to later tool calls, making capability gating unreliable. Session
	// injection for HTTP is wired in Phase 1 alongside the Streamable HTTP
	// transport and the per-clientId session store (see
	// docs/revisions-roadmap.md).
	if clientID := r.URL.Query().Get("clientId"); clientID != "" {
		ctx = ContextWithClientID(ctx, clientID)
	}

	if h.maxRequestBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.maxRequestBytes)
	}

	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		resp := protocol.NewErrorResponse(nil, protocol.NewParseError("Invalid JSON"))
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	resp, err := handler.HandleRequest(ctx, &req)

	// A JSON-RPC notification carries no id and, per spec, MUST NOT be
	// answered with a response body. Acknowledge receipt and stop.
	if req.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if err != nil {
		resp = protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error()))
	}

	if resp != nil {
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (h *HTTP) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	if !originAllowed(r, h.allowedOrigins, h.allowAllOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if h.authorizeFn != nil {
		if err := h.authorizeFn(r); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// Derive request-scoped identity the same way the request/response path
	// does, so an mTLS or header-derived caller value is available for the
	// lifetime of the push stream instead of being silently skipped.
	ctx := r.Context()
	if h.requestContextFn != nil {
		ctx = h.requestContextFn(ctx, r)
	}

	// A client that uses the server-push protocol picks its own unguessable
	// (crypto/rand) correlation id and sends it on both this SSE connection and
	// its POSTs. We honor it for correlation but NEVER let it overwrite a live
	// entry (see registerSSEClient) — that is what stops an attacker from
	// picking a victim's id to steal their push channel. When no id is supplied
	// we mint one server-side with crypto/rand rather than a guessable
	// timestamp.
	clientID := r.URL.Query().Get("clientId")
	if clientID == "" {
		minted, err := newSessionID()
		if err != nil {
			http.Error(w, "failed to allocate session", http.StatusInternalServerError)
			return
		}
		clientID = minted
	}

	messageCh := make(chan []byte, 10)
	if !h.registerSSEClient(clientID, messageCh) {
		// Either the connection cap is reached or the id is already registered.
		// Refusing rather than clobbering keeps an existing client's channel
		// intact (no hijack) and avoids orphaning it via a colliding delete.
		http.Error(w, "connection refused", http.StatusServiceUnavailable)
		return
	}

	defer func() {
		h.sseClientsMu.Lock()
		// Only remove the entry if it still points at THIS connection's
		// channel, so a later reconnect that reused the id is not orphaned.
		if h.sseClients[clientID] == messageCh {
			delete(h.sseClients, clientID)
		}
		h.sseClientsMu.Unlock()
		h.mu.RLock()
		hook := h.onDisconnect
		h.mu.RUnlock()
		if hook != nil {
			hook(clientID)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if h.sessionStore != nil {
		w.Header().Set("Link", fmt.Sprintf(`<%s/mcp/sse?clientId=%s>; rel="stream"`, h.Addr(), clientID))
	}

	// Use the shared SSE writer so the event grammar matches the client's
	// shared SSE reader (no duplicated "data: " framing).
	sse := NewSSEWriter(w, flusher)
	escapedClientID, _ := json.Marshal(clientID)
	_ = sse.WriteEvent("connected", []byte(fmt.Sprintf(`{"clientId":%s}`, string(escapedClientID))))

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messageCh:
			if !ok {
				return
			}
			_ = sse.WriteData(msg)
		}
	}
}

// registerSSEClient atomically enforces the concurrent-connection cap and
// refuses to overwrite an id already present, then records the push channel.
// It returns false when the cap is reached or the id collides, so the caller
// can reject with 503.
func (h *HTTP) registerSSEClient(clientID string, ch chan []byte) bool {
	h.sseClientsMu.Lock()
	defer h.sseClientsMu.Unlock()
	if h.maxSSEClients > 0 && len(h.sseClients) >= h.maxSSEClients {
		return false
	}
	if _, exists := h.sseClients[clientID]; exists {
		return false
	}
	h.sseClients[clientID] = ch
	return true
}

func (h *HTTP) Broadcast(data []byte) {
	h.sseClientsMu.RLock()
	defer h.sseClientsMu.RUnlock()

	for _, ch := range h.sseClients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (h *HTTP) SendTo(clientID string, data []byte) bool {
	h.sseClientsMu.RLock()
	defer h.sseClientsMu.RUnlock()

	if ch, ok := h.sseClients[clientID]; ok {
		select {
		case ch <- data:
			return true
		default:
			return false
		}
	}
	return false
}

// mcpSessionHeader is the HTTP header carrying the Streamable HTTP session id.
const mcpSessionHeader = "Mcp-Session-Id"

// Modern (MCP 2026-07-28) Streamable HTTP routing headers. They let an
// intermediary route a POST without parsing its JSON body: Mcp-Method mirrors
// the JSON-RPC method, and Mcp-Name mirrors the body's primary named target
// (tools/call -> name, resources/read -> uri, prompts/get -> name).
const (
	mcpMethodHeader = "Mcp-Method"
	mcpNameHeader   = "Mcp-Name"
)

// routingParams carries the body fields the modern routing headers mirror. Only
// the primary named target of a name-bearing method is captured; other params
// are ignored so the parse stays cheap and tolerant of unknown fields.
type routingParams struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

// bodyRouteName returns the primary named target the Mcp-Name header must match
// for a name-bearing method, and ok=false for methods that have no such target
// (Mcp-Name is then not validated). tools/call and prompts/get key on the
// "name" param; resources/read keys on the "uri" param.
func bodyRouteName(req *protocol.Request) (string, bool) {
	var field *string
	var params routingParams
	switch req.Method {
	case protocol.MethodToolsCall, protocol.MethodPromptsGet:
		field = &params.Name
	case protocol.MethodResourcesRead:
		field = &params.URI
	default:
		return "", false
	}
	if len(req.Params) > 0 {
		// Ignore malformed params here: a body that fails to unmarshal is caught
		// downstream by the handler; treat the route name as empty for matching.
		_ = json.Unmarshal(req.Params, &params)
	}
	return *field, true
}

// validateRoutingHeaders enforces the modern routing headers when present
// (validate-when-present): a supplied Mcp-Method must equal the body method, and
// a supplied Mcp-Name must equal the body's primary named target for
// name-bearing methods. It returns a -32020 error on mismatch, else nil.
//
// The headers are validated only, not required. The roadmap lists them as
// "required" for the stateless revision; hard-requiring them (rejecting a POST
// that omits Mcp-Method) is a deferred follow-up, most likely gated behind an
// explicit Stateless option, since the modern Streamable transport is still
// opt-in/experimental here.
func validateRoutingHeaders(r *http.Request, req *protocol.Request) *protocol.Error {
	if hdr := r.Header.Get(mcpMethodHeader); hdr != "" && hdr != req.Method {
		return protocol.NewHeaderMismatch(fmt.Sprintf(
			"Mcp-Method header %q does not match request method %q", hdr, req.Method))
	}
	if hdr := r.Header.Get(mcpNameHeader); hdr != "" {
		if want, named := bodyRouteName(req); named && hdr != want {
			return protocol.NewHeaderMismatch(fmt.Sprintf(
				"Mcp-Name header %q does not match request target %q", hdr, want))
		}
	}
	return nil
}

// notifierFunc adapts a plain function to the NotificationSender interface so a
// per-request notification sink can be injected into the handler context.
type notifierFunc func(method string, params any) error

// SendNotification implements NotificationSender.
func (f notifierFunc) SendNotification(method string, params any) error { return f(method, params) }

// marshalNotification encodes a JSON-RPC notification (a request with no id).
func marshalNotification(method string, params any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	msg, err := json.Marshal(protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  raw,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal notification: %w", err)
	}
	return msg, nil
}

// sessionStreamNotifier returns a NotificationSender that routes notifications
// to the session's standing GET SSE stream (if one is open). Used for the
// JSON-framed POST path, where in-handler notifications cannot ride the reply.
func (h *HTTP) sessionStreamNotifier(sessionID string) NotificationSender {
	return notifierFunc(func(method string, params any) error {
		msg, err := marshalNotification(method, params)
		if err != nil {
			return err
		}
		if !h.SendTo(sessionID, msg) {
			return fmt.Errorf("session %s: no standing stream or buffer full", sessionID)
		}
		return nil
	})
}

// handleStreamable multiplexes the Streamable HTTP methods on the single /mcp
// endpoint. Security controls (origin, authorize) are applied per-method by the
// concrete handlers so the same policy guards every verb.
func (h *HTTP) handleStreamable(w http.ResponseWriter, r *http.Request, handler Handler) {
	switch r.Method {
	case http.MethodPost:
		h.handleStreamablePost(w, r, handler)
	case http.MethodGet:
		h.handleStreamableGet(w, r)
	case http.MethodDelete:
		h.handleStreamableDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleStreamablePost handles a JSON-RPC request POSTed to the streamable
// endpoint. It mints a session id on initialize, enforces the echoed
// Mcp-Session-Id on subsequent requests, and replies with either a single JSON
// object or an SSE stream depending on the client's Accept header.
// resolveStreamableSession applies the Streamable HTTP session lifecycle: it
// mints a new Mcp-Session-Id on initialize (persisting a marker in the session
// store) and, for every other request, requires and validates the header
// against the store. It returns the resolved session id and ok=false when it
// has already written an error response (the caller must then return).
func (h *HTTP) resolveStreamableSession(w http.ResponseWriter, r *http.Request, ctx context.Context, req *protocol.Request) (string, bool) {
	sessionID := r.Header.Get(mcpSessionHeader)
	switch {
	case req.Method == protocol.MethodInitialize:
		minted, err := newSessionID()
		if err != nil {
			http.Error(w, "failed to allocate session", http.StatusInternalServerError)
			return "", false
		}
		sessionID = minted
		if h.sessionStore != nil {
			// Store a marker so later requests validate against a known session.
			if err := h.sessionStore.StoreSession(ctx, sessionID, []byte("{}")); err != nil {
				http.Error(w, "failed to persist session", http.StatusInternalServerError)
				return "", false
			}
		}
	case h.sessionStore != nil:
		if sessionID == "" {
			http.Error(w, "Mcp-Session-Id header required", http.StatusBadRequest)
			return "", false
		}
		data, err := h.sessionStore.GetSession(ctx, sessionID)
		if err != nil || data == nil {
			http.Error(w, "unknown or expired session", http.StatusNotFound)
			return "", false
		}
	}
	return sessionID, true
}

// resolveStreamablePOSTSession applies the POST-path session policy and returns
// (sessionID, ctx, ok). In stateless mode (MCP 2026-07-28) it enforces the
// mandatory Mcp-Method header and mints no session id; otherwise it runs the
// Mcp-Session-Id lifecycle and echoes the id, threading it onto the context. It
// returns ok=false when it has already written an error response.
func (h *HTTP) resolveStreamablePOSTSession(w http.ResponseWriter, r *http.Request, ctx context.Context, req *protocol.Request) (string, context.Context, bool) {
	if h.stateless {
		if r.Header.Get(mcpMethodHeader) == "" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(protocol.NewErrorResponse(req.ID,
				protocol.NewHeaderMismatch("Mcp-Method header is required in stateless mode")))
			return "", ctx, false
		}
		return "", ctx, true
	}
	sessionID, ok := h.resolveStreamableSession(w, r, ctx, req)
	if !ok {
		return "", ctx, false
	}
	if sessionID != "" {
		// Echo the session id so the client can capture (initialize) or confirm it.
		w.Header().Set(mcpSessionHeader, sessionID)
		ctx = ContextWithClientID(ctx, sessionID)
	}
	return sessionID, ctx, true
}

func (h *HTTP) handleStreamablePost(w http.ResponseWriter, r *http.Request, handler Handler) {
	if !originAllowed(r, h.allowedOrigins, h.allowAllOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if h.authorizeFn != nil {
		if err := h.authorizeFn(r); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	ctx := r.Context()
	if h.requestContextFn != nil {
		ctx = h.requestContextFn(ctx, r)
	}

	if h.maxRequestBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.maxRequestBytes)
	}

	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(protocol.NewErrorResponse(nil, protocol.NewParseError("Invalid JSON")))
		return
	}

	// Modern routing headers (MCP 2026-07-28): when supplied, Mcp-Method and
	// Mcp-Name must agree with the body so intermediaries can trust them for
	// routing. A disagreement is a -32020 JSON-RPC error carrying the request id.
	if verr := validateRoutingHeaders(r, &req); verr != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(protocol.NewErrorResponse(req.ID, verr))
		return
	}

	sessionID, ctx, ok := h.resolveStreamablePOSTSession(w, r, ctx, &req)
	if !ok {
		return
	}

	// subscriptions/listen (MCP 2026-07-28): a single long-lived POST-response
	// SSE stream that replaces the GET stream + resources/subscribe/unsubscribe.
	// The handler runs once to register the subscription and return a
	// subscriptionId; the response then stays open, forwarding subscriptionId-
	// tagged notifications until the client disconnects. Streamed regardless of
	// the Accept header, since the method's semantics require a stream.
	if req.Method == protocol.MethodSubscriptionsListen {
		if flusher, flushable := w.(http.Flusher); flushable {
			h.streamableSubscriptionsListen(ctx, w, &req, handler, flusher)
			return
		}
	}

	// A notification carries no id and MUST NOT be answered with a body. Run it
	// for side effects (routing any emitted notifications to the standing stream)
	// and acknowledge with 202.
	if req.IsNotification() {
		ctx = ContextWithNotificationSender(ctx, h.sessionStreamNotifier(sessionID))
		_, _ = handler.HandleRequest(ctx, &req)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Negotiate the reply framing. SSE is used only when the client accepts
	// text/event-stream but not application/json; otherwise a single JSON object
	// is returned. Both are spec-valid for a streamable server.
	accept := r.Header.Get("Accept")
	useSSE := strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json")
	flusher, flushable := w.(http.Flusher)
	if useSSE && flushable {
		h.streamablePostSSE(ctx, w, &req, handler, flusher)
		return
	}

	// JSON reply. In-handler notifications ride the session's standing GET stream
	// (if open) since a single JSON object cannot carry interleaved events.
	w.Header().Set("Content-Type", "application/json")
	ctx = ContextWithNotificationSender(ctx, h.sessionStreamNotifier(sessionID))
	resp, err := handler.HandleRequest(ctx, &req)
	if err != nil {
		resp = protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error()))
	}
	if resp != nil {
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// streamablePostSSE answers a POST with an SSE stream: notifications emitted by
// the handler are written as data frames, followed by the final JSON-RPC
// response as the last data frame, then the stream is closed.
func (h *HTTP) streamablePostSSE(ctx context.Context, w http.ResponseWriter, req *protocol.Request, handler Handler, flusher http.Flusher) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sse := NewSSEWriter(w, flusher)
	ctx = ContextWithNotificationSender(ctx, notifierFunc(func(method string, params any) error {
		msg, err := marshalNotification(method, params)
		if err != nil {
			return err
		}
		return sse.WriteData(msg)
	}))

	resp, err := handler.HandleRequest(ctx, req)
	if err != nil {
		resp = protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error()))
	}
	if resp != nil {
		body, mErr := json.Marshal(resp)
		if mErr != nil {
			body, _ = json.Marshal(protocol.NewErrorResponse(req.ID, protocol.NewInternalError(mErr.Error())))
		}
		_ = sse.WriteData(body)
	}
}

// subscriptionStreamBuffer bounds the per-subscription notification backlog,
// mirroring the standing GET stream's channel depth.
const subscriptionStreamBuffer = 10

// streamableSubscriptionsListen realizes subscriptions/listen (MCP 2026-07-28)
// as a long-lived POST-response SSE stream. It runs the handler once to register
// the subscription and obtain the subscriptionId, opens an SSE response keyed by
// that id, writes the subscription acknowledgement as the first frame, and then
// forwards any notifications delivered via NotifySubscription (each tagged with
// io.modelcontextprotocol/subscriptionId) until the client disconnects.
func (h *HTTP) streamableSubscriptionsListen(ctx context.Context, w http.ResponseWriter, req *protocol.Request, handler Handler, flusher http.Flusher) {
	resp, err := handler.HandleRequest(ctx, req)
	if err != nil {
		resp = protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error()))
	}
	subID := subscriptionIDFromResponse(resp)
	if resp == nil || resp.Error != nil || subID == "" {
		// The request did not yield a subscription (e.g. a validation error, or a
		// non-modern caller): reply with a single JSON object, no stream.
		w.Header().Set("Content-Type", "application/json")
		if resp != nil {
			_ = json.NewEncoder(w).Encode(resp)
		}
		return
	}

	ch := make(chan []byte, subscriptionStreamBuffer)
	if !h.registerSSEClient(subID, ch) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(protocol.NewErrorResponse(req.ID,
			protocol.NewInternalError("subscription stream unavailable")))
		return
	}
	defer func() {
		h.sseClientsMu.Lock()
		if h.sseClients[subID] == ch {
			delete(h.sseClients, subID)
		}
		h.sseClientsMu.Unlock()
		h.mu.RLock()
		hook := h.onDisconnect
		h.mu.RUnlock()
		if hook != nil {
			hook(subID)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sse := NewSSEWriter(w, flusher)
	if body, mErr := json.Marshal(resp); mErr == nil {
		_ = sse.WriteData(body)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_ = sse.WriteData(msg)
		}
	}
}

// subscriptionIDFromResponse extracts the subscriptionId a subscriptions/listen
// handler returned, or "" if the response does not carry one.
func subscriptionIDFromResponse(resp *protocol.Response) string {
	if resp == nil {
		return ""
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		return ""
	}
	id, _ := m["subscriptionId"].(string)
	return id
}

// NotifySubscription delivers a notification to an open subscriptions/listen
// stream, tagging its params with io.modelcontextprotocol/subscriptionId so the
// client correlates it to the subscription (MCP 2026-07-28). It returns an error
// if no stream is open for the id or the stream's buffer is full.
func (h *HTTP) NotifySubscription(subscriptionID, method string, params any) error {
	tagged, err := tagSubscriptionParams(params, subscriptionID)
	if err != nil {
		return fmt.Errorf("notify subscription %s: %w", subscriptionID, err)
	}
	msg, err := json.Marshal(protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  tagged,
	})
	if err != nil {
		return fmt.Errorf("notify subscription %s: marshal notification: %w", subscriptionID, err)
	}
	if !h.SendTo(subscriptionID, msg) {
		return fmt.Errorf("subscription %s: no open stream or buffer full", subscriptionID)
	}
	return nil
}

// tagSubscriptionParams injects io.modelcontextprotocol/subscriptionId into the
// params' `_meta` object. Non-object params (which cannot carry `_meta`) are
// returned unchanged.
func tagSubscriptionParams(params any, subscriptionID string) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw, nil // not an object; cannot carry _meta
	}
	idRaw, _ := json.Marshal(subscriptionID)
	meta := map[string]json.RawMessage{}
	if existing, ok := obj["_meta"]; ok {
		_ = json.Unmarshal(existing, &meta)
	}
	meta[protocol.MetaKeySubscriptionID] = idRaw
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal _meta: %w", err)
	}
	obj["_meta"] = metaRaw
	return json.Marshal(obj)
}

// handleStreamableGet opens a standing server->client SSE stream keyed by the
// Mcp-Session-Id header, mirroring the legacy /mcp/sse push loop but correlated
// by session id rather than a clientId query parameter.
func (h *HTTP) handleStreamableGet(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	if !originAllowed(r, h.allowedOrigins, h.allowAllOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if h.authorizeFn != nil {
		if err := h.authorizeFn(r); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	ctx := r.Context()
	if h.requestContextFn != nil {
		ctx = h.requestContextFn(ctx, r)
	}

	sessionID := r.Header.Get(mcpSessionHeader)
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header required", http.StatusBadRequest)
		return
	}
	if h.sessionStore != nil {
		data, err := h.sessionStore.GetSession(ctx, sessionID)
		if err != nil || data == nil {
			http.Error(w, "unknown or expired session", http.StatusNotFound)
			return
		}
	}

	messageCh := make(chan []byte, 10)
	if !h.registerSSEClient(sessionID, messageCh) {
		// Cap reached or a stream is already open for this session. Refusing
		// rather than clobbering keeps the existing stream intact.
		http.Error(w, "connection refused", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		h.sseClientsMu.Lock()
		if h.sseClients[sessionID] == messageCh {
			delete(h.sseClients, sessionID)
		}
		h.sseClientsMu.Unlock()
		h.mu.RLock()
		hook := h.onDisconnect
		h.mu.RUnlock()
		if hook != nil {
			hook(sessionID)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set(mcpSessionHeader, sessionID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sse := NewSSEWriter(w, flusher)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messageCh:
			if !ok {
				return
			}
			_ = sse.WriteData(msg)
		}
	}
}

// handleStreamableDelete tears down the session named by Mcp-Session-Id,
// releasing its store entry. Any open standing stream closes on its own when
// the underlying connection drops.
func (h *HTTP) handleStreamableDelete(w http.ResponseWriter, r *http.Request) {
	if !originAllowed(r, h.allowedOrigins, h.allowAllOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if h.authorizeFn != nil {
		if err := h.authorizeFn(r); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	sessionID := r.Header.Get(mcpSessionHeader)
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header required", http.StatusBadRequest)
		return
	}
	if h.sessionStore != nil {
		_ = h.sessionStore.DeleteSession(r.Context(), sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}
