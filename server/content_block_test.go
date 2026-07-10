package server

import (
	"encoding/json"
	"testing"
)

// TestContentBlock_Union verifies each content-block constructor produces the
// MCP wire shape, and that text/image blocks are unchanged by the union fields
// (everything beyond type is omitempty).
func TestContentBlock_Union(t *testing.T) {
	tests := []struct {
		name     string
		block    ContentBlock
		wantJSON string
	}{
		{"text", NewTextContent("hi"), `{"type":"text","text":"hi"}`},
		{"image", NewImageContent("image/png", "aGk="), `{"type":"image","mimeType":"image/png","data":"aGk="}`},
		{"audio", NewAudioContent("audio/wav", "aGk="), `{"type":"audio","mimeType":"audio/wav","data":"aGk="}`},
		{"resource_link", NewResourceLink("file://x", "x"), `{"type":"resource_link","uri":"file://x","name":"x"}`},
		{
			"resource",
			NewEmbeddedResource(EmbeddedResource{URI: "file://x", Text: "body"}),
			`{"type":"resource","resource":{"uri":"file://x","text":"body"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tt.wantJSON {
				t.Errorf("got  %s\nwant %s", b, tt.wantJSON)
			}
		})
	}
}

// TestContentBlock_IsContentAlias confirms ContentBlock and Content are the same
// type, so existing Content-typed APIs accept the new constructors. Functions
// typed on each must accept the same value.
func TestContentBlock_IsContentAlias(t *testing.T) {
	takesContent := func(c Content) string { return c.Type }
	takesBlock := func(b ContentBlock) string { return b.Type }

	block := NewAudioContent("audio/wav", "aGk=")
	if takesContent(block) != contentTypeAudio || takesBlock(block) != contentTypeAudio {
		t.Error("ContentBlock and Content are not interchangeable")
	}
}
