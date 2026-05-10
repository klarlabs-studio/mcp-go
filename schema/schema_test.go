package schema

import (
	"encoding/json"
	"testing"
)

func TestGenerate(t *testing.T) {
	t.Run("generates schema for simple struct", func(t *testing.T) {
		type Input struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if schema.Type != "object" {
			t.Errorf("Type = %q, want %q", schema.Type, "object")
		}

		if len(schema.Properties) != 2 {
			t.Fatalf("expected 2 properties, got %d", len(schema.Properties))
		}

		nameProp, ok := schema.Properties["name"]
		if !ok {
			t.Fatal("expected 'name' property")
		}
		if nameProp.Type != "string" {
			t.Errorf("name.Type = %q, want %q", nameProp.Type, "string")
		}

		ageProp, ok := schema.Properties["age"]
		if !ok {
			t.Fatal("expected 'age' property")
		}
		if ageProp.Type != "integer" {
			t.Errorf("age.Type = %q, want %q", ageProp.Type, "integer")
		}
	})

	t.Run("handles required fields", func(t *testing.T) {
		type Input struct {
			Required string `json:"required" jsonschema:"required"`
			Optional string `json:"optional"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(schema.Required) != 1 {
			t.Fatalf("expected 1 required field, got %d", len(schema.Required))
		}

		if schema.Required[0] != "required" {
			t.Errorf("Required[0] = %q, want %q", schema.Required[0], "required")
		}
	})

	t.Run("handles description", func(t *testing.T) {
		type Input struct {
			Query string `json:"query" jsonschema:"description=Search query string"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		queryProp := schema.Properties["query"]
		if queryProp.Description != "Search query string" {
			t.Errorf("Description = %q, want %q", queryProp.Description, "Search query string")
		}
	})

	t.Run("handles nested structs", func(t *testing.T) {
		type Address struct {
			City    string `json:"city"`
			Country string `json:"country"`
		}
		type Person struct {
			Name    string  `json:"name"`
			Address Address `json:"address"`
		}

		schema, err := Generate(Person{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		addrProp, ok := schema.Properties["address"]
		if !ok {
			t.Fatal("expected 'address' property")
		}

		if addrProp.Type != "object" {
			t.Errorf("address.Type = %q, want %q", addrProp.Type, "object")
		}

		if len(addrProp.Properties) != 2 {
			t.Errorf("expected 2 address properties, got %d", len(addrProp.Properties))
		}
	})

	t.Run("handles slices", func(t *testing.T) {
		type Input struct {
			Tags []string `json:"tags"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tagsProp := schema.Properties["tags"]
		if tagsProp.Type != "array" {
			t.Errorf("tags.Type = %q, want %q", tagsProp.Type, "array")
		}

		if tagsProp.Items == nil {
			t.Fatal("expected Items to be set for array")
		}

		if tagsProp.Items.Type != "string" {
			t.Errorf("tags.Items.Type = %q, want %q", tagsProp.Items.Type, "string")
		}
	})

	t.Run("handles boolean", func(t *testing.T) {
		type Input struct {
			Active bool `json:"active"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		activeProp := schema.Properties["active"]
		if activeProp.Type != "boolean" {
			t.Errorf("active.Type = %q, want %q", activeProp.Type, "boolean")
		}
	})

	t.Run("handles float", func(t *testing.T) {
		type Input struct {
			Price float64 `json:"price"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		priceProp := schema.Properties["price"]
		if priceProp.Type != "number" {
			t.Errorf("price.Type = %q, want %q", priceProp.Type, "number")
		}
	})

	t.Run("handles pointers", func(t *testing.T) {
		type Input struct {
			Value *string `json:"value"`
		}

		schema, err := Generate(Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		valueProp := schema.Properties["value"]
		if valueProp.Type != "string" {
			t.Errorf("value.Type = %q, want %q", valueProp.Type, "string")
		}
	})
}

func TestSchema_MarshalJSON(t *testing.T) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"name": {Type: "string", Description: "The name"},
		},
		Required: []string{"name"},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result["type"] != "object" {
		t.Errorf("type = %v, want %q", result["type"], "object")
	}
}

// TestSchema_EmptyStructIsStrictModeCompatible regression-tests the
// OpenAI strict tool-calling failure where zero-arg handlers produced
// `{"type":"object"}` and were rejected with
// `object schema missing properties. (format)`.
//
// The encoded schema for an empty struct must:
//   - include a "properties" key (empty object), not omit it
//   - declare additionalProperties: false (closed object)
func TestSchema_EmptyStructIsStrictModeCompatible(t *testing.T) {
	type Empty struct{}

	s, err := Generate(Empty{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	props, ok := got["properties"]
	if !ok {
		t.Fatalf("properties key missing; raw=%s", data)
	}
	if pm, ok := props.(map[string]any); !ok || len(pm) != 0 {
		t.Errorf("properties = %v, want empty object", props)
	}
	if got["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false; raw=%s", got["additionalProperties"], data)
	}
	if got["type"] != "object" {
		t.Errorf("type = %v, want object", got["type"])
	}
}

// TestSchema_StructIsClosed verifies non-empty struct schemas are also
// closed (additionalProperties: false) so they pass strict mode.
func TestSchema_StructIsClosed(t *testing.T) {
	type Input struct {
		Name string `json:"name"`
	}

	s, err := Generate(Input{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false; raw=%s", got["additionalProperties"], data)
	}
}

// TestSchema_NonObjectUnaffected ensures string/number/etc. schemas
// don't pick up spurious properties keys from the object-only path.
func TestSchema_NonObjectUnaffected(t *testing.T) {
	s := &Schema{Type: "string"}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, has := got["properties"]; has {
		t.Errorf("string schema should not emit properties; raw=%s", data)
	}
	if _, has := got["additionalProperties"]; has {
		t.Errorf("string schema should not emit additionalProperties; raw=%s", data)
	}
}
