package server

// SamplingMessage represents a message in a sampling request.
type SamplingMessage struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

// Role represents the role of a message sender.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Content represents the content of a message.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// NewTextContent creates a text content block.
func NewTextContent(text string) Content {
	return Content{
		Type: "text",
		Text: text,
	}
}

// NewImageContent creates an image content block.
func NewImageContent(mimeType, data string) Content {
	return Content{
		Type:     "image",
		MimeType: mimeType,
		Data:     data,
	}
}

// CreateMessageRequest is sent by the server to request an LLM completion from the client.
type CreateMessageRequest struct {
	Messages         []SamplingMessage `json:"messages"`
	MaxTokens        int               `json:"maxTokens"`
	StopSequences    []string          `json:"stopSequences,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	IncludeContext   string            `json:"includeContext,omitempty"` // "none", "thisServer", "allServers"
	ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
}

// ModelPreferences expresses preferences for model selection.
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         *float64    `json:"costPriority,omitempty"`         // 0-1
	SpeedPriority        *float64    `json:"speedPriority,omitempty"`        // 0-1
	IntelligencePriority *float64    `json:"intelligencePriority,omitempty"` // 0-1
}

// ModelHint hints at a model the client should use.
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// CreateMessageResult is the response from a sampling request.
type CreateMessageResult struct {
	Role       Role    `json:"role"`
	Content    Content `json:"content"`
	Model      string  `json:"model"`
	StopReason string  `json:"stopReason,omitempty"` // "endTurn", "stopSequence", "maxTokens"
}

// SamplingClient is an interface for clients that support sampling.
type SamplingClient interface {
	// CreateMessage sends a sampling request to get an LLM completion.
	CreateMessage(req *CreateMessageRequest) (*CreateMessageResult, error)
}
