package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
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

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		h.handleMCP(w, r, handler)
	})

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
