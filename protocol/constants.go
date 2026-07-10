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
