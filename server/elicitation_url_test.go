package server

import (
	"context"
	"encoding/json"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// captureSender records the last request and returns a canned response.
type captureSender struct {
	last   *protocol.Request
	result any
}

func (c *captureSender) SendRequest(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
	c.last = req
	return &protocol.Response{Result: c.result}, nil
}

func TestElicitURL_SendsURLMode(t *testing.T) {
	sender := &captureSender{result: map[string]any{"action": "accept"}}
	sess := NewSession("t", sender, nil, WithClientCapabilities(ClientCapabilities{
		Elicitation: true, ElicitationURL: true,
	}))

	res, err := NewElicitor(sess).ElicitURL(context.Background(),
		"Provide your API key", "https://example.com/set_key", "elic-1")
	if err != nil {
		t.Fatalf("ElicitURL: %v", err)
	}
	if res.Action != "accept" {
		t.Errorf("action = %q, want accept", res.Action)
	}

	// The sent request must carry url-mode fields.
	var sent ElicitRequest
	if err := json.Unmarshal(sender.last.Params, &sent); err != nil {
		t.Fatalf("unmarshal sent params: %v", err)
	}
	if sent.Mode != ElicitModeURL || sent.URL != "https://example.com/set_key" || sent.ElicitationID != "elic-1" {
		t.Errorf("sent request = %+v, want url-mode with url + id", sent)
	}
}

func TestElicitURL_RequiresURLCapability(t *testing.T) {
	// Client supports form elicitation but not url mode.
	sender := &captureSender{result: map[string]any{"action": "accept"}}
	sess := NewSession("t", sender, nil, WithClientCapabilities(ClientCapabilities{Elicitation: true}))
	_, err := NewElicitor(sess).ElicitURL(context.Background(), "m", "https://x", "id")
	if err == nil {
		t.Fatal("expected error when client lacks url elicitation capability")
	}
}

func TestClientCapabilities_ElicitationURLParse(t *testing.T) {
	sess := NewSession("t", nil, nil)
	sess.SetClientCapabilitiesJSON(json.RawMessage(`{"elicitation":{"url":{}}}`))
	caps := sess.ClientCapabilities()
	if !caps.Elicitation || !caps.ElicitationURL {
		t.Errorf("expected Elicitation && ElicitationURL, got %+v", caps)
	}

	// Empty elicitation object means form-only (no url).
	sess2 := NewSession("t", nil, nil)
	sess2.SetClientCapabilitiesJSON(json.RawMessage(`{"elicitation":{}}`))
	if c := sess2.ClientCapabilities(); !c.Elicitation || c.ElicitationURL {
		t.Errorf("empty elicitation should be form-only, got %+v", c)
	}
}

func TestURLElicitationRequiredError(t *testing.T) {
	err := protocol.NewURLElicitationRequired("need info", []map[string]any{{"elicitationId": "x"}})
	if err.Code != protocol.CodeURLElicitationRequired {
		t.Errorf("code = %d, want %d", err.Code, protocol.CodeURLElicitationRequired)
	}
}
