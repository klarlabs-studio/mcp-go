package server

import (
	"context"
	"encoding/json"
	"fmt"

	"go.klarlabs.de/mcp/protocol"
)

// ElicitRequest is sent by the server to request structured input from the user.
type ElicitRequest struct {
	// Message is a human-readable message describing what input is needed.
	Message string `json:"message"`
	// RequestedSchema is a JSON Schema describing the desired input structure.
	RequestedSchema map[string]any `json:"requestedSchema,omitempty"`
}

// ElicitResult is the response from an elicitation request.
type ElicitResult struct {
	// Action indicates the user's response: "accept", "decline", or "cancel".
	Action string `json:"action"`
	// Content contains the user-provided data (present when Action is "accept").
	Content map[string]any `json:"content,omitempty"`
}

// Elicitor allows tool handlers to request structured input from users.
type Elicitor struct {
	session *Session
}

// NewElicitor creates a new Elicitor for the given session.
func NewElicitor(session *Session) *Elicitor {
	return &Elicitor{session: session}
}

// Elicit sends an elicitation request to the client and waits for the response.
// Returns an error if the client doesn't support elicitation.
func (e *Elicitor) Elicit(ctx context.Context, req *ElicitRequest) (*ElicitResult, error) {
	if e == nil || e.session == nil {
		return nil, fmt.Errorf("elicitation not available: no session")
	}

	if !e.session.SupportsFeature("elicitation") {
		return nil, fmt.Errorf("client does not support elicitation")
	}

	params, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	idRaw, err := json.Marshal(e.session.requestID.Add(1))
	if err != nil {
		return nil, fmt.Errorf("marshal request ID: %w", err)
	}

	rpcReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      idRaw,
		Method:  protocol.MethodElicitationCreate,
		Params:  params,
	}

	resp, err := e.session.sender.SendRequest(ctx, rpcReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	var result ElicitResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// elicitorKey is the context key for the elicitor.
type elicitorKey struct{}

// ContextWithElicitor returns a context with the elicitor attached.
func ContextWithElicitor(ctx context.Context, e *Elicitor) context.Context {
	return context.WithValue(ctx, elicitorKey{}, e)
}

// ElicitFromContext returns the elicitor from context, or nil if not available.
func ElicitFromContext(ctx context.Context) *Elicitor {
	e, _ := ctx.Value(elicitorKey{}).(*Elicitor)
	return e
}
