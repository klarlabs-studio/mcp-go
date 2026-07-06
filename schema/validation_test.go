package schema

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSchema_Validate(t *testing.T) {
	t.Run("validates required fields", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"name":  {Type: "string"},
				"email": {Type: "string"},
			},
			Required: []string{"name", "email"},
		}

		// Valid case
		valid := json.RawMessage(`{"name": "John", "email": "john@example.com"}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Missing required field
		invalid := json.RawMessage(`{"name": "John"}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error")
		}
		if !strings.Contains(err.Error(), "email") {
			t.Errorf("expected error about 'email', got: %v", err)
		}
	})

	t.Run("validates string type", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"name": {Type: "string"},
			},
		}

		// Valid string
		valid := json.RawMessage(`{"name": "John"}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Invalid type
		invalid := json.RawMessage(`{"name": 123}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error")
		}
		if !strings.Contains(err.Error(), "expected string") {
			t.Errorf("expected type error, got: %v", err)
		}
	})

	t.Run("validates integer type", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"age": {Type: "integer"},
			},
		}

		// Valid integer
		valid := json.RawMessage(`{"age": 25}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Invalid: decimal
		invalid := json.RawMessage(`{"age": 25.5}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error for decimal")
		}

		// Invalid: string
		invalid2 := json.RawMessage(`{"age": "twenty-five"}`)
		err = schema.Validate(invalid2)
		if err == nil {
			t.Error("expected validation error for string")
		}
	})

	t.Run("validates number type", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"price": {Type: "number"},
			},
		}

		// Valid number
		valid := json.RawMessage(`{"price": 19.99}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Integer is also valid for number
		valid2 := json.RawMessage(`{"price": 20}`)
		if err := schema.Validate(valid2); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
	})

	t.Run("validates boolean type", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"active": {Type: "boolean"},
			},
		}

		// Valid boolean
		valid := json.RawMessage(`{"active": true}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Invalid: string
		invalid := json.RawMessage(`{"active": "yes"}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error")
		}
	})

	t.Run("validates array type", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"tags": {
					Type:  "array",
					Items: &Schema{Type: "string"},
				},
			},
		}

		// Valid array
		valid := json.RawMessage(`{"tags": ["a", "b", "c"]}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Invalid item type
		invalid := json.RawMessage(`{"tags": ["a", 123, "c"]}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error")
		}
		if !strings.Contains(err.Error(), "tags[1]") {
			t.Errorf("expected error with path 'tags[1]', got: %v", err)
		}
	})

	t.Run("validates nested objects", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"user": {
					Type: "object",
					Properties: map[string]*Schema{
						"email": {Type: "string"},
					},
					Required: []string{"email"},
				},
			},
		}

		// Valid nested
		valid := json.RawMessage(`{"user": {"email": "test@example.com"}}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Missing nested required
		invalid := json.RawMessage(`{"user": {}}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error")
		}
		if !strings.Contains(err.Error(), "user.email") {
			t.Errorf("expected error with path 'user.email', got: %v", err)
		}
	})

	t.Run("validates minimum/maximum", func(t *testing.T) {
		minVal := 0.0
		maxVal := 100.0
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"score": {
					Type:    "number",
					Minimum: &minVal,
					Maximum: &maxVal,
				},
			},
		}

		// Valid
		valid := json.RawMessage(`{"score": 50}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Below minimum
		belowMin := json.RawMessage(`{"score": -10}`)
		err := schema.Validate(belowMin)
		if err == nil {
			t.Error("expected validation error for below minimum")
		}
		if !strings.Contains(err.Error(), "minimum") {
			t.Errorf("expected minimum error, got: %v", err)
		}

		// Above maximum
		aboveMax := json.RawMessage(`{"score": 150}`)
		err = schema.Validate(aboveMax)
		if err == nil {
			t.Error("expected validation error for above maximum")
		}
		if !strings.Contains(err.Error(), "maximum") {
			t.Errorf("expected maximum error, got: %v", err)
		}
	})

	t.Run("validates enum", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"status": {
					Type: "string",
					Enum: []any{"pending", "active", "completed"},
				},
			},
		}

		// Valid enum value
		valid := json.RawMessage(`{"status": "active"}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}

		// Invalid enum value
		invalid := json.RawMessage(`{"status": "unknown"}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected validation error")
		}
		if !strings.Contains(err.Error(), "must be one of") {
			t.Errorf("expected enum error, got: %v", err)
		}
	})

	t.Run("handles invalid JSON", func(t *testing.T) {
		schema := &Schema{Type: "object"}

		invalid := json.RawMessage(`{invalid json}`)
		err := schema.Validate(invalid)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
		if !strings.Contains(err.Error(), "invalid JSON") {
			t.Errorf("expected JSON error, got: %v", err)
		}
	})

	t.Run("handles null values", func(t *testing.T) {
		schema := &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"optional": {Type: "string"},
			},
		}

		// null value should be valid
		valid := json.RawMessage(`{"optional": null}`)
		if err := schema.Validate(valid); err != nil {
			t.Errorf("expected valid for null, got error: %v", err)
		}
	})
}

func TestValidationErrors(t *testing.T) {
	t.Run("single error format", func(t *testing.T) {
		errs := ValidationErrors{
			{Path: "name", Message: "required field is missing"},
		}
		expected := "name: required field is missing"
		if errs.Error() != expected {
			t.Errorf("expected %q, got %q", expected, errs.Error())
		}
	})

	t.Run("multiple errors format", func(t *testing.T) {
		errs := ValidationErrors{
			{Path: "name", Message: "required"},
			{Path: "email", Message: "invalid format"},
		}
		result := errs.Error()
		if !strings.Contains(result, "name: required") {
			t.Errorf("expected 'name: required' in output, got: %s", result)
		}
		if !strings.Contains(result, "email: invalid format") {
			t.Errorf("expected 'email: invalid format' in output, got: %s", result)
		}
	})

	t.Run("empty errors", func(t *testing.T) {
		errs := ValidationErrors{}
		if errs.Error() != "" {
			t.Errorf("expected empty string, got %q", errs.Error())
		}
	})
}

func TestCheckJSONDepth(t *testing.T) {
	t.Run("accepts moderately nested JSON", func(t *testing.T) {
		data := []byte(`{"a":{"b":{"c":[1,2,3]}}}`)
		if err := checkJSONDepth(data); err != nil {
			t.Errorf("unexpected error for shallow JSON: %v", err)
		}
	})

	t.Run("ignores braces inside strings", func(t *testing.T) {
		// A string full of brackets must not count toward nesting depth.
		data := []byte(`{"note":"[[[[[[[[[[ not real nesting ]]]]]]]]]]"}`)
		if err := checkJSONDepth(data); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("handles escaped quotes inside strings", func(t *testing.T) {
		data := []byte(`{"q":"a \" [ b"}`)
		if err := checkJSONDepth(data); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects input beyond max depth", func(t *testing.T) {
		data := append(bytes.Repeat([]byte("["), maxJSONDepth+1), bytes.Repeat([]byte("]"), maxJSONDepth+1)...)
		if err := checkJSONDepth(data); err == nil {
			t.Fatal("expected error for over-deep JSON, got nil")
		}
	})
}

func TestSchema_Validate_DeepNesting(t *testing.T) {
	// A frame of deeply nested arrays must be rejected as a parse error rather
	// than driving encoding/json into stack exhaustion (an uncatchable fatal).
	schema := &Schema{Type: typeObject}

	deep := 5000
	data := append(bytes.Repeat([]byte("["), deep), bytes.Repeat([]byte("]"), deep)...)

	err := schema.Validate(data)
	if err == nil {
		t.Fatal("expected validation error for deeply nested JSON, got nil")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected nesting-depth error, got: %v", err)
	}
}
