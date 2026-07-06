package server

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"

	"go.klarlabs.de/mcp/protocol"
)

// ProgressToken is a unique identifier for tracking progress of a request.
type ProgressToken string

// Progress represents a progress update for a long-running operation.
type Progress struct {
	// Current progress value (must increase with each update)
	Progress float64 `json:"progress"`
	// Total expected value (optional, omit if unknown)
	Total *float64 `json:"total,omitempty"`
	// Optional message describing the current state
	Message string `json:"message,omitempty"`
}

// ProgressReporter allows tool handlers to report progress updates.
type ProgressReporter interface {
	// Report sends a progress update.
	// Progress value must increase with each call.
	Report(progress float64, total *float64) error
	// ReportWithMessage sends a progress update with a descriptive message.
	ReportWithMessage(progress float64, total *float64, message string) error
	// Token returns the progress token, or empty string if none.
	Token() ProgressToken
}

// progressReporter implements ProgressReporter.
type progressReporter struct {
	token    ProgressToken
	notifier NotificationSender
	mu       sync.Mutex
	last     float64
}

// NotificationSender can send JSON-RPC notifications.
type NotificationSender interface {
	SendNotification(method string, params any) error
}

// NewProgressReporter creates a new progress reporter.
func NewProgressReporter(token ProgressToken, notifier NotificationSender) ProgressReporter {
	return &progressReporter{
		token:    token,
		notifier: notifier,
	}
}

func (p *progressReporter) Token() ProgressToken {
	return p.token
}

func (p *progressReporter) Report(progress float64, total *float64) error {
	return p.ReportWithMessage(progress, total, "")
}

func (p *progressReporter) ReportWithMessage(progress float64, total *float64, message string) error {
	if p.token == "" || p.notifier == nil {
		return nil // No progress tracking requested
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Progress must increase
	if progress <= p.last {
		progress = p.last + 0.1
	}
	p.last = progress

	params := map[string]any{
		"progressToken": string(p.token),
		"progress":      progress,
	}
	if total != nil {
		params["total"] = *total
	}
	if message != "" {
		params["message"] = message
	}

	return p.notifier.SendNotification(protocol.MethodProgress, params)
}

// progressContextKey is the context key for progress reporter.
type progressContextKey struct{}

// ContextWithProgress returns a context with the progress reporter attached.
func ContextWithProgress(ctx context.Context, reporter ProgressReporter) context.Context {
	return context.WithValue(ctx, progressContextKey{}, reporter)
}

// ProgressFromContext returns the progress reporter from context, or a no-op reporter if none.
func ProgressFromContext(ctx context.Context) ProgressReporter {
	if reporter, ok := ctx.Value(progressContextKey{}).(ProgressReporter); ok {
		return reporter
	}
	return &noopProgressReporter{}
}

// noopProgressReporter is a no-op implementation when no progress tracking is requested.
type noopProgressReporter struct{}

func (n *noopProgressReporter) Report(_ float64, _ *float64) error                      { return nil }
func (n *noopProgressReporter) ReportWithMessage(_ float64, _ *float64, _ string) error { return nil }
func (n *noopProgressReporter) Token() ProgressToken                                    { return "" }

// ExtractProgressToken extracts the progress token from request params.
func ExtractProgressToken(params json.RawMessage) ProgressToken {
	if params == nil {
		return ""
	}

	// MCP allows progressToken to be a string OR an integer, so it is decoded
	// as a raw token and normalized rather than forced into a string (which
	// would fail the whole unmarshal for a numeric token, silently dropping it).
	var meta struct {
		Meta struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}

	if err := json.Unmarshal(params, &meta); err != nil {
		return ""
	}

	return parseProgressToken(meta.Meta.ProgressToken)
}

// parseProgressToken converts a raw progressToken (string or number, per the
// MCP spec) into an opaque ProgressToken. A JSON string is unquoted; a numeric
// token is preserved verbatim as its digits. Absent, null, or malformed tokens
// yield the empty token.
func parseProgressToken(raw json.RawMessage) ProgressToken {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return ProgressToken(s)
	}

	// Numeric (or other scalar) token: keep the raw JSON as an opaque token.
	return ProgressToken(raw)
}
