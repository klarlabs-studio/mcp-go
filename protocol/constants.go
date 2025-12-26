package protocol

// MCP protocol version.
const MCPVersion = "2024-11-05"

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
)

// Client feature methods (server requests these from client).
const (
	MethodSamplingCreateMessage = "sampling/createMessage"
	MethodRootsList             = "roots/list"
	MethodLoggingSetLevel       = "logging/setLevel"
)

// Resource subscription methods.
const (
	MethodResourcesSubscribe   = "resources/subscribe"
	MethodResourcesUnsubscribe = "resources/unsubscribe"
)
