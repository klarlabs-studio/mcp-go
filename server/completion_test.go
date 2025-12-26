package server

import (
	"context"
	"testing"
)

func TestCompletionRegistry(t *testing.T) {
	t.Run("prompt completion", func(t *testing.T) {
		reg := newCompletionRegistry()

		reg.RegisterPromptCompletion("code-review", func(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
			if arg.Name == "language" {
				return &CompletionResult{
					Values: []string{"python", "go", "javascript"},
					Total:  3,
				}, nil
			}
			return &CompletionResult{Values: []string{}}, nil
		})

		result, err := reg.Handle(context.Background(), CompletionRef{
			Type: "ref/prompt",
			Name: "code-review",
		}, CompletionArgument{
			Name:  "language",
			Value: "py",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 3 {
			t.Errorf("expected 3 values, got %d", len(result.Values))
		}
	})

	t.Run("resource completion", func(t *testing.T) {
		reg := newCompletionRegistry()

		reg.RegisterResourceCompletion("file://{path}", func(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{
				Values: []string{"/home", "/etc", "/var"},
				Total:  3,
			}, nil
		})

		result, err := reg.Handle(context.Background(), CompletionRef{
			Type: "ref/resource",
			URI:  "file://{path}",
		}, CompletionArgument{
			Name:  "path",
			Value: "/",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 3 {
			t.Errorf("expected 3 values, got %d", len(result.Values))
		}
	})

	t.Run("no handler returns empty", func(t *testing.T) {
		reg := newCompletionRegistry()

		result, err := reg.Handle(context.Background(), CompletionRef{
			Type: "ref/prompt",
			Name: "unknown",
		}, CompletionArgument{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 0 {
			t.Errorf("expected 0 values, got %d", len(result.Values))
		}
	})

	t.Run("enforces max 100 values", func(t *testing.T) {
		reg := newCompletionRegistry()

		// Generate 150 values
		values := make([]string, 150)
		for i := range values {
			values[i] = "value"
		}

		reg.RegisterPromptCompletion("test", func(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{
				Values: values,
				Total:  150,
			}, nil
		})

		result, err := reg.Handle(context.Background(), CompletionRef{
			Type: "ref/prompt",
			Name: "test",
		}, CompletionArgument{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 100 {
			t.Errorf("expected max 100 values, got %d", len(result.Values))
		}
		if !result.HasMore {
			t.Error("expected HasMore to be true")
		}
	})

	t.Run("default handler", func(t *testing.T) {
		reg := newCompletionRegistry()

		reg.SetDefaultHandler(func(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{
				Values: []string{"default"},
				Total:  1,
			}, nil
		})

		result, err := reg.Handle(context.Background(), CompletionRef{
			Type: "ref/prompt",
			Name: "unknown",
		}, CompletionArgument{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 1 || result.Values[0] != "default" {
			t.Errorf("expected default handler result")
		}
	})
}

func TestServerCompletion(t *testing.T) {
	t.Run("prompt completion builder", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.PromptCompletion("greet").Handler(func(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{
				Values: []string{"hello", "hi", "hey"},
				Total:  3,
			}, nil
		})

		result, err := srv.HandleCompletion(context.Background(), CompletionRef{
			Type: "ref/prompt",
			Name: "greet",
		}, CompletionArgument{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 3 {
			t.Errorf("expected 3 values, got %d", len(result.Values))
		}
	})

	t.Run("resource completion builder", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.ResourceCompletion("file://{path}").Handler(func(ctx context.Context, ref CompletionRef, arg CompletionArgument) (*CompletionResult, error) {
			return &CompletionResult{
				Values: []string{"/home", "/etc"},
				Total:  2,
			}, nil
		})

		result, err := srv.HandleCompletion(context.Background(), CompletionRef{
			Type: "ref/resource",
			URI:  "file://{path}",
		}, CompletionArgument{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 2 {
			t.Errorf("expected 2 values, got %d", len(result.Values))
		}
	})

	t.Run("no completion handlers returns empty", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		result, err := srv.HandleCompletion(context.Background(), CompletionRef{
			Type: "ref/prompt",
			Name: "unknown",
		}, CompletionArgument{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Values) != 0 {
			t.Errorf("expected 0 values, got %d", len(result.Values))
		}
	})
}

func TestResourceTemplates(t *testing.T) {
	t.Run("lists only templates", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		// Static resource (not a template)
		srv.Resource("config://settings").
			Name("Settings").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{URI: uri, Text: "{}"}, nil
			})

		// Template resource
		srv.Resource("file://{path}").
			Name("File").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{URI: uri, Text: "content"}, nil
			})

		templates := srv.ResourceTemplates()
		if len(templates) != 1 {
			t.Errorf("expected 1 template, got %d", len(templates))
		}
		if templates[0].URITemplate != "file://{path}" {
			t.Errorf("expected file://{path}, got %s", templates[0].URITemplate)
		}
	})
}
