package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// A jsonschema tag is comma-separated, which collides with prose. A description
// containing a comma used to be truncated at it, and the caller had no way to
// notice: for an MCP server this text is the contract with the model.
//
// Found in a real server, where `create_knowledge.kind` advertised
// "What sort of document this is: runbook" while six other kinds were valid,
// and a limit field advertised "Maximum results to return (default 10" --
// cut mid-parenthesis.
func TestJSONSchemaTag_DescriptionKeepsCommas(t *testing.T) {
	type args struct {
		Kind  string `json:"kind" jsonschema:"description=One of runbook, adr, postmortem or policy"`
		Limit int    `json:"limit" jsonschema:"description=Maximum results to return (default 10, capped at 50)"`
		Plain string `json:"plain" jsonschema:"description=No commas here"`
	}

	s, err := Generate(args{})
	if err != nil {
		t.Fatalf("GenerateFromType: %v", err)
	}

	for field, want := range map[string]string{
		"kind":  "One of runbook, adr, postmortem or policy",
		"limit": "Maximum results to return (default 10, capped at 50)",
		"plain": "No commas here",
	} {
		got := s.Properties[field].Description
		if got != want {
			t.Errorf("%s description = %q, want %q", field, got, want)
		}
	}
}

func TestJSONSchemaTag_HeaderAnnotation(t *testing.T) {
	type args struct {
		Region string `json:"region" jsonschema:"required,header=Region"`
		Query  string `json:"query" jsonschema:"description=SQL"`
	}
	s, err := Generate(args{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Properties["region"].XMcpHeader != "Region" {
		t.Fatalf("XMcpHeader = %q", s.Properties["region"].XMcpHeader)
	}
	raw, err := json.Marshal(s.Properties["region"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"x-mcp-header":"Region"`) {
		t.Fatalf("marshaled schema missing x-mcp-header: %s", raw)
	}
}

// required must keep working wherever it sits in the tag, including after a
// description that contains commas.
func TestJSONSchemaTag_RequiredAlongsideCommaDescription(t *testing.T) {
	type args struct {
		A string `json:"a" jsonschema:"required,description=First, second, third"`
		B string `json:"b" jsonschema:"description=Alpha, beta,required"`
		C string `json:"c" jsonschema:"description=No directive follows me"`
	}

	s, err := Generate(args{})
	if err != nil {
		t.Fatalf("GenerateFromType: %v", err)
	}

	if got := s.Properties["a"].Description; got != "First, second, third" {
		t.Errorf("a description = %q", got)
	}
	if got := s.Properties["b"].Description; got != "Alpha, beta" {
		t.Errorf("b description = %q, want the prose without the trailing directive", got)
	}

	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	if !required["a"] || !required["b"] {
		t.Errorf("required = %v, want both a and b", s.Required)
	}
	if required["c"] {
		t.Errorf("c must not be required")
	}
}

// minimum, maximum, default and enum were accepted and discarded, so a field
// carrying them advertised no constraint at all.
func TestJSONSchemaTag_ConstraintsAreEmitted(t *testing.T) {
	type args struct {
		Depth     int    `json:"depth" jsonschema:"description=Traversal depth cap,minimum=1,maximum=5,default=4"`
		Direction string `json:"direction" jsonschema:"enum=downstream|upstream|both,default=downstream"`
		Flag      bool   `json:"flag" jsonschema:"default=true"`
	}

	s, err := Generate(args{})
	if err != nil {
		t.Fatalf("GenerateFromType: %v", err)
	}

	depth := s.Properties["depth"]
	if depth.Minimum == nil || *depth.Minimum != 1 {
		t.Errorf("depth minimum = %v, want 1", depth.Minimum)
	}
	if depth.Maximum == nil || *depth.Maximum != 5 {
		t.Errorf("depth maximum = %v, want 5", depth.Maximum)
	}
	// Typed, not "4": a strict validator rejects a string default on an integer.
	if depth.Default != int64(4) {
		t.Errorf("depth default = %#v, want int64(4)", depth.Default)
	}
	if depth.Description != "Traversal depth cap" {
		t.Errorf("depth description = %q", depth.Description)
	}

	dir := s.Properties["direction"]
	if len(dir.Enum) != 3 {
		t.Fatalf("direction enum = %v, want 3 values", dir.Enum)
	}
	if dir.Default != "downstream" {
		t.Errorf("direction default = %#v", dir.Default)
	}

	if s.Properties["flag"].Default != true {
		t.Errorf("flag default = %#v, want bool true", s.Properties["flag"].Default)
	}
}

// The emitted JSON must carry the constraints, since that is what a client
// actually reads.
func TestJSONSchemaTag_ConstraintsSurviveMarshalling(t *testing.T) {
	type args struct {
		Depth int `json:"depth" jsonschema:"description=Depth, capped,minimum=1,maximum=5"`
	}
	s, err := Generate(args{})
	if err != nil {
		t.Fatalf("GenerateFromType: %v", err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"minimum":1`, `"maximum":5`, `"description":"Depth, capped"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("emitted schema missing %s\n  got: %s", want, raw)
		}
	}
}
