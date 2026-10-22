package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.klarlabs.de/mcp/protocol"
)

// Multi Round-Trip Requests (MRTR, MCP 2026-07-28). A stateless server that
// needs sampling, elicitation, or roots from the client answers tools/call,
// prompts/get, or resources/read with resultType "input_required" instead of
// sending a server→client request. The client fulfills the inputRequests and
// retries the original call with inputResponses (and the echoed requestState)
// in the request params.
//
// The client surfaces such a result as an *InputRequiredError. To retry, call
// the same method again with a context from WithInputResponses.

// ErrInputRequired matches (errors.Is) the *InputRequiredError a call returns
// when the server answered with resultType "input_required".
var ErrInputRequired = errors.New("server requires input (MRTR)")

// InputRequest is one server→client request carried in an input_required
// result: a plain request object such as
// {"method": "elicitation/create", "params": {…}}.
type InputRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// InputRequiredError reports a result with resultType "input_required". Its
// fields are the spec's InputRequiredResult: InputRequests is keyed by the
// server-assigned ids the retry's inputResponses must reuse, and RequestState
// is the opaque string to echo back unchanged ("" when the server sent none).
type InputRequiredError struct {
	InputRequests map[string]InputRequest
	RequestState  string
}

// Error describes the outstanding requests by id and method.
func (e *InputRequiredError) Error() string {
	ids := make([]string, 0, len(e.InputRequests))
	for id := range e.InputRequests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+"="+e.InputRequests[id].Method)
	}
	if len(parts) == 0 {
		return ErrInputRequired.Error() + ": retry with requestState"
	}
	return ErrInputRequired.Error() + ": " + strings.Join(parts, ", ")
}

// Is makes errors.Is(err, ErrInputRequired) true.
func (e *InputRequiredError) Is(target error) bool { return target == ErrInputRequired }

// Retry returns a context that makes the next call carry responses as its
// inputResponses and this result's requestState. responses maps each id in
// InputRequests to the client's result for it: an ElicitResult,
// CreateMessageResult, or ListRootsResult object.
func (e *InputRequiredError) Retry(ctx context.Context, responses map[string]any) context.Context {
	return WithInputResponses(ctx, responses, e.RequestState)
}

type inputRetryKey struct{}

type inputRetry struct {
	responses    map[string]any
	requestState string
}

// WithInputResponses returns a context that makes the next tools/call,
// prompts/get, or resources/read made with it a retry of an input_required
// result: params carry inputResponses (keyed by the server's input request ids)
// and, when non-empty, requestState. Other methods ignore it.
func WithInputResponses(ctx context.Context, responses map[string]any, requestState string) context.Context {
	return context.WithValue(ctx, inputRetryKey{}, inputRetry{responses: responses, requestState: requestState})
}

// retryMethods are the requests that may answer input_required and therefore
// carry inputResponses/requestState on a retry (MCP 2026-07-28).
var retryMethods = map[string]bool{
	protocol.MethodToolsCall:     true,
	protocol.MethodPromptsGet:    true,
	protocol.MethodResourcesRead: true,
}

// withInputRetry splices the retry fields from ctx into params.
func withInputRetry(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	retry, ok := ctx.Value(inputRetryKey{}).(inputRetry)
	if !ok || !retryMethods[method] {
		return params, nil
	}
	if retry.requestState != "" {
		state, err := json.Marshal(retry.requestState)
		if err != nil {
			return nil, fmt.Errorf("marshal requestState: %w", err)
		}
		if params, err = spliceObjectField(params, "requestState", state); err != nil {
			return nil, err
		}
	}
	if len(retry.responses) > 0 {
		responses, err := json.Marshal(retry.responses)
		if err != nil {
			return nil, fmt.Errorf("marshal inputResponses: %w", err)
		}
		if params, err = spliceObjectField(params, "inputResponses", responses); err != nil {
			return nil, err
		}
	}
	return params, nil
}

// inputRequiredFrom returns the *InputRequiredError for a result with
// resultType "input_required", nil for any other result, and an error wrapping
// ErrInvalidResult when the result breaks the spec (no inputRequests and no
// requestState, or fields of the wrong type).
func inputRequiredFrom(result any) error {
	m, ok := result.(map[string]any)
	if !ok || m["resultType"] != protocol.ResultTypeInputRequired {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResult, err)
	}
	var wire struct {
		InputRequests map[string]InputRequest `json:"inputRequests"`
		RequestState  *string                 `json:"requestState"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return fmt.Errorf("%w: input_required: %w", ErrInvalidResult, err)
	}
	if len(wire.InputRequests) == 0 && wire.RequestState == nil {
		return fmt.Errorf("%w: input_required carries neither inputRequests nor requestState", ErrInvalidResult)
	}
	out := &InputRequiredError{InputRequests: wire.InputRequests}
	if wire.RequestState != nil {
		out.RequestState = *wire.RequestState
	}
	return out
}
