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

func TestParseInputResponses(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []InputResponse
		wantErr bool
	}{
		{name: "empty", raw: ``},
		{name: "null", raw: `null`},
		{
			name: "spec map, sorted by id",
			raw:  `{"b":{"action":"decline"},"a":{"roots":[]}}`,
			want: []InputResponse{
				{ID: "a", Payload: json.RawMessage(`{"roots":[]}`)},
				{ID: "b", Payload: json.RawMessage(`{"action":"decline"}`)},
			},
		},
		{
			name: "pre-spec array",
			raw:  `[{"id":"ir-0","payload":{"action":"accept"}}]`,
			want: []InputResponse{{ID: "ir-0", Payload: json.RawMessage(`{"action":"accept"}`)}},
		},
		{name: "string", raw: `"x"`, wantErr: true},
		{name: "broken map", raw: `{"a":`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseInputResponses(json.RawMessage(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID || string(got[i].Payload) != string(tt.want[i].Payload) {
					t.Errorf("[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInputRequest_JSON(t *testing.T) {
	raw, err := json.Marshal(InputRequest{ID: "ir-0", Kind: InputKindRoots})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"method":"roots/list"}` {
		t.Errorf("roots request = %s, want {\"method\":\"roots/list\"}", raw)
	}
	var back InputRequest
	if err := json.Unmarshal([]byte(`{"method":"sampling/createMessage","params":{"maxTokens":1}}`), &back); err != nil {
		t.Fatal(err)
	}
	if back.Kind != InputKindSampling || string(back.Payload) != `{"maxTokens":1}` {
		t.Errorf("decoded = %+v", back)
	}
	if err := json.Unmarshal([]byte(`{"params":{}}`), &back); err == nil {
		t.Error("a request object without a method must not decode")
	}
	if got := InputKindMethod("custom/method"); got != "custom/method" {
		t.Errorf("unknown kind maps to %q, want it unchanged", got)
	}
}

func TestInputRequiredResult_RejectsBadIDs(t *testing.T) {
	for name, reqs := range map[string][]InputRequest{
		"empty id":     {{Kind: InputKindRoots}},
		"duplicate id": {{ID: "a", Kind: InputKindRoots}, {ID: "a", Kind: InputKindRoots}},
	} {
		if _, err := json.Marshal(InputRequiredResult{InputRequests: reqs}); err == nil {
			t.Errorf("%s: marshal succeeded, want an error", name)
		}
	}
}

func TestInputBroker_FillsRequiredParams(t *testing.T) {
	broker := NewInputBroker(nil, nil)
	s := brokerSession(t, broker)
	_, _ = NewElicitor(s).Elicit(context.Background(), &ElicitRequest{Message: "?"})
	_, _ = s.CreateMessage(context.Background(), &CreateMessageRequest{MaxTokens: 1})
	raw, err := json.Marshal(broker.Result())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"resultType":"input_required","inputRequests":{` +
		`"ir-0":{"method":"elicitation/create","params":{"message":"?","requestedSchema":{"properties":{},"type":"object"}}},` +
		`"ir-1":{"method":"sampling/createMessage","params":{"messages":[],"maxTokens":1}}}}`
	if string(raw) != want {
		t.Errorf("result = %s\nwant     %s", raw, want)
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
	// On the wire the state is the opaque string the spec requires.
	raw, err := json.Marshal(broker.Result())
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		RequestState string `json:"requestState"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.RequestState != `{"round":2}` {
		t.Errorf("wire requestState = %s (%v), want the string {\"round\":2}", raw, err)
	}
}
