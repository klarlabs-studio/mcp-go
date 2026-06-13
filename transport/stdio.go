package transport

import (
	"bufio"
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

	mu sync.Mutex
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

// Serve starts processing requests from stdin.
func (s *Stdio) Serve(ctx context.Context, handler Handler) error {
	scanner := bufio.NewScanner(s.in)
	// Raise the read buffer above bufio's 64KB default so large request bodies
	// are not rejected with ErrTooLong (mirrors the client stdio reader).
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	// Channel for scanner results
	lines := make(chan string)
	scanErr := make(chan error, 1)

	go func() {
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			scanErr <- err
		}
		close(lines)
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

// SendNotification sends a JSON-RPC notification to the client.
func (s *Stdio) SendNotification(method string, params any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	_, err = s.out.Write(data)
	if err != nil {
		return err
	}
	_, err = s.out.Write([]byte("\n"))
	return err
}

func (s *Stdio) handleLine(ctx context.Context, handler Handler, line string) {
	// Parse request
	var req protocol.Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		return
	}

	_, _ = s.out.Write(data)
	_, _ = s.out.Write([]byte("\n"))
}
