package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// MRTR wire conformance against the published MCP 2026-07-28 schema.
//
// testdata/spec/2026-07-28/schema.json is the official JSON schema, vendored
// unmodified from modelcontextprotocol/modelcontextprotocol at
// schema/2026-07-28/schema.json (commit 271ecc9accafdd9b83a3c869fa67c22953b2af80).
// Spec-following clients such as Claude Code validate input_required results
// against it, so these tests validate mcp-go's output with a full JSON Schema
// 2020-12 validator rather than the library's own subset validator.

const specSchemaURL = "https://modelcontextprotocol.io/schema/2026-07-28/schema.json"

var (
	specCompilerOnce sync.Once
	specCompiler     *jsonschema.Compiler
	specCompilerErr  error
)

// specDef compiles one $defs entry of the vendored 2026-07-28 schema.
func specDef(t *testing.T, def string) *jsonschema.Schema {
	t.Helper()
	specCompilerOnce.Do(func() {
		raw, err := os.ReadFile("testdata/spec/2026-07-28/schema.json")
		if err != nil {
			specCompilerErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			specCompilerErr = err
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(specSchemaURL, doc); err != nil {
			specCompilerErr = err
			return
		}
		specCompiler = c
	})
	if specCompilerErr != nil {
		t.Fatalf("load spec schema: %v", specCompilerErr)
	}
	sch, err := specCompiler.Compile(specSchemaURL + "#/$defs/" + def)
	if err != nil {
		t.Fatalf("compile %s: %v", def, err)
	}
	return sch
}

// validateSpec fails the test unless data (JSON) is a valid instance of def.
func validateSpec(t *testing.T, def string, data []byte) {
	t.Helper()
	if err := specValid(t, def, data); err != nil {
		t.Fatalf("%s does not match the 2026-07-28 schema: %v\n%s", def, err, data)
	}
}

func specValid(t *testing.T, def string, data []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode instance: %v", err)
	}
	return specDef(t, def).Validate(inst)
}

// assertJSONEq compares two JSON documents structurally.
func assertJSONEq(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("decode got: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

// allInputsServer exposes "gather", a tool that asks for an elicitation, a
// sampling completion, and the workspace roots in one round. Each input call
// that has no response yet is recorded; the handler keeps going so all three
// land in a single input_required result, then reports ErrInputRequired.
func allInputsServer() *Server {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("gather").Handler(func(ctx context.Context, _ struct{}) (string, error) {
		sess := server.SessionFromContext(ctx)
		var pending bool
		note := func(err error) error {
			if errors.Is(err, server.ErrInputRequired) {
				pending = true
				return nil
			}
			return err
		}
		el, err := server.ElicitFromContext(ctx).Elicit(ctx, &server.ElicitRequest{
			Message: "Please provide your GitHub username",
			RequestedSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name"},
			},
		})
		if err := note(err); err != nil {
			return "", err
		}
		//nolint:staticcheck // the deprecated sampling path is what MRTR intercepts.
		sm, err := sess.CreateMessageWithTools(ctx, &server.CreateMessageRequest{
			Messages: []server.SamplingMessage{{
				Role:    server.RoleUser,
				Content: server.NewTextContent("What is the capital of France?"),
			}},
			MaxTokens: 100,
		}, nil, nil)
		if err := note(err); err != nil {
			return "", err
		}
		//nolint:staticcheck // the deprecated ListRoots path is what MRTR intercepts for roots.
		roots, err := sess.ListRoots(ctx)
		if err := note(err); err != nil {
			return "", err
		}
		if pending {
			return "", server.ErrInputRequired
		}
		return el.Content["name"].(string) + "|" + sm.Content.Text + "|" + roots.Roots[0].URI, nil
	})
	return srv
}

var allInputCaps = map[string]any{
	"elicitation": map[string]any{},
	"sampling":    map[string]any{},
	"roots":       map[string]any{},
}

// gatherParams builds a modern tools/call for "gather" with the given retry
// fields set directly in params, where InputResponseRequestParams puts them.
func gatherParams(t *testing.T, retry map[string]any) json.RawMessage {
	t.Helper()
	p := map[string]any{
		"name":      "gather",
		"arguments": map[string]any{},
		"_meta": map[string]any{
			protocol.MetaKeyProtocolVersion:    protocol.ModernVersion,
			protocol.MetaKeyClientInfo:         map[string]any{"name": "c", "version": "1"},
			protocol.MetaKeyClientCapabilities: allInputCaps,
		},
	}
	for k, v := range retry {
		p[k] = v
	}
	return mustParams(t, p)
}

func callGather(t *testing.T, h *requestHandler, id string, params json.RawMessage) []byte {
	t.Helper()
	resp, err := h.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(id), Method: protocol.MethodToolsCall, Params: params,
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return raw
}

// goldenInputRequired is the exact first-round result for "gather": the
// InputRequests map keyed by mcp-go's ir-N ids, each value a plain
// {"method", "params"} request object with no jsonrpc or id.
const goldenInputRequired = `{
  "resultType": "input_required",
  "inputRequests": {
    "ir-0": {
      "method": "elicitation/create",
      "params": {
        "message": "Please provide your GitHub username",
        "requestedSchema": {
          "type": "object",
          "properties": {"name": {"type": "string"}},
          "required": ["name"]
        }
      }
    },
    "ir-1": {
      "method": "sampling/createMessage",
      "params": {
        "messages": [
          {"role": "user", "content": {"type": "text", "text": "What is the capital of France?"}}
        ],
        "maxTokens": 100
      }
    },
    "ir-2": {
      "method": "roots/list"
    }
  }
}`

func TestMRTRSpec_InputRequiredGolden(t *testing.T) {
	h := newRequestHandler(allInputsServer())
	got := callGather(t, h, "1", gatherParams(t, nil))

	assertJSONEq(t, got, goldenInputRequired)
	validateSpec(t, "InputRequiredResult", got)

	// Each entry is the specific request type, not merely one of the union.
	var res struct {
		InputRequests map[string]json.RawMessage `json:"inputRequests"`
	}
	if err := json.Unmarshal(got, &res); err != nil {
		t.Fatal(err)
	}
	for id, def := range map[string]string{
		"ir-0": "ElicitRequest",
		"ir-1": "CreateMessageRequest",
		"ir-2": "ListRootsRequest",
	} {
		validateSpec(t, def, res.InputRequests[id])
	}
	// The whole JSON-RPC response is a CallToolResultResponse.
	validateSpec(t, "CallToolResultResponse", mustParams(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "result": json.RawMessage(got),
	}))
}

// TestMRTRSpec_PreviousShapeRejected is the negative control: the array shape
// mcp-go emitted before this fix fails the schema, so the validator above is
// actually checking the shape.
func TestMRTRSpec_PreviousShapeRejected(t *testing.T) {
	old := []byte(`{"resultType":"input_required","inputRequests":[{"id":"ir-0","kind":"elicitation","payload":{"message":"m","requestedSchema":{"type":"object","properties":{}}}}]}`)
	if err := specValid(t, "InputRequiredResult", old); err == nil {
		t.Fatal("the pre-fix array shape must not validate against InputRequiredResult")
	}
}

func TestMRTRSpec_RetryWithSpecInputResponses(t *testing.T) {
	h := newRequestHandler(allInputsServer())
	retry := map[string]any{
		"inputResponses": map[string]any{
			"ir-0": map[string]any{"action": "accept", "content": map[string]any{"name": "octocat"}},
			"ir-1": map[string]any{
				"role": "assistant", "model": "claude-3-sonnet-20240307", "stopReason": "endTurn",
				"content": map[string]any{"type": "text", "text": "Paris"},
			},
			"ir-2": map[string]any{"roots": []any{map[string]any{"uri": "file:///work", "name": "work"}}},
		},
	}
	params := gatherParams(t, retry)
	// The retry this test sends is itself spec-shaped.
	validateSpec(t, "CallToolRequestParams", params)

	got := callGather(t, h, "2", params)
	var res struct {
		ResultType string `json:"resultType"`
		Content    []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(got, &res); err != nil {
		t.Fatal(err)
	}
	if res.ResultType != protocol.ResultTypeComplete || len(res.Content) != 1 ||
		res.Content[0].Text != "octocat|Paris|file:///work" {
		t.Fatalf("retry result = %s, want complete with octocat|Paris|file:///work", got)
	}
	validateSpec(t, "CallToolResult", got)
}

// TestMRTRSpec_PartialRetryEchoesRequestState: a retry that answers only some
// requests gets a new input_required naming the rest, and the requestState it
// carried comes back as the same opaque string.
func TestMRTRSpec_PartialRetryEchoesRequestState(t *testing.T) {
	h := newRequestHandler(allInputsServer())
	const state = "eyJsb2NhdGlvbiI6Ik5ldyBZb3JrIn0"
	got := callGather(t, h, "2", gatherParams(t, map[string]any{
		"inputResponses": map[string]any{
			"ir-0": map[string]any{"action": "accept", "content": map[string]any{"name": "octocat"}},
		},
		"requestState": state,
	}))
	validateSpec(t, "InputRequiredResult", got)
	var res struct {
		InputRequests map[string]json.RawMessage `json:"inputRequests"`
		RequestState  any                        `json:"requestState"`
	}
	if err := json.Unmarshal(got, &res); err != nil {
		t.Fatal(err)
	}
	if res.RequestState != state {
		t.Fatalf("requestState = %#v, want the string %q", res.RequestState, state)
	}
	if _, answered := res.InputRequests["ir-0"]; answered || len(res.InputRequests) != 2 {
		t.Fatalf("inputRequests = %s, want only ir-1 and ir-2 outstanding", got)
	}
}

func TestMRTRSpec_AtLeastOneOfRequestsOrState(t *testing.T) {
	if _, err := json.Marshal(server.InputRequiredResult{ResultType: protocol.ResultTypeInputRequired}); err == nil {
		t.Fatal("an input_required result with neither inputRequests nor requestState must not marshal")
	}
	stateOnly, err := json.Marshal(server.InputRequiredResult{
		ResultType:   protocol.ResultTypeInputRequired,
		RequestState: json.RawMessage(`"eyJwcm9ncmVzcyI6IjUwJSJ9"`),
	})
	if err != nil {
		t.Fatalf("state-only result: %v", err)
	}
	assertJSONEq(t, stateOnly, `{"resultType":"input_required","requestState":"eyJwcm9ncmVzcyI6IjUwJSJ9"}`)
	validateSpec(t, "InputRequiredResult", stateOnly)

	// Non-string state (a value an older caller put in _meta) still goes out as
	// a string, since the spec allows nothing else.
	objState, err := json.Marshal(server.InputRequiredResult{RequestState: json.RawMessage(`{"round": 2}`)})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEq(t, objState, `{"resultType":"input_required","requestState":"{\"round\":2}"}`)
}

func TestMRTRSpec_InputRequiredResultRoundTrips(t *testing.T) {
	var res server.InputRequiredResult
	if err := json.Unmarshal([]byte(goldenInputRequired), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.InputRequests) != 3 || res.InputRequests[0].ID != "ir-0" ||
		res.InputRequests[0].Kind != server.InputKindElicitation ||
		res.InputRequests[2].Kind != server.InputKindRoots {
		t.Fatalf("decoded = %+v", res.InputRequests)
	}
	again, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEq(t, again, goldenInputRequired)
}

func TestMRTRSpec_MalformedRetryFields(t *testing.T) {
	h := newRequestHandler(allInputsServer())
	for name, retry := range map[string]map[string]any{
		"inputResponses not an object": {"inputResponses": "nope"},
		"requestState not a string":    {"requestState": 42},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.HandleRequest(context.Background(), &protocol.Request{
				JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
				Params: gatherParams(t, retry),
			})
			var mcpErr *protocol.Error
			if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
				t.Fatalf("got %v, want -32602 invalid params", err)
			}
		})
	}
}

// TestMRTRSpec_TaskInputRequests: a task paused at input_required lists its
// outstanding requests on tasks/get in the same InputRequests shape (SEP-2663).
func TestMRTRSpec_TaskInputRequests(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	srv.Tool("ask").Description("elicit during task").TaskSupport(TaskSupportRequired).
		Handler(func(ctx context.Context, _ struct{}) (string, error) {
			res, err := server.ElicitFromContext(ctx).Elicit(ctx, &server.ElicitRequest{Message: "Please enter your name."})
			if err != nil {
				return "", err
			}
			return res.Content["input"].(string), nil
		})
	h := newRequestHandler(srv)
	resp, err := h.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodToolsCall,
		Params: modernTaskParams(t, map[string]any{"name": "ask", "arguments": map[string]any{}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := resp.Result.(map[string]any)[fieldTaskID].(string)

	got := waitTaskStatus(t, h, id, "input_required")
	wire, err := json.Marshal(got[fieldInputRequests])
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEq(t, wire, `{"ir-0":{"method":"elicitation/create","params":{
		"message":"Please enter your name.",
		"requestedSchema":{"type":"object","properties":{}}}}}`)
	validateSpec(t, "InputRequests", wire)

	// tasks/update carries spec-shaped inputResponses; the task completes.
	if _, err := h.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodTasksUpdate,
		Params: modernTaskParams(t, map[string]any{
			fieldTaskID:         id,
			fieldInputResponses: map[string]any{"ir-0": map[string]any{"action": "accept", "content": map[string]any{"input": "Luca"}}},
		}),
	}); err != nil {
		t.Fatalf("tasks/update: %v", err)
	}
	done := waitTaskStatus(t, h, id, "completed")
	if raw, _ := json.Marshal(done[fieldResult]); !bytes.Contains(raw, []byte("Luca")) {
		t.Fatalf("completed result = %s, want Luca", raw)
	}
}
