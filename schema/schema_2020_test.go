package schema

import (
	"encoding/json"
	"testing"
)

// TestDialect2020_12Constant pins the exported dialect identifier to the
// value mandated by the MCP spec revision 2025-11-25 (SEP-1613).
func TestDialect2020_12Constant(t *testing.T) {
	const want = "https://json-schema.org/draft/2020-12/schema"
	if Dialect2020_12 != want {
		t.Errorf("Dialect2020_12 = %q, want %q", Dialect2020_12, want)
	}
}

// TestGenerate_EmitsDialectMarker verifies that root schemas produced by the
// public entry points advertise the 2020-12 dialect via "$schema".
func TestGenerate_EmitsDialectMarker(t *testing.T) {
	type Input struct {
		Name string `json:"name" jsonschema:"required"`
	}

	t.Run("Generate sets $schema on object root", func(t *testing.T) {
		s, err := Generate(Input{})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if s.Dialect != Dialect2020_12 {
			t.Errorf("Dialect = %q, want %q", s.Dialect, Dialect2020_12)
		}

		got := marshalToMap(t, s)
		if got["$schema"] != Dialect2020_12 {
			t.Errorf("$schema = %v, want %q; raw keys=%v", got["$schema"], Dialect2020_12, got)
		}
		// Backward-compat: existing keys must be preserved.
		if got["type"] != "object" {
			t.Errorf("type = %v, want object", got["type"])
		}
		if got["additionalProperties"] != false {
			t.Errorf("additionalProperties = %v, want false", got["additionalProperties"])
		}
		if _, ok := got["properties"]; !ok {
			t.Errorf("properties key missing; raw=%v", got)
		}
	})

	t.Run("Generate sets $schema on non-object root", func(t *testing.T) {
		s, err := Generate([]string{})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if s.Dialect != Dialect2020_12 {
			t.Errorf("Dialect = %q, want %q", s.Dialect, Dialect2020_12)
		}
		got := marshalToMap(t, s)
		if got["$schema"] != Dialect2020_12 {
			t.Errorf("$schema = %v, want %q; raw=%v", got["$schema"], Dialect2020_12, got)
		}
		if got["type"] != "array" {
			t.Errorf("type = %v, want array", got["type"])
		}
	})
}

// TestGenerate_DialectOnlyOnRoot ensures the dialect marker is declared once
// at the document root and never leaks into nested sub-schemas, matching JSON
// Schema convention.
func TestGenerate_DialectOnlyOnRoot(t *testing.T) {
	type Address struct {
		City string `json:"city"`
	}
	type Person struct {
		Name    string   `json:"name"`
		Tags    []string `json:"tags"`
		Address Address  `json:"address"`
	}

	s, err := Generate(Person{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Nested object schema must not carry a dialect marker.
	addr := s.Properties["address"]
	if addr.Dialect != "" {
		t.Errorf("nested address.Dialect = %q, want empty", addr.Dialect)
	}
	addrMap := marshalToMap(t, addr)
	if _, has := addrMap["$schema"]; has {
		t.Errorf("nested object schema must not emit $schema; raw=%v", addrMap)
	}

	// Array item schema must not carry a dialect marker.
	tags := s.Properties["tags"]
	if tags.Items.Dialect != "" {
		t.Errorf("array item Dialect = %q, want empty", tags.Items.Dialect)
	}
}

// TestGenerate_Array2020Keywords documents that array schemas use the "items"
// keyword with a single sub-schema. In JSON Schema 2020-12 a single-schema
// "items" validates every element (list validation); "prefixItems" is only
// used for tuple validation, which homogeneous Go slices/arrays never produce.
// This keyword usage is therefore already 2020-12 compliant.
func TestGenerate_Array2020Keywords(t *testing.T) {
	type Input struct {
		Tags []string `json:"tags"`
	}
	s, err := Generate(Input{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tags := marshalToMap(t, s.Properties["tags"])
	if tags["type"] != "array" {
		t.Errorf("type = %v, want array", tags["type"])
	}
	items, ok := tags["items"].(map[string]any)
	if !ok {
		t.Fatalf("items = %v, want a single sub-schema object", tags["items"])
	}
	if items["type"] != "string" {
		t.Errorf("items.type = %v, want string", items["type"])
	}
	// 2020-12: tuple-only keyword must not appear for homogeneous arrays.
	if _, has := tags["prefixItems"]; has {
		t.Errorf("homogeneous array must not emit prefixItems; raw=%v", tags)
	}
}

func marshalToMap(t *testing.T, s *Schema) map[string]any {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return got
}
