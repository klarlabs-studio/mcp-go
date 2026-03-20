package middleware

import (
	"context"
	"encoding/json"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

type AuditEvent struct {
	Timestamp     time.Time      `json:"timestamp"`
	CorrelationID string         `json:"correlationId"`
	RequestID     string         `json:"requestId,omitempty"`
	Method        string         `json:"method"`
	Action        string         `json:"action"`
	Actor         string         `json:"actor,omitempty"`
	Resource      string         `json:"resource,omitempty"`
	Status        string         `json:"status"`
	Error         string         `json:"error,omitempty"`
	Duration      time.Duration  `json:"duration"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type AuditLogger interface {
	LogEvent(ctx context.Context, event AuditEvent)
}

type AuditMiddleware struct {
	logger AuditLogger
}

func NewAuditMiddleware(logger AuditLogger) *AuditMiddleware {
	return &AuditMiddleware{logger: logger}
}

func (a *AuditMiddleware) Middleware() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			start := time.Now()

			correlationID := CorrelationIDFromContext(ctx)
			requestID := RequestIDFromContext(ctx)

			action := classifyAction(req.Method)

			event := AuditEvent{
				Timestamp:     start,
				CorrelationID: correlationID,
				RequestID:     requestID,
				Method:        req.Method,
				Action:        action,
			}

			resp, err := next(ctx, req)

			event.Duration = time.Since(start)
			event.Error = ""
			switch {
			case err != nil:
				event.Status = "error"
				event.Error = err.Error()
			case resp != nil && resp.Error != nil:
				event.Status = "error"
				event.Error = resp.Error.Message
			default:
				event.Status = "success"
			}

			a.logger.LogEvent(ctx, event)

			return resp, err
		}
	}
}

func classifyAction(method string) string {
	switch method {
	case "initialize":
		return "session.start"
	case "notifications/initialized":
		return "session.ready"
	case "tools/list":
		return "tools.list"
	case "tools/call":
		return "tools.execute"
	case "resources/list":
		return "resources.list"
	case "resources/read":
		return "resources.read"
	case "resources/subscribe":
		return "resources.subscribe"
	case "resources/unsubscribe":
		return "resources.unsubscribe"
	case "prompts/list":
		return "prompts.list"
	case "prompts/get":
		return "prompts.get"
	case "ping":
		return "health.ping"
	case "tasks/create":
		return "tasks.create"
	case "tasks/get":
		return "tasks.get"
	case "tasks/list":
		return "tasks.list"
	case "tasks/cancel":
		return "tasks.cancel"
	case "logging/setLevel":
		return "logging.configure"
	default:
		return "unknown"
	}
}

type JSONAuditLogger struct{}

func (JSONAuditLogger) LogEvent(ctx context.Context, event AuditEvent) {
	data, _ := json.Marshal(event)
	println("[AUDIT]", string(data))
}
