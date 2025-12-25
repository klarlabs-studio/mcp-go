package server

import (
	"fmt"
	"reflect"
	"strconv"
)

// ExtractParams extracts URI template parameters into a typed struct.
// The struct fields should have `uri` tags matching the URI template parameters.
//
// Example:
//
//	type UserParams struct {
//	    ID   string `uri:"id"`
//	    Name string `uri:"name"`
//	}
//
//	srv.Resource("users://{id}/{name}").Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
//	    p, err := mcp.ExtractParams[UserParams](params)
//	    if err != nil {
//	        return nil, err
//	    }
//	    // Use p.ID and p.Name
//	})
func ExtractParams[T any](params map[string]string) (T, error) {
	var result T
	rv := reflect.ValueOf(&result).Elem()
	rt := rv.Type()

	if rt.Kind() != reflect.Struct {
		return result, fmt.Errorf("ExtractParams: T must be a struct type, got %s", rt.Kind())
	}

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Get the uri tag
		tag := field.Tag.Get("uri")
		if tag == "" {
			// Fall back to json tag if uri tag not present
			tag = field.Tag.Get("json")
			if tag == "" {
				continue
			}
			// Handle json tag options like "name,omitempty"
			if idx := indexByte(tag, ','); idx != -1 {
				tag = tag[:idx]
			}
		}

		// Get the value from params
		value, ok := params[tag]
		if !ok {
			continue
		}

		// Set the field value based on type
		if err := setFieldValue(fieldValue, value); err != nil {
			return result, fmt.Errorf("ExtractParams: field %s: %w", field.Name, err)
		}
	}

	return result, nil
}

// setFieldValue sets a reflect.Value from a string.
func setFieldValue(field reflect.Value, value string) error {
	if !field.CanSet() {
		return fmt.Errorf("cannot set field")
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int: %w", err)
		}
		field.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid uint: %w", err)
		}
		field.SetUint(n)

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float: %w", err)
		}
		field.SetFloat(f)

	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool: %w", err)
		}
		field.SetBool(b)

	default:
		return fmt.Errorf("unsupported type: %s", field.Kind())
	}

	return nil
}

// indexByte returns the index of c in s, or -1 if not present.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
