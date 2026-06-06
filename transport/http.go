package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

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

	mu         sync.RWMutex
	listenAddr string
	server     *http.Server

	sseClients   map[string]chan []byte
	sseClientsMu sync.RWMutex
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
// certificate and stash a derived identity for downstream authz.
//
//	NewHTTP(addr,
//	    WithTLSConfig(mtlsCfg),
//	    WithRequestContextFn(func(ctx context.Context, r *http.Request) context.Context {
//	        if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
//	            cn := r.TLS.PeerCertificates[0].Subject.CommonName
//	            ctx = mcp.ContextWithIdentity(ctx, &mcp.Identity{ID: cn, Name: cn})
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

func NewHTTP(addr string, opts ...HTTPOption) *HTTP {
	h := &HTTP{
		addr:            addr,
		readTimeout:     30 * time.Second,
		writeTimeout:    30 * time.Second,
		shutdownTimeout: 30 * time.Second,
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

	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	if h.requestContextFn != nil {
		ctx = h.requestContextFn(ctx, r)
	}

	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resp := protocol.NewErrorResponse(nil, protocol.NewParseError("Invalid JSON"))
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	resp, err := handler.HandleRequest(ctx, &req)
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

	clientID := r.URL.Query().Get("clientId")
	if clientID == "" {
		clientID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	messageCh := make(chan []byte, 10)

	h.sseClientsMu.Lock()
	h.sseClients[clientID] = messageCh
	h.sseClientsMu.Unlock()

	defer func() {
		h.sseClientsMu.Lock()
		delete(h.sseClients, clientID)
		h.sseClientsMu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if h.sessionStore != nil {
		w.Header().Set("Link", fmt.Sprintf(`<%s/mcp/sse?clientId=%s>; rel="stream"`, h.Addr(), clientID))
	}

	escapedClientID, _ := json.Marshal(clientID)
	fmt.Fprintf(w, "event: connected\ndata: {\"clientId\":%s}\n\n", string(escapedClientID))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-messageCh:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
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
