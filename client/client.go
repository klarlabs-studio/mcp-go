// Package client provides an MCP client for connecting to MCP servers.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// Sentinel errors for client operations.
var (
	// ErrInvalidResult indicates the server returned an unexpected result type.
	ErrInvalidResult = errors.New("invalid result type")
	// ErrNoContent indicates the server returned no content for a resource.
	ErrNoContent = errors.New("no content")
	// ErrToolError indicates the server reported a tool execution failure via
	// the MCP isError flag. The wrapping error carries the tool's error text.
	ErrToolError = errors.New("tool reported an error")
	// ErrNoToolContent indicates a typed tool call produced neither
	// structuredContent nor a decodable text content block. It is the
	// tool-domain counterpart to ErrNoContent (which covers resources), so
	// callers can distinguish the two no-content cases.
	ErrNoToolContent = errors.New("no decodable tool content")
)

// JSON field names used in MCP handshake payloads.
const (
	fieldName            = "name"
	fieldVersion         = "version"
	fieldProtocolVersion = "protocolVersion"
)

// Transport defines the interface for client-side transport.
type Transport interface {
	// Send sends a request and waits for a response.
	Send(ctx context.Context, req *protocol.Request) (*protocol.Response, error)
	// Close closes the transport connection.
	Close() error
}

// Client is an MCP client that communicates with an MCP server.
type Client struct {
	transport Transport
	opts      clientOptions

	mu                      sync.RWMutex
	serverInfo              *ServerInfo
	resourceUpdatedHandlers []func(uri string)
	requestID               atomic.Int64
}

// Icon represents an icon for UI display.
type Icon struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Size     int    `json:"size,omitempty"`
}

// BuildInfo contains build metadata for debugging and version verification.
type BuildInfo struct {
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"buildDate,omitempty"`
}

// ServerInfo contains information about the connected server.
type ServerInfo struct {
	Name            string
	Version         string
	Title           string
	Description     string
	WebsiteURL      string
	Icons           []Icon
	BuildInfo       *BuildInfo
	ProtocolVersion string
	Capabilities    Capabilities
}

// Capabilities describes what features the server supports.
type Capabilities struct {
	Tools     bool
	Resources bool
	Prompts   bool
}

// ToolInfo describes a tool exposed by the server, as returned by ListTools.
// It is metadata only; to invoke a tool use the typed Call / NewTypedTool
// APIs (or, for dynamic use, the Tool interface escape hatch).
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ToolResult is the result of calling a tool.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
	// StructuredContent is the canonical typed channel a server emits for
	// tools that declare an output schema. When present it holds the typed
	// result as raw JSON; typed decoders should prefer it over the display
	// text content. It is nil when the server emits no structuredContent.
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

// ContentItem represents a content item in a tool result.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"`
}

// Resource represents a resource exposed by the server.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent is the content of a resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Prompt represents a prompt exposed by the server.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes an argument for a prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// PromptResult is the result of getting a prompt.
type PromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// PromptMessage is a message in a prompt result.
type PromptMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// Option configures a Client.
type Option func(*clientOptions)

type clientOptions struct {
	timeout     time.Duration
	clientName  string
	clientVer   string
	protocolVer string
}

// WithTimeout sets the default timeout for requests.
func WithTimeout(d time.Duration) Option {
	return func(o *clientOptions) {
		o.timeout = d
	}
}

// WithClientInfo sets the client name and version for initialization.
func WithClientInfo(name, version string) Option {
	return func(o *clientOptions) {
		o.clientName = name
		o.clientVer = version
	}
}

// WithProtocolVersion sets the protocol version to use.
func WithProtocolVersion(version string) Option {
	return func(o *clientOptions) {
		o.protocolVer = version
	}
}

// New creates a new MCP client with the given transport.
func New(transport Transport, opts ...Option) *Client {
	options := clientOptions{
		timeout:     30 * time.Second,
		clientName:  "mcp-go-client",
		clientVer:   "1.0.0",
		protocolVer: "2024-11-05",
	}

	for _, opt := range opts {
		opt(&options)
	}

	return &Client{
		transport: transport,
		opts:      options,
	}
}

// parseIcons parses an array of icon data into Icon structs.
func parseIcons(icons []any) []Icon {
	result := make([]Icon, 0, len(icons))
	for _, item := range icons {
		if m, ok := item.(map[string]any); ok {
			icon := Icon{}
			if uri, ok := m["uri"].(string); ok {
				icon.URI = uri
			}
			if mime, ok := m["mimeType"].(string); ok {
				icon.MimeType = mime
			}
			if size, ok := m["size"].(float64); ok {
				icon.Size = int(size)
			}
			result = append(result, icon)
		}
	}
	return result
}

// parseBuildInfo parses build info data into a BuildInfo struct.
func parseBuildInfo(bi map[string]any) *BuildInfo {
	info := &BuildInfo{}
	if commit, ok := bi["commit"].(string); ok {
		info.Commit = commit
	}
	if buildDate, ok := bi["buildDate"].(string); ok {
		info.BuildDate = buildDate
	}
	return info
}

// Initialize performs the MCP handshake with the server.
func (c *Client) Initialize(ctx context.Context) (*ServerInfo, error) {
	params := map[string]any{
		fieldProtocolVersion: c.opts.protocolVer,
		"clientInfo": map[string]any{
			fieldName:    c.opts.clientName,
			fieldVersion: c.opts.clientVer,
		},
		"capabilities": map[string]any{},
	}

	resp, err := c.call(ctx, protocol.MethodInitialize, params)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("initialize: %w", ErrInvalidResult)
	}

	info := &ServerInfo{}

	if pv, ok := result[fieldProtocolVersion].(string); ok {
		info.ProtocolVersion = pv
	}

	if si, ok := result["serverInfo"].(map[string]any); ok {
		if name, ok := si[fieldName].(string); ok {
			info.Name = name
		}
		if ver, ok := si[fieldVersion].(string); ok {
			info.Version = ver
		}
		if title, ok := si["title"].(string); ok {
			info.Title = title
		}
		if desc, ok := si["description"].(string); ok {
			info.Description = desc
		}
		if url, ok := si["websiteUrl"].(string); ok {
			info.WebsiteURL = url
		}
		if icons, ok := si["icons"].([]any); ok {
			info.Icons = parseIcons(icons)
		}
		if bi, ok := si["buildInfo"].(map[string]any); ok {
			info.BuildInfo = parseBuildInfo(bi)
		}
	}

	if caps, ok := result["capabilities"].(map[string]any); ok {
		if _, ok := caps["tools"]; ok {
			info.Capabilities.Tools = true
		}
		if _, ok := caps["resources"]; ok {
			info.Capabilities.Resources = true
		}
		if _, ok := caps["prompts"]; ok {
			info.Capabilities.Prompts = true
		}
	}

	c.mu.Lock()
	c.serverInfo = info
	c.mu.Unlock()

	return info, nil
}

// ListTools returns the list of tools available on the server.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	resp, err := c.call(ctx, protocol.MethodToolsList, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("list tools: %w", ErrInvalidResult)
	}

	toolsRaw, ok := result["tools"].([]any)
	if !ok {
		return nil, fmt.Errorf("list tools: %w", ErrInvalidResult)
	}

	tools := make([]ToolInfo, 0, len(toolsRaw))
	for _, tr := range toolsRaw {
		tm, ok := tr.(map[string]any)
		if !ok {
			continue
		}

		tool := ToolInfo{}
		if name, ok := tm["name"].(string); ok {
			tool.Name = name
		}
		if desc, ok := tm["description"].(string); ok {
			tool.Description = desc
		}
		if schema, ok := tm["inputSchema"]; ok {
			tool.InputSchema = schema
		}
		tools = append(tools, tool)
	}

	return tools, nil
}

// CallTool calls a tool on the server with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, arguments any) (*ToolResult, error) {
	params := map[string]any{
		"name": name,
	}
	if arguments != nil {
		params["arguments"] = arguments
	}

	resp, err := c.call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf("call tool %q: %w", name, err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("call tool %q: %w", name, ErrInvalidResult)
	}

	toolResult := &ToolResult{}

	if isErr, ok := result["isError"].(bool); ok {
		toolResult.IsError = isErr
	}

	// structuredContent is the canonical typed channel. Re-marshal the
	// decoded object so typed callers can unmarshal it into their output
	// type directly, preferring it over the display text content.
	if sc, ok := result["structuredContent"]; ok && sc != nil {
		structured, err := json.Marshal(sc)
		if err != nil {
			return nil, fmt.Errorf("call tool %q: marshal structuredContent: %w", name, err)
		}
		toolResult.StructuredContent = structured
	}

	if content, ok := result["content"].([]any); ok {
		for _, cr := range content {
			cm, ok := cr.(map[string]any)
			if !ok {
				continue
			}

			item := ContentItem{}
			if t, ok := cm["type"].(string); ok {
				item.Type = t
			}
			if text, ok := cm["text"].(string); ok {
				item.Text = text
			}
			if data, ok := cm["data"].(string); ok {
				item.Data = data
			}
			toolResult.Content = append(toolResult.Content, item)
		}
	}

	return toolResult, nil
}

// ListResources returns the list of resources available on the server.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	resp, err := c.call(ctx, protocol.MethodResourcesList, nil)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("list resources: %w", ErrInvalidResult)
	}

	resourcesRaw, ok := result["resources"].([]any)
	if !ok {
		return nil, fmt.Errorf("list resources: %w", ErrInvalidResult)
	}

	resources := make([]Resource, 0, len(resourcesRaw))
	for _, rr := range resourcesRaw {
		rm, ok := rr.(map[string]any)
		if !ok {
			continue
		}

		resource := Resource{}
		if uri, ok := rm["uri"].(string); ok {
			resource.URI = uri
		}
		if name, ok := rm["name"].(string); ok {
			resource.Name = name
		}
		if desc, ok := rm["description"].(string); ok {
			resource.Description = desc
		}
		if mime, ok := rm["mimeType"].(string); ok {
			resource.MimeType = mime
		}
		resources = append(resources, resource)
	}

	return resources, nil
}

// ReadResource reads a resource from the server.
func (c *Client) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	params := map[string]any{
		"uri": uri,
	}

	resp, err := c.call(ctx, protocol.MethodResourcesRead, params)
	if err != nil {
		return nil, fmt.Errorf("read resource %q: %w", uri, err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("read resource %q: %w", uri, ErrInvalidResult)
	}

	contents, ok := result["contents"].([]any)
	if !ok || len(contents) == 0 {
		return nil, fmt.Errorf("read resource %q: %w", uri, ErrNoContent)
	}

	cm, ok := contents[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("read resource %q: %w", uri, ErrInvalidResult)
	}

	content := &ResourceContent{}
	if u, ok := cm["uri"].(string); ok {
		content.URI = u
	}
	if mime, ok := cm["mimeType"].(string); ok {
		content.MimeType = mime
	}
	if text, ok := cm["text"].(string); ok {
		content.Text = text
	}
	if blob, ok := cm["blob"].(string); ok {
		content.Blob = blob
	}

	return content, nil
}

// ListPrompts returns the list of prompts available on the server.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	resp, err := c.call(ctx, protocol.MethodPromptsList, nil)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("list prompts: %w", ErrInvalidResult)
	}

	promptsRaw, ok := result["prompts"].([]any)
	if !ok {
		return nil, fmt.Errorf("list prompts: %w", ErrInvalidResult)
	}

	prompts := make([]Prompt, 0, len(promptsRaw))
	for _, pr := range promptsRaw {
		pm, ok := pr.(map[string]any)
		if !ok {
			continue
		}

		prompt := Prompt{}
		if name, ok := pm["name"].(string); ok {
			prompt.Name = name
		}
		if desc, ok := pm["description"].(string); ok {
			prompt.Description = desc
		}
		if args, ok := pm["arguments"].([]any); ok {
			for _, ar := range args {
				am, ok := ar.(map[string]any)
				if !ok {
					continue
				}
				arg := PromptArgument{}
				if name, ok := am["name"].(string); ok {
					arg.Name = name
				}
				if desc, ok := am["description"].(string); ok {
					arg.Description = desc
				}
				if req, ok := am["required"].(bool); ok {
					arg.Required = req
				}
				prompt.Arguments = append(prompt.Arguments, arg)
			}
		}
		prompts = append(prompts, prompt)
	}

	return prompts, nil
}

// GetPrompt gets a prompt with the given arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*PromptResult, error) {
	params := map[string]any{
		"name": name,
	}
	if arguments != nil {
		params["arguments"] = arguments
	}

	resp, err := c.call(ctx, protocol.MethodPromptsGet, params)
	if err != nil {
		return nil, fmt.Errorf("get prompt %q: %w", name, err)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("get prompt %q: %w", name, ErrInvalidResult)
	}

	promptResult := &PromptResult{}

	if desc, ok := result["description"].(string); ok {
		promptResult.Description = desc
	}

	if messages, ok := result["messages"].([]any); ok {
		for _, mr := range messages {
			mm, ok := mr.(map[string]any)
			if !ok {
				continue
			}
			msg := PromptMessage{}
			if role, ok := mm["role"].(string); ok {
				msg.Role = role
			}
			if content, ok := mm["content"]; ok {
				msg.Content = content
			}
			promptResult.Messages = append(promptResult.Messages, msg)
		}
	}

	return promptResult, nil
}

// Ping sends a ping to the server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, protocol.MethodPing, nil)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// ServerInfo returns the cached server info from initialization.
func (c *Client) ServerInfo() *ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.transport.Close()
}

// call makes a JSON-RPC call to the server.
func (c *Client) call(ctx context.Context, method string, params any) (*protocol.Response, error) {
	id := c.requestID.Add(1)

	var paramsRaw json.RawMessage
	if params != nil {
		var err error
		paramsRaw, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	idRaw, err := json.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("marshal request ID: %w", err)
	}
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  paramsRaw,
	}

	if c.opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp, nil
}
