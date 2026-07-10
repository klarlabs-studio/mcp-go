package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
	"go.klarlabs.de/mcp/transport"
)

// This file implements the stateless "modern" request path (MCP 2026-07-28,
// SEP-2575). A modern request carries its protocol version, client identity,
// and capabilities in `_meta` on every request instead of establishing them
// once via an initialize handshake. mcp-go is dual-era: a request without the
// modern `_meta` keys is served under the legacy (session-negotiated) semantics
// unchanged.

// modernVersions is the set of stateless protocol revisions this server can
// serve. Kept separate from protocol.SupportedVersions (which drives the legacy
// initialize handshake) while the modern path is built out.
var modernVersions = []string{protocol.DraftVersion}

// modernMeta holds the reserved per-request _meta fields of a modern request.
type modernMeta struct {
	protocolVersion string
	clientInfo      json.RawMessage
	clientCaps      json.RawMessage
	logLevel        string
	// MRTR retry fields: inputResponses fulfills the inputRequests of an earlier
	// input_required result; requestState is the opaque token echoed back.
	inputResponses []server.InputResponse
	requestState   json.RawMessage
}

// parseModernMeta inspects a request's params `_meta`. It returns (meta, true)
// when the modern protocolVersion key is present, (nil, false) for a legacy
// request, and an error only on malformed params.
func parseModernMeta(params json.RawMessage) (*modernMeta, bool, error) {
	if len(params) == 0 {
		return nil, false, nil
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, false, protocol.NewInvalidParams(err.Error())
	}
	raw, ok := envelope.Meta[protocol.MetaKeyProtocolVersion]
	if !ok {
		return nil, false, nil // legacy request
	}
	m := &modernMeta{
		clientInfo: envelope.Meta[protocol.MetaKeyClientInfo],
		clientCaps: envelope.Meta[protocol.MetaKeyClientCapabilities],
	}
	_ = json.Unmarshal(raw, &m.protocolVersion)
	if ll, ok := envelope.Meta[protocol.MetaKeyLogLevel]; ok {
		_ = json.Unmarshal(ll, &m.logLevel)
	}
	if ir, ok := envelope.Meta[protocol.MetaKeyInputResponses]; ok {
		_ = json.Unmarshal(ir, &m.inputResponses)
	}
	m.requestState = envelope.Meta[protocol.MetaKeyRequestState]
	return m, true, nil
}

// applyModern validates a modern request's metadata and, on success, returns a
// context carrying a per-request session built from the client's declared
// capabilities. A validation failure returns a protocol error (the caller maps
// it to the response): missing required fields → -32602, unsupported version →
// -32022. server/discover is exempt from the version check since a client uses
// it precisely to learn which versions the server supports.
func (h *requestHandler) applyModern(ctx context.Context, method string, m *modernMeta) (context.Context, error) {
	// Required per-request fields (SEP-2575). Absent → malformed → -32602.
	if m.protocolVersion == "" || len(m.clientInfo) == 0 || len(m.clientCaps) == 0 {
		return ctx, protocol.NewInvalidParams("modern request missing required _meta (protocolVersion, clientInfo, clientCapabilities)")
	}
	if method != protocol.MethodServerDiscover && !isModernVersion(m.protocolVersion) {
		return ctx, protocol.NewUnsupportedProtocolVersion(modernVersions, m.protocolVersion)
	}

	// Build a request-scoped session from the declared capabilities so
	// per-request feature gating works without any connection state.
	sess := server.NewSession("modern", nil, transport.NotificationSenderFromContext(ctx))
	sess.SetClientCapabilitiesJSON(m.clientCaps)
	if m.logLevel != "" {
		sess.SetLogLevel(server.LogLevel(m.logLevel))
	}
	// Attach an MRTR broker so server→client requests (sampling, elicitation,
	// roots) resolve statelessly: fulfilled from inputResponses on a retry, or
	// recorded as pending for an input_required result on the first round.
	sess.SetInputBroker(server.NewInputBroker(m.inputResponses, m.requestState))
	return server.ContextWithSession(ctx, sess), nil
}

func isModernVersion(v string) bool {
	return slices.Contains(modernVersions, v)
}

// newSubscriptionID mints a stable, non-empty identifier for a
// subscriptions/listen call (MCP 2026-07-28). It is cryptographically random so
// a client cannot guess or collide with another listener's id. The value is
// what would populate _meta[protocol.MetaKeySubscriptionID] on the notifications
// delivered for this subscription.
func newSubscriptionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// inputRequiredResponse converts a paused stateless handler into an MRTR
// input_required result (MCP 2026-07-28). It returns (response, true) when the
// request's broker recorded unfulfilled input requests — the handler called
// sampling/elicitation/roots without a supplied response — and (nil, false)
// otherwise, so the caller handles a genuine error normally.
func inputRequiredResponse(ctx context.Context, req *protocol.Request) (*protocol.Response, bool) {
	sess := server.SessionFromContext(ctx)
	if sess == nil {
		return nil, false
	}
	broker := sess.InputBroker()
	if broker == nil || !broker.HasPending() {
		return nil, false
	}
	return protocol.NewResponse(req.ID, broker.Result()), true
}

// withResultType stamps resultType:"complete" on a modern result that does not
// already carry one (server/discover and MRTR set their own). Legacy responses
// are left untouched — an absent resultType is treated as "complete" by clients.
func withResultType(resp *protocol.Response) {
	if resp == nil {
		return
	}
	if m, ok := resp.Result.(map[string]any); ok {
		if _, present := m["resultType"]; !present {
			m["resultType"] = protocol.ResultTypeComplete
		}
	}
}

// cacheableMethods are the read/list operations whose results carry a
// CacheableResult hint (ttlMs/cacheScope) in the modern protocol.
var cacheableMethods = map[string]bool{
	protocol.MethodToolsList:              true,
	protocol.MethodPromptsList:            true,
	protocol.MethodResourcesList:          true,
	protocol.MethodResourcesRead:          true,
	protocol.MethodResourcesTemplatesList: true,
}

// applyCacheHint stamps ttlMs/cacheScope onto a cacheable modern result when the
// server has a cache hint configured (WithResultCache). server/discover sets its
// own hint. A no-op otherwise.
func (h *requestHandler) applyCacheHint(method string, resp *protocol.Response) {
	if resp == nil || !cacheableMethods[method] {
		return
	}
	ttlMs, scope, ok := h.srv.ResultCache()
	if !ok {
		return
	}
	if m, mok := resp.Result.(map[string]any); mok {
		if _, present := m["ttlMs"]; !present {
			m["ttlMs"] = ttlMs
		}
		if scope != "" {
			if _, present := m["cacheScope"]; !present {
				m["cacheScope"] = scope
			}
		}
	}
}

// modernizeError adapts a legacy protocol error to the modern code scheme: the
// resource-not-found code (mcp-go emits -32001; the spec's -32002 is likewise
// retired) is replaced by -32602 (Invalid params), per MCP 2026-07-28.
func modernizeError(err error) error {
	var mcpErr *protocol.Error
	if errors.As(err, &mcpErr) && mcpErr.Code == protocol.CodeNotFound {
		return &protocol.Error{Code: protocol.CodeInvalidParams, Message: mcpErr.Message, Data: mcpErr.Data}
	}
	return err
}
