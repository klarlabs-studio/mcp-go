package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// recNode is a self-referential type used to prove that generation breaks
// recursion with $ref/$defs rather than inlining forever.
type recNode struct {
	Value    string    `json:"value"`
	Children []recNode `json:"children"`
}

// TestGenerate_RecursiveTypeEmitsDefs verifies that a self-referential Go type
// generates a "#/$defs/..." reference plus a hoisted $defs entry instead of
// inlining without bound.
func TestGenerate_RecursiveTypeEmitsDefs(t *testing.T) {
	s, err := Generate(recNode{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The root type is itself recursive, so it is expressed as a reference
	// into $defs.
	if s.Ref != defRefPrefix+"recNode" {
		t.Errorf("root Ref = %q, want %q", s.Ref, defRefPrefix+"recNode")
	}
	if s.Dialect != Dialect2020_12 {
		t.Errorf("root Dialect = %q, want %q", s.Dialect, Dialect2020_12)
	}

	def, ok := s.Defs["recNode"]
	if !ok {
		t.Fatalf("expected $defs[recNode]; defs=%v", s.Defs)
	}
	if def.Type != typeObject {
		t.Errorf("def.Type = %q, want object", def.Type)
	}
	// The definition must not carry the dialect marker (root-only convention).
	if def.Dialect != "" {
		t.Errorf("def.Dialect = %q, want empty", def.Dialect)
	}

	// The recursive field points back into $defs.
	children := def.Properties["children"]
	if children == nil || children.Items == nil {
		t.Fatalf("children/items missing: %+v", def.Properties)
	}
	if children.Items.Ref != defRefPrefix+"recNode" {
		t.Errorf("children.items.Ref = %q, want %q", children.Items.Ref, defRefPrefix+"recNode")
	}

	// Marshaled JSON must round-trip the reference and definitions and must
	// not force a "properties" key onto the pure-reference root.
	raw := marshalToMap(t, s)
	if raw["$ref"] != defRefPrefix+"recNode" {
		t.Errorf("marshaled $ref = %v, want %q", raw["$ref"], defRefPrefix+"recNode")
	}
	if _, has := raw["properties"]; has {
		t.Errorf("reference root must not emit properties; raw=%v", raw)
	}
	if _, has := raw["$defs"]; !has {
		t.Errorf("marshaled root must emit $defs; raw=%v", raw)
	}
}

// TestGenerate_NestedRecursiveTypeHoistsDefs verifies recursion breaking when
// the recursive type is a nested field rather than the root type: the root
// stays a plain object and the recursive type is hoisted into $defs.
func TestGenerate_NestedRecursiveTypeHoistsDefs(t *testing.T) {
	type tree struct {
		Root recNode `json:"root"`
	}

	s, err := Generate(tree{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if s.Type != typeObject {
		t.Errorf("root Type = %q, want object", s.Type)
	}
	if _, ok := s.Defs["recNode"]; !ok {
		t.Fatalf("expected $defs[recNode]; defs=%v", s.Defs)
	}
	root := s.Properties["root"]
	if root == nil || root.Ref != defRefPrefix+"recNode" {
		t.Errorf("root property Ref = %+v, want reference to recNode", root)
	}
}

// TestSchema_MarshalCompositionKeywords proves the struct marshals the full
// 2020-12 composition vocabulary and never forces "properties" onto a
// combinator schema.
func TestSchema_MarshalCompositionKeywords(t *testing.T) {
	tests := []struct {
		name   string
		schema *Schema
		key    string
		count  int
	}{
		{
			name: "oneOf",
			schema: &Schema{OneOf: []*Schema{
				{Type: typeString}, {Type: typeInteger},
			}},
			key:   "oneOf",
			count: 2,
		},
		{
			name: "anyOf",
			schema: &Schema{AnyOf: []*Schema{
				{Type: typeString}, {Type: typeBoolean},
			}},
			key:   "anyOf",
			count: 2,
		},
		{
			name: "allOf",
			schema: &Schema{AllOf: []*Schema{
				{Type: typeObject}, {Required: []string{"id"}},
			}},
			key:   "allOf",
			count: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := marshalToMap(t, tt.schema)
			arr, ok := raw[tt.key].([]any)
			if !ok {
				t.Fatalf("%s = %v, want array", tt.key, raw[tt.key])
			}
			if len(arr) != tt.count {
				t.Errorf("len(%s) = %d, want %d", tt.key, len(arr), tt.count)
			}
			// Combinators are not plain objects: no forced properties.
			if _, has := raw["properties"]; has {
				t.Errorf("combinator must not emit properties; raw=%v", raw)
			}
		})
	}
}

// TestSchema_MarshalIfThenElse verifies the conditional applicator round-trips.
func TestSchema_MarshalIfThenElse(t *testing.T) {
	s := &Schema{
		Type: typeObject,
		Properties: map[string]*Schema{
			"kind": {Type: typeString},
		},
		If:   &Schema{Properties: map[string]*Schema{"kind": {Enum: []any{"a"}}}},
		Then: &Schema{Required: []string{"a"}},
		Else: &Schema{Required: []string{"b"}},
	}

	raw := marshalToMap(t, s)
	for _, key := range []string{"if", "then", "else"} {
		if _, has := raw[key]; !has {
			t.Errorf("missing %q in marshaled schema; raw=%v", key, raw)
		}
	}
	// A plain object that also carries if/then/else still gets properties.
	if _, has := raw["properties"]; !has {
		t.Errorf("object schema must still emit properties; raw=%v", raw)
	}
}

// TestValidate_Ref confirms the validator resolves $ref against $defs and
// enforces the referenced definition (both accept and reject paths), and that
// recursive references terminate on finite data.
func TestValidate_Ref(t *testing.T) {
	s, err := Generate(recNode{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	valid := json.RawMessage(`{"value":"root","children":[{"value":"leaf","children":[]}]}`)
	if err := s.Validate(valid); err != nil {
		t.Errorf("expected valid recursive input, got: %v", err)
	}

	// A wrong type deep inside a referenced definition must be caught.
	invalid := json.RawMessage(`{"value":123,"children":[]}`)
	if err := s.Validate(invalid); err == nil {
		t.Error("expected validation error for wrong-typed value field")
	}
}

// TestValidate_UnresolvableRefIsLenient documents that an author-supplied
// reference the validator cannot resolve never rejects valid input.
func TestValidate_UnresolvableRefIsLenient(t *testing.T) {
	s := &Schema{Ref: "#/$defs/Missing"}
	if err := s.Validate(json.RawMessage(`{"anything":true}`)); err != nil {
		t.Errorf("unresolvable $ref must be lenient, got: %v", err)
	}
}

// TestValidate_OneOf enforces the exactly-one semantics of oneOf.
func TestValidate_OneOf(t *testing.T) {
	s := &Schema{OneOf: []*Schema{
		{Type: typeString},
		{Type: typeInteger},
	}}

	if err := s.Validate(json.RawMessage(`"hello"`)); err != nil {
		t.Errorf("string should match exactly one branch, got: %v", err)
	}
	if err := s.Validate(json.RawMessage(`42`)); err != nil {
		t.Errorf("integer should match exactly one branch, got: %v", err)
	}
	// Matches neither branch.
	if err := s.Validate(json.RawMessage(`true`)); err == nil {
		t.Error("expected oneOf failure when no branch matches")
	}
}

// TestValidate_AnyOf enforces the at-least-one semantics of anyOf.
func TestValidate_AnyOf(t *testing.T) {
	s := &Schema{AnyOf: []*Schema{
		{Type: typeString},
		{Type: typeBoolean},
	}}

	if err := s.Validate(json.RawMessage(`"ok"`)); err != nil {
		t.Errorf("string should satisfy anyOf, got: %v", err)
	}
	if err := s.Validate(json.RawMessage(`10`)); err == nil {
		t.Error("expected anyOf failure when no branch matches")
	}
}

// TestValidate_AllOf enforces that every branch must pass.
func TestValidate_AllOf(t *testing.T) {
	s := &Schema{AllOf: []*Schema{
		{Type: typeObject, Properties: map[string]*Schema{"id": {Type: typeString}}},
		{Type: typeObject, Required: []string{"id"}},
	}}

	if err := s.Validate(json.RawMessage(`{"id":"x"}`)); err != nil {
		t.Errorf("object satisfying all branches should pass, got: %v", err)
	}
	// Missing required "id" fails the second branch.
	if err := s.Validate(json.RawMessage(`{}`)); err == nil {
		t.Error("expected allOf failure when a branch is unsatisfied")
	}
	// Wrong type for "id" fails the first branch.
	err := s.Validate(json.RawMessage(`{"id":123}`))
	if err == nil || !strings.Contains(err.Error(), "expected string") {
		t.Errorf("expected string type failure from allOf branch, got: %v", err)
	}
}

// TestValidate_IfThenElse enforces the conditional applicator on both the
// then and else paths.
func TestValidate_IfThenElse(t *testing.T) {
	// If "kind" == "email", require "address"; otherwise require "phone".
	s := &Schema{
		Type: typeObject,
		Properties: map[string]*Schema{
			"kind":    {Type: typeString},
			"address": {Type: typeString},
			"phone":   {Type: typeString},
		},
		If: &Schema{
			Type:       typeObject,
			Properties: map[string]*Schema{"kind": {Type: typeString, Enum: []any{"email"}}},
		},
		Then: &Schema{Type: typeObject, Required: []string{"address"}},
		Else: &Schema{Type: typeObject, Required: []string{"phone"}},
	}

	if err := s.Validate(json.RawMessage(`{"kind":"email","address":"a@b.com"}`)); err != nil {
		t.Errorf("then branch should pass, got: %v", err)
	}
	if err := s.Validate(json.RawMessage(`{"kind":"email"}`)); err == nil {
		t.Error("expected then-branch failure: address required")
	}
	if err := s.Validate(json.RawMessage(`{"kind":"sms","phone":"123"}`)); err != nil {
		t.Errorf("else branch should pass, got: %v", err)
	}
	if err := s.Validate(json.RawMessage(`{"kind":"sms"}`)); err == nil {
		t.Error("expected else-branch failure: phone required")
	}
}
