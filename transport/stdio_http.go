package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// HTTPFrame type values and common header values for stdio+http.
const (
	httpFrameRequest      = "request"
	httpFrameResponse     = "response"
	httpFrameNotification = "notification"
	contentTypeJSON       = "application/json"
	headerContentType     = "Content-Type"
)

// HTTPFrame is a pragmatic Streamable-HTTP-over-stdio envelope (MCP roadmap:
// "HTTP over stdio"). Until the Transports WG finalizes HTTP/2-over-stdio,
// mcp-go speaks the same NDJSON outer framing as classic stdio, with each
// line carrying an HTTP-shaped request or response that preserves Mcp-Method,
// Accept, and other Streamable HTTP headers.
//
// Wire shape (one JSON object per line):
//
//	{"type":"request","id":"1","method":"POST","path":"/mcp",
//	 "headers":{"Mcp-Method":"tools/list","Accept":"application/json"},
//	 "body":{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}}
//	{"type":"response","id":"1","status":200,
//	 "headers":{"Content-Type":"application/json"},
//	 "body":{"jsonrpc":"2.0","id":1,"result":{...}}}
//
// Notifications emitted during a request (progress, channel messages) are
// separate frames with type "notification" sharing the parent request id.
type HTTPFrame struct {
	Type    string            `json:"type"` // request | response | notification
	ID      string            `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Status  int               `json:"status,omitempty"`
}

// StdioHTTP serves Streamable HTTP semantics over stdin/stdout using HTTPFrame
// NDJSON framing. Classic newline-delimited JSON-RPC (NewStdio) remains the
// default for local subprocesses; opt into this transport when the client
// speaks the HTTP-over-stdio envelope.
type StdioHTTP struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	writer *NewlineFramer
}

// StdioHTTPOption configures StdioHTTP.
type StdioHTTPOption func(*StdioHTTP)

// WithStdioHTTPStdin sets a custom stdin reader.
func WithStdioHTTPStdin(r io.Reader) StdioHTTPOption {
	return func(s *StdioHTTP) { s.in = r }
}

// WithStdioHTTPStdout sets a custom stdout writer.
func WithStdioHTTPStdout(w io.Writer) StdioHTTPOption {
	return func(s *StdioHTTP) { s.out = w }
}

// WithStdioHTTPStderr sets a custom stderr writer.
func WithStdioHTTPStderr(w io.Writer) StdioHTTPOption {
	return func(s *StdioHTTP) { s.errOut = w }
}

// NewStdioHTTP creates a Streamable-HTTP-over-stdio transport.
func NewStdioHTTP(opts ...StdioHTTPOption) *StdioHTTP {
	s := &StdioHTTP{
		in:     os.Stdin,
		out:    os.Stdout,
		errOut: os.Stderr,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.writer = NewNewlineFramer(nil, s.out)
	return s
}

// Addr returns the transport address.
func (s *StdioHTTP) Addr() string { return "stdio+http" }

// Serve reads HTTPFrame requests from stdin and writes response/notification
// frames to stdout.
func (s *StdioHTTP) Serve(ctx context.Context, handler Handler) error {
	reader := NewNewlineFramer(s.in, nil)
	ctx = server.ContextWithSession(ctx, server.NewSession("stdio+http", nil, s))

	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		for {
			line, err := reader.ReadMessage()
			if err != nil {
				scanErr <- err
				return
			}
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-scanErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			if errors.Is(err, ErrFrameTooLarge) {
				s.noteSkippedFrame()
				continue
			}
			return err
		case line := <-lines:
			s.handleFrame(ctx, handler, line)
		}
	}
}

// SendNotification emits a notification frame (no parent request id).
func (s *StdioHTTP) SendNotification(method string, params any) error {
	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}
	body, err := json.Marshal(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	})
	if err != nil {
		return err
	}
	return s.writer.WriteMessage(HTTPFrame{
		Type: httpFrameNotification,
		Body: body,
		Headers: map[string]string{
			mcpMethodHeader: method,
		},
	})
}

func (s *StdioHTTP) noteSkippedFrame() {
	if s.errOut == nil {
		return
	}
	_, _ = io.WriteString(s.errOut, "mcp-go/transport: dropped oversized stdio+http frame\n")
}

func (s *StdioHTTP) handleFrame(ctx context.Context, handler Handler, line []byte) {
	var frame HTTPFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		s.writeErrorFrame("", 400, protocol.NewParseError(err.Error()))
		return
	}
	if frame.Type != httpFrameRequest {
		s.writeErrorFrame(frame.ID, 400, protocol.NewInvalidRequest("stdio+http: expected type=request"))
		return
	}
	if frame.Method != "" && !strings.EqualFold(frame.Method, "POST") {
		s.writeErrorFrame(frame.ID, 405, protocol.NewInvalidRequest("stdio+http: only POST is supported"))
		return
	}

	var req protocol.Request
	if len(frame.Body) == 0 {
		s.writeErrorFrame(frame.ID, 400, protocol.NewInvalidRequest("stdio+http: empty body"))
		return
	}
	if err := json.Unmarshal(frame.Body, &req); err != nil {
		s.writeErrorFrame(frame.ID, 400, protocol.NewParseError(err.Error()))
		return
	}

	// Mirror Streamable HTTP Mcp-Method validation when the header is present.
	if want := headerValue(frame.Headers, mcpMethodHeader); want != "" && req.Method != "" && want != req.Method {
		s.writeErrorFrame(frame.ID, 400, protocol.NewHeaderMismatch(
			"Mcp-Method header "+want+" does not match body method "+req.Method))
		return
	}

	parentID := frame.ID
	notifier := &stdioHTTPRequestNotifier{parent: s, requestID: parentID}
	ctx = ContextWithNotificationSender(ctx, notifier)

	resp, err := handler.HandleRequest(ctx, &req)
	if req.IsNotification() {
		return
	}
	if err != nil {
		var mcpErr *protocol.Error
		if errors.As(err, &mcpErr) {
			resp = protocol.NewErrorResponse(req.ID, mcpErr)
		} else {
			resp = protocol.NewErrorResponse(req.ID, protocol.NewInternalError(err.Error()))
		}
	}
	if resp == nil {
		return
	}
	body, err := json.Marshal(resp)
	if err != nil {
		s.writeErrorFrame(parentID, 500, protocol.NewInternalError(err.Error()))
		return
	}
	_ = s.writer.WriteMessage(HTTPFrame{
		Type:   httpFrameResponse,
		ID:     parentID,
		Status: 200,
		Headers: map[string]string{
			headerContentType: contentTypeJSON,
		},
		Body: body,
	})
}

func (s *StdioHTTP) writeErrorFrame(id string, status int, mcpErr *protocol.Error) {
	resp := protocol.NewErrorResponse(nil, mcpErr)
	body, err := json.Marshal(resp)
	if err != nil {
		body = []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal error"}}`)
	}
	_ = s.writer.WriteMessage(HTTPFrame{
		Type:   httpFrameResponse,
		ID:     id,
		Status: status,
		Headers: map[string]string{
			headerContentType: contentTypeJSON,
		},
		Body: body,
	})
}

type stdioHTTPRequestNotifier struct {
	parent    *StdioHTTP
	requestID string
}

func (n *stdioHTTPRequestNotifier) SendNotification(method string, params any) error {
	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}
	body, err := json.Marshal(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	})
	if err != nil {
		return err
	}
	return n.parent.writer.WriteMessage(HTTPFrame{
		Type: httpFrameNotification,
		ID:   n.requestID,
		Body: body,
		Headers: map[string]string{
			mcpMethodHeader: method,
		},
	})
}

func headerValue(h map[string]string, key string) string {
	if h == nil {
		return ""
	}
	if v, ok := h[key]; ok {
		return v
	}
	for k, v := range h {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// Ensure compile-time interface satisfaction.
var (
	_ Transport          = (*StdioHTTP)(nil)
	_ NotificationSender = (*StdioHTTP)(nil)
)
