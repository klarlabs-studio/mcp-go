package middleware

import (
	"context"
	"time"

	"github.com/felixgeelhaar/fortify/ratelimit"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// RateLimitOption configures the rate limiter.
type RateLimitOption func(*rateLimitConfig)

type rateLimitConfig struct {
	keyFunc func(*protocol.Request) string
	logger  Logger
}

// WithRateLimitKeyFunc sets a function to extract a rate limit key from requests.
// This allows per-client or per-method rate limiting.
func WithRateLimitKeyFunc(fn func(*protocol.Request) string) RateLimitOption {
	return func(o *rateLimitConfig) {
		o.keyFunc = fn
	}
}

// WithRateLimitLogger sets the logger for rate limit events.
func WithRateLimitLogger(l Logger) RateLimitOption {
	return func(o *rateLimitConfig) {
		o.logger = l
	}
}

// RateLimit returns middleware that limits request rate using a token bucket algorithm.
// The rate is specified as requests per second.
// Burst allows short bursts above the rate limit.
func RateLimit(rate int, burst int, opts ...RateLimitOption) Middleware {
	cfg := &rateLimitConfig{
		keyFunc: func(_ *protocol.Request) string { return "global" }, // Global by default
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
			key := cfg.keyFunc(req)

			if !limiter.Allow(ctx, key) {
				if cfg.logger != nil {
					cfg.logger.Warn("rate limit exceeded",
						Field{Key: "method", Value: req.Method},
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
