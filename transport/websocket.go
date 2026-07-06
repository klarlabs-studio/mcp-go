package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"go.klarlabs.de/mcp/protocol"
)

// defaultWSMaxMessageBytes bounds a single inbound WebSocket message. A
// non-positive value disables the limit.
const defaultWSMaxMessageBytes = 4 << 20 // 4 MiB

// defaultWSMaxConnections caps concurrent WebSocket connections.
const defaultWSMaxConnections = 1024

// WebSocket implements MCP transport over WebSocket connections.
type WebSocket struct {
	addr     string
	upgrader websocket.Upgrader
	server   *http.Server

	readTimeout  time.Duration
	writeTimeout time.Duration
	tlsConfig    *tls.Config

	allowedOrigins  []string
	allowAllOrigins bool
	maxMessageBytes int64
	maxConnections  int

	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

// wsClient represents a single WebSocket connection.
type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// WebSocketOption configures a WebSocket transport.
type WebSocketOption func(*WebSocket)

// WithWebSocketReadTimeout sets the read timeout for WebSocket messages.
func WithWebSocketReadTimeout(d time.Duration) WebSocketOption {
	return func(ws *WebSocket) {
		ws.readTimeout = d
	}
}

// WithWebSocketWriteTimeout sets the write timeout for WebSocket messages.
func WithWebSocketWriteTimeout(d time.Duration) WebSocketOption {
	return func(ws *WebSocket) {
		ws.writeTimeout = d
	}
}

// WithWebSocketCheckOrigin sets the origin check function for WebSocket
// upgrades, fully overriding the secure default. Return true to accept the
// upgrade. Prefer WithWebSocketAllowedOrigins unless you need custom logic.
func WithWebSocketCheckOrigin(fn func(r *http.Request) bool) WebSocketOption {
	return func(ws *WebSocket) {
		ws.upgrader.CheckOrigin = fn
	}
}

// WithWebSocketAllowedOrigins restricts which browser Origins may complete a
// WebSocket upgrade. Requests without an Origin header (non-browser clients)
// are allowed; browser requests must match a listed origin, the same-origin
// Host, or a loopback address. This is the defense against Cross-Site
// WebSocket Hijacking.
func WithWebSocketAllowedOrigins(origins ...string) WebSocketOption {
	return func(ws *WebSocket) {
		ws.allowedOrigins = origins
	}
}

// WithWebSocketAllowAllOrigins disables origin enforcement on the upgrade,
// restoring the pre-hardening behavior where any website could open a socket.
// Named "insecure" behavior deliberately: only use it behind a trusted proxy.
func WithWebSocketAllowAllOrigins() WebSocketOption {
	return func(ws *WebSocket) {
		ws.allowAllOrigins = true
	}
}

// WithWebSocketMaxMessageBytes caps the size of a single inbound message. A
// non-positive value disables the limit. Default: 4 MiB.
func WithWebSocketMaxMessageBytes(n int64) WebSocketOption {
	return func(ws *WebSocket) {
		ws.maxMessageBytes = n
	}
}

// WithWebSocketMaxConnections caps concurrent connections; once reached, new
// upgrade attempts receive 503. A non-positive value disables the cap.
// Default: 1024.
func WithWebSocketMaxConnections(n int) WebSocketOption {
	return func(ws *WebSocket) {
		ws.maxConnections = n
	}
}

// WithWebSocketTLSConfig enables wss:// by terminating TLS at the
// transport. Bring your own certificate strategy — mcp-go does not
// load or rotate certs. Set ClientCAs + ClientAuth for mTLS.
func WithWebSocketTLSConfig(cfg *tls.Config) WebSocketOption {
	return func(ws *WebSocket) {
		ws.tlsConfig = cfg
	}
}

// NewWebSocket creates a new WebSocket transport.
func NewWebSocket(addr string, opts ...WebSocketOption) *WebSocket {
	ws := &WebSocket{
		addr: addr,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		readTimeout:     60 * time.Second,
		writeTimeout:    10 * time.Second,
		maxMessageBytes: defaultWSMaxMessageBytes,
		maxConnections:  defaultWSMaxConnections,
		clients:         make(map[*wsClient]struct{}),
	}

	// Secure default: reject cross-origin upgrades (Cross-Site WebSocket
	// Hijacking). The closure reads ws.allowedOrigins/allowAllOrigins at
	// upgrade time, so options applied below take effect. A caller can fully
	// override this via WithWebSocketCheckOrigin.
	ws.upgrader.CheckOrigin = func(r *http.Request) bool {
		return originAllowed(r, ws.allowedOrigins, ws.allowAllOrigins)
	}

	for _, opt := range opts {
		opt(ws)
	}

	return ws
}

// Addr returns the transport address.
func (ws *WebSocket) Addr() string {
	return ws.addr
}

// Serve starts the WebSocket server.
func (ws *WebSocket) Serve(ctx context.Context, handler Handler) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws.handleConnection(ctx, w, r, handler)
	})

	ws.server = &http.Server{
		Addr:         ws.addr,
		Handler:      mux,
		ReadTimeout:  ws.readTimeout,
		WriteTimeout: ws.writeTimeout,
		TLSConfig:    ws.tlsConfig,
	}

	errChan := make(chan error, 1)
	go func() {
		var srvErr error
		if ws.tlsConfig != nil {
			// ListenAndServeTLS reads from server.TLSConfig when given
			// empty cert/key filenames.
			srvErr = ws.server.ListenAndServeTLS("", "")
		} else {
			srvErr = ws.server.ListenAndServe()
		}
		if srvErr != nil && !errors.Is(srvErr, http.ErrServerClosed) {
			errChan <- srvErr
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ws.closeAllClients()
		return ws.server.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}

func (ws *WebSocket) handleConnection(ctx context.Context, w http.ResponseWriter, r *http.Request, handler Handler) {
	// Enforce the connection cap before upgrading — once the handshake
	// completes we can no longer reply with an HTTP status.
	if ws.maxConnections > 0 {
		ws.mu.RLock()
		n := len(ws.clients)
		ws.mu.RUnlock()
		if n >= ws.maxConnections {
			http.Error(w, "too many connections", http.StatusServiceUnavailable)
			return
		}
	}

	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// Bound inbound message size so a client cannot exhaust memory.
	if ws.maxMessageBytes > 0 {
		conn.SetReadLimit(ws.maxMessageBytes)
	}

	client := &wsClient{conn: conn}

	ws.mu.Lock()
	ws.clients[client] = struct{}{}
	ws.mu.Unlock()

	defer func() {
		ws.mu.Lock()
		delete(ws.clients, client)
		ws.mu.Unlock()
		_ = conn.Close()
	}()

	// Create notification sender for this client
	sender := &wsNotificationSender{client: client}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read message
		if ws.readTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(ws.readTimeout))
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			// Expected close errors are normal (client disconnected)
			// Unexpected errors could be logged if needed
			return
		}

		// Parse request
		var req protocol.Request
		if err := json.Unmarshal(message, &req); err != nil {
			resp := protocol.NewErrorResponse(nil, protocol.NewParseError(err.Error()))
			_ = client.writeJSON(resp)
			continue
		}

		// Attach notification sender to context
		reqCtx := ContextWithNotificationSender(ctx, sender)

		// Handle request
		resp, err := handler.HandleRequest(reqCtx, &req)

		// For notifications, don't send response
		if req.IsNotification() {
			continue
		}

		// Handle handler errors
		if err != nil {
			var mcpErr *protocol.Error
			if errors.As(err, &mcpErr) {
				resp = protocol.NewErrorResponse(req.ID, mcpErr)
			} else {
				resp = protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error()))
			}
		}

		if resp != nil {
			_ = client.writeJSON(resp)
		}
	}
}

func (ws *WebSocket) closeAllClients() {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	for client := range ws.clients {
		client.close()
	}
}

func (c *wsClient) writeJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(v)
}

func (c *wsClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = c.conn.Close()
}

// wsNotificationSender sends notifications to a WebSocket client.
type wsNotificationSender struct {
	client *wsClient
}

func (s *wsNotificationSender) SendNotification(method string, params any) error {
	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	}

	return s.client.writeJSON(notif)
}
