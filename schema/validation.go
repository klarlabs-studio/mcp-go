package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Schema type constants.
const (
	typeObject  = "object"
	typeArray   = "array"
	typeString  = "string"
	typeInteger = "integer"
	typeNumber  = "number"
	typeBoolean = "boolean"
)

// maxJSONDepth bounds the nesting depth of untrusted JSON before it is handed
// to encoding/json. encoding/json decodes with unbounded recursion, so a
// deeply nested payload (e.g. "[[[[…") can exhaust the goroutine stack and
// crash the process with a fatal error that recover cannot catch. Rejecting
// over-deep input up front turns that DoS into an ordinary parse error.
const maxJSONDepth = 100

// checkJSONDepth performs a fast, allocation-free pre-scan of raw JSON,
// rejecting input whose container nesting ({ or [) exceeds maxJSONDepth.
// Bytes inside string literals (including escaped quotes) are ignored so that
// braces or brackets within strings do not count toward depth.
func checkJSONDepth(data []byte) error {
	depth := 0
	inString := false
	escaped := false

	for _, c := range data {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxJSONDepth {
				return &ValidationError{
					Message: fmt.Sprintf("invalid JSON: nesting depth exceeds maximum of %d", maxJSONDepth),
				}
			}
		case '}', ']':
			depth--
		}
	}

	return nil
}

// ValidationError represents a schema validation error.
type ValidationError struct {
	Path    string // JSON path to the invalid field (e.g., "user.email")
	Message string // Human-readable error message
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []*ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}

	var sb strings.Builder
	sb.WriteString("validation failed:\n")
	for i, err := range e {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("  - ")
		sb.WriteString(err.Error())
	}
	return sb.String()
}

// valContext carries the state shared across a single validation pass. root is
// the top-level schema whose $defs are consulted to resolve "$ref" pointers;
// sub-schemas (properties, items, combinators) do not carry their own $defs.
type valContext struct {
	root *Schema
}

// resolveRef looks up a "#/$defs/<name>" pointer in the root document's $defs.
// It returns nil for pointers it cannot resolve (external or unknown), which
// callers treat leniently: an unresolvable reference never fails valid input.
func (vc *valContext) resolveRef(ref string) *Schema {
	name, ok := strings.CutPrefix(ref, defRefPrefix)
	if !ok || vc.root == nil {
		return nil
	}
	return vc.root.Defs[name]
}

// matches reports whether value validates cleanly against sub. It is used by
// the boolean combinators (oneOf/anyOf) and the "if" applicator, where a
// non-match must be counted rather than surfaced as an error.
func (vc *valContext) matches(sub *Schema, value any) bool {
	var errs ValidationErrors
	sub.validate(vc, "", value, &errs)
	return len(errs) == 0
}

// Validate validates JSON data against a schema.
// Returns nil if valid, or ValidationErrors if invalid.
func (s *Schema) Validate(data json.RawMessage) error {
	// Reject pathologically nested input before encoding/json's recursive
	// decoder can exhaust the stack (uncatchable fatal error).
	if err := checkJSONDepth(data); err != nil {
		return err
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return &ValidationError{Message: fmt.Sprintf("invalid JSON: %s", err)}
	}

	var errs ValidationErrors
	s.validate(&valContext{root: s}, "", value, &errs)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateValue validates a Go value against a schema.
func (s *Schema) ValidateValue(value any) error {
	var errs ValidationErrors
	s.validate(&valContext{root: s}, "", value, &errs)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (s *Schema) validate(vc *valContext, path string, value any, errs *ValidationErrors) {
	// Handle nil values
	if value == nil {
		// null is valid for any type unless required is enforced elsewhere
		return
	}

	// $ref delegates validation to the referenced definition. Generated
	// recursion-breaking nodes are pure references, so resolving and
	// delegating fully validates them; an unresolvable reference is ignored.
	if s.Ref != "" {
		if target := vc.resolveRef(s.Ref); target != nil {
			target.validate(vc, path, value, errs)
		}
		return
	}

	s.validateCompositions(vc, path, value, errs)

	switch s.Type {
	case typeObject:
		s.validateObject(vc, path, value, errs)
	case typeArray:
		s.validateArray(vc, path, value, errs)
	case typeString:
		s.validateString(path, value, errs)
	case typeInteger:
		s.validateInteger(path, value, errs)
	case typeNumber:
		s.validateNumber(path, value, errs)
	case typeBoolean:
		s.validateBoolean(path, value, errs)
	}
}

// validateCompositions enforces the 2020-12 boolean combinators and the
// if/then/else conditional: allOf requires every branch to pass, anyOf at
// least one, oneOf exactly one, and if/then/else selects a branch by whether
// the "if" schema matches. Each is checked only when present, so a schema
// using none of them behaves exactly as before.
func (s *Schema) validateCompositions(vc *valContext, path string, value any, errs *ValidationErrors) {
	for _, sub := range s.AllOf {
		sub.validate(vc, path, value, errs)
	}

	if len(s.AnyOf) > 0 {
		matched := false
		for _, sub := range s.AnyOf {
			if vc.matches(sub, value) {
				matched = true
				break
			}
		}
		if !matched {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: "value does not match any schema in anyOf",
			})
		}
	}

	if len(s.OneOf) > 0 {
		count := 0
		for _, sub := range s.OneOf {
			if vc.matches(sub, value) {
				count++
			}
		}
		if count != 1 {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value must match exactly one schema in oneOf, matched %d", count),
			})
		}
	}

	if s.If != nil {
		switch {
		case vc.matches(s.If, value):
			if s.Then != nil {
				s.Then.validate(vc, path, value, errs)
			}
		case s.Else != nil:
			s.Else.validate(vc, path, value, errs)
		}
	}
}

func (s *Schema) validateObject(vc *valContext, path string, value any, errs *ValidationErrors) {
	obj, ok := value.(map[string]any)
	if !ok {
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("expected object, got %T", value),
		})
		return
	}

	// Check required fields
	for _, req := range s.Required {
		if _, exists := obj[req]; !exists {
			fieldPath := joinPath(path, req)
			*errs = append(*errs, &ValidationError{
				Path:    fieldPath,
				Message: "required field is missing",
			})
		}
	}

	// Validate properties
	for name, propSchema := range s.Properties {
		if val, exists := obj[name]; exists {
			fieldPath := joinPath(path, name)
			propSchema.validate(vc, fieldPath, val, errs)
		}
	}
}

func (s *Schema) validateArray(vc *valContext, path string, value any, errs *ValidationErrors) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("expected array, got %T", value),
		})
		return
	}

	if s.Items == nil {
		return
	}

	for i := 0; i < rv.Len(); i++ {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		s.Items.validate(vc, itemPath, rv.Index(i).Interface(), errs)
	}
}

func (s *Schema) validateString(path string, value any, errs *ValidationErrors) {
	str, ok := value.(string)
	if !ok {
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("expected string, got %T", value),
		})
		return
	}

	// Validate enum
	if len(s.Enum) > 0 {
		found := false
		for _, e := range s.Enum {
			if e == str {
				found = true
				break
			}
		}
		if !found {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value must be one of: %v", s.Enum),
			})
		}
	}
}

func (s *Schema) validateInteger(path string, value any, errs *ValidationErrors) {
	var num float64
	switch v := value.(type) {
	case float64:
		num = v
		// Check if it's actually an integer
		if num != float64(int64(num)) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: "expected integer, got decimal number",
			})
			return
		}
	case int:
		num = float64(v)
	case int64:
		num = float64(v)
	default:
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("expected integer, got %T", value),
		})
		return
	}

	s.validateNumericConstraints(path, num, errs)
}

func (s *Schema) validateNumber(path string, value any, errs *ValidationErrors) {
	var num float64
	switch v := value.(type) {
	case float64:
		num = v
	case float32:
		num = float64(v)
	case int:
		num = float64(v)
	case int64:
		num = float64(v)
	default:
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("expected number, got %T", value),
		})
		return
	}

	s.validateNumericConstraints(path, num, errs)
}

func (s *Schema) validateNumericConstraints(path string, num float64, errs *ValidationErrors) {
	if s.Minimum != nil && num < *s.Minimum {
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("value %v is less than minimum %v", num, *s.Minimum),
		})
	}

	if s.Maximum != nil && num > *s.Maximum {
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("value %v is greater than maximum %v", num, *s.Maximum),
		})
	}
}

func (s *Schema) validateBoolean(path string, value any, errs *ValidationErrors) {
	if _, ok := value.(bool); !ok {
		*errs = append(*errs, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("expected boolean, got %T", value),
		})
	}
}

func joinPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
}
