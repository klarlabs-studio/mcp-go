package server

import (
	"context"
	"encoding/json"
	"fmt"

	"go.klarlabs.de/mcp/protocol"
)

// Elicitation modes (MCP 2025-11-25). Form collects structured data in-band;
// URL directs the user to an out-of-band URL for sensitive interactions
// (credentials, payment, third-party OAuth) that must not pass through the client.
const (
	ElicitModeForm = "form"
	ElicitModeURL  = "url"
)

// ElicitRequest is sent by the server to request input from the user.
type ElicitRequest struct {
	// Mode is "form" (default) or "url" (MCP 2025-11-25). Omitted means form.
	Mode string `json:"mode,omitempty"`
	// Message is a human-readable message describing what input is needed.
	Message string `json:"message"`
	// RequestedSchema is a JSON Schema describing the desired input structure
	// (form mode). It is a restricted flat object of primitives; enum variants
	// (oneOf/anyOf with const+title) and per-primitive `default` are expressed
	// directly in this map (SEP-1330 / SEP-1034).
	RequestedSchema map[string]any `json:"requestedSchema,omitempty"`
	// URL is the location the user should navigate to (url mode).
	URL string `json:"url,omitempty"`
	// ElicitationID uniquely identifies a url-mode elicitation so a later
	// notifications/elicitation/complete can be correlated.
	ElicitationID string `json:"elicitationId,omitempty"`
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
	// A url-mode request requires the client's url elicitation capability.
	if req.Mode == ElicitModeURL && !e.session.SupportsFeature("elicitation.url") {
		return nil, fmt.Errorf("client does not support url-mode elicitation")
	}
	if b := e.session.InputBroker(); b != nil {
		return b.elicit(req)
	}
	if e.session.sender == nil {
		return nil, ErrNoRequestSender
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

// ElicitURL sends a url-mode elicitation (MCP 2025-11-25): it directs the user
// to an out-of-band URL for a sensitive interaction (credentials, payment,
// third-party OAuth) that must not pass through the client. An action of
// "accept" means the user consented to open the URL, not that the out-of-band
// interaction completed — the server learns that separately. elicitationID
// correlates a later notifications/elicitation/complete.
func (e *Elicitor) ElicitURL(ctx context.Context, message, url, elicitationID string) (*ElicitResult, error) {
	return e.Elicit(ctx, &ElicitRequest{
		Mode:          ElicitModeURL,
		Message:       message,
		URL:           url,
		ElicitationID: elicitationID,
	})
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
