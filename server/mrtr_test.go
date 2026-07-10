package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// samplingSession builds a session whose client declares every capability and
// whose server→client requests resolve through the given broker.
func brokerSession(t *testing.T, broker *InputBroker) *Session {
	t.Helper()
	s := NewSession("test", nil, nil)
	s.SetClientCapabilities(ClientCapabilities{
		Sampling:    true,
		Elicitation: true,
		Roots:       &RootsCapability{},
	})
	s.SetInputBroker(broker)
	return s
}

func TestInputBroker_FirstRoundRecordsPending(t *testing.T) {
	broker := NewInputBroker(nil, nil)
	s := brokerSession(t, broker)

	_, err := s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 10})
	if !errors.Is(err, ErrInputRequired) {
		t.Fatalf("first sampling call: got err %v, want ErrInputRequired", err)
	}
	if !broker.HasPending() {
		t.Fatal("broker should have a pending request after an unfulfilled call")
	}
	res := broker.Result()
	if res.ResultType != protocol.ResultTypeInputRequired {
		t.Errorf("resultType = %q, want %q", res.ResultType, protocol.ResultTypeInputRequired)
	}
	if len(res.InputRequests) != 1 {
		t.Fatalf("inputRequests = %d, want 1", len(res.InputRequests))
	}
	got := res.InputRequests[0]
	if got.ID != "ir-0" {
		t.Errorf("id = %q, want ir-0", got.ID)
	}
	if got.Kind != InputKindSampling {
		t.Errorf("kind = %q, want %q", got.Kind, InputKindSampling)
	}
}

func TestInputBroker_ResolvesFromSuppliedResponse(t *testing.T) {
	want := CreateMessageResult{Role: RoleAssistant, Model: "test-model", Content: NewTextContent("hi")}
	payload, _ := json.Marshal(want)
	broker := NewInputBroker([]InputResponse{{ID: "ir-0", Payload: payload}}, nil)
	s := brokerSession(t, broker)

	got, err := s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 10})
	if err != nil {
		t.Fatalf("CreateMessage with supplied response: %v", err)
	}
	if broker.HasPending() {
		t.Error("broker should have no pending requests when the response was supplied")
	}
	if got.Model != want.Model || got.Content.Text != "hi" {
		t.Errorf("got %+v, want model=%q text=hi", got, want.Model)
	}
}

func TestInputBroker_ResponseErrorSurfacesToHandler(t *testing.T) {
	broker := NewInputBroker([]InputResponse{{
		ID:    "ir-0",
		Error: &protocol.Error{Code: protocol.CodeInvalidParams, Message: "user declined"},
	}}, nil)
	s := brokerSession(t, broker)

	_, err := s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 10})
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Message != "user declined" {
		t.Fatalf("got err %v, want the supplied protocol error", err)
	}
}

func TestInputBroker_SequentialIDsAndReplay(t *testing.T) {
	// Round 1: only the elicitation is answered; the following sampling call is
	// unfulfilled, so it must be recorded as ir-1.
	elicit, _ := json.Marshal(ElicitResult{Action: "accept", Content: map[string]any{"ok": true}})
	broker := NewInputBroker([]InputResponse{{ID: "ir-0", Payload: elicit}}, nil)
	s := brokerSession(t, broker)

	er, err := NewElicitor(s).Elicit(context.Background(), &ElicitRequest{Message: "name?"})
	if err != nil {
		t.Fatalf("elicit (ir-0) should resolve: %v", err)
	}
	if er.Action != "accept" {
		t.Errorf("elicit action = %q, want accept", er.Action)
	}

	_, err = s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 5})
	if !errors.Is(err, ErrInputRequired) {
		t.Fatalf("sampling (ir-1) should be unfulfilled: %v", err)
	}
	reqs := broker.Result().InputRequests
	if len(reqs) != 1 || reqs[0].ID != "ir-1" || reqs[0].Kind != InputKindSampling {
		t.Fatalf("pending = %+v, want single ir-1 sampling", reqs)
	}
}

func TestInputBroker_RootsRoundTrip(t *testing.T) {
	want := ListRootsResult{Roots: []Root{{URI: "file:///work", Name: "work"}}}
	payload, _ := json.Marshal(want)
	broker := NewInputBroker([]InputResponse{{ID: "ir-0", Payload: payload}}, nil)
	s := brokerSession(t, broker)

	got, err := s.ListRoots(context.Background())
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(got.Roots) != 1 || got.Roots[0].URI != "file:///work" {
		t.Fatalf("roots = %+v, want file:///work", got.Roots)
	}
	// The result must also populate the session's cached roots.
	if cached := s.Roots(); len(cached) != 1 || cached[0].URI != "file:///work" {
		t.Errorf("cached roots = %+v, want file:///work", cached)
	}
}

func TestInputBroker_CapabilityGateStillApplies(t *testing.T) {
	// A client that does not declare sampling must be refused before the broker,
	// exactly as under legacy semantics.
	s := NewSession("test", nil, nil)
	s.SetInputBroker(NewInputBroker(nil, nil))
	_, err := s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 1})
	if err == nil || errors.Is(err, ErrInputRequired) {
		t.Fatalf("got %v, want a capability error", err)
	}
}

func TestInputBroker_RequestStateEcho(t *testing.T) {
	state := json.RawMessage(`{"round":2}`)
	broker := NewInputBroker(nil, state)
	s := brokerSession(t, broker)
	_, _ = s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 1})
	if string(broker.Result().RequestState) != `{"round":2}` {
		t.Errorf("requestState = %s, want the echoed token", broker.Result().RequestState)
	}
}
