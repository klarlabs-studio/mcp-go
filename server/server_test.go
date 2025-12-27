package server

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	t.Run("creates server with info", func(t *testing.T) {
		srv := New(Info{
			Name:    "test-server",
			Version: "1.0.0",
		})

		if srv == nil {
			t.Fatal("expected server to be created")
		}

		info := srv.Info()
		if info.Name != "test-server" {
			t.Errorf("Name = %q, want %q", info.Name, "test-server")
		}
		if info.Version != "1.0.0" {
			t.Errorf("Version = %q, want %q", info.Version, "1.0.0")
		}
	})

	t.Run("creates server with capabilities", func(t *testing.T) {
		srv := New(Info{
			Name:    "test-server",
			Version: "1.0.0",
			Capabilities: Capabilities{
				Tools:     true,
				Resources: true,
				Prompts:   true,
			},
		})

		caps := srv.Info().Capabilities
		if !caps.Tools {
			t.Error("expected Tools capability to be true")
		}
		if !caps.Resources {
			t.Error("expected Resources capability to be true")
		}
		if !caps.Prompts {
			t.Error("expected Prompts capability to be true")
		}
	})

	t.Run("applies functional options", func(t *testing.T) {
		called := false
		opt := func(s *Server) {
			called = true
		}

		New(Info{Name: "test", Version: "1.0.0"}, opt)

		if !called {
			t.Error("expected option to be called")
		}
	})
}

func TestServer_Tool(t *testing.T) {
	t.Run("returns tool builder", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		builder := srv.Tool("search")

		if builder == nil {
			t.Fatal("expected builder to be created")
		}
	})

	t.Run("registers tool with server", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		type SearchInput struct {
			Query string `json:"query"`
		}

		srv.Tool("search").
			Description("Search for items").
			Handler(func(input SearchInput) (string, error) {
				return "result", nil
			})

		tools := srv.Tools()
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}

		if tools[0].Name != "search" {
			t.Errorf("tool name = %q, want %q", tools[0].Name, "search")
		}
		if tools[0].Description != "Search for items" {
			t.Errorf("tool description = %q, want %q", tools[0].Description, "Search for items")
		}
	})
}

func TestServer_Middleware(t *testing.T) {
	t.Run("registers middleware", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		middleware := func(next HandlerFunc) HandlerFunc {
			return next
		}

		srv.Use(middleware)

		if len(srv.middleware) != 1 {
			t.Errorf("expected 1 middleware, got %d", len(srv.middleware))
		}
	})

	t.Run("registers multiple middleware", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		m1 := func(next HandlerFunc) HandlerFunc { return next }
		m2 := func(next HandlerFunc) HandlerFunc { return next }

		srv.Use(m1, m2)

		if len(srv.middleware) != 2 {
			t.Errorf("expected 2 middleware, got %d", len(srv.middleware))
		}
	})
}

func TestServer_Manifest(t *testing.T) {
	srv := New(Info{
		Name:    "manifest-test",
		Version: "2.0.0",
		Capabilities: Capabilities{
			Tools: true,
		},
	})

	manifest := srv.Manifest()

	if manifest.Name != "manifest-test" {
		t.Errorf("Name = %q, want %q", manifest.Name, "manifest-test")
	}
	if manifest.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", manifest.Version, "2.0.0")
	}
	if manifest.ProtocolVersion == "" {
		t.Error("expected ProtocolVersion to be set")
	}
	if !manifest.Capabilities.Tools {
		t.Error("expected Tools capability to be true")
	}
}

func TestServer_WithInstructions(t *testing.T) {
	t.Run("sets instructions via option", func(t *testing.T) {
		instructions := "Use the search tool to find documents. Always validate results."
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithInstructions(instructions),
		)

		if srv.Instructions() != instructions {
			t.Errorf("Instructions() = %q, want %q", srv.Instructions(), instructions)
		}
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		if srv.Instructions() != "" {
			t.Errorf("Instructions() = %q, want empty string", srv.Instructions())
		}
	})
}
