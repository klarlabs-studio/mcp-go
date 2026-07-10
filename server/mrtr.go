package server

import (
	"encoding/json"
	"errors"
	"fmt"

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

// Input request kinds carried in an InputRequest.Kind (MCP 2026-07-28, MRTR).
const (
	InputKindSampling    = "sampling"    // an LLM completion (replaces sampling/createMessage)
	InputKindElicitation = "elicitation" // structured user input (replaces elicitation/create)
	InputKindRoots       = "roots"       // workspace roots (replaces roots/list)
)

// InputRequest describes one piece of input a stateless tool handler needs
// before it can produce a final result. It is carried in an InputRequiredResult;
// the client fulfills it and retries the original call with a matching
// InputResponse (correlated by ID).
type InputRequest struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Payload is the request parameters for the kind: a CreateMessageRequest for
	// sampling, an ElicitRequest for elicitation. Empty for roots (no params).
	Payload json.RawMessage `json:"payload,omitempty"`
}

// InputResponse carries the client's fulfillment of one InputRequest, correlated
// by ID. Exactly one of Payload or Error is meaningful: Payload holds the result
// (CreateMessageResult / ElicitResult / ListRootsResult) the handler receives;
// Error surfaces a client-side failure to the handler as the input call's error.
type InputResponse struct {
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *protocol.Error `json:"error,omitempty"`
}

// InputRequiredResult is a tool result with resultType "input_required" (MCP
// 2026-07-28, MRTR): the handler paused because it needs input the client must
// supply before retrying. RequestState is an opaque token the server may set and
// the client echoes back on the retry for its own correlation.
type InputRequiredResult struct {
	ResultType    string          `json:"resultType"`
	InputRequests []InputRequest  `json:"inputRequests"`
	RequestState  json.RawMessage `json:"requestState,omitempty"`
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
// back on the next InputRequiredResult.
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
	payload, err := json.Marshal(req)
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
	payload, err := json.Marshal(req)
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
