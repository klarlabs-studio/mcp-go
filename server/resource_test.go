package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestServer_Resource(t *testing.T) {
	t.Run("returns resource builder", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		builder := srv.Resource("files://{path}")

		if builder == nil {
			t.Fatal("expected builder to be created")
		}
	})

	t.Run("registers resource with server", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Resource("files://{path}").
			Description("Read file contents").
			MimeType("text/plain").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{
					URI:      uri,
					MimeType: "text/plain",
					Text:     "file contents",
				}, nil
			})

		resources := srv.Resources()
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}

		if resources[0].URITemplate != "files://{path}" {
			t.Errorf("URITemplate = %q, want %q", resources[0].URITemplate, "files://{path}")
		}
		if resources[0].Description != "Read file contents" {
			t.Errorf("Description = %q, want %q", resources[0].Description, "Read file contents")
		}
		if resources[0].MimeType != "text/plain" {
			t.Errorf("MimeType = %q, want %q", resources[0].MimeType, "text/plain")
		}
	})
}

func TestResourceBuilder(t *testing.T) {
	t.Run("builds resource with all options", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Resource("db://users/{id}").
			Name("User Record").
			Description("Get user by ID").
			MimeType("application/json").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{
					URI:      uri,
					MimeType: "application/json",
					Text:     `{"id": "` + params["id"] + `"}`,
				}, nil
			})

		resources := srv.Resources()
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}

		if resources[0].Name != "User Record" {
			t.Errorf("Name = %q, want %q", resources[0].Name, "User Record")
		}
	})
}

func TestResource_Read(t *testing.T) {
	t.Run("reads resource with parameters", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Resource("users://{id}/profile").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{
					URI:      uri,
					MimeType: "application/json",
					Text:     `{"userId": "` + params["id"] + `"}`,
				}, nil
			})

		resource, ok := srv.GetResource("users://{id}/profile")
		if !ok {
			t.Fatal("resource not found")
		}

		content, err := resource.Read(context.Background(), "users://123/profile")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if content.URI != "users://123/profile" {
			t.Errorf("URI = %q, want %q", content.URI, "users://123/profile")
		}
		if content.Text != `{"userId": "123"}` {
			t.Errorf("Text = %q, want %q", content.Text, `{"userId": "123"}`)
		}
	})

	t.Run("extracts multiple parameters", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Resource("repos://{owner}/{repo}/files/{path}").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{
					URI:  uri,
					Text: params["owner"] + "/" + params["repo"] + "/" + params["path"],
				}, nil
			})

		resource, _ := srv.GetResource("repos://{owner}/{repo}/files/{path}")
		content, err := resource.Read(context.Background(), "repos://alice/myrepo/files/main.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if content.Text != "alice/myrepo/main.go" {
			t.Errorf("Text = %q, want %q", content.Text, "alice/myrepo/main.go")
		}
	})

	t.Run("returns handler error", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		expectedErr := errors.New("not found")
		srv.Resource("missing://{id}").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return nil, expectedErr
			})

		resource, _ := srv.GetResource("missing://{id}")
		_, err := resource.Read(context.Background(), "missing://123")

		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("returns error for non-matching URI", func(t *testing.T) {
		srv := New(Info{Name: "test", Version: "1.0.0"})

		srv.Resource("users://{id}").
			Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
				return &ResourceContent{URI: uri}, nil
			})

		resource, _ := srv.GetResource("users://{id}")
		_, err := resource.Read(context.Background(), "other://123")

		if err == nil {
			t.Error("expected error for non-matching URI")
		}
	})
}

func TestMatchURI(t *testing.T) {
	tests := []struct {
		name     string
		template string
		uri      string
		want     map[string]string
		wantOK   bool
	}{
		{
			name:     "simple parameter",
			template: "users://{id}",
			uri:      "users://123",
			want:     map[string]string{"id": "123"},
			wantOK:   true,
		},
		{
			name:     "multiple parameters",
			template: "repos://{owner}/{repo}",
			uri:      "repos://alice/myrepo",
			want:     map[string]string{"owner": "alice", "repo": "myrepo"},
			wantOK:   true,
		},
		{
			name:     "parameter in path",
			template: "files://{path}/content",
			uri:      "files://docs/content",
			want:     map[string]string{"path": "docs"},
			wantOK:   true,
		},
		{
			name:     "no parameters",
			template: "static://resource",
			uri:      "static://resource",
			want:     map[string]string{},
			wantOK:   true,
		},
		{
			name:     "non-matching scheme",
			template: "users://{id}",
			uri:      "other://123",
			wantOK:   false,
		},
		{
			name:     "non-matching path structure",
			template: "users://{id}/profile",
			uri:      "users://123",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := matchURITemplate(tt.template, tt.uri)

			if ok != tt.wantOK {
				t.Errorf("matchURITemplate() ok = %v, want %v", ok, tt.wantOK)
				return
			}

			if !tt.wantOK {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("matchURITemplate() got %d params, want %d", len(got), len(tt.want))
				return
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("matchURITemplate() got[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestContainedPath(t *testing.T) {
	root := t.TempDir()
	// Create a real file inside root and a directory to symlink from.
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("accepts a contained relative path", func(t *testing.T) {
		got, err := ContainedPath(root, "data.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantReal, _ := filepath.EvalSymlinks(filepath.Join(root, "data.txt"))
		if got != wantReal {
			t.Fatalf("expected %q, got %q", wantReal, got)
		}
	})

	t.Run("accepts a not-yet-existing contained path", func(t *testing.T) {
		got, err := ContainedPath(root, "subdir/new.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Join(root, "subdir/new.txt") {
			t.Fatalf("unexpected path %q", got)
		}
	})

	t.Run("rejects dot-dot traversal", func(t *testing.T) {
		if _, err := ContainedPath(root, "../etc/passwd"); err == nil {
			t.Fatal("expected traversal to be rejected")
		}
	})

	t.Run("rejects deep dot-dot traversal that escapes", func(t *testing.T) {
		if _, err := ContainedPath(root, "a/b/../../../secret"); err == nil {
			t.Fatal("expected escaping traversal to be rejected")
		}
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		if _, err := ContainedPath(root, "/etc/passwd"); err == nil {
			t.Fatal("expected absolute path to be rejected")
		}
	})

	t.Run("rejects empty parameter", func(t *testing.T) {
		if _, err := ContainedPath(root, ""); err == nil {
			t.Fatal("expected empty parameter to be rejected")
		}
	})

	t.Run("rejects symlink escape", func(t *testing.T) {
		// A symlink inside root pointing outside it must be rejected once
		// symlinks are resolved.
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(root, "escape")
		if err := os.Symlink(outside, linkPath); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		if _, err := ContainedPath(root, "escape/secret.txt"); err == nil {
			t.Fatal("expected symlink escape to be rejected")
		}
	})

	t.Run("accepts symlink that stays within root", func(t *testing.T) {
		sub := filepath.Join(root, "real")
		if err := os.MkdirAll(sub, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "inside.txt"), []byte("hi"), 0o600); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(root, "alias")
		if err := os.Symlink(sub, linkPath); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		if _, err := ContainedPath(root, "alias/inside.txt"); err != nil {
			t.Fatalf("expected contained symlink to be accepted, got %v", err)
		}
	})
}
