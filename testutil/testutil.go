// Package testutil provides testing utilities for MCP servers.
//
// This package helps developers write tests for their MCP servers by providing
// mock transports, test clients, and assertion helpers.
//
// Example usage:
//
//	func TestMyServer(t *testing.T) {
//	    srv := mcp.NewServer(mcp.ServerInfo{Name: "test", Version: "1.0.0"})
//	    srv.Tool("greet").Handler(func(ctx context.Context, input GreetInput) (string, error) {
//	        return "Hello, " + input.Name, nil
//	    })
//
//	    tc := testutil.NewTestClient(t, srv)
//	    defer tc.Close()
//
//	    result, err := tc.CallTool("greet", map[string]any{"name": "World"})
//	    require.NoError(t, err)
//	    assert.Equal(t, "Hello, World", result)
//	}
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/felixgeelhaar/mcp-go/protocol"
	"github.com/felixgeelhaar/mcp-go/server"
	"github.com/felixgeelhaar/mcp-go/transport"
)

// JSON field names used in testutil request/response payloads.
const (
	fieldName      = "name"
	fieldArguments = "arguments"
	fieldURI       = "uri"
	fieldText      = "text"
)

// TestClient is a test client for MCP servers.
type TestClient struct {
	t       testing.TB
	srv     *server.Server
	handler transport.Handler
	reqID   int64
	mu      sync.Mutex
}

// NewTestClient creates a new test client for the given server.
func NewTestClient(t testing.TB, srv *server.Server) *TestClient {
	t.Helper()

	handler := &requestHandler{srv: srv}
	tc := &TestClient{
		t:       t,
		srv:     srv,
		handler: handler,
	}

	// Initialize the server
	_, err := tc.Initialize()
	if err != nil {
		t.Fatalf("failed to initialize server: %v", err)
	}

	return tc
}

// NewTestClientWithHandler creates a test client with a custom handler.
// This is useful for testing middleware.
func NewTestClientWithHandler(t testing.TB, handler transport.Handler) *TestClient {
	t.Helper()
	return &TestClient{
		t:       t,
		handler: handler,
	}
}

// Close closes the test client (no-op for now, but good for future cleanup).
func (tc *TestClient) Close() {
	// No cleanup needed for in-memory client
}

// nextID returns the next request ID.
func (tc *TestClient) nextID() json.RawMessage {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.reqID++
	return json.RawMessage(fmt.Sprintf("%d", tc.reqID))
}

// SendRequest sends a raw request and returns the response.
func (tc *TestClient) SendRequest(method string, params any) (*protocol.Response, error) {
	tc.t.Helper()

	var paramsData json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		paramsData = data
	}

	req := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      tc.nextID(),
		Method:  method,
		Params:  paramsData,
	}

	resp, err := tc.handler.HandleRequest(context.Background(), req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Initialize sends an initialize request to the server.
func (tc *TestClient) Initialize() (map[string]any, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodInitialize, map[string]any{
		"protocolVersion": protocol.MCPVersion,
		"clientInfo": map[string]any{
			fieldName: "test-client",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	return result, nil
}

// ListTools lists all available tools.
func (tc *TestClient) ListTools() ([]map[string]any, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodToolsList, nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	// Handle both []any (from JSON) and []map[string]any (from direct call)
	var toolMaps []map[string]any
	switch v := result["tools"].(type) {
	case []any:
		toolMaps = make([]map[string]any, len(v))
		for i, t := range v {
			toolMaps[i], _ = t.(map[string]any)
		}
	case []map[string]any:
		toolMaps = v
	default:
		return nil, fmt.Errorf("unexpected tools type: %T", result["tools"])
	}

	return toolMaps, nil
}

// CallTool calls a tool with the given arguments and returns the text result.
func (tc *TestClient) CallTool(name string, args any) (string, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodToolsCall, map[string]any{
		fieldName:      name,
		fieldArguments: args,
	})
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	// Handle both []any (from JSON) and []map[string]any (from direct call)
	var first map[string]any
	switch v := result["content"].(type) {
	case []any:
		if len(v) == 0 {
			return "", fmt.Errorf("empty content array")
		}
		first, _ = v[0].(map[string]any)
	case []map[string]any:
		if len(v) == 0 {
			return "", fmt.Errorf("empty content array")
		}
		first = v[0]
	default:
		return "", fmt.Errorf("unexpected content type: %T", result["content"])
	}

	if first == nil {
		return "", fmt.Errorf("nil content item")
	}

	text, _ := first[fieldText].(string)
	return text, nil
}

// CallToolRaw calls a tool and returns the raw response.
func (tc *TestClient) CallToolRaw(name string, args any) (*protocol.Response, error) {
	tc.t.Helper()

	return tc.SendRequest(protocol.MethodToolsCall, map[string]any{
		fieldName:      name,
		fieldArguments: args,
	})
}

// ListResources lists all available resources.
func (tc *TestClient) ListResources() ([]map[string]any, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodResourcesList, nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	// Handle both []any (from JSON) and []map[string]any (from direct call)
	var resourceMaps []map[string]any
	switch v := result["resources"].(type) {
	case []any:
		resourceMaps = make([]map[string]any, len(v))
		for i, r := range v {
			resourceMaps[i], _ = r.(map[string]any)
		}
	case []map[string]any:
		resourceMaps = v
	default:
		return nil, fmt.Errorf("unexpected resources type: %T", result["resources"])
	}

	return resourceMaps, nil
}

// ReadResource reads a resource by URI.
func (tc *TestClient) ReadResource(uri string) (string, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodResourcesRead, map[string]any{
		fieldURI: uri,
	})
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	// Handle both []any (from JSON) and []map[string]any (from direct call)
	var first map[string]any
	switch v := result["contents"].(type) {
	case []any:
		if len(v) == 0 {
			return "", fmt.Errorf("empty contents array")
		}
		first, _ = v[0].(map[string]any)
	case []map[string]any:
		if len(v) == 0 {
			return "", fmt.Errorf("empty contents array")
		}
		first = v[0]
	default:
		return "", fmt.Errorf("unexpected contents type: %T", result["contents"])
	}

	if first == nil {
		return "", fmt.Errorf("nil contents item")
	}

	text, _ := first[fieldText].(string)
	return text, nil
}

// ListPrompts lists all available prompts.
func (tc *TestClient) ListPrompts() ([]map[string]any, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodPromptsList, nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	// Handle both []any (from JSON) and []map[string]any (from direct call)
	var promptMaps []map[string]any
	switch v := result["prompts"].(type) {
	case []any:
		promptMaps = make([]map[string]any, len(v))
		for i, p := range v {
			promptMaps[i], _ = p.(map[string]any)
		}
	case []map[string]any:
		promptMaps = v
	default:
		return nil, fmt.Errorf("unexpected prompts type: %T", result["prompts"])
	}

	return promptMaps, nil
}

// GetPrompt gets a prompt by name with the given arguments.
func (tc *TestClient) GetPrompt(name string, args map[string]string) (map[string]any, error) {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodPromptsGet, map[string]any{
		fieldName:      name,
		fieldArguments: args,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", resp.Result)
	}

	return result, nil
}

// Ping sends a ping request.
func (tc *TestClient) Ping() error {
	tc.t.Helper()

	resp, err := tc.SendRequest(protocol.MethodPing, nil)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}

	return nil
}

// requestHandler adapts Server to transport.Handler for in-memory testing.
type requestHandler struct {
	srv *server.Server
}

func (h *requestHandler) HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	switch req.Method {
	case protocol.MethodInitialize:
		return h.handleInitialize(req)
	case protocol.MethodToolsList:
		return h.handleToolsList(req)
	case protocol.MethodToolsCall:
		return h.handleToolsCall(ctx, req)
	case protocol.MethodResourcesList:
		return h.handleResourcesList(req)
	case protocol.MethodResourcesRead:
		return h.handleResourcesRead(ctx, req)
	case protocol.MethodPromptsList:
		return h.handlePromptsList(req)
	case protocol.MethodPromptsGet:
		return h.handlePromptsGet(ctx, req)
	case protocol.MethodPing:
		return protocol.NewResponse(req.ID, map[string]any{}), nil
	default:
		return nil, protocol.NewMethodNotFound(req.Method)
	}
}

func (h *requestHandler) handleInitialize(req *protocol.Request) (*protocol.Response, error) {
	manifest := h.srv.Manifest()

	capabilities := make(map[string]any)
	if manifest.Capabilities.Tools {
		capabilities["tools"] = map[string]any{}
	}
	if manifest.Capabilities.Resources {
		capabilities["resources"] = map[string]any{}
	}
	if manifest.Capabilities.Prompts {
		capabilities["prompts"] = map[string]any{}
	}

	result := map[string]any{
		"protocolVersion": manifest.ProtocolVersion,
		"serverInfo": map[string]any{
			fieldName: manifest.Name,
			"version": manifest.Version,
		},
		"capabilities": capabilities,
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handleToolsList(req *protocol.Request) (*protocol.Response, error) {
	tools := h.srv.Tools()

	toolList := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		toolList = append(toolList, map[string]any{
			fieldName:     t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}

	return protocol.NewResponse(req.ID, map[string]any{"tools": toolList}), nil
}

func (h *requestHandler) handleToolsCall(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	tool, ok := h.srv.GetTool(params.Name)
	if !ok {
		return nil, protocol.NewNotFound("tool not found: " + params.Name)
	}

	result, err := tool.Execute(ctx, params.Arguments)
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"content": []map[string]any{
			{"type": fieldText, fieldText: result},
		},
	}

	return protocol.NewResponse(req.ID, response), nil
}

func (h *requestHandler) handleResourcesList(req *protocol.Request) (*protocol.Response, error) {
	resources := h.srv.Resources()

	resourceList := make([]map[string]any, 0, len(resources))
	for _, r := range resources {
		item := map[string]any{
			fieldURI:  r.URITemplate,
			fieldName: r.Name,
		}
		if r.Description != "" {
			item["description"] = r.Description
		}
		if r.MimeType != "" {
			item["mimeType"] = r.MimeType
		}
		resourceList = append(resourceList, item)
	}

	return protocol.NewResponse(req.ID, map[string]any{"resources": resourceList}), nil
}

func (h *requestHandler) handleResourcesRead(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	resource, ok := h.srv.FindResourceForURI(params.URI)
	if !ok {
		return nil, protocol.NewNotFound("resource not found: " + params.URI)
	}

	content, err := resource.Read(ctx, params.URI)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"contents": []map[string]any{
			{
				"uri":      content.URI,
				"mimeType": content.MimeType,
				"text":     content.Text,
			},
		},
	}

	return protocol.NewResponse(req.ID, result), nil
}

func (h *requestHandler) handlePromptsList(req *protocol.Request) (*protocol.Response, error) {
	prompts := h.srv.Prompts()

	promptList := make([]map[string]any, 0, len(prompts))
	for _, p := range prompts {
		item := map[string]any{
			fieldName: p.Name,
		}
		if p.Description != "" {
			item["description"] = p.Description
		}
		if len(p.Arguments) > 0 {
			args := make([]map[string]any, 0, len(p.Arguments))
			for _, arg := range p.Arguments {
				argItem := map[string]any{
					fieldName:  arg.Name,
					"required": arg.Required,
				}
				if arg.Description != "" {
					argItem["description"] = arg.Description
				}
				args = append(args, argItem)
			}
			item[fieldArguments] = args
		}
		promptList = append(promptList, item)
	}

	return protocol.NewResponse(req.ID, map[string]any{"prompts": promptList}), nil
}

func (h *requestHandler) handlePromptsGet(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	prompt, ok := h.srv.GetPrompt(params.Name)
	if !ok {
		return nil, protocol.NewNotFound("prompt not found: " + params.Name)
	}

	result, err := prompt.Get(ctx, params.Arguments)
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"messages": result.Messages,
	}
	if result.Description != "" {
		response["description"] = result.Description
	}

	return protocol.NewResponse(req.ID, response), nil
}

// MockTransport is a mock transport for testing.
type MockTransport struct {
	in  *bytes.Buffer
	out *bytes.Buffer
	mu  sync.Mutex
}

// NewMockTransport creates a new mock transport.
func NewMockTransport() *MockTransport {
	return &MockTransport{
		in:  &bytes.Buffer{},
		out: &bytes.Buffer{},
	}
}

// Write writes a request to the mock transport.
func (m *MockTransport) Write(req *protocol.Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	_, err = m.in.Write(data)
	if err != nil {
		return err
	}
	_, err = m.in.WriteString("\n")
	return err
}

// ReadResponse reads a response from the mock transport.
func (m *MockTransport) ReadResponse() (*protocol.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	line, err := m.out.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(line) == 0 {
		return nil, io.EOF
	}

	var resp protocol.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Input returns the input reader.
func (m *MockTransport) Input() io.Reader {
	return m.in
}

// Output returns the output writer.
func (m *MockTransport) Output() io.Writer {
	return m.out
}

// SendRequest sends a request to the mock transport and returns immediately.
// Use ReadResponse to get the response.
func (m *MockTransport) SendRequest(method string, params any) error {
	var paramsData json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		paramsData = data
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	req := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  paramsData,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	_, err = m.in.Write(data)
	if err != nil {
		return err
	}
	_, err = m.in.WriteString("\n")
	return err
}

// ReadRequest reads a request from the mock transport input.
func (m *MockTransport) ReadRequest() (*protocol.Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	line, err := m.in.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(line) == 0 {
		return nil, io.EOF
	}

	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, err
	}

	return &req, nil
}

// WriteResponse writes a response to the mock transport output.
func (m *MockTransport) WriteResponse(result any, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var resp protocol.Response
	resp.JSONRPC = protocol.JSONRPCVersion
	resp.ID = json.RawMessage(`1`)

	if err != nil {
		resp.Error = &protocol.Error{
			Code:    protocol.CodeInternalError,
			Message: err.Error(),
		}
	} else {
		resp.Result = result
	}

	data, marshalErr := json.Marshal(&resp)
	if marshalErr != nil {
		return marshalErr
	}

	_, writeErr := m.out.Write(data)
	if writeErr != nil {
		return writeErr
	}
	_, writeErr = m.out.WriteString("\n")
	return writeErr
}

// recorded tracks sent requests for assertions
type recorded struct {
	requests []*protocol.Request
	mu       sync.Mutex
}

// MockTransportRecorder wraps a MockTransport and records all requests.
type MockTransportRecorder struct {
	*MockTransport
	recorded recorded
}

// NewMockTransportRecorder creates a new mock transport that records requests.
func NewMockTransportRecorder() *MockTransportRecorder {
	return &MockTransportRecorder{
		MockTransport: NewMockTransport(),
	}
}

// SendRequest sends a request and records it.
func (m *MockTransportRecorder) SendRequest(method string, params any) error {
	err := m.MockTransport.SendRequest(method, params)
	if err != nil {
		return err
	}

	// Record the request
	var paramsData json.RawMessage
	if params != nil {
		data, _ := json.Marshal(params)
		paramsData = data
	}

	m.recorded.mu.Lock()
	m.recorded.requests = append(m.recorded.requests, &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  paramsData,
	})
	m.recorded.mu.Unlock()

	return nil
}

// RecordedRequests returns all recorded requests.
func (m *MockTransportRecorder) RecordedRequests() []*protocol.Request {
	m.recorded.mu.Lock()
	defer m.recorded.mu.Unlock()
	result := make([]*protocol.Request, len(m.recorded.requests))
	copy(result, m.recorded.requests)
	return result
}

// Reset clears the mock transport state.
func (m *MockTransportRecorder) Reset() {
	m.mu.Lock()
	m.in.Reset()
	m.out.Reset()
	m.mu.Unlock()

	m.recorded.mu.Lock()
	m.recorded.requests = nil
	m.recorded.mu.Unlock()
}

// AssertToolExists asserts that a tool with the given name exists.
func (tc *TestClient) AssertToolExists(name string) {
	tc.t.Helper()

	tools, err := tc.ListTools()
	if err != nil {
		tc.t.Fatalf("ListTools failed: %v", err)
	}

	for _, tool := range tools {
		if tool["name"] == name {
			return
		}
	}
	tc.t.Errorf("tool %q not found", name)
}

// AssertResourceExists asserts that a resource matching the given URI pattern exists.
func (tc *TestClient) AssertResourceExists(uriPattern string) {
	tc.t.Helper()

	resources, err := tc.ListResources()
	if err != nil {
		tc.t.Fatalf("ListResources failed: %v", err)
	}

	for _, res := range resources {
		if res["uri"] == uriPattern {
			return
		}
	}
	tc.t.Errorf("resource %q not found", uriPattern)
}

// AssertPromptExists asserts that a prompt with the given name exists.
func (tc *TestClient) AssertPromptExists(name string) {
	tc.t.Helper()

	prompts, err := tc.ListPrompts()
	if err != nil {
		tc.t.Fatalf("ListPrompts failed: %v", err)
	}

	for _, prompt := range prompts {
		if prompt["name"] == name {
			return
		}
	}
	tc.t.Errorf("prompt %q not found", name)
}
