package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"

	"go.klarlabs.de/mcp/protocol"
)

// Stdio implements MCP transport over stdin/stdout.
type Stdio struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer

	framerMu sync.Mutex
	framer   *NewlineFramer
	// fallback is a single write-only framer over s.out, constructed lazily and
	// reused for every out-of-Serve write (SendNotification/writeResponse before
	// or after Serve). Reusing one framer means all such writes share the same
	// mutex and therefore never interleave on s.out. Guarded by framerMu.
	fallback *NewlineFramer
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

	return s
}

// Addr returns the transport address.
func (s *Stdio) Addr() string {
	return "stdio"
}

// Serve starts processing requests from stdin. It frames messages as
// newline-delimited JSON via the shared transport.NewlineFramer, the same
// primitive the stdio client uses, so the wire format never drifts between the
// two sides.
func (s *Stdio) Serve(ctx context.Context, handler Handler) error {
	framer := NewNewlineFramer(s.in, s.out)
	s.setFramer(framer)
	defer s.setFramer(nil)

	// Channel for scanner results
	lines := make(chan []byte)
	scanErr := make(chan error, 1)

	go func() {
		for {
			line, err := framer.ReadMessage()
			if err != nil {
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

func (s *Stdio) setFramer(f *NewlineFramer) {
	s.framerMu.Lock()
	s.framer = f
	s.framerMu.Unlock()
}

func (s *Stdio) currentFramer() *NewlineFramer {
	s.framerMu.Lock()
	defer s.framerMu.Unlock()
	if s.framer != nil {
		return s.framer
	}
	// Serve has not been called (or has returned): fall back to a single
	// write-only framer over the configured output so SendNotification/
	// writeResponse stay usable in tests and out-of-band sends. The fallback is
	// cached and reused so all out-of-Serve writes share one mutex and never
	// interleave on s.out.
	if s.fallback == nil {
		s.fallback = NewNewlineFramer(nil, s.out)
	}
	return s.fallback
}

// SendNotification sends a JSON-RPC notification to the client.
func (s *Stdio) SendNotification(method string, params any) error {
	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	return s.currentFramer().WriteMessage(Notification{
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
	_ = s.currentFramer().WriteMessage(resp)
}
