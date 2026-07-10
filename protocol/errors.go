// Package protocol implements the MCP protocol layer including JSON-RPC 2.0.
package protocol

import "fmt"

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// MCP-specific error codes.
const (
	CodeNotFound     = -32001
	CodeUnauthorized = -32002
	CodeRateLimited  = -32003
	// CodeURLElicitationRequired signals that a request cannot proceed until a
	// url-mode elicitation is completed (MCP 2025-11-25, SEP-1036). The error
	// data carries the required elicitations.
	CodeURLElicitationRequired = -32042
)

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("mcp: %s (code: %d)", e.Message, e.Code)
}

// Is implements errors.Is comparison by error code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// WithData returns a copy of the error with additional data attached.
func (e *Error) WithData(data any) *Error {
	return &Error{
		Code:    e.Code,
		Message: e.Message,
		Data:    data,
	}
}

// NewParseError creates a parse error (-32700).
func NewParseError(msg string) *Error {
	return &Error{Code: CodeParseError, Message: msg}
}

// NewInvalidRequest creates an invalid request error (-32600).
func NewInvalidRequest(msg string) *Error {
	return &Error{Code: CodeInvalidRequest, Message: msg}
}

// NewMethodNotFound creates a method not found error (-32601).
func NewMethodNotFound(msg string) *Error {
	return &Error{Code: CodeMethodNotFound, Message: msg}
}

// NewInvalidParams creates an invalid params error (-32602).
func NewInvalidParams(msg string) *Error {
	return &Error{Code: CodeInvalidParams, Message: msg}
}

// NewInternalError creates an internal error (-32603).
func NewInternalError(msg string) *Error {
	return &Error{Code: CodeInternalError, Message: msg}
}

// NewNotFound creates a not found error (-32001).
func NewNotFound(msg string) *Error {
	return &Error{Code: CodeNotFound, Message: msg}
}

// NewUnauthorized creates an unauthorized error (-32002).
func NewUnauthorized(msg string) *Error {
	return &Error{Code: CodeUnauthorized, Message: msg}
}

// NewURLElicitationRequired creates a URL-elicitation-required error (-32042,
// MCP 2025-11-25). elicitations is the list of url-mode elicitations the client
// must complete before retrying, carried in the error data.
func NewURLElicitationRequired(msg string, elicitations any) *Error {
	return &Error{
		Code:    CodeURLElicitationRequired,
		Message: msg,
		Data:    map[string]any{"elicitations": elicitations},
	}
}
