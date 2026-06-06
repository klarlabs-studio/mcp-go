package server_test

import (
	"context"
	"testing"

	"go.klarlabs.de/mcp/server"
)

func TestToolAnnotations(t *testing.T) {
	t.Run("ReadOnly sets correct hints", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("reader").
			Description("Reads data").
			ReadOnly().
			Handler(func(input struct{}) (string, error) {
				return "data", nil
			})

		tools := srv.Tools()
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}

		ann := tools[0].Annotations
		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.ReadOnlyHint == nil || !*ann.ReadOnlyHint {
			t.Error("expected ReadOnlyHint to be true")
		}

		if ann.DestructiveHint == nil || *ann.DestructiveHint {
			t.Error("expected DestructiveHint to be false")
		}
	})

	t.Run("Destructive sets correct hint", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("deleter").
			Description("Deletes data").
			Destructive().
			Handler(func(input struct{}) (string, error) {
				return "deleted", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.DestructiveHint == nil || !*ann.DestructiveHint {
			t.Error("expected DestructiveHint to be true")
		}
	})

	t.Run("Idempotent sets correct hint", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("setter").
			Description("Sets a value").
			Idempotent().
			Handler(func(input struct{}) (string, error) {
				return "set", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.IdempotentHint == nil || !*ann.IdempotentHint {
			t.Error("expected IdempotentHint to be true")
		}
	})

	t.Run("OpenWorld sets correct hint", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("fetcher").
			Description("Fetches external data").
			OpenWorld().
			Handler(func(input struct{}) (string, error) {
				return "fetched", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.OpenWorldHint == nil || !*ann.OpenWorldHint {
			t.Error("expected OpenWorldHint to be true")
		}
	})

	t.Run("ClosedWorld sets correct hint", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("calculator").
			Description("Calculates locally").
			ClosedWorld().
			Handler(func(input struct{}) (string, error) {
				return "calculated", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.OpenWorldHint == nil || *ann.OpenWorldHint {
			t.Error("expected OpenWorldHint to be false")
		}
	})

	t.Run("Title sets correct value", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("my-tool").
			Description("A tool").
			Title("My Amazing Tool").
			Handler(func(input struct{}) (string, error) {
				return "done", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.Title != "My Amazing Tool" {
			t.Errorf("expected title 'My Amazing Tool', got '%s'", ann.Title)
		}
	})

	t.Run("Annotations sets custom annotations", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("custom").
			Description("Custom annotations").
			Annotations(server.ToolAnnotations{
				Title:           "Custom Tool",
				ReadOnlyHint:    server.Bool(true),
				DestructiveHint: server.Bool(false),
				IdempotentHint:  server.Bool(true),
				OpenWorldHint:   server.Bool(false),
			}).
			Handler(func(input struct{}) (string, error) {
				return "done", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.Title != "Custom Tool" {
			t.Errorf("expected title 'Custom Tool', got '%s'", ann.Title)
		}
		if !*ann.ReadOnlyHint {
			t.Error("expected ReadOnlyHint to be true")
		}
		if *ann.DestructiveHint {
			t.Error("expected DestructiveHint to be false")
		}
		if !*ann.IdempotentHint {
			t.Error("expected IdempotentHint to be true")
		}
		if *ann.OpenWorldHint {
			t.Error("expected OpenWorldHint to be false")
		}
	})

	t.Run("multiple annotations chain correctly", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Tool("chained").
			Description("Chained annotations").
			Title("Chained Tool").
			ReadOnly().
			Idempotent().
			ClosedWorld().
			Handler(func(input struct{}) (string, error) {
				return "done", nil
			})

		tools := srv.Tools()
		ann := tools[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.Title != "Chained Tool" {
			t.Errorf("expected title 'Chained Tool', got '%s'", ann.Title)
		}
		if !*ann.ReadOnlyHint {
			t.Error("expected ReadOnlyHint to be true")
		}
		if *ann.DestructiveHint {
			t.Error("expected DestructiveHint to be false")
		}
		if !*ann.IdempotentHint {
			t.Error("expected IdempotentHint to be true")
		}
		if *ann.OpenWorldHint {
			t.Error("expected OpenWorldHint to be false")
		}
	})
}

func TestResourceAnnotations(t *testing.T) {
	t.Run("Audience sets correct values", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Resource("file://{path}").
			Description("A file resource").
			Audience("user", "assistant").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*server.ResourceContent, error) {
				return &server.ResourceContent{URI: uri, Text: "content"}, nil
			})

		resources := srv.Resources()
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}

		ann := resources[0].Annotations
		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if len(ann.Audience) != 2 {
			t.Fatalf("expected 2 audience values, got %d", len(ann.Audience))
		}
		if ann.Audience[0] != "user" || ann.Audience[1] != "assistant" {
			t.Errorf("unexpected audience values: %v", ann.Audience)
		}
	})

	t.Run("Priority sets correct value", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Resource("file://{path}").
			Description("A high-priority resource").
			Priority(0.9).
			Handler(func(ctx context.Context, uri string, params map[string]string) (*server.ResourceContent, error) {
				return &server.ResourceContent{URI: uri, Text: "content"}, nil
			})

		resources := srv.Resources()
		ann := resources[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.Priority == nil {
			t.Fatal("expected priority to be set")
		}
		if *ann.Priority != 0.9 {
			t.Errorf("expected priority 0.9, got %f", *ann.Priority)
		}
	})

	t.Run("Annotate sets custom annotations", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Resource("file://{path}").
			Description("A custom annotated resource").
			Annotate(server.ResourceAnnotations{
				Audience: []string{"assistant"},
				Priority: server.Float(0.5),
			}).
			Handler(func(ctx context.Context, uri string, params map[string]string) (*server.ResourceContent, error) {
				return &server.ResourceContent{URI: uri, Text: "content"}, nil
			})

		resources := srv.Resources()
		ann := resources[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if len(ann.Audience) != 1 || ann.Audience[0] != "assistant" {
			t.Errorf("unexpected audience: %v", ann.Audience)
		}
		if *ann.Priority != 0.5 {
			t.Errorf("expected priority 0.5, got %f", *ann.Priority)
		}
	})
}

func TestPromptAnnotations(t *testing.T) {
	t.Run("Audience sets correct values", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("greeting").
			Description("A greeting prompt").
			Audience("user").
			Handler(func(ctx context.Context, args map[string]string) (*server.PromptResult, error) {
				return &server.PromptResult{
					Messages: []server.PromptMessage{
						{Role: "assistant", Content: "Hello!"},
					},
				}, nil
			})

		prompts := srv.Prompts()
		if len(prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(prompts))
		}

		ann := prompts[0].Annotations
		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if len(ann.Audience) != 1 || ann.Audience[0] != "user" {
			t.Errorf("unexpected audience: %v", ann.Audience)
		}
	})

	t.Run("Priority sets correct value", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("important").
			Description("An important prompt").
			Priority(1.0).
			Handler(func(ctx context.Context, args map[string]string) (*server.PromptResult, error) {
				return &server.PromptResult{
					Messages: []server.PromptMessage{
						{Role: "assistant", Content: "Important!"},
					},
				}, nil
			})

		prompts := srv.Prompts()
		ann := prompts[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if ann.Priority == nil || *ann.Priority != 1.0 {
			t.Errorf("expected priority 1.0, got %v", ann.Priority)
		}
	})

	t.Run("Annotate sets custom annotations", func(t *testing.T) {
		srv := server.New(server.Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("custom").
			Description("A custom annotated prompt").
			Annotate(server.PromptAnnotations{
				Audience: []string{"assistant"},
				Priority: server.Float(0.7),
			}).
			Handler(func(ctx context.Context, args map[string]string) (*server.PromptResult, error) {
				return &server.PromptResult{
					Messages: []server.PromptMessage{
						{Role: "assistant", Content: "Custom!"},
					},
				}, nil
			})

		prompts := srv.Prompts()
		ann := prompts[0].Annotations

		if ann == nil {
			t.Fatal("expected annotations to be set")
		}

		if len(ann.Audience) != 1 || ann.Audience[0] != "assistant" {
			t.Errorf("unexpected audience: %v", ann.Audience)
		}
		if *ann.Priority != 0.7 {
			t.Errorf("expected priority 0.7, got %f", *ann.Priority)
		}
	})
}

func TestHelperFunctions(t *testing.T) {
	t.Run("Bool returns pointer", func(t *testing.T) {
		truePtr := server.Bool(true)
		falsePtr := server.Bool(false)

		if truePtr == nil || !*truePtr {
			t.Error("expected Bool(true) to return pointer to true")
		}
		if falsePtr == nil || *falsePtr {
			t.Error("expected Bool(false) to return pointer to false")
		}
	})

	t.Run("Float returns pointer", func(t *testing.T) {
		ptr := server.Float(0.5)

		if ptr == nil || *ptr != 0.5 {
			t.Error("expected Float(0.5) to return pointer to 0.5")
		}
	})
}
