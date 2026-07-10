// Package schema provides JSON Schema generation from Go types.
//
// This package automatically generates JSON Schema definitions from Go structs,
// supporting common Go types and struct tags for customization.
//
// # Basic Usage
//
// Generate a schema from a Go value:
//
//	type Person struct {
//	    Name string `json:"name" jsonschema:"required"`
//	    Age  int    `json:"age"`
//	}
//
//	schema, err := schema.Generate(Person{})
//
// # Supported Types
//
// The generator supports the following Go types:
//
//   - Structs: Converted to JSON objects with properties
//   - Strings: Converted to JSON string type
//   - Integers (all sizes): Converted to JSON integer type
//   - Floats: Converted to JSON number type
//   - Booleans: Converted to JSON boolean type
//   - Slices/Arrays: Converted to JSON array type
//   - Maps: Converted to JSON object type
//   - Pointers: Dereferenced and converted based on element type
//
// # Struct Tags
//
// The package recognizes the following struct tags:
//
//	type Example struct {
//	    // json tag controls field name
//	    Name string `json:"name"`
//
//	    // jsonschema:"required" marks field as required
//	    Required string `json:"required" jsonschema:"required"`
//
//	    // jsonschema:"description=..." adds description
//	    Desc string `json:"desc" jsonschema:"description=Field description"`
//
//	    // json:"-" excludes field
//	    Ignored string `json:"-"`
//	}
//
// # Generated Schema
//
// The Schema type represents a JSON Schema:
//
//	type Schema struct {
//	    Dialect     string             `json:"$schema,omitempty"`
//	    Type        string             `json:"type,omitempty"`
//	    Properties  map[string]*Schema `json:"properties,omitempty"`
//	    Required    []string           `json:"required,omitempty"`
//	    Description string             `json:"description,omitempty"`
//	    Items       *Schema            `json:"items,omitempty"`
//	}
//
// # Dialect
//
// Root schemas produced by Generate and GenerateFromType advertise the JSON
// Schema 2020-12 dialect via the "$schema" keyword (see Dialect2020_12), the
// default dialect required by the MCP specification revision 2025-11-25
// (SEP-1613). The marker is set only on the document root; nested sub-schemas
// leave it empty per JSON Schema convention.
//
// # 2020-12 Referencing and Composition
//
// The Schema type carries the full 2020-12 referencing and composition
// vocabulary so that inputSchema/outputSchema are not limited to the flat
// object/array subset:
//
//   - $ref / $defs — Ref points at a reusable definition inside the root
//     document's Defs map (e.g. "#/$defs/Node").
//   - oneOf / anyOf / allOf — boolean combinators (OneOf, AnyOf, AllOf).
//   - if / then / else — the conditional applicator (If, Then, Else).
//
// Generation emits $ref/$defs automatically to break recursive Go types:
// when a struct type refers back to itself (directly or transitively) the
// self-reference becomes a "#/$defs/<TypeName>" pointer and the type's
// definition is hoisted into the root schema's $defs, so a recursive type no
// longer inlines forever. The combinator and conditional keywords are
// author-supplied (reflection cannot infer a discriminated union from a Go
// type); the struct marshals and validates against all of them.
//
// The runtime validator understands every one of these keywords: it resolves
// $ref against the root $defs, enforces allOf (all branches), anyOf (at least
// one), oneOf (exactly one), and if/then/else. Unresolvable references are
// treated leniently and never reject otherwise-valid input.
package schema
