package middleware

import (
	"context"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// Logger is the interface for structured logging.
type Logger interface {
	Info(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Debug(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
}

// Field represents a key-value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

// NewField creates a new Field with the given key and value.
func NewField(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// F is an alias for NewField for brevity in internal usage.
//
// Deprecated: Use NewField instead for new code.
var F = NewField

// Logging returns middleware that logs request details.
// Successful requests are logged at info level, errors at error level.
func Logging(logger Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			start := time.Now()

			resp, err := next(ctx, req)

			duration := time.Since(start)

			// Build fields
			fields := []Field{
				NewField(fieldKeyMethod, req.Method),
				NewField("duration", duration),
			}

			// Add request ID if present
			if requestID := RequestIDFromContext(ctx); requestID != "" {
				fields = append(fields, NewField("request_id", requestID))
			}

			if err != nil {
				fields = append(fields, NewField(fieldKeyError, err.Error()))
				logger.Error("request failed", fields...)
			} else {
				logger.Info("request completed", fields...)
			}

			return resp, err
		}
	}
}

// NopLogger is a logger that discards all log entries.
type NopLogger struct{}

func (NopLogger) Info(msg string, fields ...Field)  {}
func (NopLogger) Error(msg string, fields ...Field) {}
func (NopLogger) Debug(msg string, fields ...Field) {}
func (NopLogger) Warn(msg string, fields ...Field)  {}
