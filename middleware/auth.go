package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

// Identity represents an authenticated identity.
type Identity struct {
	// ID is a unique identifier for the identity (e.g., user ID, API key ID).
	ID string
	// Name is a human-readable name for the identity.
	Name string
	// Metadata contains additional identity information.
	Metadata map[string]any
}

// identityContextKey is the context key for storing the identity.
type identityContextKey struct{}

// IdentityFromContext returns the authenticated identity from the context.
// Returns nil if no identity is present.
func IdentityFromContext(ctx context.Context) *Identity {
	if id, ok := ctx.Value(identityContextKey{}).(*Identity); ok {
		return id
	}
	return nil
}

// ContextWithIdentity returns a new context with the identity attached.
func ContextWithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// AuthOption configures the authentication middleware.
type AuthOption func(*authConfig)

type authConfig struct {
	logger       Logger
	skipMethods  map[string]bool
	realm        string
	errorMessage string
}

// WithAuthLogger sets the logger for auth events.
func WithAuthLogger(l Logger) AuthOption {
	return func(c *authConfig) {
		c.logger = l
	}
}

// WithAuthSkipMethods specifies methods that don't require authentication.
// By default, "initialize" and "ping" are always skipped.
func WithAuthSkipMethods(methods ...string) AuthOption {
	return func(c *authConfig) {
		for _, m := range methods {
			c.skipMethods[m] = true
		}
	}
}

// WithAuthRealm sets the realm for authentication errors.
func WithAuthRealm(realm string) AuthOption {
	return func(c *authConfig) {
		c.realm = realm
	}
}

// WithAuthErrorMessage sets a custom error message for auth failures.
func WithAuthErrorMessage(msg string) AuthOption {
	return func(c *authConfig) {
		c.errorMessage = msg
	}
}

// Authenticator is a function that validates credentials and returns an identity.
// It receives the request and should return an identity if authentication succeeds,
// or nil with an error if it fails.
type Authenticator func(ctx context.Context, req *protocol.Request) (*Identity, error)

// Auth returns middleware that authenticates requests using the provided authenticator.
// If authentication fails, the request is rejected with an authentication error.
func Auth(authenticator Authenticator, opts ...AuthOption) Middleware {
	cfg := &authConfig{
		skipMethods: map[string]bool{
			protocol.MethodInitialize: true,
			protocol.MethodPing:       true,
		},
		realm:        "mcp",
		errorMessage: "authentication required",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			// Skip authentication for certain methods
			if cfg.skipMethods[req.Method] {
				return next(ctx, req)
			}

			// Authenticate the request
			identity, err := authenticator(ctx, req)
			if err != nil {
				if cfg.logger != nil {
					cfg.logger.Warn("authentication failed",
						Field{Key: "method", Value: req.Method},
						Field{Key: "error", Value: err.Error()},
					)
				}
				return nil, &protocol.Error{
					Code:    protocol.CodeUnauthorized,
					Message: cfg.errorMessage,
				}
			}

			if identity == nil {
				if cfg.logger != nil {
					cfg.logger.Warn("authentication failed: no identity",
						Field{Key: "method", Value: req.Method},
					)
				}
				return nil, &protocol.Error{
					Code:    protocol.CodeUnauthorized,
					Message: cfg.errorMessage,
				}
			}

			if cfg.logger != nil {
				cfg.logger.Debug("authenticated",
					Field{Key: "method", Value: req.Method},
					Field{Key: "identity", Value: identity.ID},
				)
			}

			// Add identity to context and continue
			ctx = ContextWithIdentity(ctx, identity)
			return next(ctx, req)
		}
	}
}

// APIKeyAuthenticator creates an authenticator that validates API keys.
// The keyValidator function should return the identity for a valid key, or nil for invalid.
func APIKeyAuthenticator(headerName string, keyValidator func(key string) *Identity) Authenticator {
	return func(ctx context.Context, req *protocol.Request) (*Identity, error) {
		// For MCP over stdio, API key would typically be passed via initialization
		// For HTTP transports, it would come from headers (handled at transport level)
		// Here we check if the key was passed in request metadata
		key := protocol.GetRequestMeta(ctx, headerName)
		if key == "" {
			// Also check common variations
			key = protocol.GetRequestMeta(ctx, strings.ToLower(headerName))
		}
		if key == "" {
			return nil, nil
		}

		return keyValidator(key), nil
	}
}

// BearerTokenAuthenticator creates an authenticator that validates bearer tokens.
// The tokenValidator function should return the identity for a valid token, or nil for invalid.
func BearerTokenAuthenticator(tokenValidator func(token string) *Identity) Authenticator {
	return func(ctx context.Context, req *protocol.Request) (*Identity, error) {
		auth := protocol.GetRequestMeta(ctx, "Authorization")
		if auth == "" {
			auth = protocol.GetRequestMeta(ctx, "authorization")
		}
		if auth == "" {
			return nil, nil
		}

		// Parse "Bearer <token>"
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			return nil, nil
		}

		token := strings.TrimPrefix(auth, prefix)
		if token == "" {
			return nil, nil
		}

		return tokenValidator(token), nil
	}
}

// StaticAPIKeys creates a simple key validator from a map of key -> identity.
func StaticAPIKeys(keys map[string]*Identity) func(string) *Identity {
	return func(key string) *Identity {
		return keys[key]
	}
}

// StaticTokens creates a simple token validator from a map of token -> identity.
func StaticTokens(tokens map[string]*Identity) func(string) *Identity {
	return func(token string) *Identity {
		return tokens[token]
	}
}

// ChainAuthenticators chains multiple authenticators, returning the first successful identity.
func ChainAuthenticators(authenticators ...Authenticator) Authenticator {
	return func(ctx context.Context, req *protocol.Request) (*Identity, error) {
		for _, auth := range authenticators {
			identity, err := auth(ctx, req)
			if err != nil {
				return nil, err
			}
			if identity != nil {
				return identity, nil
			}
		}
		return nil, nil
	}
}

type OAuth2Config struct {
	TokenIntrospectionURL string
	ClientID              string
	ClientSecret          string
	RequiredScopes        []string
}

type TokenClaims struct {
	Subject   string
	Audience  []string
	ExpiresAt int64
	IssuedAt  int64
	Scopes    []string
	Issuer    string
}

type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (*TokenClaims, error)
}

type OAuth2Authenticator struct {
	config   OAuth2Config
	validate TokenValidator
}

func NewOAuth2Authenticator(config OAuth2Config, validator TokenValidator) *OAuth2Authenticator {
	return &OAuth2Authenticator{
		config:   config,
		validate: validator,
	}
}

func (a *OAuth2Authenticator) Authenticate(ctx context.Context, req *protocol.Request) (*Identity, error) {
	auth := protocol.GetRequestMeta(ctx, "Authorization")
	if auth == "" {
		auth = protocol.GetRequestMeta(ctx, "authorization")
	}
	if auth == "" {
		return nil, nil
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return nil, nil
	}

	token := strings.TrimPrefix(auth, prefix)
	if token == "" {
		return nil, nil
	}

	claims, err := a.validate.ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	expTime := claims.ExpiresAt
	if expTime > 0 && expTime < 10000000000 {
		expTime *= 1000
	}

	if expTime > 0 && expTime < time.Now().UnixMilli() {
		return nil, &protocol.Error{
			Code:    protocol.CodeUnauthorized,
			Message: "token expired",
		}
	}

	if len(a.config.RequiredScopes) > 0 {
		for _, required := range a.config.RequiredScopes {
			hasScope := false
			for _, scope := range claims.Scopes {
				if scope == required {
					hasScope = true
					break
				}
			}
			if !hasScope {
				return nil, &protocol.Error{
					Code:    protocol.CodeUnauthorized,
					Message: "insufficient scope",
				}
			}
		}
	}

	return &Identity{
		ID:   claims.Subject,
		Name: claims.Subject,
		Metadata: map[string]any{
			"scopes":   claims.Scopes,
			"audience": claims.Audience,
			"issuer":   claims.Issuer,
		},
	}, nil
}

type JWTValidator struct {
	secret []byte
}

func NewJWTValidator(secret string) *JWTValidator {
	return &JWTValidator{secret: []byte(secret)}
}

func (v *JWTValidator) ValidateToken(ctx context.Context, token string) (*TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, &protocol.Error{
			Code:    protocol.CodeUnauthorized,
			Message: "invalid token format",
		}
	}

	decoded, err := base64Decode(parts[1])
	if err != nil {
		return nil, err
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, err
	}

	tokenClaims := &TokenClaims{}

	if sub, ok := claims["sub"].(string); ok {
		tokenClaims.Subject = sub
	}

	if iss, ok := claims["iss"].(string); ok {
		tokenClaims.Issuer = iss
	}

	if exp, ok := claims["exp"].(float64); ok {
		tokenClaims.ExpiresAt = int64(exp)
	}

	if iat, ok := claims["iat"].(float64); ok {
		tokenClaims.IssuedAt = int64(iat)
	}

	if aud, ok := claims["aud"].(string); ok {
		tokenClaims.Audience = []string{aud}
	} else if auds, ok := claims["aud"].([]any); ok {
		for _, a := range auds {
			if s, ok := a.(string); ok {
				tokenClaims.Audience = append(tokenClaims.Audience, s)
			}
		}
	}

	if scope, ok := claims["scope"].(string); ok {
		tokenClaims.Scopes = strings.Split(scope, " ")
	} else if scopes, ok := claims["scopes"].([]any); ok {
		for _, s := range scopes {
			if str, ok := s.(string); ok {
				tokenClaims.Scopes = append(tokenClaims.Scopes, str)
			}
		}
	}

	return tokenClaims, nil
}

func base64Decode(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	return base64.StdEncoding.DecodeString(s)
}
