package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/server"
)

// server.StructuredResult's MarshalJSON omits a nil StructuredContent, but the
// tools/call dispatch does not marshal the struct: buildToolCallResponse copies
// its fields into a response map, and that copy wrote structuredContent: null
// for every result without a structured payload — every error result among
// them. The struct-level test passed while clients still received the null
// (reported downstream as roady #92). Assert on the dispatch output itself.
func TestBuildToolCallResponseOmitsNilStructuredContent(t *testing.T) {
	tests := []struct {
		name    string
		result  any
		want    string // substring that must be present
		notWant string // substring that must be absent
	}{
		{
			name: "error result without payload",
			result: &server.StructuredResult{
				IsError: true,
				Content: []server.Content{{Type: "text", Text: "task not found"}},
			},
			want:    `"isError":true`,
			notWant: `"structuredContent":null`,
		},
		{
			name:    "value result without payload",
			result:  server.StructuredResult{Content: []server.Content{{Type: "text", Text: "ok"}}},
			want:    `"text":"ok"`,
			notWant: `"structuredContent"`,
		},
		{
			name: "explicit empty payload survives as {}",
			result: &server.StructuredResult{
				Content:           []server.Content{{Type: "text", Text: "ok"}},
				StructuredContent: map[string]any{},
			},
			want: `"structuredContent":{}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := buildToolCallResponse(nil, tc.result)
			if err != nil {
				t.Fatalf("buildToolCallResponse: %v", err)
			}
			b, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := string(b)
			if !strings.Contains(got, tc.want) {
				t.Errorf("response %s does not contain %s", got, tc.want)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("response %s contains %s", got, tc.notWant)
			}
		})
	}
}
