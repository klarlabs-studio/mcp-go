package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"go.klarlabs.de/mcp/protocol"
)

// Multi Round-Trip Requests (MRTR, MCP 2026-07-28) replace every
// server-initiated request — sampling, elicitation, roots/list — in the
// stateless model. A stateless server holds no connection to call back over, so
// instead of blocking on a server→client request it returns a result with
// resultType "input_required" listing the inputs it needs. The client fulfills
// them locally and retries the original call carrying the responses; the handler
// re-runs and its input calls resolve from those responses instead of blocking.
//
// The handler is replayed on every round. Its input calls must therefore be
// deterministic up to the point where new input is needed: each call is assigned
// a stable id (ir-0, ir-1, …) by call order, so a response supplied on one round
// is matched back to the same call on the next. This is a replay/continuation
// model — analogous to algebraic effects simulated by re-execution.

const (
	// jsonNull is the JSON null literal.
	jsonNull = "null"
	// schemaTypeObject is the JSON Schema object type.
	schemaTypeObject = "object"
)

// Input request kinds carried in an InputRequest.Kind (MCP 2026-07-28, MRTR).
// On the wire each kind is the method of the request object it stands for; see
// InputKindMethod.
const (
	InputKindSampling    = "sampling"    // an LLM completion (sampling/createMessage)
	InputKindElicitation = "elicitation" // structured user input (elicitation/create)
	InputKindRoots       = "roots"       // workspace roots (roots/list)
)

// InputKindMethod maps an input request kind to the JSON-RPC method of the
// request object that carries it on the wire. An unknown kind is returned
// unchanged, so a caller that already stores a method name passes through.
func InputKindMethod(kind string) string {
	switch kind {
	case InputKindSampling:
		return protocol.MethodSamplingCreateMessage
	case InputKindElicitation:
		return protocol.MethodElicitationCreate
	case InputKindRoots:
		return protocol.MethodRootsList
	default:
		return kind
	}
}

// inputMethodKind is the inverse of InputKindMethod.
func inputMethodKind(method string) string {
	switch method {
	case protocol.MethodSamplingCreateMessage:
		return InputKindSampling
	case protocol.MethodElicitationCreate:
		return InputKindElicitation
	case protocol.MethodRootsList:
		return InputKindRoots
	default:
		return method
	}
}

// InputRequest describes one piece of input a stateless tool handler needs
// before it can produce a final result. It is carried in an InputRequiredResult;
// the client fulfills it and retries the original call with a matching
// InputResponse (correlated by ID).
//
// On the wire (MCP 2026-07-28 InputRequest) the entry is a plain request
// object, {"method": …, "params": …}, stored under its ID as the key of the
// inputRequests map; MarshalJSON writes that object and UnmarshalJSON reads it.
// The ID is the map key and therefore not part of the object.
type InputRequest struct {
	ID   string
	Kind string
	// Payload is the request parameters for the kind: CreateMessageRequestParams
	// for sampling, ElicitRequestParams for elicitation. Empty for roots (no
	// params). Sent as the request object's params.
	Payload json.RawMessage
}

// MarshalJSON encodes the request as the spec's request object:
// {"method": "<method>", "params": {…}}.
func (r InputRequest) MarshalJSON() ([]byte, error) {
	wire := struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}{Method: InputKindMethod(r.Kind)}
	if p := bytes.TrimSpace(r.Payload); len(p) > 0 && !bytes.Equal(p, []byte(jsonNull)) {
		wire.Params = p
	}
	return json.Marshal(wire)
}

// UnmarshalJSON decodes a spec request object. ID is left empty: it is the key
// the object was stored under in the inputRequests map.
func (r *InputRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Method == "" {
		return errors.New("input request has no method")
	}
	r.Kind = inputMethodKind(wire.Method)
	r.Payload = wire.Params
	return nil
}

// InputResponse carries the client's fulfillment of one InputRequest, correlated
// by ID. On the wire (MCP 2026-07-28) the client sends an inputResponses map
// from each request ID to the bare result object (CreateMessageResult /
// ElicitResult / ListRootsResult); ParseInputResponses turns that map into
// InputResponses. Payload holds the result the handler receives.
//
// Error is not part of the spec. It is filled only from the pre-1.30 array
// shape ([{"id", "payload", "error"}]) that ParseInputResponses still accepts,
// and surfaces a client-side failure to the handler as the input call's error.
type InputResponse struct {
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *protocol.Error `json:"error,omitempty"`
}

// ParseInputResponses decodes the inputResponses a client sent on an MRTR retry
// or a tasks/update. The spec shape is an object keyed by input request ID
// whose values are the bare results. A JSON array of {"id", "payload",
// "error"} entries, the shape mcp-go itself used before it followed the
// published schema, is still accepted on input. Empty or null input yields no
// responses; any other JSON value is an error.
func ParseInputResponses(raw json.RawMessage) ([]InputResponse, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte(jsonNull)) {
		return nil, nil
	}
	switch raw[0] {
	case '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("inputResponses: %w", err)
		}
		out := make([]InputResponse, 0, len(m))
		for id, payload := range m {
			out = append(out, InputResponse{ID: id, Payload: payload})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	case '[':
		var arr []InputResponse
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("inputResponses: %w", err)
		}
		return arr, nil
	default:
		return nil, errors.New("inputResponses must be an object keyed by input request id")
	}
}

// InputRequiredResult is a result with resultType "input_required" (MCP
// 2026-07-28, MRTR): the handler paused because it needs input the client must
// supply before retrying.
//
// It marshals to the spec's InputRequiredResult: inputRequests is an object
// keyed by each request's ID, and requestState is an opaque string. The spec
// requires at least one of the two, so MarshalJSON fails when both are empty
// rather than emit a result a conforming client must reject.
type InputRequiredResult struct {
	ResultType    string
	InputRequests []InputRequest
	// RequestState is the raw JSON of the opaque requestState string the client
	// echoes back on the retry. A JSON string is sent as is; any other JSON
	// value is sent as a string holding its text, since the spec allows only a
	// string here.
	RequestState json.RawMessage
}

// errEmptyInputRequired reports an InputRequiredResult that carries neither
// inputRequests nor requestState.
var errEmptyInputRequired = errors.New("mcp: input_required result needs inputRequests or requestState")

// MarshalJSON encodes the spec wire shape.
func (r InputRequiredResult) MarshalJSON() ([]byte, error) {
	resultType := r.ResultType
	if resultType == "" {
		resultType = protocol.ResultTypeInputRequired
	}
	wire := struct {
		ResultType    string                  `json:"resultType"`
		InputRequests map[string]InputRequest `json:"inputRequests,omitempty"`
		RequestState  string                  `json:"requestState,omitempty"`
	}{ResultType: resultType, RequestState: RequestStateString(r.RequestState)}
	if len(r.InputRequests) > 0 {
		wire.InputRequests = make(map[string]InputRequest, len(r.InputRequests))
		for _, req := range r.InputRequests {
			if req.ID == "" {
				return nil, errors.New("mcp: input request without an id")
			}
			if _, dup := wire.InputRequests[req.ID]; dup {
				return nil, fmt.Errorf("mcp: duplicate input request id %q", req.ID)
			}
			wire.InputRequests[req.ID] = req
		}
	}
	if len(wire.InputRequests) == 0 && wire.RequestState == "" {
		return nil, errEmptyInputRequired
	}
	return json.Marshal(wire)
}

// UnmarshalJSON decodes the spec wire shape. InputRequests are ordered by ID so
// the result is deterministic.
func (r *InputRequiredResult) UnmarshalJSON(data []byte) error {
	var wire struct {
		ResultType    string                  `json:"resultType"`
		InputRequests map[string]InputRequest `json:"inputRequests"`
		RequestState  *string                 `json:"requestState"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = InputRequiredResult{ResultType: wire.ResultType}
	for id, req := range wire.InputRequests {
		req.ID = id
		r.InputRequests = append(r.InputRequests, req)
	}
	sort.Slice(r.InputRequests, func(i, j int) bool { return r.InputRequests[i].ID < r.InputRequests[j].ID })
	if wire.RequestState != nil {
		state, err := json.Marshal(*wire.RequestState)
		if err != nil {
			return err
		}
		r.RequestState = state
	}
	return nil
}

// RequestStateString returns the opaque requestState string held in raw JSON:
// the decoded value of a JSON string, the compact text of any other JSON value,
// and "" for empty or null input.
func RequestStateString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte(jsonNull)) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return buf.String()
	}
	return string(raw)
}

// ErrInputRequired halts a stateless tool handler when it requests input the
// client has not yet supplied. A handler receives it as the error from an input
// call (CreateMessage / Elicit / ListRoots) and should propagate it unchanged;
// the dispatcher converts an unfulfilled input request into an
// InputRequiredResult rather than an error response.
var ErrInputRequired = errors.New("mcp: input required (MRTR); client must fulfill inputRequests and retry")

// InputBroker fulfills a stateless handler's input calls from responses the
// client supplied on retry, and records the requests that remain unfulfilled so
// the dispatcher can return them as an InputRequiredResult. It is single-use per
// request and not safe for concurrent input calls (handlers are sequential).
type InputBroker struct {
	byID    map[string]InputResponse
	state   json.RawMessage
	counter int
	pending []InputRequest
}

// NewInputBroker builds a broker seeded with the input responses the client
// supplied on a retry (empty on the first round) and the request state to echo
// back on the next InputRequiredResult (raw JSON of the opaque string; see
// InputRequiredResult.RequestState).
func NewInputBroker(responses []InputResponse, state json.RawMessage) *InputBroker {
	byID := make(map[string]InputResponse, len(responses))
	for _, r := range responses {
		byID[r.ID] = r
	}
	return &InputBroker{byID: byID, state: state}
}

// request resolves one input call. It assigns the call a stable id by order,
// returns the previously-supplied response payload when present, and otherwise
// records the request as pending and returns ErrInputRequired.
func (b *InputBroker) request(kind string, payload json.RawMessage) (json.RawMessage, error) {
	id := fmt.Sprintf("ir-%d", b.counter)
	b.counter++
	if r, ok := b.byID[id]; ok {
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Payload, nil
	}
	b.pending = append(b.pending, InputRequest{ID: id, Kind: kind, Payload: payload})
	return nil, ErrInputRequired
}

// HasPending reports whether the handler left any input request unfulfilled.
func (b *InputBroker) HasPending() bool { return len(b.pending) > 0 }

// Pending returns a copy of the unfulfilled input requests recorded this round.
func (b *InputBroker) Pending() []InputRequest {
	if b == nil || len(b.pending) == 0 {
		return nil
	}
	out := make([]InputRequest, len(b.pending))
	copy(out, b.pending)
	return out
}

// Result assembles the InputRequiredResult for the requests the handler could
// not fulfill this round.
func (b *InputBroker) Result() InputRequiredResult {
	return InputRequiredResult{
		ResultType:    protocol.ResultTypeInputRequired,
		InputRequests: b.pending,
		RequestState:  b.state,
	}
}

// sampling resolves a CreateMessage call through the broker.
func (b *InputBroker) sampling(req *CreateMessageRequest) (*CreateMessageResult, error) {
	// CreateMessageRequestParams requires messages; send [] rather than null.
	wire := *req
	if wire.Messages == nil {
		wire.Messages = []SamplingMessage{}
	}
	payload, err := json.Marshal(&wire)
	if err != nil {
		return nil, fmt.Errorf("marshal sampling request: %w", err)
	}
	raw, err := b.request(InputKindSampling, payload)
	if err != nil {
		return nil, err
	}
	var res CreateMessageResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode sampling input response: %w", err)
	}
	return &res, nil
}

// elicit resolves an Elicit call through the broker.
func (b *InputBroker) elicit(req *ElicitRequest) (*ElicitResult, error) {
	// ElicitRequestFormParams requires requestedSchema; a form request that
	// names none asks for an empty object.
	wire := *req
	if wire.Mode != ElicitModeURL && wire.RequestedSchema == nil {
		wire.RequestedSchema = map[string]any{"type": schemaTypeObject, "properties": map[string]any{}}
	}
	payload, err := json.Marshal(&wire)
	if err != nil {
		return nil, fmt.Errorf("marshal elicitation request: %w", err)
	}
	raw, err := b.request(InputKindElicitation, payload)
	if err != nil {
		return nil, err
	}
	var res ElicitResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode elicitation input response: %w", err)
	}
	return &res, nil
}

// listRoots resolves a ListRoots call through the broker.
func (b *InputBroker) listRoots() (*ListRootsResult, error) {
	raw, err := b.request(InputKindRoots, nil)
	if err != nil {
		return nil, err
	}
	var res ListRootsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode roots input response: %w", err)
	}
	return &res, nil
}
