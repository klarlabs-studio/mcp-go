package middleware

import (
	"context"
	"encoding/json"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// Shared string constants for middleware audit/auth/rate-limit/size-limit
// emitters. Defined here since AuditEvent is the canonical home; auth.go,
// ratelimit.go, sizelimit.go and logging.go reuse them.
const (
	statusError   = "error"
	statusSuccess = "success"

	actionToolsList    = "tools.list"
	actionToolsExecute = "tools.execute"

	// Log/audit field keys reused across middleware emitters.
	fieldKeyMethod = "method"
	fieldKeyError  = "error"
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
				event.Status = statusError
				event.Error = err.Error()
			case resp != nil && resp.Error != nil:
				event.Status = statusError
				event.Error = resp.Error.Message
			default:
				event.Status = statusSuccess
			}

			a.logger.LogEvent(ctx, event)

			return resp, err
		}
	}
}

func classifyAction(method string) string {
	switch method {
	case protocol.MethodInitialize:
		return "session.start"
	case protocol.MethodInitialized:
		return "session.ready"
	case protocol.MethodToolsList:
		return actionToolsList
	case protocol.MethodToolsCall:
		return actionToolsExecute
	case protocol.MethodResourcesList:
		return "resources.list"
	case protocol.MethodResourcesRead:
		return "resources.read"
	case protocol.MethodResourcesSubscribe:
		return "resources.subscribe"
	case protocol.MethodResourcesUnsubscribe:
		return "resources.unsubscribe"
	case protocol.MethodPromptsList:
		return "prompts.list"
	case protocol.MethodPromptsGet:
		return "prompts.get"
	case protocol.MethodPing:
		return "health.ping"
	case "tasks/create":
		return "tasks.create"
	case "tasks/get":
		return "tasks.get"
	case "tasks/list":
		return "tasks.list"
	case "tasks/cancel":
		return "tasks.cancel"
	case protocol.MethodLoggingSetLevel:
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
