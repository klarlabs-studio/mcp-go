package middleware

import (
	"context"
	"time"

	"go.klarlabs.de/fortify/ratelimit"

	"go.klarlabs.de/mcp/protocol"
)

// clientIDKey carries a per-client identifier used as the default rate-limit
// bucket key. Transports/servers can populate it via ContextWithClientID so
// RateLimit isolates clients without any extra configuration.
const clientIDKey contextKey = "clientID"

// ContextWithClientID attaches a per-client identifier to the context. RateLimit
// uses it as the default bucket key so one client cannot exhaust another's
// budget. Wire this from your transport (e.g. from an authenticated principal or
// connection id) to get per-client limiting out of the box.
func ContextWithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey, clientID)
}

// ClientIDFromContext returns the per-client identifier set by
// ContextWithClientID, or "" when none is present.
func ClientIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(clientIDKey).(string)
	return id
}

// RateLimitOption configures the rate limiter.
type RateLimitOption func(*rateLimitConfig)

type rateLimitConfig struct {
	// keyFn derives the bucket key from the request context and payload. It is
	// the single internal hook; WithRateLimitKeyFunc adapts the public
	// request-only signature onto it.
	keyFn  func(context.Context, *protocol.Request) string
	logger Logger
}

// WithRateLimitKeyFunc sets a function to extract a rate limit key from requests.
// This allows per-client or per-method rate limiting.
func WithRateLimitKeyFunc(fn func(*protocol.Request) string) RateLimitOption {
	return func(o *rateLimitConfig) {
		o.keyFn = func(_ context.Context, req *protocol.Request) string { return fn(req) }
	}
}

// WithRateLimitLogger sets the logger for rate limit events.
func WithRateLimitLogger(l Logger) RateLimitOption {
	return func(o *rateLimitConfig) {
		o.logger = l
	}
}

// RateLimit returns middleware that limits request rate using a token bucket
// algorithm. The rate is specified as requests per second; burst allows short
// bursts above the rate limit.
//
// Bucket key: by default RateLimit keys per client, using the identifier from
// ContextWithClientID when present. If no client id is on the context it falls
// back to a single shared "global" bucket — meaning one client can then consume
// the whole budget for everyone. Wire ContextWithClientID from your transport
// for safe per-client limiting, use RateLimitByClient to key off the request
// payload, or override entirely with WithRateLimitKeyFunc.
func RateLimit(rate int, burst int, opts ...RateLimitOption) Middleware {
	cfg := &rateLimitConfig{
		// Safe default: isolate per client when a client id is on the context,
		// otherwise fall back to a shared bucket (documented above).
		keyFn: func(ctx context.Context, _ *protocol.Request) string {
			if id := ClientIDFromContext(ctx); id != "" {
				return "client:" + id
			}
			return "global"
		},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Create rate limiter with fortify
	limiter := ratelimit.New(ratelimit.Config{
		Rate:     rate,
		Burst:    burst,
		Interval: time.Second,
	})

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			key := cfg.keyFn(ctx, req)

			if !limiter.Allow(ctx, key) {
				if cfg.logger != nil {
					cfg.logger.Warn("rate limit exceeded",
						Field{Key: fieldKeyMethod, Value: req.Method},
						Field{Key: "key", Value: key},
					)
				}
				return nil, &protocol.Error{
					Code:    protocol.CodeRateLimited,
					Message: "rate limit exceeded",
				}
			}

			return next(ctx, req)
		}
	}
}

// RateLimitByMethod returns rate limiting middleware that applies per-method limits.
func RateLimitByMethod(rate int, burst int, opts ...RateLimitOption) Middleware {
	allOpts := append([]RateLimitOption{
		WithRateLimitKeyFunc(func(req *protocol.Request) string {
			return req.Method
		}),
	}, opts...)
	return RateLimit(rate, burst, allOpts...)
}

// RateLimitByClient returns rate limiting middleware that applies per-client limits.
// The clientIDFunc should extract a unique client identifier from the request.
func RateLimitByClient(rate int, burst int, clientIDFunc func(*protocol.Request) string, opts ...RateLimitOption) Middleware {
	allOpts := append([]RateLimitOption{
		WithRateLimitKeyFunc(clientIDFunc),
	}, opts...)
	return RateLimit(rate, burst, allOpts...)
}
