package server

import (
	"testing"
)

func TestNewTextContent(t *testing.T) {
	content := NewTextContent("hello world")

	if content.Type != "text" {
		t.Errorf("expected type 'text', got %q", content.Type)
	}
	if content.Text != "hello world" {
		t.Errorf("expected text 'hello world', got %q", content.Text)
	}
	if content.MimeType != "" {
		t.Errorf("expected empty mimeType, got %q", content.MimeType)
	}
}

func TestNewImageContent(t *testing.T) {
	content := NewImageContent("image/png", "base64data")

	if content.Type != "image" {
		t.Errorf("expected type 'image', got %q", content.Type)
	}
	if content.MimeType != "image/png" {
		t.Errorf("expected mimeType 'image/png', got %q", content.MimeType)
	}
	if content.Data != "base64data" {
		t.Errorf("expected data 'base64data', got %q", content.Data)
	}
}

func TestSamplingMessageRoles(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
	}

	for _, tt := range tests {
		if string(tt.role) != tt.want {
			t.Errorf("Role %v: expected %q, got %q", tt.role, tt.want, string(tt.role))
		}
	}
}

func TestCreateMessageRequest(t *testing.T) {
	temp := 0.7
	req := CreateMessageRequest{
		Messages: []SamplingMessage{
			{
				Role:    RoleUser,
				Content: NewTextContent("What is 2+2?"),
			},
		},
		MaxTokens:     100,
		StopSequences: []string{"\n\n"},
		Temperature:   &temp,
		SystemPrompt:  "You are a helpful assistant.",
		ModelPreferences: &ModelPreferences{
			Hints: []ModelHint{{Name: "claude-3"}},
		},
	}

	if len(req.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(req.Messages))
	}
	if req.MaxTokens != 100 {
		t.Errorf("expected maxTokens 100, got %d", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Error("temperature not set correctly")
	}
	if req.ModelPreferences == nil || len(req.ModelPreferences.Hints) != 1 {
		t.Error("model preferences not set correctly")
	}
}

func TestCreateMessageResult(t *testing.T) {
	result := CreateMessageResult{
		Role:       RoleAssistant,
		Content:    NewTextContent("4"),
		Model:      "claude-3-sonnet",
		StopReason: "endTurn",
	}

	if result.Role != RoleAssistant {
		t.Errorf("expected role 'assistant', got %q", result.Role)
	}
	if result.Content.Text != "4" {
		t.Errorf("expected text '4', got %q", result.Content.Text)
	}
	if result.Model != "claude-3-sonnet" {
		t.Errorf("expected model 'claude-3-sonnet', got %q", result.Model)
	}
	if result.StopReason != "endTurn" {
		t.Errorf("expected stopReason 'endTurn', got %q", result.StopReason)
	}
}

func TestModelPreferences(t *testing.T) {
	cost := 0.3
	speed := 0.7
	intelligence := 0.9

	prefs := ModelPreferences{
		Hints: []ModelHint{
			{Name: "claude-3-opus"},
			{Name: "gpt-4"},
		},
		CostPriority:         &cost,
		SpeedPriority:        &speed,
		IntelligencePriority: &intelligence,
	}

	if len(prefs.Hints) != 2 {
		t.Errorf("expected 2 hints, got %d", len(prefs.Hints))
	}
	if *prefs.CostPriority != 0.3 {
		t.Error("cost priority not set correctly")
	}
	if *prefs.SpeedPriority != 0.7 {
		t.Error("speed priority not set correctly")
	}
	if *prefs.IntelligencePriority != 0.9 {
		t.Error("intelligence priority not set correctly")
	}
}
