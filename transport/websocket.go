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

// WebSocket implements MCP transport over WebSocket connections.
type WebSocket struct {
	addr     string
	upgrader websocket.Upgrader
	server   *http.Server

	readTimeout  time.Duration
	writeTimeout time.Duration
	tlsConfig    *tls.Config

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

// WithWebSocketCheckOrigin sets the origin check function for WebSocket upgrades.
func WithWebSocketCheckOrigin(fn func(r *http.Request) bool) WebSocketOption {
	return func(ws *WebSocket) {
		ws.upgrader.CheckOrigin = fn
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
			CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins by default
		},
		readTimeout:  60 * time.Second,
		writeTimeout: 10 * time.Second,
		clients:      make(map[*wsClient]struct{}),
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
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
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
