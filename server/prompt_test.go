package server

import (
	"context"
	"errors"
	"testing"
)

func TestServer_Prompt(t *testing.T) {
	t.Run("returns prompt builder", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		builder := srv.Prompt("code-review")

		if builder == nil {
			t.Fatal("expected builder to be created")
		}
	})

	t.Run("registers prompt with server", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("summarize").
			Description("Summarize text content").
			Handler(func(ctx context.Context, args map[string]string) (*PromptResult, error) {
				return &PromptResult{
					Messages: []PromptMessage{
						{Role: "user", Content: TextContent{Type: "text", Text: "Summarize this"}},
					},
				}, nil
			})

		prompts := srv.Prompts()
		if len(prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(prompts))
		}

		if prompts[0].Name != "summarize" {
			t.Errorf("Name = %q, want %q", prompts[0].Name, "summarize")
		}
		if prompts[0].Description != "Summarize text content" {
			t.Errorf("Description = %q, want %q", prompts[0].Description, "Summarize text content")
		}
	})
}

func TestPromptBuilder(t *testing.T) {
	t.Run("builds prompt with arguments", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("greet").
			Description("Generate a greeting").
			Argument("name", "Name to greet", true).
			Argument("style", "Greeting style (formal/casual)", false).
			Handler(func(ctx context.Context, args map[string]string) (*PromptResult, error) {
				return &PromptResult{}, nil
			})

		prompts := srv.Prompts()
		if len(prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(prompts))
		}

		if len(prompts[0].Arguments) != 2 {
			t.Fatalf("expected 2 arguments, got %d", len(prompts[0].Arguments))
		}

		arg1 := prompts[0].Arguments[0]
		if arg1.Name != "name" {
			t.Errorf("Arguments[0].Name = %q, want %q", arg1.Name, "name")
		}
		if !arg1.Required {
			t.Error("Arguments[0].Required should be true")
		}

		arg2 := prompts[0].Arguments[1]
		if arg2.Name != "style" {
			t.Errorf("Arguments[1].Name = %q, want %q", arg2.Name, "style")
		}
		if arg2.Required {
			t.Error("Arguments[1].Required should be false")
		}
	})
}

func TestPrompt_Get(t *testing.T) {
	t.Run("executes prompt handler", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("greet").
			Argument("name", "Name to greet", true).
			Handler(func(ctx context.Context, args map[string]string) (*PromptResult, error) {
				return &PromptResult{
					Messages: []PromptMessage{
						{
							Role: "user",
							Content: TextContent{
								Type: "text",
								Text: "Hello, " + args["name"] + "!",
							},
						},
					},
				}, nil
			})

		prompt, ok := srv.GetPrompt("greet")
		if !ok {
			t.Fatal("prompt not found")
		}

		result, err := prompt.Get(context.Background(), map[string]string{"name": "Alice"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result.Messages))
		}

		content, ok := result.Messages[0].Content.(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Messages[0].Content)
		}

		if content.Text != "Hello, Alice!" {
			t.Errorf("Text = %q, want %q", content.Text, "Hello, Alice!")
		}
	})

	t.Run("returns handler error", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		expectedErr := errors.New("prompt failed")
		srv.Prompt("failing").
			Handler(func(ctx context.Context, args map[string]string) (*PromptResult, error) {
				return nil, expectedErr
			})

		prompt, _ := srv.GetPrompt("failing")
		_, err := prompt.Get(context.Background(), nil)

		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("validates required arguments", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Prompt("require-args").
			Argument("required_arg", "A required argument", true).
			Handler(func(ctx context.Context, args map[string]string) (*PromptResult, error) {
				return &PromptResult{}, nil
			})

		prompt, _ := srv.GetPrompt("require-args")

		// Missing required argument
		_, err := prompt.Get(context.Background(), map[string]string{})
		if err == nil {
			t.Error("expected error for missing required argument")
		}

		// With required argument
		_, err = prompt.Get(context.Background(), map[string]string{"required_arg": "value"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
