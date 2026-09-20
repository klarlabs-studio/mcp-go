package protocol

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestExtractParamHeaders(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
			"query":  map[string]any{"type": "string"},
		},
	}
	got, err := ExtractParamHeaders(schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Property != "region" || got[0].HeaderName != "Region" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractParamHeaders_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"space", "My Region"},
		{"colon", "Region:Primary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := map[string]any{
				"type": "object",
				"properties": map[string]any{
					"region": map[string]any{"type": "string", "x-mcp-header": tc.header},
				},
			}
			if _, err := ExtractParamHeaders(schema); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestEncodeDecodeParamValue(t *testing.T) {
	plain, err := EncodeParamValue("us-west1")
	if err != nil || plain != "us-west1" {
		t.Fatalf("plain: %q %v", plain, err)
	}
	padded, err := EncodeParamValue(" padded ")
	if err != nil {
		t.Fatal(err)
	}
	if !stringsHasPrefixSuffix(padded) {
		t.Fatalf("expected base64 wrap, got %q", padded)
	}
	decoded, err := DecodeParamValue(padded)
	if err != nil || decoded != " padded " {
		t.Fatalf("decoded %q %v", decoded, err)
	}
	b, err := EncodeParamValue(true)
	if err != nil || b != "true" {
		t.Fatalf("bool: %q %v", b, err)
	}
	n, err := EncodeParamValue(float64(42))
	if err != nil || n != "42" {
		t.Fatalf("number: %q %v", n, err)
	}
}

func stringsHasPrefixSuffix(s string) bool {
	return len(s) > 10 && s[:9] == "=?base64?" && s[len(s)-2:] == "?="
}

func TestBuildAndValidateParamHeaders(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
		},
	}
	args := map[string]any{"region": "us-west1", "query": "select 1"}
	hdrs, err := BuildParamHeaders(schema, args)
	if err != nil {
		t.Fatal(err)
	}
	if got := hdrs.Get(HeaderParamPrefix + "Region"); got != "us-west1" {
		t.Fatalf("header = %q", got)
	}
	raw, _ := json.Marshal(args)
	if err := ValidateParamHeaders(hdrs, schema, raw); err != nil {
		t.Fatal(err)
	}
	bad := http.Header{}
	bad.Set(HeaderParamPrefix+"Region", "other")
	if err := ValidateParamHeaders(bad, schema, raw); err == nil {
		t.Fatal("expected mismatch")
	}
	missing := http.Header{}
	if err := ValidateParamHeaders(missing, schema, raw); err == nil {
		t.Fatal("expected missing header error")
	}
}
