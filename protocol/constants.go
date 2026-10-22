package protocol

import "slices"

// MCPVersion is the default protocol version the legacy `initialize` handshake
// advertises when a client requests a version it does not support (or requests
// none). It is the newest initialize-era revision. The 2026-07-28 stateless
// revision retires initialize; modern clients use server/discover and per-request
// `_meta` instead (see ModernVersion).
const MCPVersion = "2025-11-25"

// ModernVersion is the published stateless MCP revision (2026-07-28). It is
// advertised via server/discover and served on the per-request `_meta` path.
// It is not negotiated via initialize: that method does not exist in this
// revision.
const ModernVersion = "2026-07-28"

// DraftVersion is a historical alias of ModernVersion.
//
// Deprecated: 2026-07-28 is the published spec, not a draft. Use ModernVersion.
const DraftVersion = ModernVersion

// Named initialize-era revisions, used in InitializeVersions/SupportedVersions
// so the date strings are not duplicated (goconst).
const (
	version20241105 = "2024-11-05"
	version20250326 = "2025-03-26"
	version20250618 = "2025-06-18"
)

// InitializeVersions lists protocol revisions the legacy initialize handshake
// can speak, ordered oldest→newest. 2026-07-28 is omitted because that
// revision retires initialize.
var InitializeVersions = []string{
	version20241105,
	version20250326,
	version20250618,
	MCPVersion,
}

// SupportedVersions lists every MCP protocol revision this library can speak,
// ordered oldest→newest. This is what server/discover advertises. initialize
// negotiation uses InitializeVersions (a prefix of this list).
//
// Note on 2025-03-26: JSON-RPC batching was an optional feature of that
// revision and was removed again in 2025-06-18; this library never batches,
// which is conformant (batching support was never required).
var SupportedVersions = []string{
	version20241105,
	version20250326,
	version20250618,
	MCPVersion,
	ModernVersion,
}

// HeaderProtocolVersion is the Streamable HTTP header carrying the MCP
// protocol version (MCP 2025-06-18). Clients MUST send it on HTTP requests
// after initialize; servers SHOULD reject an unsupported or (for ≥2025-06-18)
// missing value.
const HeaderProtocolVersion = "MCP-Protocol-Version"

// HeaderMethod and HeaderName are the Streamable HTTP routing headers
// (MCP 2026-07-28). They mirror the JSON-RPC method and the primary named
// target so intermediaries can route without parsing the body.
const (
	HeaderMethod = "Mcp-Method"
	HeaderName   = "Mcp-Name"
)

// RequiresProtocolVersionHeader reports whether HTTP requests at protocol
// version v must carry MCP-Protocol-Version (true for 2025-06-18 and later).
func RequiresProtocolVersionHeader(v string) bool {
	return v >= version20250618
}

// IsSupportedVersion reports whether v is a protocol version this library
// implements (initialize-era or modern).
func IsSupportedVersion(v string) bool {
	return slices.Contains(SupportedVersions, v)
}

// IsInitializeVersion reports whether v can be negotiated via initialize.
func IsInitializeVersion(v string) bool {
	return slices.Contains(InitializeVersions, v)
}

// IsModernVersion reports whether v is a stateless (2026-07-28+) revision.
func IsModernVersion(v string) bool {
	return v == ModernVersion
}

// NegotiateVersion selects the protocol version the server will use given the
// version the client requested in `initialize`. If the server supports the
// requested version *as an initialize-era revision* it MUST reply with that
// same version; otherwise it replies with MCPVersion (the newest initialize
// revision) and the client decides whether it can proceed. Requesting
// ModernVersion via initialize therefore falls back to MCPVersion — that
// revision has no initialize handshake.
func NegotiateVersion(requested string) string {
	if requested == "" {
		return MCPVersion
	}
	if IsInitializeVersion(requested) {
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

	// MethodTasksUpdate fulfills in-task inputRequests (MCP 2026-07-28 tasks
	// extension) and may refresh ttl. tasks/list and tasks/result are retired
	// on the modern path — clients poll tasks/get instead of blocking on
	// tasks/result, and the extension favors direct handles over listing.
	MethodTasksUpdate = "tasks/update"

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

// Reserved per-request _meta keys for the stateless (modern) request model
// (MCP 2026-07-28). Every modern request carries protocol version, client
// identity, and capabilities here instead of via an initialize handshake.
const (
	MetaKeyProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaKeyClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaKeyClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaKeyServerInfo         = "io.modelcontextprotocol/serverInfo" // result _meta; servers SHOULD set
	MetaKeyLogLevel           = "io.modelcontextprotocol/logLevel"
	MetaKeySubscriptionID     = "io.modelcontextprotocol/subscriptionId"
	MetaKeyRelatedTask        = "io.modelcontextprotocol/related-task"

	// MRTR (Multi Round-Trip Requests, MCP 2026-07-28): the spec carries a
	// retry's inputResponses and requestState directly in the request params
	// (InputResponseRequestParams), not in _meta. These _meta keys are what
	// mcp-go read before it followed the published schema; the server still
	// accepts them from older callers but no conforming client sends them.
	MetaKeyInputResponses = "io.modelcontextprotocol/inputResponses"
	MetaKeyRequestState   = "io.modelcontextprotocol/requestState"

	// W3C Trace Context (MCP 2026-07-28): a modern request carries the caller's
	// distributed-trace position in _meta so the server span joins the client's
	// trace. traceparent/tracestate map to the propagation.TraceContext
	// propagator; baggage to the propagation.Baggage propagator.
	MetaKeyTraceparent = "io.modelcontextprotocol/traceparent"
	MetaKeyTracestate  = "io.modelcontextprotocol/tracestate"
	MetaKeyBaggage     = "io.modelcontextprotocol/baggage"
)

// Extension identifiers (reverse-DNS) negotiated via capabilities.extensions
// (MCP 2026-07-28, SEP-2133).
const (
	ExtensionUI     = "io.modelcontextprotocol/ui"     // MCP Apps
	ExtensionTasks  = "io.modelcontextprotocol/tasks"  // Tasks
	ExtensionSkills = "io.modelcontextprotocol/skills" // Agent Skills over MCP (SEP-2640, experimental)
)

// ResultType values for polymorphic results (MCP 2026-07-28). An absent
// resultType is treated as "complete" for backward compatibility.
const (
	ResultTypeComplete      = "complete"
	ResultTypeInputRequired = "input_required"
	ResultTypeTask          = "task" // CreateTaskResult (SEP-2663)
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
	MethodElicitationComplete = "notifications/elicitation/complete"
	MethodTasks               = "notifications/tasks"
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
