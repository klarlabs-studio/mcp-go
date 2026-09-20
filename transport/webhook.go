package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// WebhookNotifier delivers JSON-RPC notifications to a client-controlled HTTP
// endpoint. It is the out-of-band counterpart to in-session channel push
// (notifications/channel/message) and resource-updated notifications — useful
// when the client cannot keep a long-lived SSE/stdio stream open.
//
// This is a pragmatic delivery helper ahead of a formal webhooks SEP: the
// payload shape is the same JSON-RPC notification the live transport would
// emit. Authentication of the webhook URL and TLS pinning remain the caller's
// responsibility.
type WebhookNotifier struct {
	URL     string
	Client  *http.Client
	Headers http.Header

	mu     sync.Mutex
	client *http.Client
}

// NewWebhookNotifier creates a notifier that POSTs notifications to url.
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{URL: url}
}

// SendNotification POSTs a JSON-RPC 2.0 notification to the configured URL.
func (w *WebhookNotifier) SendNotification(method string, params any) error {
	if w == nil || w.URL == "" {
		return fmt.Errorf("webhook: no URL configured")
	}
	paramsData, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("webhook: marshal params: %w", err)
	}
	msg, err := json.Marshal(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	})
	if err != nil {
		return fmt.Errorf("webhook: marshal notification: %w", err)
	}

	client := w.httpClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcpMethodHeader, method)
	for k, vals := range w.Headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: POST %s: %w", method, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: POST %s: unexpected status %d", method, resp.StatusCode)
	}
	return nil
}

func (w *WebhookNotifier) httpClient() *http.Client {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Client != nil {
		return w.Client
	}
	if w.client == nil {
		w.client = &http.Client{Timeout: 10 * time.Second}
	}
	return w.client
}

// MultiNotifier fans a single SendNotification out to multiple notifiers.
// Errors from individual notifiers are joined; a nil entry is skipped.
type MultiNotifier struct {
	Notifiers []NotificationSender
}

// SendNotification delivers to every configured notifier.
func (m *MultiNotifier) SendNotification(method string, params any) error {
	if m == nil {
		return fmt.Errorf("multi-notifier: nil")
	}
	var errs []error
	for _, n := range m.Notifiers {
		if n == nil {
			continue
		}
		if err := n.SendNotification(method, params); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	msg := errs[0].Error()
	for _, e := range errs[1:] {
		msg += "; " + e.Error()
	}
	return fmt.Errorf("multi-notifier: %s", msg)
}

// Ensure WebhookNotifier satisfies NotificationSender.
var (
	_ NotificationSender = (*WebhookNotifier)(nil)
	_ NotificationSender = (*MultiNotifier)(nil)
)

// ChannelWebhook is a convenience that POSTs a channel message via webhook.
func (w *WebhookNotifier) SendChannelMessage(channel string, content any) error {
	return w.SendNotification(protocol.MethodChannelMessage, map[string]any{
		"channel": channel,
		"message": content,
	})
}
