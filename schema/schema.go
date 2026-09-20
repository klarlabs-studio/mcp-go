// Package schema provides JSON Schema generation from Go types.
package schema

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// timeType is cached so the time.Time special case in generateFromType
// is a single pointer comparison rather than a string compare on every
// field traversal.
var timeType = reflect.TypeOf(time.Time{})

// formatDateTime is the JSON Schema "format" annotation used for
// time.Time values, which marshal as RFC3339 strings.
const formatDateTime = "date-time"

const tagRequired = "required"

// Dialect2020_12 is the JSON Schema 2020-12 dialect identifier. It is the
// default dialect mandated by the MCP specification revision 2025-11-25
// (SEP-1613) for tool inputSchema/outputSchema. Generated root schemas
// advertise it via the "$schema" keyword so strict validators select the
// correct dialect semantics.
const Dialect2020_12 = "https://json-schema.org/draft/2020-12/schema"

// Schema represents a JSON Schema.
//
// AdditionalProperties is encoded only when explicitly set. For struct-derived
// schemas it is set to bool(false) so the resulting JSON satisfies OpenAI
// strict tool-calling, which requires closed objects. Map-derived schemas
// leave it unset so they remain open.
//
// Dialect carries the "$schema" dialect marker and is set only on root
// schemas produced by Generate/GenerateFromType. Nested (sub-)schemas leave
// it empty, matching JSON Schema convention where the dialect is declared
// once at the document root.
type Schema struct {
	Dialect              string             `json:"$schema,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties any                `json:"-"`
	Required             []string           `json:"required,omitempty"`
	Description          string             `json:"description,omitempty"`
	Default              any                `json:"default,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	Items                *Schema            `json:"items,omitempty"`

	// XMcpHeader is the SEP-2243 annotation that mirrors this property into an
	// HTTP header named Mcp-Param-{value} on Streamable HTTP tools/call.
	// Set via `jsonschema:"header=Region"` (or the longer `mcp-header=` form).
	XMcpHeader string `json:"x-mcp-header,omitempty"`

	// JSON Schema 2020-12 referencing and composition keywords. These make
	// inputSchema/outputSchema express the full 2020-12 vocabulary rather than
	// the flat object/array subset. Ref points at a definition inside Defs
	// (e.g. "#/$defs/Node"); Defs is the document's reusable definition map and
	// is carried only on root schemas. OneOf/AnyOf/AllOf are the boolean
	// combinators, and If/Then/Else form a conditional applicator. All are
	// optional and omitted from JSON when unset.
	Ref   string             `json:"$ref,omitempty"`
	Defs  map[string]*Schema `json:"$defs,omitempty"`
	OneOf []*Schema          `json:"oneOf,omitempty"`
	AnyOf []*Schema          `json:"anyOf,omitempty"`
	AllOf []*Schema          `json:"allOf,omitempty"`
	If    *Schema            `json:"if,omitempty"`
	Then  *Schema            `json:"then,omitempty"`
	Else  *Schema            `json:"else,omitempty"`
}

// isComposition reports whether the schema is a reference or boolean
// combinator rather than a plain object. Such schemas must not have an empty
// "properties" key forced onto them by MarshalJSON: a $ref/oneOf/anyOf/allOf
// node validates by delegation, and injecting "properties":{} would both be
// meaningless and, for a closed ("additionalProperties":false) sibling,
// actively wrong.
func (s Schema) isComposition() bool {
	return s.Ref != "" || len(s.OneOf) > 0 || len(s.AnyOf) > 0 || len(s.AllOf) > 0
}

// MarshalJSON encodes the schema. For plain object-typed schemas it forces
// the "properties" key to be present (emitting `{}` when empty) and always
// writes AdditionalProperties when set.
//
// Why force properties: OpenAI's strict function-calling mode rejects object
// schemas that omit "properties" with the error
// `object schema missing properties. (format)`, which would otherwise break
// any tool whose handler input is `struct{}`. Forcing properties to
// materialize removes the footgun for downstream consumers.
//
// The encoder marshals every field through the struct tags (so 2020-12
// keywords such as $ref/$defs/oneOf/if round-trip automatically) and only
// patches in the two keys the tags cannot express: the forced "properties"
// and the "-"-tagged AdditionalProperties. Composition schemas
// ($ref/oneOf/anyOf/allOf) are deliberately excluded from the properties
// forcing so they are not mistaken for closed objects.
func (s Schema) MarshalJSON() ([]byte, error) {
	type plain Schema
	data, err := json.Marshal(plain(s))
	if err != nil {
		return nil, err
	}

	forceProps := s.Type == typeObject && !s.isComposition()
	if !forceProps && s.AdditionalProperties == nil {
		return data, nil
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	if forceProps {
		if _, ok := m["properties"]; !ok {
			m["properties"] = json.RawMessage(`{}`)
		}
	}
	if s.AdditionalProperties != nil {
		ap, err := json.Marshal(s.AdditionalProperties)
		if err != nil {
			return nil, err
		}
		m["additionalProperties"] = ap
	}
	return json.Marshal(m)
}

// Generate creates a JSON Schema from a Go value. The returned root schema
// advertises the JSON Schema 2020-12 dialect via the "$schema" keyword.
func Generate(v any) (*Schema, error) {
	t := reflect.TypeOf(v)
	s, err := generateFromType(t)
	if err != nil {
		return nil, err
	}
	s.Dialect = Dialect2020_12
	return s, nil
}

// GenerateFromType creates a JSON Schema from a reflect.Type. The returned
// root schema advertises the JSON Schema 2020-12 dialect via the "$schema"
// keyword.
func GenerateFromType(t reflect.Type) (*Schema, error) {
	s, err := generateFromType(t)
	if err != nil {
		return nil, err
	}
	s.Dialect = Dialect2020_12
	return s, nil
}

// defRefPrefix is the JSON-pointer prefix under which generated definitions
// are registered and referenced ("#/$defs/<TypeName>").
const defRefPrefix = "#/$defs/"

// defRef returns the "$ref" string that points at the definition for t.
func defRef(t reflect.Type) string {
	return defRefPrefix + t.Name()
}

// genContext threads the state needed to break recursive Go types across a
// single generation. visiting holds the struct types currently on the
// generation stack (a back-edge to one of them is a cycle); recursive records
// which of those were actually referenced recursively and therefore need a
// hoisted definition; defs collects those hoisted definitions, which are
// attached to the root schema's $defs.
type genContext struct {
	visiting  map[reflect.Type]struct{}
	recursive map[reflect.Type]bool
	defs      map[string]*Schema
}

func newGenContext() *genContext {
	return &genContext{
		visiting:  make(map[reflect.Type]struct{}),
		recursive: make(map[reflect.Type]bool),
		defs:      make(map[string]*Schema),
	}
}

func generateFromType(t reflect.Type) (*Schema, error) {
	ctx := newGenContext()
	s, err := ctx.gen(t)
	if err != nil {
		return nil, err
	}
	// Hoist any recursion-breaking definitions onto the root document so the
	// "#/$defs/..." references resolve.
	if len(ctx.defs) > 0 {
		s.Defs = ctx.defs
	}
	return s, nil
}

func (c *genContext) gen(t reflect.Type) (*Schema, error) {
	// Handle pointers
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// time.Time marshals as an RFC3339 string, not as a struct of its
	// internal fields. Treat it as a string-format date-time so the
	// generated schema agrees with the actual JSON output. Without this
	// special case, time.Time fields produce object schemas that any
	// strict MCP client rejects (`structuredContent does not match the
	// tool's output schema: data/... must be object`).
	if t == timeType {
		return &Schema{Type: typeString, Format: formatDateTime}, nil
	}

	switch t.Kind() {
	case reflect.Struct:
		return c.genStruct(t)
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
		return c.genArray(t)
	case reflect.Map:
		return &Schema{Type: typeObject}, nil
	default:
		return &Schema{}, nil
	}
}

func (c *genContext) genStruct(t reflect.Type) (*Schema, error) {
	// A back-edge to a type already on the stack is a cycle. Emit a $ref
	// instead of recursing forever, and flag the type so its definition is
	// hoisted into $defs once the outermost generation finishes.
	if _, onStack := c.visiting[t]; onStack {
		c.recursive[t] = true
		return &Schema{Ref: defRef(t)}, nil
	}

	c.visiting[t] = struct{}{}

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
		fieldSchema, err := c.gen(field.Type)
		if err != nil {
			return nil, err
		}

		// Parse jsonschema tag
		parseJSONSchemaTag(field.Tag.Get("jsonschema"), fieldSchema, &schema.Required, fieldName)

		schema.Properties[fieldName] = fieldSchema
	}

	delete(c.visiting, t)

	// If this type was referenced recursively, hoist it into $defs and return
	// a $ref to it (from every position, including the outermost). Keeping the
	// definition in exactly one place avoids sharing a pointer with the root,
	// which would otherwise let the root's dialect marker leak into $defs.
	if c.recursive[t] {
		c.defs[t.Name()] = schema
		return &Schema{Ref: defRef(t)}, nil
	}

	return schema, nil
}

func (c *genContext) genArray(t reflect.Type) (*Schema, error) {
	itemSchema, err := c.gen(t.Elem())
	if err != nil {
		return nil, err
	}

	return &Schema{
		Type:  typeArray,
		Items: itemSchema,
	}, nil
}

// directivePrefixes are the key= forms this parser understands. A comma-
// separated part that starts with none of them, and is not a bare directive,
// is prose belonging to the preceding description.
var directivePrefixes = []string{
	"description=",
	"minimum=",
	"maximum=",
	"default=",
	"enum=",
	"header=",
	"mcp-header=",
}

// parseJSONSchemaTag reads a `jsonschema:"..."` struct tag into schema.
//
// The tag is comma-separated, which collides with prose: a description like
//
//	jsonschema:"description=Maximum results to return (default 10, capped at 50)"
//
// splits into two parts, and the second is not a directive. This used to drop
// it, silently truncating the description at the first comma -- so a tool
// advertised "Maximum results to return (default 10" to its callers, cut
// mid-parenthesis. For an MCP server the description is the contract with the
// model, and the model has no way to notice it was cut.
//
// Rather than require escaping, a part matching no known directive is treated
// as a continuation of the description and rejoined with the comma it was split
// on. Well-formed tags parse exactly as before; previously-truncated ones now
// keep their text.
//
// minimum, maximum, default and enum were listed in a TODO here and accepted
// but discarded, so a field carrying them advertised no constraint at all.
// They populate their Schema fields now. enum takes "|" as its separator,
// since "," already delimits directives.
func parseJSONSchemaTag(tag string, schema *Schema, required *[]string, fieldName string) {
	if tag == "" {
		return
	}

	var lastDescription *string

	for _, part := range strings.Split(tag, ",") {
		trimmed := strings.TrimSpace(part)

		if trimmed == tagRequired {
			*required = append(*required, fieldName)
			lastDescription = nil
			continue
		}

		if !isDirective(trimmed) {
			// Prose that was split off a description. Restore the comma and
			// the original spacing; anything before the first description is
			// unattributable, so it is dropped as it always was.
			if lastDescription != nil {
				*lastDescription += "," + part
			}
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "description="):
			schema.Description = strings.TrimPrefix(trimmed, "description=")
			lastDescription = &schema.Description
		case strings.HasPrefix(trimmed, "minimum="):
			if v, err := strconv.ParseFloat(strings.TrimPrefix(trimmed, "minimum="), 64); err == nil {
				schema.Minimum = &v
			}
			lastDescription = nil
		case strings.HasPrefix(trimmed, "maximum="):
			if v, err := strconv.ParseFloat(strings.TrimPrefix(trimmed, "maximum="), 64); err == nil {
				schema.Maximum = &v
			}
			lastDescription = nil
		case strings.HasPrefix(trimmed, "default="):
			schema.Default = parseTagValue(strings.TrimPrefix(trimmed, "default="))
			lastDescription = nil
		case strings.HasPrefix(trimmed, "enum="):
			for _, v := range strings.Split(strings.TrimPrefix(trimmed, "enum="), "|") {
				if v = strings.TrimSpace(v); v != "" {
					schema.Enum = append(schema.Enum, parseTagValue(v))
				}
			}
			lastDescription = nil
		case strings.HasPrefix(trimmed, "header="):
			schema.XMcpHeader = strings.TrimPrefix(trimmed, "header=")
			lastDescription = nil
		case strings.HasPrefix(trimmed, "mcp-header="):
			schema.XMcpHeader = strings.TrimPrefix(trimmed, "mcp-header=")
			lastDescription = nil
		}
	}
}

// isDirective reports whether a tag part is something this parser acts on,
// rather than prose split off a description by a comma.
func isDirective(part string) bool {
	if part == tagRequired {
		return true
	}
	for _, prefix := range directivePrefixes {
		if strings.HasPrefix(part, prefix) {
			return true
		}
	}
	return false
}

// parseTagValue types a tag value so `default=4` on an int field encodes as 4
// rather than "4", which a strict validator would reject against type integer.
func parseTagValue(raw string) any {
	if b, err := strconv.ParseBool(raw); err == nil {
		return b
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	return raw
}
