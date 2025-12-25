package server

import (
	"testing"
)

func TestExtractParams(t *testing.T) {
	t.Run("string fields", func(t *testing.T) {
		type Params struct {
			ID   string `uri:"id"`
			Name string `uri:"name"`
		}

		params := map[string]string{
			"id":   "123",
			"name": "test",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ID != "123" {
			t.Errorf("expected ID '123', got %q", result.ID)
		}
		if result.Name != "test" {
			t.Errorf("expected Name 'test', got %q", result.Name)
		}
	})

	t.Run("int fields", func(t *testing.T) {
		type Params struct {
			ID    int   `uri:"id"`
			Count int64 `uri:"count"`
		}

		params := map[string]string{
			"id":    "42",
			"count": "100",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ID != 42 {
			t.Errorf("expected ID 42, got %d", result.ID)
		}
		if result.Count != 100 {
			t.Errorf("expected Count 100, got %d", result.Count)
		}
	})

	t.Run("float fields", func(t *testing.T) {
		type Params struct {
			Score float64 `uri:"score"`
		}

		params := map[string]string{
			"score": "3.14",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Score != 3.14 {
			t.Errorf("expected Score 3.14, got %f", result.Score)
		}
	})

	t.Run("bool fields", func(t *testing.T) {
		type Params struct {
			Active bool `uri:"active"`
		}

		params := map[string]string{
			"active": "true",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Active {
			t.Error("expected Active true")
		}
	})

	t.Run("fallback to json tag", func(t *testing.T) {
		type Params struct {
			ID string `json:"id"`
		}

		params := map[string]string{
			"id": "json-tag-value",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ID != "json-tag-value" {
			t.Errorf("expected ID 'json-tag-value', got %q", result.ID)
		}
	})

	t.Run("json tag with options", func(t *testing.T) {
		type Params struct {
			ID string `json:"id,omitempty"`
		}

		params := map[string]string{
			"id": "with-options",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ID != "with-options" {
			t.Errorf("expected ID 'with-options', got %q", result.ID)
		}
	})

	t.Run("missing params are ignored", func(t *testing.T) {
		type Params struct {
			ID   string `uri:"id"`
			Name string `uri:"name"`
		}

		params := map[string]string{
			"id": "123",
			// "name" is missing
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ID != "123" {
			t.Errorf("expected ID '123', got %q", result.ID)
		}
		if result.Name != "" {
			t.Errorf("expected Name '', got %q", result.Name)
		}
	})

	t.Run("invalid int value", func(t *testing.T) {
		type Params struct {
			ID int `uri:"id"`
		}

		params := map[string]string{
			"id": "not-a-number",
		}

		_, err := ExtractParams[Params](params)
		if err == nil {
			t.Error("expected error for invalid int")
		}
	})

	t.Run("invalid bool value", func(t *testing.T) {
		type Params struct {
			Active bool `uri:"active"`
		}

		params := map[string]string{
			"active": "maybe",
		}

		_, err := ExtractParams[Params](params)
		if err == nil {
			t.Error("expected error for invalid bool")
		}
	})

	t.Run("uint fields", func(t *testing.T) {
		type Params struct {
			Count uint `uri:"count"`
		}

		params := map[string]string{
			"count": "999",
		}

		result, err := ExtractParams[Params](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Count != 999 {
			t.Errorf("expected Count 999, got %d", result.Count)
		}
	})
}
