package protocol

import "slices"

// MCPVersion is the default protocol version this library advertises when a
// client requests a version it does not support (or requests none). It is the
// newest revision fully implemented and certified by the conformance suite.
const MCPVersion = "2025-11-25"

// SupportedVersions lists every MCP protocol revision this library can speak,
// ordered oldest→newest. The server negotiates the best match against the
// client's requested version. New revisions are appended here as each phase of
// the spec-revisions roadmap (docs/revisions-roadmap.md) is certified.
//
// Note on 2025-03-26: JSON-RPC batching was an optional feature of that
// revision and was removed again in 2025-06-18; this library never batches,
// which is conformant (batching support was never required).
var SupportedVersions = []string{
	"2024-11-05",
	"2025-03-26",
	"2025-06-18",
	"2025-11-25",
}

// IsSupportedVersion reports whether v is a protocol version this library
// implements.
func IsSupportedVersion(v string) bool {
	return slices.Contains(SupportedVersions, v)
}

// NegotiateVersion selects the protocol version the server will use given the
// version the client requested in `initialize`. Per the MCP lifecycle spec: if
// the server supports the requested version it MUST reply with that same
// version; otherwise it replies with a version it does support (its preferred
// default), and the client decides whether it can proceed. An empty request
// falls back to the default.
func NegotiateVersion(requested string) string {
	if requested == "" {
		return MCPVersion
	}
	if IsSupportedVersion(requested) {
		return requested
	}
	return MCPVersion
}

// MCP method names.
const (
	MethodInitialize             = "initialize"
	MethodInitialized            = "notifications/initialized"
	MethodToolsList              = "tools/list"
	MethodToolsCall              = "tools/call"
	MethodResourcesList          = "resources/list"
	MethodResourcesRead          = "resources/read"
	MethodResourcesTemplatesList = "resources/templates/list"
	MethodPromptsList            = "prompts/list"
	MethodPromptsGet             = "prompts/get"
	MethodCompletionComplete     = "completion/complete"
	MethodPing                   = "ping"

	// Task-augmented requests (MCP 2025-11-25, SEP-1686).
	MethodTasksGet    = "tasks/get"
	MethodTasksResult = "tasks/result"
	MethodTasksCancel = "tasks/cancel"
	MethodTasksList   = "tasks/list"

	// Stateless discovery (MCP 2026-07-28, SEP-2575) — replaces initialize for
	// modern clients.
	MethodServerDiscover = "server/discover"

	// Stateless subscription (MCP 2026-07-28, SEP) — a modern client opts into
	// the notification types (and resource URIs) it wants via a single method,
	// replacing the GET SSE stream plus resources/subscribe and
	// resources/unsubscribe. Delivered notifications are tagged with
	// MetaKeySubscriptionID so the client can correlate them.
	MethodSubscriptionsListen = "subscriptions/listen"
)

// DraftVersion is the 2026-07-28 release-candidate ("modern", stateless)
// protocol revision. It is advertised via server/discover but is NOT yet in
// SupportedVersions (which drives the legacy initialize handshake): the modern
// stateless request path is being built incrementally (docs/revisions-roadmap.md
// Phase 4).
const DraftVersion = "2026-07-28"

// Reserved per-request _meta keys for the stateless (modern) request model
// (MCP 2026-07-28). Every modern request carries protocol version, client
// identity, and capabilities here instead of via an initialize handshake.
const (
	MetaKeyProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaKeyClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaKeyClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaKeyLogLevel           = "io.modelcontextprotocol/logLevel"
	MetaKeySubscriptionID     = "io.modelcontextprotocol/subscriptionId"
	MetaKeyRelatedTask        = "io.modelcontextprotocol/related-task"

	// MRTR (Multi Round-Trip Requests, MCP 2026-07-28): a client retrying a
	// call that returned resultType "input_required" carries its fulfillment of
	// the earlier inputRequests under MetaKeyInputResponses and echoes the
	// server's opaque MetaKeyRequestState.
	MetaKeyInputResponses = "io.modelcontextprotocol/inputResponses"
	MetaKeyRequestState   = "io.modelcontextprotocol/requestState"
)

// Extension identifiers (reverse-DNS) negotiated via capabilities.extensions
// (MCP 2026-07-28, SEP-2133).
const (
	ExtensionUI    = "io.modelcontextprotocol/ui"    // MCP Apps
	ExtensionTasks = "io.modelcontextprotocol/tasks" // Tasks
)

// ResultType values for polymorphic results (MCP 2026-07-28). An absent
// resultType is treated as "complete" for backward compatibility.
const (
	ResultTypeComplete      = "complete"
	ResultTypeInputRequired = "input_required"
)

// MCP notification methods.
const (
	MethodProgress            = "notifications/progress"
	MethodCancelled           = "notifications/cancelled"
	MethodLoggingMessage      = "notifications/message"
	MethodResourceUpdated     = "notifications/resources/updated"
	MethodResourceListChanged = "notifications/resources/list_changed"
	MethodToolListChanged     = "notifications/tools/list_changed"
	MethodPromptListChanged   = "notifications/prompts/list_changed"
	MethodRootsListChanged    = "notifications/roots/list_changed"
	MethodChannelMessage      = "notifications/channel/message"
)

// Client feature methods (server requests these from client).
const (
	MethodSamplingCreateMessage = "sampling/createMessage"
	MethodRootsList             = "roots/list"
	MethodLoggingSetLevel       = "logging/setLevel"
	MethodElicitationCreate     = "elicitation/create"
)

// Resource subscription methods.
const (
	MethodResourcesSubscribe   = "resources/subscribe"
	MethodResourcesUnsubscribe = "resources/unsubscribe"
)
