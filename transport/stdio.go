package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// Stdio implements MCP transport over stdin/stdout.
type Stdio struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer

	// writer is the single write-only framer over s.out. EVERY write to s.out —
	// Serve responses, out-of-band SendNotification, and pre/post-Serve
	// writeResponse — goes through this one framer, so all of them share one
	// mutex and can never interleave bytes on s.out. Reads use a separate
	// read-only framer created per Serve call.
	writer *NewlineFramer
}

// StdioOption configures a Stdio transport.
type StdioOption func(*Stdio)

// WithStdin sets a custom stdin reader.
func WithStdin(r io.Reader) StdioOption {
	return func(s *Stdio) {
		s.in = r
	}
}

// WithStdout sets a custom stdout writer.
func WithStdout(w io.Writer) StdioOption {
	return func(s *Stdio) {
		s.out = w
	}
}

// WithStderr sets a custom stderr writer.
func WithStderr(w io.Writer) StdioOption {
	return func(s *Stdio) {
		s.errOut = w
	}
}

// NewStdio creates a new stdio transport.
func NewStdio(opts ...StdioOption) *Stdio {
	s := &Stdio{
		in:     os.Stdin,
		out:    os.Stdout,
		errOut: os.Stderr,
	}

	for _, opt := range opts {
		opt(s)
	}

	// Construct the single shared write framer after options settle s.out. All
	// writes to s.out flow through it so there is exactly one write mutex.
	s.writer = NewNewlineFramer(nil, s.out)

	return s
}

// Addr returns the transport address.
func (s *Stdio) Addr() string {
	return "stdio"
}

// Serve starts processing requests from stdin. It frames messages as
// newline-delimited JSON via the shared transport.NewlineFramer, the same
// primitive the stdio client uses, so the wire format never drifts between the
// two sides. Reads use a dedicated read-only framer; all writes go through the
// shared write framer (s.writer) so out-of-band notifications cannot interleave
// with Serve responses.
func (s *Stdio) Serve(ctx context.Context, handler Handler) error {
	reader := NewNewlineFramer(s.in, nil)

	// One session per stdio connection (stdio is inherently single-connection).
	// The transport is the notifier, enabling one-way server→client features
	// (logging, channels, resource-updated). There is no bidirectional request
	// sender for stdio yet, so sampling/elicitation report ErrNoRequestSender
	// rather than silently no-op. initialize populates the session's client
	// capabilities, which persists for every later request on this connection.
	ctx = server.ContextWithSession(ctx, server.NewSession("stdio", nil, s))

	// Channel for scanner results
	lines := make(chan []byte)
	scanErr := make(chan error, 1)

	go func() {
		for {
			line, err := reader.ReadMessage()
			if err != nil {
				if errors.Is(err, ErrFrameTooLarge) {
					// A single over-cap frame must not wedge the transport: the
					// framer already drained it, so skip and keep reading.
					s.noteSkippedFrame()
					continue
				}
				if !errors.Is(err, io.EOF) {
					scanErr <- err
				}
				close(lines)
				return
			}
			// Copy: the framer reuses its buffer between reads.
			buf := make([]byte, len(line))
			copy(buf, line)
			select {
			case lines <- buf:
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
			return err
		case line, ok := <-lines:
			if !ok {
				return nil // EOF
			}
			s.handleLine(ctx, handler, line)
		}
	}
}

// noteSkippedFrame emits a best-effort diagnostic when an over-cap frame is
// dropped. Errors are ignored: a broken stderr must not affect request serving.
func (s *Stdio) noteSkippedFrame() {
	if s.errOut == nil {
		return
	}
	_, _ = io.WriteString(s.errOut, "mcp-go/transport: dropped oversized stdio frame\n")
}

// SendNotification sends a JSON-RPC notification to the client.
func (s *Stdio) SendNotification(method string, params any) error {
	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	return s.writer.WriteMessage(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	})
}

func (s *Stdio) handleLine(ctx context.Context, handler Handler, line []byte) {
	// Parse request
	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		// Send parse error
		resp := protocol.NewErrorResponse(nil, protocol.NewParseError(err.Error()))
		s.writeResponse(resp)
		return
	}

	// Attach notification sender to context for progress reporting
	ctx = ContextWithNotificationSender(ctx, s)

	// Handle request
	resp, err := handler.HandleRequest(ctx, &req)

	// For notifications, don't send response
	if req.IsNotification() {
		return
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
		s.writeResponse(resp)
	}
}

func (s *Stdio) writeResponse(resp *protocol.Response) {
	_ = s.writer.WriteMessage(resp)
}
