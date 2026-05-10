package middleware

import (
	"context"
	"fmt"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// SizeLimitOption configures the size limit middleware.
type SizeLimitOption func(*sizeLimitConfig)

type sizeLimitConfig struct {
	logger Logger
}

// WithSizeLimitLogger sets the logger for size limit events.
func WithSizeLimitLogger(l Logger) SizeLimitOption {
	return func(o *sizeLimitConfig) {
		o.logger = l
	}
}

// SizeLimit returns middleware that rejects requests exceeding the specified size.
// The maxBytes parameter is the maximum allowed size of the request params in bytes.
func SizeLimit(maxBytes int64, opts ...SizeLimitOption) Middleware {
	cfg := &sizeLimitConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			if req.Params != nil {
				size := int64(len(req.Params))
				if size > maxBytes {
					if cfg.logger != nil {
						cfg.logger.Warn("request size limit exceeded",
							Field{Key: fieldKeyMethod, Value: req.Method},
							Field{Key: "size", Value: size},
							Field{Key: "max", Value: maxBytes},
						)
					}
					return nil, &protocol.Error{
						Code:    protocol.CodeInvalidRequest,
						Message: fmt.Sprintf("request size %d exceeds limit of %d bytes", size, maxBytes),
					}
				}
			}

			return next(ctx, req)
		}
	}
}

// Common size limit presets.
const (
	// KB is 1024 bytes.
	KB = 1024
	// MB is 1024 * 1024 bytes.
	MB = 1024 * 1024
)
