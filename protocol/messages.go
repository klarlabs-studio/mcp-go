package protocol

import "encoding/json"

// JSONRPCVersion is the JSON-RPC protocol version.
const JSONRPCVersion = "2.0"

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification returns true if this request has no ID (is a notification).
func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// Response represents a JSON-RPC 2.0 response.
//
// Marshaling is handled by MarshalJSON to guarantee JSON-RPC 2.0 compliance:
// "id" is always present (null when unset, per §5), and exactly one of
// "result" or "error" is emitted — a success response always carries "result"
// (null when the result value is nil), an error response never does.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// MarshalJSON implements json.Marshaler for spec-compliant JSON-RPC 2.0 output.
//
// JSON-RPC 2.0 §5 requires the "id" member on every response — it MUST be null
// when the request id could not be determined (e.g. a parse error). A nil ID is
// therefore emitted as an explicit null rather than omitted. Per the same
// section, a response is either a success (with "result") or a failure (with
// "error"), never both and never neither: when Error is set only "error" is
// emitted; otherwise "result" is always present (nil marshals to null).
func (r Response) MarshalJSON() ([]byte, error) {
	id := r.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}

	if r.Error != nil {
		return json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   *Error          `json:"error"`
		}{JSONRPC: r.JSONRPC, ID: id, Error: r.Error})
	}

	result, err := json.Marshal(r.Result)
	if err != nil {
		return nil, err
	}

	return json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{JSONRPC: r.JSONRPC, ID: id, Result: result})
}

// NewResponse creates a successful response.
func NewResponse(id json.RawMessage, result any) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id json.RawMessage, err *Error) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   err,
	}
}
