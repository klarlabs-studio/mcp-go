package transport

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// originAllowed reports whether a request's Origin header is permitted to
// reach a transport handler. Only browsers set Origin (on cross-origin
// requests and same-origin non-GET requests), so it is the primary control
// against DNS-rebinding, cross-site request forgery, and cross-site
// WebSocket hijacking. Non-browser clients (curl, Go's net/http, native MCP
// clients) omit the header entirely; an empty Origin is therefore treated as
// trusted because the browser same-origin policy is what this check backstops.
//
// When allowAll is set the caller has explicitly opted out of origin
// enforcement (WithInsecureAllowAllOrigins / WithWebSocketAllowAllOrigins).
// Otherwise the origin must match the allowlist exactly, or — by default —
// be same-origin with the request Host or a loopback address, which keeps
// localhost development working without opening the server to the public web.
// loopbackHostname is the DNS name that resolves to the local host; treated as
// same-origin so localhost development works without an explicit allowlist.
const loopbackHostname = "localhost"

func originAllowed(r *http.Request, allowlist []string, allowAll bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if allowAll {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	for _, a := range allowlist {
		if a == "*" {
			return true
		}
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	switch strings.ToLower(u.Hostname()) {
	case loopbackHostname, "127.0.0.1", "::1":
		return true
	}
	return false
}

// CORSConfig configures CORS behavior for HTTP transports.
type CORSConfig struct {
	// AllowOrigins is a list of origins that are allowed.
	// Use "*" to allow all origins, or specify exact origins.
	AllowOrigins []string

	// AllowMethods is a list of allowed HTTP methods.
	// Default: GET, POST, OPTIONS
	AllowMethods []string

	// AllowHeaders is a list of allowed request headers.
	// Default: Content-Type, Authorization, X-Request-ID
	AllowHeaders []string

	// ExposeHeaders is a list of headers the browser is allowed to access.
	ExposeHeaders []string

	// AllowCredentials indicates whether credentials are allowed.
	AllowCredentials bool

	// MaxAge indicates how long preflight results can be cached (in seconds).
	// Default: 86400 (24 hours)
	MaxAge int
}

// DefaultCORSConfig returns a permissive CORS configuration suitable for development.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type", "Authorization", "X-Request-ID"},
		MaxAge:       86400,
	}
}

// CORSHandler wraps an http.Handler with CORS support.
func CORSHandler(config CORSConfig, next http.Handler) http.Handler {
	// Set defaults
	if len(config.AllowMethods) == 0 {
		config.AllowMethods = []string{"GET", "POST", "OPTIONS"}
	}
	if len(config.AllowHeaders) == 0 {
		config.AllowHeaders = []string{"Content-Type", "Authorization", "X-Request-ID"}
	}
	if config.MaxAge == 0 {
		config.MaxAge = 86400
	}

	allowAllOrigins := len(config.AllowOrigins) == 1 && config.AllowOrigins[0] == "*"
	// A wildcard origin combined with credentials is rejected by every browser
	// (the spec forbids "Access-Control-Allow-Origin: *" alongside
	// "Access-Control-Allow-Credentials: true") and, if honored, would
	// authorize every site on the web to make credentialed requests. Normalize
	// the misconfiguration by dropping credentials rather than emitting an
	// unsafe, non-functional header pair.
	if allowAllOrigins && config.AllowCredentials {
		config.AllowCredentials = false
	}
	allowedOrigins := make(map[string]bool)
	for _, origin := range config.AllowOrigins {
		allowedOrigins[origin] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		var allowOrigin string
		if allowAllOrigins {
			allowOrigin = "*"
		} else if origin != "" && allowedOrigins[origin] {
			allowOrigin = origin
		}

		// Set CORS headers if origin is allowed
		if allowOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight request
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ", "))
				if config.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Set expose headers for actual requests
			if len(config.ExposeHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(config.ExposeHeaders, ", "))
			}
		}

		next.ServeHTTP(w, r)
	})
}

// WithCORS configures CORS for the HTTP transport.
func WithCORS(config CORSConfig) HTTPOption {
	return func(h *HTTP) {
		h.corsConfig = &config
	}
}

// WithDefaultCORS enables CORS with default permissive settings.
func WithDefaultCORS() HTTPOption {
	config := DefaultCORSConfig()
	return func(h *HTTP) {
		h.corsConfig = &config
	}
}
