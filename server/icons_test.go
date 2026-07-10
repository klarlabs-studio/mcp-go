package server

import (
	"context"
	"reflect"
	"testing"
)

func testServer() *Server {
	return New(Info{Name: "test", Version: "1.0.0"})
}

func sampleIcons() []Icon {
	return []Icon{
		{URI: "https://example.com/icon.png", MimeType: "image/png", Size: 48},
		{URI: "data:image/svg+xml;base64,AAAA", MimeType: "image/svg+xml"},
	}
}

func TestToolBuilder_Icons(t *testing.T) {
	icons := sampleIcons()
	srv := testServer()
	srv.Tool("with-icons").
		Description("has icons").
		Icons(icons...).
		Handler(func(struct{}) (string, error) { return "ok", nil })

	tool, ok := srv.GetTool("with-icons")
	if !ok {
		t.Fatal("tool not registered")
	}
	if got := tool.Icons(); !reflect.DeepEqual(got, icons) {
		t.Fatalf("Tool.Icons() = %+v, want %+v", got, icons)
	}
}

func TestToolBuilder_Icons_EmptyByDefault(t *testing.T) {
	srv := testServer()
	srv.Tool("no-icons").
		Handler(func(struct{}) (string, error) { return "ok", nil })

	tool, ok := srv.GetTool("no-icons")
	if !ok {
		t.Fatal("tool not registered")
	}
	if got := tool.Icons(); got != nil {
		t.Fatalf("Tool.Icons() = %+v, want nil", got)
	}
}

func TestResourceBuilder_Icons(t *testing.T) {
	icons := sampleIcons()
	srv := testServer()
	handler := func(context.Context, string, map[string]string) (*ResourceContent, error) {
		return &ResourceContent{URI: "data://x", Text: "x"}, nil
	}
	srv.Resource("data://x").
		Name("x").
		Icons(icons...).
		Handler(handler)

	res, ok := srv.GetResource("data://x")
	if !ok {
		t.Fatal("resource not registered")
	}
	if got := res.Icons(); !reflect.DeepEqual(got, icons) {
		t.Fatalf("Resource.Icons() = %+v, want %+v", got, icons)
	}
}

func TestResourceBuilder_Icons_EmptyByDefault(t *testing.T) {
	srv := testServer()
	handler := func(context.Context, string, map[string]string) (*ResourceContent, error) {
		return &ResourceContent{URI: "data://y", Text: "y"}, nil
	}
	srv.Resource("data://y").Handler(handler)

	res, ok := srv.GetResource("data://y")
	if !ok {
		t.Fatal("resource not registered")
	}
	if got := res.Icons(); got != nil {
		t.Fatalf("Resource.Icons() = %+v, want nil", got)
	}
}

func TestPromptBuilder_Icons(t *testing.T) {
	icons := sampleIcons()
	srv := testServer()
	handler := func(context.Context, map[string]string) (*PromptResult, error) {
		return &PromptResult{Messages: []PromptMessage{}}, nil
	}
	srv.Prompt("with-icons").
		Description("has icons").
		Icons(icons...).
		Handler(handler)

	p, ok := srv.GetPrompt("with-icons")
	if !ok {
		t.Fatal("prompt not registered")
	}
	if got := p.Icons(); !reflect.DeepEqual(got, icons) {
		t.Fatalf("Prompt.Icons() = %+v, want %+v", got, icons)
	}
}

func TestPromptBuilder_Icons_EmptyByDefault(t *testing.T) {
	srv := testServer()
	handler := func(context.Context, map[string]string) (*PromptResult, error) {
		return &PromptResult{Messages: []PromptMessage{}}, nil
	}
	srv.Prompt("no-icons").Handler(handler)

	p, ok := srv.GetPrompt("no-icons")
	if !ok {
		t.Fatal("prompt not registered")
	}
	if got := p.Icons(); got != nil {
		t.Fatalf("Prompt.Icons() = %+v, want nil", got)
	}
}

func TestResourceTemplateInfo_IconsField(t *testing.T) {
	icons := sampleIcons()
	info := ResourceTemplateInfo{URITemplate: "data://{id}", Icons: icons}
	if !reflect.DeepEqual(info.Icons, icons) {
		t.Fatalf("ResourceTemplateInfo.Icons = %+v, want %+v", info.Icons, icons)
	}
}
