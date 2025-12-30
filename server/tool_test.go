package server

import (
	"context"
	"errors"
	"testing"
)

func TestToolBuilder(t *testing.T) {
	t.Run("builds tool with description", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type Input struct {
			Query string `json:"query"`
		}

		srv.Tool("search").
			Description("Search for items").
			Handler(func(input Input) (string, error) {
				return "ok", nil
			})

		tools := srv.Tools()
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}

		if tools[0].Description != "Search for items" {
			t.Errorf("Description = %q, want %q", tools[0].Description, "Search for items")
		}
	})

	t.Run("handles various handler signatures", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type Input struct {
			Value int `json:"value"`
		}

		// Handler with context
		srv.Tool("with-context").
			Handler(func(ctx context.Context, input Input) (int, error) {
				return input.Value * 2, nil
			})

		// Handler without context
		srv.Tool("without-context").
			Handler(func(input Input) (int, error) {
				return input.Value * 3, nil
			})

		tools := srv.Tools()
		if len(tools) != 2 {
			t.Fatalf("expected 2 tools, got %d", len(tools))
		}
	})
}

func TestTool_Execute(t *testing.T) {
	t.Run("executes handler with input", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type Input struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		type Output struct {
			Sum int `json:"sum"`
		}

		srv.Tool("add").
			Handler(func(input Input) (Output, error) {
				return Output{Sum: input.A + input.B}, nil
			})

		tool, ok := srv.GetTool("add")
		if !ok {
			t.Fatal("tool not found")
		}

		result, err := tool.Execute(context.Background(), []byte(`{"a": 5, "b": 3}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output, ok := result.(Output)
		if !ok {
			t.Fatalf("result type = %T, want Output", result)
		}

		if output.Sum != 8 {
			t.Errorf("Sum = %d, want 8", output.Sum)
		}
	})

	t.Run("executes handler with context", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type Input struct {
			Value string `json:"value"`
		}

		srv.Tool("echo").
			Handler(func(ctx context.Context, input Input) (string, error) {
				if ctx == nil {
					return "", errors.New("context is nil")
				}
				return input.Value, nil
			})

		tool, _ := srv.GetTool("echo")
		result, err := tool.Execute(context.Background(), []byte(`{"value": "hello"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "hello" {
			t.Errorf("result = %q, want %q", result, "hello")
		}
	})

	t.Run("returns handler error", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type Input struct{}

		expectedErr := errors.New("handler failed")
		srv.Tool("failing").
			Handler(func(input Input) (string, error) {
				return "", expectedErr
			})

		tool, _ := srv.GetTool("failing")
		_, err := tool.Execute(context.Background(), []byte(`{}`))

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type Input struct {
			Value int `json:"value"`
		}

		srv.Tool("parse-test").
			Handler(func(input Input) (int, error) {
				return input.Value, nil
			})

		tool, _ := srv.GetTool("parse-test")
		_, err := tool.Execute(context.Background(), []byte(`{invalid`))

		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
