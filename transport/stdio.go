package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

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

	// Server→client requests (SendRequest). pending maps each in-flight
	// request's wire ID to the slot its response is delivered to; reqClosed is
	// non-nil whenever no reader is running (before Serve, after EOF/shutdown)
	// so SendRequest fails fast instead of waiting for a response that cannot
	// arrive. wake nudges a back-pressured reader (see readFrames).
	reqMu     sync.Mutex
	pending   map[string]chan stdioResult
	reqClosed error
	nextReqID atomic.Uint64
	wake      chan struct{}
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
		in:        os.Stdin,
		out:       os.Stdout,
		errOut:    os.Stderr,
		pending:   make(map[string]chan stdioResult),
		reqClosed: errNotServing,
		wake:      make(chan struct{}, 1),
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
//
// A dedicated reader goroutine parses each frame once. Responses to
// server-initiated requests (see [Stdio.SendRequest]) are delivered from the
// reader itself, so a handler blocked waiting on the client (elicitation,
// sampling, roots) never stops the read path. Everything else — client
// requests and notifications — is queued and handled by Serve's loop one at a
// time, in arrival order, exactly as before.
func (s *Stdio) Serve(ctx context.Context, handler Handler) error {
	reader := NewNewlineFramer(s.in, nil)

	// One session per stdio connection (stdio is inherently single-connection).
	// The transport is both the notifier (one-way server→client features:
	// logging, channels, resource-updated) and the request sender (sampling,
	// elicitation, roots/list). Capability gating stays in the session: a
	// client that did not declare a feature at initialize still gets the
	// session's "client does not support …" error, and a modern (2026-07-28)
	// request carries its own request-scoped session whose MRTR broker takes
	// precedence, so the stateless path never reaches this sender.
	ctx = server.ContextWithSession(ctx, server.NewSession("stdio", s, s))

	s.openRequests()
	defer s.closeRequests(ErrConnectionClosed)

	queue := newFrameQueue()
	go s.readFrames(ctx, reader, queue)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, done, err := queue.pop()
		if frame != nil {
			s.signalReader()
			s.handleFrame(ctx, handler, frame)
			continue
		}
		if done {
			return err // nil on EOF
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-queue.ready:
		}
	}
}

// readFrames is the single reader goroutine: it reads and parses each frame
// once, routes responses to pending server→client requests, and queues the
// rest for Serve's loop. When nothing is pending it stops reading once
// maxQueuedFrames are waiting, preserving the pipe's back-pressure on a client
// that floods requests; while a request is pending it keeps reading so the
// awaited response can always arrive.
func (s *Stdio) readFrames(ctx context.Context, reader *NewlineFramer, queue *frameQueue) {
	for {
		line, err := reader.ReadMessage()
		if err != nil {
			if errors.Is(err, ErrFrameTooLarge) {
				// A single over-cap frame must not wedge the transport: the
				// framer already drained it, so skip and keep reading.
				s.noteSkippedFrame()
				continue
			}
			if errors.Is(err, io.EOF) {
				err = nil
			}
			// No response can arrive any more: fail waiting senders now so a
			// handler blocked on the client returns before Serve drains.
			s.closeRequests(readClosedError(err))
			queue.finish(err)
			return
		}

		// Unmarshal copies into the frame, so the framer's reused buffer is
		// not retained.
		frame := &stdioFrame{}
		if perr := json.Unmarshal(line, frame); perr != nil {
			queue.push(&queuedFrame{parseErr: perr})
		} else if frame.isResponse() {
			s.deliverResponse(frame)
			continue
		} else {
			queue.push(&queuedFrame{req: frame.request()})
		}

		for queue.len() >= maxQueuedFrames && !s.hasPendingRequests() {
			select {
			case <-s.wake:
			case <-ctx.Done():
				return
			}
		}
	}
}

// readClosedError describes why the read side ended, for pending senders.
func readClosedError(cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: client closed stdin", ErrConnectionClosed)
	}
	return fmt.Errorf("%w: %w", ErrConnectionClosed, cause)
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

// handleFrame processes one client request or notification queued by the
// reader. A frame that failed to parse gets a JSON-RPC parse error response.
func (s *Stdio) handleFrame(ctx context.Context, handler Handler, frame *queuedFrame) {
	if frame.parseErr != nil {
		resp := protocol.NewErrorResponse(nil, protocol.NewParseError(frame.parseErr.Error()))
		s.writeResponse(resp)
		return
	}
	req := frame.req

	// Attach notification sender to context for progress reporting
	ctx = ContextWithNotificationSender(ctx, s)

	// Handle request
	resp, err := handler.HandleRequest(ctx, req)

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
