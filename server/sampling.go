package server

import "encoding/json"

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

// Content type tag values used inside Content.Type. These are every content
// block kind MCP defines; the framework grows into them across the spec-
// revisions roadmap (audio: 2025-03-26; resource_link: 2025-06-18).
const (
	contentTypeText         = "text"
	contentTypeImage        = "image"
	contentTypeAudio        = "audio"
	contentTypeResource     = "resource"
	contentTypeResourceLink = "resource_link"
)

// ContentBlock is the canonical MCP content-block union. It is the single type
// every content constructor returns and that tool results, prompt messages, and
// sampling messages carry. The active fields depend on Type:
//
//	text          → Text
//	image, audio  → Data + MimeType
//	resource_link → URI + Name + Description + MimeType
//	resource      → Resource (embedded contents)
//
// Content is retained as an alias for backward compatibility.
type ContentBlock = Content

// Content represents an MCP content block. See ContentBlock for the field/Type
// matrix. Every field beyond Type is omitempty, so text/image blocks serialize
// exactly as before this became a full union.
type Content struct {
	Type        string              `json:"type"`
	Text        string              `json:"text,omitempty"`
	MimeType    string              `json:"mimeType,omitempty"`
	Data        string              `json:"data,omitempty"`
	URI         string              `json:"uri,omitempty"`
	Name        string              `json:"name,omitempty"`
	Description string              `json:"description,omitempty"`
	Resource    *EmbeddedResource   `json:"resource,omitempty"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
}

// EmbeddedResource is the payload of a `type:"resource"` content block: a
// resource's contents inlined directly into a message or tool result.
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// ContentAnnotations carries optional audience/priority hints on a content
// block, per the MCP annotations schema.
type ContentAnnotations struct {
	Audience []Role   `json:"audience,omitempty"`
	Priority *float64 `json:"priority,omitempty"`
}

// NewTextContent creates a text content block.
func NewTextContent(text string) Content {
	return Content{
		Type: contentTypeText,
		Text: text,
	}
}

// NewImageContent creates an image content block.
func NewImageContent(mimeType, data string) Content {
	return Content{
		Type:     contentTypeImage,
		MimeType: mimeType,
		Data:     data,
	}
}

// NewAudioContent creates an audio content block (MCP 2025-03-26+). data is
// base64-encoded audio.
func NewAudioContent(mimeType, data string) Content {
	return Content{
		Type:     contentTypeAudio,
		MimeType: mimeType,
		Data:     data,
	}
}

// NewResourceLink creates a resource_link content block (MCP 2025-06-18+): a
// pointer to a resource by URI rather than inlined content.
func NewResourceLink(uri, name string) Content {
	return Content{
		Type: contentTypeResourceLink,
		URI:  uri,
		Name: name,
	}
}

// NewEmbeddedResource creates a resource content block that inlines a
// resource's contents.
func NewEmbeddedResource(res EmbeddedResource) Content {
	return Content{
		Type:     contentTypeResource,
		Resource: &res,
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
	// Tools advertises server-side tools the model may call during sampling
	// (MCP 2025-11-25, SEP-1577). Omitted entirely when empty so requests to
	// clients that predate tool-calling serialize exactly as before.
	Tools []SamplingTool `json:"tools,omitempty"`
	// ToolChoice constrains whether/which of Tools the model must call. Nil
	// leaves the decision to the client/model (equivalent to "auto").
	ToolChoice *SamplingToolChoice `json:"toolChoice,omitempty"`
}

// SamplingTool describes a tool the model may call while producing a sampling
// completion (MCP 2025-11-25, SEP-1577). InputSchema is a JSON Schema object
// describing the tool's arguments; it is `any` so callers can pass either a
// generated schema value or a raw map.
type SamplingTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

// SamplingToolChoice constrains how the model selects among the advertised
// Tools. Type is one of:
//
//	"auto" → model decides whether to call a tool (default when omitted)
//	"any"  → model must call one of the provided tools
//	"tool" → model must call the specific tool named by Name
//	"none" → model must not call any tool
//
// Name is only meaningful (and only serialized) when Type == "tool".
type SamplingToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
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
	StopReason string  `json:"stopReason,omitempty"` // "endTurn", "stopSequence", "maxTokens", "toolUse"
	// ToolCalls carries the tool invocations the model requested when tools
	// were offered (MCP 2025-11-25, SEP-1577). The Content union has no
	// tool-use variant, so tool calls are surfaced here rather than in
	// Content. Populated when StopReason == "toolUse"; omitted otherwise.
	ToolCalls []SamplingToolCall `json:"toolCalls,omitempty"`
}

// SamplingToolCall is a single tool invocation the model requested during
// sampling (MCP 2025-11-25, SEP-1577). ID correlates the call with the
// tool-result the caller must feed back on the next turn; Arguments holds the
// model-supplied arguments as raw JSON.
type SamplingToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// SamplingClient is an interface for clients that support sampling.
type SamplingClient interface {
	// CreateMessage sends a sampling request to get an LLM completion.
	CreateMessage(req *CreateMessageRequest) (*CreateMessageResult, error)
}
