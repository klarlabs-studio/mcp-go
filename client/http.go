package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// HTTPTransport speaks JSON-RPC to an MCP server over HTTP. It mirrors
// the wire format served by transport.HTTP at the /mcp endpoint:
// every request is POSTed as application/json and the response body
// is the matching JSON-RPC response.
//
// The transport is safe for concurrent use: the underlying
// http.Client serializes connection reuse, and Send carries no shared
// per-request state.
type HTTPTransport struct {
	endpoint   string
	httpClient *http.Client
	headers    http.Header
}

// HTTPTransportOption configures an HTTPTransport.
type HTTPTransportOption func(*httpTransportOptions)

type httpTransportOptions struct {
	httpClient *http.Client
	headers    http.Header
	bearer     string
	caBundle   *x509.CertPool
	insecure   bool
	timeout    time.Duration
	endpoint   string
}

// WithHTTPClient overrides the http.Client used for requests. When
// supplied, WithBearerToken / WithCABundle / WithInsecureSkipVerify /
// WithRequestTimeout become no-ops on the transport TLS configuration —
// the caller is expected to have set those on the supplied client.
func WithHTTPClient(c *http.Client) HTTPTransportOption {
	return func(o *httpTransportOptions) { o.httpClient = c }
}

// WithBearerToken attaches Authorization: Bearer <token> to every
// outbound request. Empty token is a no-op so callers can pass it
// unconditionally.
func WithBearerToken(token string) HTTPTransportOption {
	return func(o *httpTransportOptions) { o.bearer = token }
}

// WithHTTPHeader attaches a static header to every outbound request.
// Repeated calls accumulate; pass the same key twice to send multiple
// values. Use for forwarding tenant headers, telemetry headers, etc.
func WithHTTPHeader(key, value string) HTTPTransportOption {
	return func(o *httpTransportOptions) {
		if o.headers == nil {
			o.headers = http.Header{}
		}
		o.headers.Add(key, value)
	}
}

// WithCABundle pins the set of CAs used to verify the upstream TLS
// certificate. Use this when the operator runs a private MCP server
// behind their own PKI. Ignored when WithHTTPClient supplies a fully
// configured client.
func WithCABundle(pool *x509.CertPool) HTTPTransportOption {
	return func(o *httpTransportOptions) { o.caBundle = pool }
}

// WithInsecureSkipVerify disables TLS certificate verification.
// Dangerous; only useful for local development. Ignored when
// WithHTTPClient supplies a fully configured client.
func WithInsecureSkipVerify() HTTPTransportOption {
	return func(o *httpTransportOptions) { o.insecure = true }
}

// WithRequestTimeout sets the per-request timeout on the underlying
// http.Client. The Send context still applies and overrides this when
// it expires sooner. Default: 30s.
func WithRequestTimeout(d time.Duration) HTTPTransportOption {
	return func(o *httpTransportOptions) { o.timeout = d }
}

// WithEndpointPath overrides the URL path used for requests. The
// default ("/mcp") matches transport.HTTP. Use this only when an
// operator runs the server behind a path-rewriting proxy.
func WithEndpointPath(path string) HTTPTransportOption {
	return func(o *httpTransportOptions) { o.endpoint = path }
}

// NewHTTPTransport constructs a transport that POSTs JSON-RPC
// requests to the supplied base URL. The base URL must include a
// scheme (http or https) and host; the transport appends "/mcp" by
// default (override via WithEndpointPath).
//
// Returns an error when baseURL is malformed or supplies a scheme
// other than http/https.
func NewHTTPTransport(baseURL string, opts ...HTTPTransportOption) (*HTTPTransport, error) {
	if baseURL == "" {
		return nil, errors.New("mcp-go/client: HTTP transport requires a base URL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("HTTP transport scheme %q not supported (use http or https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("HTTP transport base URL missing host")
	}

	options := httpTransportOptions{
		timeout:  30 * time.Second,
		endpoint: "/mcp",
	}
	for _, o := range opts {
		o(&options)
	}

	endpoint := joinURL(u, options.endpoint)

	client := options.httpClient
	if client == nil {
		client = buildHTTPClient(options)
	}

	headers := options.headers
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	if options.bearer != "" {
		headers.Set("Authorization", "Bearer "+options.bearer)
	}

	return &HTTPTransport{
		endpoint:   endpoint,
		httpClient: client,
		headers:    headers,
	}, nil
}

func buildHTTPClient(o httpTransportOptions) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.caBundle != nil || o.insecure {
		tr.TLSClientConfig = &tls.Config{
			RootCAs:            o.caBundle,
			InsecureSkipVerify: o.insecure,
			MinVersion:         tls.VersionTLS12,
		}
	}
	return &http.Client{
		Timeout:   o.timeout,
		Transport: tr,
	}
}

func joinURL(base *url.URL, path string) string {
	if path == "" {
		return base.String()
	}
	cloned := *base
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	cloned.Path = strings.TrimRight(cloned.Path, "/") + path
	return cloned.String()
}

// Send marshals the JSON-RPC request, POSTs it to the configured
// endpoint, and returns the decoded response. Network errors and
// non-2xx HTTP statuses surface as errors; a JSON-RPC-level error
// (resp.Error != nil) is preserved on the returned response so the
// Client can surface protocol errors cleanly.
func (t *HTTPTransport) Send(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, vs := range t.headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPStatusError{
			Status:  resp.StatusCode,
			Body:    truncate(respBody, 512),
			Headers: resp.Header.Clone(),
		}
	}

	var out protocol.Response
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// Close releases idle connections held by the underlying http.Client.
// The transport itself is reusable after Close — Send opens a new
// connection on the next call.
func (t *HTTPTransport) Close() error {
	if c, ok := t.httpClient.Transport.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
	return nil
}

// HTTPStatusError signals a non-2xx HTTP response from the server.
// Callers can branch on Status to decide between retry-on-5xx and
// fail-fast-on-4xx semantics.
type HTTPStatusError struct {
	Status  int
	Body    string
	Headers http.Header
}

// Error implements error.
func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("mcp HTTP %d: %s", e.Status, e.Body)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
