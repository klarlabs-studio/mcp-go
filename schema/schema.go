// Package schema provides JSON Schema generation from Go types.
package schema

import (
	"encoding/json"
	"reflect"
	"strings"
)

const tagRequired = "required"

// Schema represents a JSON Schema.
//
// AdditionalProperties is encoded only when explicitly set. For struct-derived
// schemas it is set to bool(false) so the resulting JSON satisfies OpenAI
// strict tool-calling, which requires closed objects. Map-derived schemas
// leave it unset so they remain open.
type Schema struct {
	Type                 string             `json:"type,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties any                `json:"-"`
	Required             []string           `json:"required,omitempty"`
	Description          string             `json:"description,omitempty"`
	Default              any                `json:"default,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
}

// MarshalJSON encodes the schema. For object-typed schemas it forces the
// "properties" key to be present (emitting `{}` when empty) and writes
// AdditionalProperties when set.
//
// Why: OpenAI's strict function-calling mode rejects object schemas that
// omit "properties" with the error
// `object schema missing properties. (format)`, which would otherwise
// break any tool whose handler input is `struct{}`. Forcing properties
// to materialize removes the footgun for downstream consumers.
func (s Schema) MarshalJSON() ([]byte, error) {
	if s.Type != typeObject {
		type plain Schema
		return json.Marshal(plain(s))
	}

	out := map[string]any{"type": typeObject}
	props := s.Properties
	if props == nil {
		props = map[string]*Schema{}
	}
	out["properties"] = props
	if s.AdditionalProperties != nil {
		out["additionalProperties"] = s.AdditionalProperties
	}
	if len(s.Required) > 0 {
		out[tagRequired] = s.Required
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Default != nil {
		out["default"] = s.Default
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Minimum != nil {
		out["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		out["maximum"] = *s.Maximum
	}
	if s.Items != nil {
		out["items"] = s.Items
	}
	return json.Marshal(out)
}

// Generate creates a JSON Schema from a Go value.
func Generate(v any) (*Schema, error) {
	t := reflect.TypeOf(v)
	return generateFromType(t)
}

// GenerateFromType creates a JSON Schema from a reflect.Type.
func GenerateFromType(t reflect.Type) (*Schema, error) {
	return generateFromType(t)
}

func generateFromType(t reflect.Type) (*Schema, error) {
	// Handle pointers
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		return generateStructSchema(t)
	case reflect.String:
		return &Schema{Type: typeString}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: typeInteger}, nil
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: typeNumber}, nil
	case reflect.Bool:
		return &Schema{Type: typeBoolean}, nil
	case reflect.Slice, reflect.Array:
		return generateArraySchema(t)
	case reflect.Map:
		return &Schema{Type: typeObject}, nil
	default:
		return &Schema{}, nil
	}
}

func generateStructSchema(t reflect.Type) (*Schema, error) {
	// AdditionalProperties: false marks the object as closed so OpenAI
	// strict tool-calling accepts the schema. Maps, which can grow at
	// runtime, leave AdditionalProperties unset (handled separately).
	schema := &Schema{
		Type:                 typeObject,
		Properties:           make(map[string]*Schema),
		AdditionalProperties: false,
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get JSON field name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		fieldName := field.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				fieldName = parts[0]
			}
		}

		// Generate field schema
		fieldSchema, err := generateFromType(field.Type)
		if err != nil {
			return nil, err
		}

		// Parse jsonschema tag
		parseJSONSchemaTag(field.Tag.Get("jsonschema"), fieldSchema, &schema.Required, fieldName)

		schema.Properties[fieldName] = fieldSchema
	}

	return schema, nil
}

func generateArraySchema(t reflect.Type) (*Schema, error) {
	itemSchema, err := generateFromType(t.Elem())
	if err != nil {
		return nil, err
	}

	return &Schema{
		Type:  typeArray,
		Items: itemSchema,
	}, nil
}

func parseJSONSchemaTag(tag string, schema *Schema, required *[]string, fieldName string) {
	if tag == "" {
		return
	}

	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == tagRequired {
			*required = append(*required, fieldName)
			continue
		}

		if strings.HasPrefix(part, "description=") {
			schema.Description = strings.TrimPrefix(part, "description=")
			continue
		}

		// Add more tag parsing as needed (minimum, maximum, enum, etc.)
	}
}
