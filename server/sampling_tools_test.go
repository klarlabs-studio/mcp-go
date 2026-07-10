package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestCreateMessageRequest_ToolsMarshal verifies that Tools and ToolChoice
// serialize into the MCP wire keys `tools` and `toolChoice` (SEP-1577), and
// that they are omitted entirely when unset.
func TestCreateMessageRequest_ToolsMarshal(t *testing.T) {
	t.Run("tools and toolChoice present", func(t *testing.T) {
		req := &CreateMessageRequest{
			Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
			MaxTokens: 100,
			Tools: []SamplingTool{
				{
					Name:        "search",
					Description: "Search the web",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
					},
				},
			},
			ToolChoice: &SamplingToolChoice{Type: "tool", Name: "search"},
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		toolsRaw, ok := wire["tools"]
		if !ok {
			t.Fatalf("expected `tools` key, got: %s", data)
		}
		tools, ok := toolsRaw.([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("expected one tool, got: %v", toolsRaw)
		}
		tool := tools[0].(map[string]any)
		if tool["name"] != "search" {
			t.Errorf("tool name = %v, want search", tool["name"])
		}
		if tool["description"] != "Search the web" {
			t.Errorf("tool description = %v, want Search the web", tool["description"])
		}
		if _, ok := tool["inputSchema"]; !ok {
			t.Errorf("expected `inputSchema` key on tool, got: %v", tool)
		}

		choiceRaw, ok := wire["toolChoice"]
		if !ok {
			t.Fatalf("expected `toolChoice` key, got: %s", data)
		}
		choice := choiceRaw.(map[string]any)
		if choice["type"] != "tool" {
			t.Errorf("toolChoice.type = %v, want tool", choice["type"])
		}
		if choice["name"] != "search" {
			t.Errorf("toolChoice.name = %v, want search", choice["name"])
		}
	})

	t.Run("omitted when unset", func(t *testing.T) {
		req := &CreateMessageRequest{
			Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
			MaxTokens: 100,
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := wire["tools"]; ok {
			t.Errorf("expected `tools` omitted, got: %s", data)
		}
		if _, ok := wire["toolChoice"]; ok {
			t.Errorf("expected `toolChoice` omitted, got: %s", data)
		}
	})

	t.Run("toolChoice auto omits name", func(t *testing.T) {
		data, err := json.Marshal(&SamplingToolChoice{Type: "auto"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if wire["type"] != "auto" {
			t.Errorf("type = %v, want auto", wire["type"])
		}
		if _, ok := wire["name"]; ok {
			t.Errorf("expected `name` omitted for auto, got: %s", data)
		}
	})
}

// TestCreateMessageResult_ToolCallsUnmarshal verifies a tool-use sampling
// result round-trips through CreateMessageResult.
func TestCreateMessageResult_ToolCallsUnmarshal(t *testing.T) {
	raw := `{
		"role": "assistant",
		"model": "test-model",
		"stopReason": "toolUse",
		"content": {"type": "text", "text": ""},
		"toolCalls": [
			{"id": "call_1", "name": "search", "arguments": {"query": "go"}}
		]
	}`

	var result CreateMessageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.StopReason != "toolUse" {
		t.Errorf("stopReason = %q, want toolUse", result.StopReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "search" {
		t.Errorf("tool call = %+v, want id=call_1 name=search", tc)
	}
	if len(tc.Arguments) == 0 {
		t.Errorf("expected arguments to be populated, got empty")
	}
}

// TestCreateMessageWithTools_NoRequestSender verifies the nil-sender guard is
// preserved for the tool-calling entry point: with sampling advertised but no
// RequestSender, it returns ErrNoRequestSender.
func TestCreateMessageWithTools_NoRequestSender(t *testing.T) {
	session := NewSession("t", nil, nil, WithClientCapabilities(ClientCapabilities{Sampling: true}))

	req := &CreateMessageRequest{
		Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
		MaxTokens: 100,
	}
	tools := []SamplingTool{{Name: "search"}}
	choice := &SamplingToolChoice{Type: "auto"}

	_, err := session.CreateMessageWithTools(context.Background(), req, tools, choice)
	if !errors.Is(err, ErrNoRequestSender) {
		t.Fatalf("expected ErrNoRequestSender, got %v", err)
	}

	// The helper must also have applied the tools/choice onto the request.
	if len(req.Tools) != 1 || req.ToolChoice == nil {
		t.Errorf("expected Tools and ToolChoice set on req, got Tools=%v ToolChoice=%v", req.Tools, req.ToolChoice)
	}
}
