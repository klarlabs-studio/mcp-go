package mcp

import (
	"context"
	"encoding/json"
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
	return server.ContextWithSession(ctx, sess), nil
}

func isModernVersion(v string) bool {
	return slices.Contains(modernVersions, v)
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
