package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.klarlabs.de/mcp/protocol"
)

// NotificationHandler receives a server-initiated JSON-RPC notification: a
// message with a method but no id (e.g. notifications/resources/updated).
type NotificationHandler func(method string, params json.RawMessage)

// StreamingTransport is an optional Transport that also receives
// server-initiated notifications over a persistent channel (HTTP+SSE). The
// Client uses it to deliver subscription updates.
type StreamingTransport interface {
	Transport
	// Stream opens the server-push channel and delivers notifications to
	// handler until ctx is cancelled or the connection drops. It blocks;
	// callers run it in a goroutine.
	Stream(ctx context.Context, handler NotificationHandler) error
}

// postURL is the JSON-RPC POST endpoint with the correlation clientId so the
// server can target this transport's SSE stream.
func (t *HTTPTransport) postURL() string {
	sep := "?"
	if strings.Contains(t.endpoint, "?") {
		sep = "&"
	}
	return t.endpoint + sep + "clientId=" + t.clientID
}

// streamURL is the SSE endpoint sibling of the POST endpoint (…/mcp → …/mcp/sse).
func (t *HTTPTransport) streamURL() string {
	return t.endpoint + "/sse?clientId=" + t.clientID
}

// Stream connects to the server's SSE endpoint and dispatches each inbound
// JSON-RPC notification (a message with a method and no id) to handler. It
// returns when ctx is cancelled or the stream ends/fails.
func (t *HTTPTransport) Stream(ctx context.Context, handler NotificationHandler) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.streamURL(), nil)
	if err != nil {
		return fmt.Errorf("build stream request: %w", err)
	}
	for k, vs := range t.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("open stream: unexpected status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "" {
			continue // event lines, comments, blank separators, the connected frame
		}
		var msg protocol.Request
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			continue // not a JSON-RPC frame (e.g. the {"clientId":...} hello)
		}
		// Only notifications (method, no id) are dispatched here.
		if msg.Method != "" && msg.IsNotification() {
			handler(msg.Method, msg.Params)
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return ctx.Err()
}
