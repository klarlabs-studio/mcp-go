package server

import (
	"testing"
)

func TestRoot(t *testing.T) {
	root := Root{
		URI:  "file:///home/user/project",
		Name: "My Project",
	}

	if root.URI != "file:///home/user/project" {
		t.Errorf("expected URI 'file:///home/user/project', got %q", root.URI)
	}
	if root.Name != "My Project" {
		t.Errorf("expected Name 'My Project', got %q", root.Name)
	}
}

func TestRootWithoutName(t *testing.T) {
	root := Root{
		URI: "file:///workspace",
	}

	if root.URI != "file:///workspace" {
		t.Errorf("expected URI 'file:///workspace', got %q", root.URI)
	}
	if root.Name != "" {
		t.Errorf("expected empty Name, got %q", root.Name)
	}
}

func TestListRootsResult(t *testing.T) {
	result := ListRootsResult{
		Roots: []Root{
			{URI: "file:///project1", Name: "Project 1"},
			{URI: "file:///project2", Name: "Project 2"},
		},
	}

	if len(result.Roots) != 2 {
		t.Errorf("expected 2 roots, got %d", len(result.Roots))
	}
	if result.Roots[0].Name != "Project 1" {
		t.Errorf("expected first root name 'Project 1', got %q", result.Roots[0].Name)
	}
	if result.Roots[1].URI != "file:///project2" {
		t.Errorf("expected second root URI 'file:///project2', got %q", result.Roots[1].URI)
	}
}

func TestListRootsResultEmpty(t *testing.T) {
	result := ListRootsResult{
		Roots: []Root{},
	}

	if len(result.Roots) != 0 {
		t.Errorf("expected 0 roots, got %d", len(result.Roots))
	}
}
