package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

// HeaderParamPrefix is the SEP-2243 prefix for tool-parameter mirror headers.
// A schema property annotated `"x-mcp-header": "Region"` becomes
// `Mcp-Param-Region` on Streamable HTTP tools/call requests.
const HeaderParamPrefix = "Mcp-Param-"

const (
	mcpParamB64Prefix = "=?base64?"
	mcpParamB64Suffix = "?="
)

// ParamHeader maps a tool argument property to its Mcp-Param-* header name.
type ParamHeader struct {
	// Property is the JSON property name under inputSchema.properties
	// (top-level only for the Go generator; nested walk supports deeper paths).
	Property string
	// HeaderName is the suffix after "Mcp-Param-" (e.g. "Region").
	HeaderName string
}

// ExtractParamHeaders walks a JSON Schema object for x-mcp-header annotations.
// It returns nil when none are present. Invalid annotations surface as an
// error so Streamable HTTP clients can exclude the tool from tools/list.
func ExtractParamHeaders(inputSchema any) ([]ParamHeader, error) {
	root, ok := asObject(inputSchema)
	if !ok {
		return nil, nil
	}
	props, _ := asObject(root["properties"])
	if props == nil {
		return nil, nil
	}
	var out []ParamHeader
	seen := map[string]string{} // lower(header) → property
	for propName, raw := range props {
		prop, ok := asObject(raw)
		if !ok {
			continue
		}
		hdr, has := prop["x-mcp-header"].(string)
		if !has {
			continue
		}
		if err := validateHeaderName(hdr); err != nil {
			return nil, fmt.Errorf("property %q: %w", propName, err)
		}
		key := strings.ToLower(hdr)
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("duplicate x-mcp-header %q on %q and %q", hdr, prev, propName)
		}
		seen[key] = propName
		if !isPrimitiveSchema(prop) {
			return nil, fmt.Errorf("property %q: x-mcp-header requires a primitive type", propName)
		}
		out = append(out, ParamHeader{Property: propName, HeaderName: hdr})
	}
	return out, nil
}

// EncodeParamValue formats a JSON argument value for an Mcp-Param-* header
// per SEP-2243 Value Encoding (bool/number coercion, Base64 sentinel wrap).
func EncodeParamValue(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", errOmitHeader
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), nil
	case json.Number:
		return x.String(), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case int32:
		return strconv.FormatInt(int64(x), 10), nil
	case string:
		return encodeStringParam(x), nil
	default:
		return "", fmt.Errorf("unsupported param type %T for Mcp-Param header", v)
	}
}

var errOmitHeader = fmt.Errorf("omit header")

func encodeStringParam(s string) string {
	if needsBase64Param(s) {
		return mcpParamB64Prefix + base64.StdEncoding.EncodeToString([]byte(s)) + mcpParamB64Suffix
	}
	return s
}

func needsBase64Param(s string) bool {
	if strings.HasPrefix(s, mcpParamB64Prefix) && strings.HasSuffix(s, mcpParamB64Suffix) {
		return true
	}
	if len(s) > 0 && (s[0] == ' ' || s[len(s)-1] == ' ') {
		return true
	}
	for _, r := range s {
		if r > unicode.MaxASCII || r < 0x20 {
			return true
		}
	}
	return false
}

// DecodeParamValue reverses EncodeParamValue (Base64 sentinel unwrap).
func DecodeParamValue(hdr string) (string, error) {
	if strings.HasPrefix(hdr, mcpParamB64Prefix) && strings.HasSuffix(hdr, mcpParamB64Suffix) {
		raw := strings.TrimSuffix(strings.TrimPrefix(hdr, mcpParamB64Prefix), mcpParamB64Suffix)
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return "", fmt.Errorf("invalid base64 Mcp-Param value: %w", err)
		}
		return string(b), nil
	}
	return hdr, nil
}

// BuildParamHeaders builds Mcp-Param-* headers for a tools/call from the
// tool's inputSchema and call arguments. Null/absent annotated arguments omit
// their header. Returns an empty header map when there are no annotations.
func BuildParamHeaders(inputSchema any, arguments any) (http.Header, error) {
	mappings, err := ExtractParamHeaders(inputSchema)
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	args, _ := asObject(arguments)
	out := make(http.Header)
	for _, m := range mappings {
		raw, ok := args[m.Property]
		if !ok || raw == nil {
			continue
		}
		val, err := EncodeParamValue(raw)
		if err == errOmitHeader {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.Property, err)
		}
		out.Set(HeaderParamPrefix+m.HeaderName, val)
	}
	return out, nil
}

// ValidateParamHeaders checks that annotated tools/call arguments match the
// corresponding Mcp-Param-* headers on an HTTP request (SEP-2243).
// No-op when the schema has no x-mcp-header annotations.
func ValidateParamHeaders(hdrs http.Header, inputSchema any, arguments json.RawMessage) error {
	if hdrs == nil {
		return nil
	}
	mappings, err := ExtractParamHeaders(inputSchema)
	if err != nil || len(mappings) == 0 {
		return err
	}
	var args map[string]any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return fmt.Errorf("arguments: %w", err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	for _, m := range mappings {
		headerKey := HeaderParamPrefix + m.HeaderName
		got := hdrs.Get(headerKey)
		raw, present := args[m.Property]
		if !present || raw == nil {
			if got != "" {
				return fmt.Errorf("%s present but argument %q is null/absent", headerKey, m.Property)
			}
			continue
		}
		want, err := EncodeParamValue(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Property, err)
		}
		if got == "" {
			return fmt.Errorf("missing %s for argument %q", headerKey, m.Property)
		}
		decoded, err := DecodeParamValue(got)
		if err != nil {
			return err
		}
		wantDecoded, err := DecodeParamValue(want)
		if err != nil {
			return err
		}
		if decoded != wantDecoded {
			return fmt.Errorf("%s does not match argument %q", headerKey, m.Property)
		}
	}
	return nil
}

func validateHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("empty x-mcp-header")
	}
	for i, r := range name {
		if !isTchar(r) {
			return fmt.Errorf("invalid x-mcp-header %q (RFC 9110 tchar, bad rune %q at %d)", name, r, i)
		}
	}
	return nil
}

func isTchar(r rune) bool {
	// RFC 9110 tchar: "!" / "#" / "$" / "%" / "&" / "'" / "*" / "+" / "-" /
	// "." / "^" / "_" / "`" / "|" / "~" / DIGIT / ALPHA
	switch r {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isPrimitiveSchema(prop map[string]any) bool {
	t, _ := prop["type"].(string)
	switch t {
	case "string", "number", "integer", "boolean":
		return true
	default:
		return false
	}
}

func asObject(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	default:
		// schema.Schema encodes via json — callers often pass *schema.Schema.
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}
