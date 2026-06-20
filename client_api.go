package mcp

import (
	"context"

	"go.klarlabs.de/mcp/client"
)

// Client is an MCP client. It speaks the typed call API to a server over a
// transport (HTTP/SSE or stdio). Construct one with NewClient or
// NewStdioClient.
type Client = client.Client

// ClientOption configures the HTTP transport used by NewClient. The primary
// option is WithHTTPClient, which is the only hook for transport-level auth:
// mcp-go never handles tokens or credentials — inject them via the
// http.Client transport.
type ClientOption = client.HTTPTransportOption

// ToolInfo describes a tool exposed by a server, as returned by
// (*Client).ListTools. It is metadata only; invoke tools with the typed Call
// or NewClientTool APIs.
type ToolInfo = client.ToolInfo

// Tool is the dynamic, untyped escape-hatch interface for invoking a tool with
// raw JSON. NOT RECOMMENDED: prefer the typed Call and NewClientTool APIs,
// which use the Go type system to marshal input and unmarshal output with
// compile-time guarantees. Tool exists only for callers whose input/output
// shapes are not known at compile time.
type Tool = client.Tool

// Client-side option re-exports.
var (
	// WithHTTPClient injects the http.Client used for requests. This is the
	// only auth hook: set your auth transport (API key, bearer, mTLS) on the
	// supplied client. mcp-go never handles credentials itself.
	WithHTTPClient = client.WithHTTPClient
	// WithHTTPHeader attaches a static header to every outbound request.
	WithHTTPHeader = client.WithHTTPHeader
	// WithEndpointPath overrides the request path (default "/mcp").
	WithEndpointPath = client.WithEndpointPath
	// WithRequestTimeout sets the per-request timeout on the HTTP transport.
	WithRequestTimeout = client.WithRequestTimeout
)

// NewClient constructs a client that connects to an MCP server over HTTP/SSE
// at the given base URL. Auth and transport customisation are injected via the
// caller-supplied http.Client (see WithHTTPClient) — mcp-go never handles
// tokens or credentials.
//
//	c, err := mcp.NewClient("https://scout.local",
//	    mcp.WithHTTPClient(&http.Client{Transport: myAuthTransport}),
//	)
func NewClient(url string, opts ...ClientOption) (*Client, error) {
	tr, err := client.NewHTTPTransport(url, opts...)
	if err != nil {
		return nil, err
	}
	return client.New(tr), nil
}

// NewStdioClient constructs a client that connects to a CLI-based MCP server
// by launching the given command and speaking JSON-RPC over its stdin/stdout.
// The command name and arguments are validated to reject shell metacharacters.
func NewStdioClient(command string, args ...string) (*Client, error) {
	tr, err := client.NewStdioTransport(command, args...)
	if err != nil {
		return nil, err
	}
	return client.New(tr), nil
}

// Call invokes a tool on the server with typed input and decodes the typed
// output. This is the primary, recommended client API: define Go types for the
// tool's input and output, and the client marshals/unmarshals for you.
//
//	out, err := mcp.Call[SearchInput, SearchOutput](ctx, c, "search",
//	    SearchInput{Query: "hello", Limit: 10})
func Call[In, Out any](ctx context.Context, c *Client, name string, in In) (Out, error) {
	return client.Call[In, Out](ctx, c, name, in)
}

// NewClientTool returns a reusable, typed handle bound to a single tool. Define
// it once and call it many times. This is part of the primary, recommended
// typed API.
//
//	search := mcp.NewClientTool[SearchInput, SearchOutput](c, "search")
//	out, err := search.Call(ctx, SearchInput{Query: "hello"})
func NewClientTool[In, Out any](c *Client, name string) *client.TypedTool[In, Out] {
	return client.NewClientTool[In, Out](c, name)
}

// NewDynamicTool returns a Tool (the raw-JSON escape hatch) for the named tool.
//
// NOT RECOMMENDED. Prefer Call or NewClientTool, which give you typed,
// compile-time-checked input and output.
func NewDynamicTool(c *Client, name string) Tool {
	return client.NewDynamicTool(c, name)
}
