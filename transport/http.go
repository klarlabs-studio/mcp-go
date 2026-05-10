package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// healthStatusField is the JSON field name used in /health and /healthz
// payloads.
const healthStatusField = "status"

type HTTP struct {
	addr            string
	readTimeout     time.Duration
	writeTimeout    time.Duration
	shutdownTimeout time.Duration
	drainDelay      time.Duration
	corsConfig      *CORSConfig
	sessionStore    SessionStore
	discovery       *ServerDiscovery

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
	}
	h.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if err := h.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
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

	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resp := protocol.NewErrorResponse(nil, protocol.NewParseError("Invalid JSON"))
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	resp, err := handler.HandleRequest(r.Context(), &req)
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
