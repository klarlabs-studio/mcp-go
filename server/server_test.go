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

func TestServer_WithTitle(t *testing.T) {
	t.Run("sets title via option", func(t *testing.T) {
		title := "My Test Server"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithTitle(title),
		)

		if srv.Info().Title != title {
			t.Errorf("Info().Title = %q, want %q", srv.Info().Title, title)
		}
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		if srv.Info().Title != "" {
			t.Errorf("Info().Title = %q, want empty string", srv.Info().Title)
		}
	})

	t.Run("manifest includes title", func(t *testing.T) {
		title := "My Test Server"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithTitle(title),
		)

		if srv.Manifest().Title != title {
			t.Errorf("Manifest().Title = %q, want %q", srv.Manifest().Title, title)
		}
	})
}

func TestServer_WithDescription(t *testing.T) {
	t.Run("sets description via option", func(t *testing.T) {
		desc := "A test server for unit testing"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithDescription(desc),
		)

		if srv.Info().Description != desc {
			t.Errorf("Info().Description = %q, want %q", srv.Info().Description, desc)
		}
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		if srv.Info().Description != "" {
			t.Errorf("Info().Description = %q, want empty string", srv.Info().Description)
		}
	})

	t.Run("manifest includes description", func(t *testing.T) {
		desc := "A test server for unit testing"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithDescription(desc),
		)

		if srv.Manifest().Description != desc {
			t.Errorf("Manifest().Description = %q, want %q", srv.Manifest().Description, desc)
		}
	})
}

func TestServer_WithWebsiteURL(t *testing.T) {
	t.Run("sets websiteURL via option", func(t *testing.T) {
		url := "https://example.com/docs"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithWebsiteURL(url),
		)

		if srv.Info().WebsiteURL != url {
			t.Errorf("Info().WebsiteURL = %q, want %q", srv.Info().WebsiteURL, url)
		}
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		if srv.Info().WebsiteURL != "" {
			t.Errorf("Info().WebsiteURL = %q, want empty string", srv.Info().WebsiteURL)
		}
	})

	t.Run("manifest includes websiteURL", func(t *testing.T) {
		url := "https://example.com/docs"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithWebsiteURL(url),
		)

		if srv.Manifest().WebsiteURL != url {
			t.Errorf("Manifest().WebsiteURL = %q, want %q", srv.Manifest().WebsiteURL, url)
		}
	})
}

func TestServer_WithIcons(t *testing.T) {
	t.Run("sets icons via option", func(t *testing.T) {
		icons := []Icon{
			{URI: "data:image/png;base64,abc123", MimeType: "image/png", Size: 32},
			{URI: "https://example.com/icon.svg", MimeType: "image/svg+xml"},
		}
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithIcons(icons...),
		)

		if len(srv.Info().Icons) != 2 {
			t.Fatalf("Info().Icons length = %d, want 2", len(srv.Info().Icons))
		}
		if srv.Info().Icons[0].URI != icons[0].URI {
			t.Errorf("Icons[0].URI = %q, want %q", srv.Info().Icons[0].URI, icons[0].URI)
		}
		if srv.Info().Icons[0].Size != 32 {
			t.Errorf("Icons[0].Size = %d, want 32", srv.Info().Icons[0].Size)
		}
	})

	t.Run("returns nil when not set", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		if srv.Info().Icons != nil {
			t.Errorf("Info().Icons = %v, want nil", srv.Info().Icons)
		}
	})

	t.Run("manifest includes icons", func(t *testing.T) {
		icons := []Icon{
			{URI: "data:image/png;base64,abc123", MimeType: "image/png"},
		}
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithIcons(icons...),
		)

		if len(srv.Manifest().Icons) != 1 {
			t.Fatalf("Manifest().Icons length = %d, want 1", len(srv.Manifest().Icons))
		}
	})
}

func TestServer_WithBuildInfo(t *testing.T) {
	t.Run("sets buildInfo via option", func(t *testing.T) {
		commit := "abc123def"
		buildDate := "2025-01-03T10:00:00Z"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithBuildInfo(commit, buildDate),
		)

		if srv.Info().BuildInfo == nil {
			t.Fatal("Info().BuildInfo is nil, want non-nil")
		}
		if srv.Info().BuildInfo.Commit != commit {
			t.Errorf("BuildInfo.Commit = %q, want %q", srv.Info().BuildInfo.Commit, commit)
		}
		if srv.Info().BuildInfo.BuildDate != buildDate {
			t.Errorf("BuildInfo.BuildDate = %q, want %q", srv.Info().BuildInfo.BuildDate, buildDate)
		}
	})

	t.Run("returns nil when not set", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		if srv.Info().BuildInfo != nil {
			t.Errorf("Info().BuildInfo = %v, want nil", srv.Info().BuildInfo)
		}
	})

	t.Run("manifest includes buildInfo", func(t *testing.T) {
		commit := "abc123def"
		buildDate := "2025-01-03T10:00:00Z"
		srv := New(
			Info{Name: "test", Version: "1.0.0"},
			WithBuildInfo(commit, buildDate),
		)

		if srv.Manifest().BuildInfo == nil {
			t.Fatal("Manifest().BuildInfo is nil, want non-nil")
		}
		if srv.Manifest().BuildInfo.Commit != commit {
			t.Errorf("Manifest().BuildInfo.Commit = %q, want %q", srv.Manifest().BuildInfo.Commit, commit)
		}
	})
}

func TestServer_AllMetadataOptions(t *testing.T) {
	// Test all metadata options together
	srv := New(
		Info{Name: "test", Version: "1.0.0"},
		WithTitle("Test Server"),
		WithDescription("A server for testing"),
		WithWebsiteURL("https://example.com"),
		WithIcons(Icon{URI: "https://example.com/icon.png", MimeType: "image/png"}),
		WithBuildInfo("abc123", "2025-01-03"),
		WithInstructions("Use wisely"),
	)

	info := srv.Info()
	if info.Title != "Test Server" {
		t.Errorf("Title = %q, want %q", info.Title, "Test Server")
	}
	if info.Description != "A server for testing" {
		t.Errorf("Description = %q, want %q", info.Description, "A server for testing")
	}
	if info.WebsiteURL != "https://example.com" {
		t.Errorf("WebsiteURL = %q, want %q", info.WebsiteURL, "https://example.com")
	}
	if len(info.Icons) != 1 {
		t.Errorf("Icons length = %d, want 1", len(info.Icons))
	}
	if info.BuildInfo == nil || info.BuildInfo.Commit != "abc123" {
		t.Error("BuildInfo not set correctly")
	}
	if srv.Instructions() != "Use wisely" {
		t.Errorf("Instructions = %q, want %q", srv.Instructions(), "Use wisely")
	}
}
