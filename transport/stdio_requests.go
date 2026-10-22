package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// ErrConnectionClosed is returned by [Stdio.SendRequest] when the stdio
// connection is not being served, or ends (EOF, read error, Serve returning)
// before the client answers. Requests waiting at that moment fail with it.
var ErrConnectionClosed = errors.New("mcp-go/transport: connection closed")

// errNotServing is the closed reason before Serve starts.
var errNotServing = fmt.Errorf("%w: stdio transport is not serving", ErrConnectionClosed)

// serverRequestIDPrefix marks the IDs of server→client requests. JSON-RPC
// gives each direction its own ID space, so a client ID can never be mistaken
// for one of these (responses are told apart from requests by shape), but a
// string ID with a fixed prefix also keeps naive clients — ones that decode
// every incoming id as their own integer counter, like this module's stdio
// client — from mismatching a server request against an outstanding call.
const serverRequestIDPrefix = "mcp-go-srv-"

// maxQueuedFrames bounds how many client frames the reader queues ahead of
// Serve's loop while no server→client request is pending.
const maxQueuedFrames = 32

// Compile-time check: the stdio transport is a session request sender.
var _ server.RequestSender = (*Stdio)(nil)

// stdioResult is what a pending SendRequest receives: the client's response,
// or the reason none will arrive.
type stdioResult struct {
	resp *protocol.Response
	err  error
}

// SendRequest sends a server→client JSON-RPC request (sampling, elicitation,
// roots/list) and waits for the client's response. It implements
// [server.RequestSender], which is how a Serve-created session reaches the
// client.
//
// The request goes out under a transport-assigned string ID; the returned
// response carries req.ID back, so callers see the ID they sent. SendRequest
// returns when the response arrives, when ctx is done — it then sends
// notifications/cancelled for the request (MCP cancellation) and frees the
// slot, so a late answer is dropped — or when the connection closes, with
// [ErrConnectionClosed]. It is safe for concurrent use.
func (s *Stdio) SendRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if req == nil || req.Method == "" {
		return nil, errors.New("mcp-go/transport: SendRequest needs a request with a method")
	}

	wireID := serverRequestIDPrefix + strconv.FormatUint(s.nextReqID.Add(1), 10)
	rawID, err := json.Marshal(wireID)
	if err != nil {
		return nil, fmt.Errorf("marshal request ID: %w", err)
	}

	slot := make(chan stdioResult, 1)
	s.reqMu.Lock()
	if s.reqClosed != nil {
		closed := s.reqClosed
		s.reqMu.Unlock()
		return nil, closed
	}
	s.pending[wireID] = slot
	s.reqMu.Unlock()
	// A reader paused for back-pressure must resume: this response has to be
	// read even if the client queued requests ahead of it.
	s.signalReader()

	out := *req
	out.ID = rawID
	if out.JSONRPC == "" {
		out.JSONRPC = protocol.JSONRPCVersion
	}
	if err := s.writer.WriteMessage(&out); err != nil {
		s.takePending(wireID)
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case res := <-slot:
		return finishResult(res, req.ID)
	case <-ctx.Done():
		if !s.takePending(wireID) {
			// The response (or a close) won the race; it is already in the slot.
			return finishResult(<-slot, req.ID)
		}
		// Best effort: tell the client to stop; any late answer is dropped.
		_ = s.SendNotification(protocol.MethodCancelled, server.CancelledNotification{
			RequestID: rawID,
			Reason:    ctx.Err().Error(),
		})
		return nil, ctx.Err()
	}
}

// finishResult restores the caller's request ID on a delivered response.
func finishResult(res stdioResult, id json.RawMessage) (*protocol.Response, error) {
	if res.err != nil {
		return nil, res.err
	}
	res.resp.ID = id
	return res.resp, nil
}

// takePending removes a pending slot, reporting whether it was still there.
func (s *Stdio) takePending(wireID string) bool {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	if _, ok := s.pending[wireID]; !ok {
		return false
	}
	delete(s.pending, wireID)
	return true
}

// hasPendingRequests reports whether any server→client request awaits a reply.
func (s *Stdio) hasPendingRequests() bool {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	return len(s.pending) > 0
}

// openRequests accepts SendRequest calls for the duration of a Serve.
func (s *Stdio) openRequests() {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	s.reqClosed = nil
}

// closeRequests stops accepting SendRequest calls and fails every pending one
// with reason. Only the first close records its reason until the next Serve.
func (s *Stdio) closeRequests(reason error) {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	if s.reqClosed == nil {
		s.reqClosed = reason
	}
	for id, slot := range s.pending {
		slot <- stdioResult{err: s.reqClosed}
		delete(s.pending, id)
	}
}

// signalReader wakes a reader paused for back-pressure. Non-blocking: one
// buffered signal is enough, the reader re-checks its condition on waking.
func (s *Stdio) signalReader() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// deliverResponse hands a client response to the SendRequest waiting for it.
// Responses for unknown IDs — never sent, already timed out, or answered
// twice — are dropped with a diagnostic on stderr.
func (s *Stdio) deliverResponse(frame *stdioFrame) {
	var wireID string
	if err := json.Unmarshal(frame.ID, &wireID); err == nil {
		s.reqMu.Lock()
		slot, ok := s.pending[wireID]
		delete(s.pending, wireID)
		s.reqMu.Unlock()
		if ok {
			slot <- stdioResult{resp: frame.response()}
			return
		}
	}
	if s.errOut != nil {
		_, _ = io.WriteString(s.errOut, "mcp-go/transport: dropped response for unknown request id "+string(frame.ID)+"\n")
	}
}

// stdioFrame is one parsed incoming message: a request, a notification, or a
// response. Raw fields are copied on unmarshal, so a frame never aliases the
// framer's reused read buffer, and params/result stay undecoded until a
// handler or caller needs them.
type stdioFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// isResponse reports a JSON-RPC response: an id, no method, and a result or an
// error member.
func (f *stdioFrame) isResponse() bool {
	if f.Method != "" || len(f.ID) == 0 || string(f.ID) == "null" {
		return false
	}
	return f.Result != nil || f.Error != nil
}

// request converts a request or notification frame.
func (f *stdioFrame) request() *protocol.Request {
	return &protocol.Request{JSONRPC: f.JSONRPC, ID: f.ID, Method: f.Method, Params: f.Params}
}

// response converts a response frame. The result stays raw JSON; an error
// member that does not decode becomes an internal error rather than a silent
// success.
func (f *stdioFrame) response() *protocol.Response {
	resp := &protocol.Response{JSONRPC: f.JSONRPC, ID: f.ID}
	if f.Error != nil && string(f.Error) != "null" {
		var rpcErr protocol.Error
		if err := json.Unmarshal(f.Error, &rpcErr); err != nil {
			rpcErr = *protocol.NewInternalError("malformed error in client response: " + err.Error())
		}
		resp.Error = &rpcErr
		return resp
	}
	resp.Result = f.Result
	return resp
}

// queuedFrame is a client request or notification awaiting Serve's loop, or a
// frame that failed to parse (answered with a parse error).
type queuedFrame struct {
	req      *protocol.Request
	parseErr error
}

// frameQueue hands frames from the reader to Serve's loop in arrival order
// without making the reader wait on a busy handler.
type frameQueue struct {
	mu    sync.Mutex
	items []*queuedFrame
	done  bool
	err   error
	ready chan struct{} // capacity 1: "something changed"
}

func newFrameQueue() *frameQueue {
	return &frameQueue{ready: make(chan struct{}, 1)}
}

func (q *frameQueue) push(f *queuedFrame) {
	q.mu.Lock()
	q.items = append(q.items, f)
	q.mu.Unlock()
	q.signal()
}

// finish marks the end of input; err is nil on a clean EOF.
func (q *frameQueue) finish(err error) {
	q.mu.Lock()
	q.done = true
	q.err = err
	q.mu.Unlock()
	q.signal()
}

// pop returns the next frame, or (nil, true, err) once input ended and the
// queue is drained, or (nil, false, nil) when the caller should wait on ready.
func (q *frameQueue) pop() (*queuedFrame, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) > 0 {
		f := q.items[0]
		q.items[0] = nil
		q.items = q.items[1:]
		return f, false, nil
	}
	return nil, q.done, q.err
}

func (q *frameQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *frameQueue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}
