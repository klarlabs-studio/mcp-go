package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// pipeTimeout guards every wait in these tests: a regression that deadlocks the
// transport fails the test instead of hanging the suite.
const pipeTimeout = 5 * time.Second

// syncBuffer is a goroutine-safe bytes.Buffer for capturing stderr.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// wireMsg is any JSON-RPC message as seen by the test client.
type wireMsg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *protocol.Error `json:"error,omitempty"`
}

// pipeClient is the client end of a Stdio transport served over two io.Pipes.
type pipeClient struct {
	t      *testing.T
	tr     *Stdio
	stderr *syncBuffer
	w      *io.PipeWriter // client → server (server stdin)
	msgs   chan wireMsg   // server → client, in order
	served chan error     // Serve's return value
	writer *NewlineFramer
}

// startPipe serves handler on a Stdio wired to in-memory pipes and returns the
// client end. Serve stops when the test ends.
func startPipe(t *testing.T, handler Handler) *pipeClient {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	stderr := &syncBuffer{}
	tr := NewStdio(WithStdin(inR), WithStdout(outW), WithStderr(stderr))

	c := &pipeClient{
		t: t, tr: tr, stderr: stderr, w: inW,
		msgs:   make(chan wireMsg, 256),
		served: make(chan error, 1),
		writer: NewNewlineFramer(nil, inW),
	}

	go func() {
		reader := NewNewlineFramer(outR, nil)
		for {
			line, err := reader.ReadMessage()
			if err != nil {
				close(c.msgs)
				return
			}
			var m wireMsg
			if err := json.Unmarshal(line, &m); err != nil {
				t.Errorf("server wrote invalid JSON %q: %v", line, err)
				continue
			}
			c.msgs <- m
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { c.served <- tr.Serve(ctx, handler) }()
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = outW.Close()
		_ = outR.Close()
	})
	return c
}

// send writes one raw JSON message to the server's stdin. It reports with
// Errorf so it is safe to call from a helper goroutine.
func (c *pipeClient) send(raw string) {
	c.t.Helper()
	if err := c.writer.WriteMessage(json.RawMessage(raw)); err != nil {
		c.t.Errorf("write %s: %v", raw, err)
	}
}

// next returns the next message the server wrote, failing on timeout.
func (c *pipeClient) next() wireMsg {
	c.t.Helper()
	select {
	case m, ok := <-c.msgs:
		if !ok {
			c.t.Fatal("server output closed")
		}
		return m
	case <-time.After(pipeTimeout):
		c.t.Fatal("timed out waiting for a server message (deadlock?)")
	}
	return wireMsg{}
}

// expectResponse reads the next message and requires it to answer id.
func (c *pipeClient) expectResponse(id string) wireMsg {
	c.t.Helper()
	m := c.next()
	if m.Method != "" || string(m.ID) != id {
		c.t.Fatalf("want response to id %s, got %+v", id, m)
	}
	return m
}

// expectRequest reads the next message and requires it to be a request.
func (c *pipeClient) expectRequest(method string) wireMsg {
	c.t.Helper()
	m := c.next()
	if m.Method != method || len(m.ID) == 0 {
		c.t.Fatalf("want %s request, got %+v", method, m)
	}
	return m
}

// elicitHandler is a minimal MCP-ish handler: "caps" records the elicitation
// capability on the connection's session (what initialize does), "ask" elicits
// through the session (honoring an optional timeoutMs), "ping" answers at once.
func elicitHandler(asked chan<- error) Handler {
	return HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		sess := server.SessionFromContext(ctx)
		switch req.Method {
		case "caps":
			sess.SetClientCapabilities(server.ClientCapabilities{Elicitation: true})
			return protocol.NewResponse(req.ID, "ok"), nil
		case "ask":
			var p struct {
				TimeoutMs int `json:"timeoutMs"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.TimeoutMs > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond)
				defer cancel()
			}
			res, err := server.NewElicitor(sess).Elicit(ctx, &server.ElicitRequest{Message: "name?"})
			if asked != nil {
				asked <- err
			}
			if err != nil {
				return protocol.NewResponse(req.ID, "error: "+err.Error()), nil
			}
			name, _ := res.Content["name"].(string)
			return protocol.NewResponse(req.ID, res.Action+":"+name), nil
		default:
			return protocol.NewResponse(req.ID, "pong"), nil
		}
	})
}

func resultString(t *testing.T, m wireMsg) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(m.Result, &s); err != nil {
		t.Fatalf("result %s is not a string: %v", m.Result, err)
	}
	return s
}

// TestStdioSendRequest_ClientAnswers covers the round trip for each kind of
// client answer: accept with content, decline, and a JSON-RPC error.
func TestStdioSendRequest_ClientAnswers(t *testing.T) {
	tests := []struct {
		name   string
		answer string // %s is the server request's id
		want   string
	}{
		{"accept", `{"jsonrpc":"2.0","id":%s,"result":{"action":"accept","content":{"name":"Ada"}}}`, "accept:Ada"},
		{"decline", `{"jsonrpc":"2.0","id":%s,"result":{"action":"decline"}}`, "decline:"},
		{"error", `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"no ui"}}`, "error: mcp: no ui (code: -32601)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := startPipe(t, elicitHandler(nil))
			c.send(`{"jsonrpc":"2.0","id":1,"method":"caps"}`)
			c.expectResponse("1")
			c.send(`{"jsonrpc":"2.0","id":2,"method":"ask"}`)

			req := c.expectRequest(protocol.MethodElicitationCreate)
			var id string
			if err := json.Unmarshal(req.ID, &id); err != nil || !strings.HasPrefix(id, serverRequestIDPrefix) {
				t.Fatalf("server request id = %s, want a string with prefix %q", req.ID, serverRequestIDPrefix)
			}
			var params server.ElicitRequest
			if err := json.Unmarshal(req.Params, &params); err != nil || params.Message != "name?" {
				t.Fatalf("params = %s (%v), want the elicitation message", req.Params, err)
			}

			c.send(fmt.Sprintf(tt.answer, req.ID))
			got := resultString(t, c.expectResponse("2"))
			if !strings.HasPrefix(got, tt.want) {
				t.Fatalf("ask result = %q, want prefix %q", got, tt.want)
			}
			if tt.name == "error" && !strings.Contains(got, "no ui") {
				t.Fatalf("ask result = %q, want the client's error message", got)
			}
		})
	}
}

// TestStdioSendRequest_ClientNeverAnswers: when the caller's context ends, the
// transport sends notifications/cancelled naming the request, frees the slot,
// drops the late answer, and keeps serving.
func TestStdioSendRequest_ClientNeverAnswers(t *testing.T) {
	asked := make(chan error, 1)
	c := startPipe(t, elicitHandler(asked))
	c.send(`{"jsonrpc":"2.0","id":1,"method":"caps"}`)
	c.expectResponse("1")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"ask","params":{"timeoutMs":50}}`)

	req := c.expectRequest(protocol.MethodElicitationCreate)

	cancelled := c.next()
	if cancelled.Method != protocol.MethodCancelled || len(cancelled.ID) != 0 {
		t.Fatalf("want notifications/cancelled, got %+v", cancelled)
	}
	var note server.CancelledNotification
	if err := json.Unmarshal(cancelled.Params, &note); err != nil {
		t.Fatalf("cancelled params: %v", err)
	}
	if string(note.RequestID) != string(req.ID) {
		t.Fatalf("cancelled requestId = %s, want %s", note.RequestID, req.ID)
	}
	if note.Reason == "" {
		t.Error("cancelled notification should carry a reason")
	}
	if err := <-asked; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Elicit error = %v, want context.DeadlineExceeded", err)
	}
	if got := resultString(t, c.expectResponse("2")); !strings.HasPrefix(got, "error:") {
		t.Fatalf("ask result = %q, want an error", got)
	}
	if c.tr.hasPendingRequests() {
		t.Fatal("timed-out request still holds a pending slot")
	}

	// The late answer is dropped and the server keeps reading.
	c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"action":"accept"}}`, req.ID))
	c.send(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if got := resultString(t, c.expectResponse("3")); got != "pong" {
		t.Fatalf("ping after late answer = %q", got)
	}
	if !strings.Contains(c.stderr.String(), "dropped response for unknown request id "+string(req.ID)) {
		t.Fatalf("stderr = %q, want a note about the dropped late response", c.stderr.String())
	}
}

// TestStdioSendRequest_ConnectionClosesWhileWaiting: EOF on stdin fails the
// waiting request with ErrConnectionClosed instead of leaving it blocked.
func TestStdioSendRequest_ConnectionClosesWhileWaiting(t *testing.T) {
	asked := make(chan error, 1)
	c := startPipe(t, elicitHandler(asked))
	c.send(`{"jsonrpc":"2.0","id":1,"method":"caps"}`)
	c.expectResponse("1")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"ask"}`)
	c.expectRequest(protocol.MethodElicitationCreate)

	_ = c.w.Close()

	select {
	case err := <-asked:
		if !errors.Is(err, ErrConnectionClosed) {
			t.Fatalf("Elicit error = %v, want ErrConnectionClosed", err)
		}
	case <-time.After(pipeTimeout):
		t.Fatal("waiting request was not released when the connection closed")
	}
	c.expectResponse("2") // the handler's own reply still goes out
	select {
	case err := <-c.served:
		if err != nil {
			t.Fatalf("Serve = %v, want nil on EOF", err)
		}
	case <-time.After(pipeTimeout):
		t.Fatal("Serve did not return after EOF")
	}

	// After Serve returns, SendRequest fails fast.
	_, err := c.tr.SendRequest(context.Background(), &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"})
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("SendRequest after Serve = %v, want ErrConnectionClosed", err)
	}
}

// TestStdioSendRequest_NotServing: SendRequest before Serve cannot succeed.
func TestStdioSendRequest_NotServing(t *testing.T) {
	tr := NewStdio(WithStdin(strings.NewReader("")), WithStdout(io.Discard))
	_, err := tr.SendRequest(context.Background(), &protocol.Request{Method: "ping"})
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("SendRequest before Serve = %v, want ErrConnectionClosed", err)
	}
	if _, err := tr.SendRequest(context.Background(), nil); err == nil {
		t.Fatal("SendRequest(nil) should fail")
	}
}

// TestStdioSendRequest_Concurrent: concurrent requests answered out of order
// each receive their own response, carrying the caller's original ID.
func TestStdioSendRequest_Concurrent(t *testing.T) {
	c := startPipe(t, elicitHandler(nil))
	// A round trip guarantees Serve has opened the request path.
	c.send(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	c.expectResponse("1")

	const n = 8
	type outcome struct {
		i    int
		resp *protocol.Response
		err  error
	}
	results := make(chan outcome, n)
	for i := range n {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), pipeTimeout)
			defer cancel()
			resp, err := c.tr.SendRequest(ctx, &protocol.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(fmt.Sprint(i)),
				Method:  "test/echo",
				Params:  json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
			})
			results <- outcome{i, resp, err}
		}()
	}

	reqs := make([]wireMsg, 0, n)
	seen := map[string]bool{}
	for range n {
		m := c.expectRequest("test/echo")
		if seen[string(m.ID)] {
			t.Fatalf("duplicate server request id %s", m.ID)
		}
		seen[string(m.ID)] = true
		reqs = append(reqs, m)
	}
	// Answer in reverse order, echoing each request's params.
	for i := len(reqs) - 1; i >= 0; i-- {
		c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, reqs[i].ID, reqs[i].Params))
	}

	for range n {
		var o outcome
		select {
		case o = <-results:
		case <-time.After(pipeTimeout):
			t.Fatal("a concurrent SendRequest never returned")
		}
		if o.err != nil {
			t.Fatalf("request %d: %v", o.i, o.err)
		}
		if string(o.resp.ID) != fmt.Sprint(o.i) {
			t.Errorf("request %d: response id %s, want the caller's id", o.i, o.resp.ID)
		}
		raw, _ := json.Marshal(o.resp.Result)
		var got struct{ I int }
		if err := json.Unmarshal(raw, &got); err != nil || got.I != o.i {
			t.Errorf("request %d got result %s, want its own", o.i, raw)
		}
	}
	if c.tr.hasPendingRequests() {
		t.Fatal("pending slots remain after all responses")
	}
}

// TestStdioSendRequest_HandlerBlockedDoesNotStopReading is the deadlock case.
// A tool blocked in elicitation runs on Serve's loop; the client sends more
// requests (well past the reader's queue bound) and a notification BEFORE it
// answers. The answer must still reach the tool — it can only be read if the
// reader routes responses itself and keeps reading while a request is pending.
// Without that, this test times out. Client requests are then answered in the
// order they arrived.
func TestStdioSendRequest_HandlerBlockedDoesNotStopReading(t *testing.T) {
	c := startPipe(t, elicitHandler(nil))
	c.send(`{"jsonrpc":"2.0","id":1,"method":"caps"}`)
	c.expectResponse("1")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"ask"}`)
	req := c.expectRequest(protocol.MethodElicitationCreate)

	const queued = 2*maxQueuedFrames + 5
	go func() {
		for i := range queued {
			c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"ping"}`, 100+i))
		}
		c.send(`{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`)
		c.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"action":"accept","content":{"name":"Ada"}}}`, req.ID))
	}()

	if got := resultString(t, c.expectResponse("2")); got != "accept:Ada" {
		t.Fatalf("ask result = %q, want accept:Ada", got)
	}
	for i := range queued {
		c.expectResponse(fmt.Sprint(100 + i))
	}
}

// TestStdioSendRequest_UnknownResponseIgnored: a response nobody asked for is
// dropped without disturbing request handling.
func TestStdioSendRequest_UnknownResponseIgnored(t *testing.T) {
	c := startPipe(t, elicitHandler(nil))
	c.send(`{"jsonrpc":"2.0","id":"mcp-go-srv-999","result":{}}`)
	c.send(`{"jsonrpc":"2.0","id":7,"error":{"code":1,"message":"x"}}`)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if got := resultString(t, c.expectResponse("1")); got != "pong" {
		t.Fatalf("ping = %q", got)
	}
	if s := c.stderr.String(); !strings.Contains(s, `"mcp-go-srv-999"`) || !strings.Contains(s, "id 7") {
		t.Fatalf("stderr = %q, want notes for both unknown responses", s)
	}
}

// TestStdioSendRequest_NoCapability: a client that did not declare elicitation
// gets the session's capability error, and nothing is sent to it.
func TestStdioSendRequest_NoCapability(t *testing.T) {
	asked := make(chan error, 1)
	c := startPipe(t, elicitHandler(asked))
	c.send(`{"jsonrpc":"2.0","id":2,"method":"ask"}`)

	got := resultString(t, c.expectResponse("2")) // first frame out is the reply, not a request
	if err := <-asked; err == nil || !strings.Contains(err.Error(), "client does not support elicitation") {
		t.Fatalf("Elicit error = %v, want the capability error", err)
	}
	if !strings.HasPrefix(got, "error:") {
		t.Fatalf("ask result = %q", got)
	}
}

// TestStdioFrame_IsResponse pins the request/response classification.
func TestStdioFrame_IsResponse(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{`{"jsonrpc":"2.0","id":"a","result":{}}`, true},
		{`{"jsonrpc":"2.0","id":1,"result":null}`, true},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":1,"message":"m"}}`, true},
		{`{"jsonrpc":"2.0","id":1,"method":"ping"}`, false},
		{`{"jsonrpc":"2.0","method":"notifications/x"}`, false},
		{`{"jsonrpc":"2.0","id":null,"result":{}}`, false},
		{`{"jsonrpc":"2.0","id":1}`, false},
	}
	for _, tt := range tests {
		var f stdioFrame
		if err := json.Unmarshal([]byte(tt.raw), &f); err != nil {
			t.Fatalf("%s: %v", tt.raw, err)
		}
		if got := f.isResponse(); got != tt.want {
			t.Errorf("isResponse(%s) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

// TestStdioFrame_MalformedErrorMember: an undecodable error member surfaces as
// an error response, never as a silent success.
func TestStdioFrame_MalformedErrorMember(t *testing.T) {
	f := stdioFrame{ID: json.RawMessage(`"x"`), Error: json.RawMessage(`"boom"`)}
	resp := f.response()
	if resp.Error == nil || resp.Error.Code != protocol.CodeInternalError {
		t.Fatalf("response = %+v, want an internal error", resp)
	}
}
