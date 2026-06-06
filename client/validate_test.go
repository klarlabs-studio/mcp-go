package client_test

import (
	"errors"
	"os/exec"
	"testing"

	"go.klarlabs.de/mcp/client"
)

func TestValidateCommand(t *testing.T) {
	t.Parallel()

	t.Run("accepts valid command in PATH", func(t *testing.T) {
		t.Parallel()

		resolved, err := client.ValidateCommand("go")
		if err != nil {
			t.Skipf("Skipping test: go command not found: %v", err)
		}
		if resolved == "" {
			t.Error("ValidateCommand() should return non-empty path")
		}
	})

	t.Run("rejects empty command", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("")
		if err == nil {
			t.Error("ValidateCommand() should reject empty command")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with semicolon", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("ls;rm")
		if err == nil {
			t.Error("ValidateCommand() should reject command with semicolon")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with pipe", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("cat|grep")
		if err == nil {
			t.Error("ValidateCommand() should reject command with pipe")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with ampersand", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("sleep&")
		if err == nil {
			t.Error("ValidateCommand() should reject command with ampersand")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with dollar sign", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("echo$HOME")
		if err == nil {
			t.Error("ValidateCommand() should reject command with dollar sign")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with backtick", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("echo`whoami`")
		if err == nil {
			t.Error("ValidateCommand() should reject command with backtick")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with parentheses", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("(ls)")
		if err == nil {
			t.Error("ValidateCommand() should reject command with parentheses")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with redirect", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("cat>file")
		if err == nil {
			t.Error("ValidateCommand() should reject command with redirect")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with single quote", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("echo'test'")
		if err == nil {
			t.Error("ValidateCommand() should reject command with single quote")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with double quote", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("echo\"test\"")
		if err == nil {
			t.Error("ValidateCommand() should reject command with double quote")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects command with newline", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("ls\nrm")
		if err == nil {
			t.Error("ValidateCommand() should reject command with newline")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects non-existent command", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("nonexistent-command-xyz123")
		if err == nil {
			t.Error("ValidateCommand() should reject non-existent command")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("accepts absolute path to valid command", func(t *testing.T) {
		t.Parallel()

		goPath, err := exec.LookPath("go")
		if err != nil {
			t.Skipf("Skipping test: go command not found: %v", err)
		}

		resolved, err := client.ValidateCommand(goPath)
		if err != nil {
			t.Fatalf("ValidateCommand() should accept valid absolute path: %v", err)
		}
		if resolved != goPath {
			t.Errorf("ValidateCommand() = %s, want %s", resolved, goPath)
		}
	})

	t.Run("rejects absolute path with traversal", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("/usr/bin/../bin/ls")
		if err == nil {
			t.Error("ValidateCommand() should reject path with traversal elements")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects absolute path to non-existent file", func(t *testing.T) {
		t.Parallel()

		_, err := client.ValidateCommand("/nonexistent/path/to/command")
		if err == nil {
			t.Error("ValidateCommand() should reject non-existent absolute path")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})
}

func TestValidateArgs(t *testing.T) {
	t.Parallel()

	t.Run("accepts clean arguments", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"--flag", "value", "-v", "path/to/file"})
		if err != nil {
			t.Errorf("ValidateArgs() should accept clean arguments: %v", err)
		}
	})

	t.Run("accepts empty args", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs(nil)
		if err != nil {
			t.Errorf("ValidateArgs() should accept nil args: %v", err)
		}
	})

	t.Run("rejects arg with semicolon", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"clean", "arg;inject"})
		if err == nil {
			t.Error("ValidateArgs() should reject arg with semicolon")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects arg with pipe", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"clean", "arg|inject"})
		if err == nil {
			t.Error("ValidateArgs() should reject arg with pipe")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects arg with ampersand", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"arg&inject"})
		if err == nil {
			t.Error("ValidateArgs() should reject arg with ampersand")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects arg with dollar sign", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"$HOME"})
		if err == nil {
			t.Error("ValidateArgs() should reject arg with dollar sign")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects arg with backtick", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"`whoami`"})
		if err == nil {
			t.Error("ValidateArgs() should reject arg with backtick")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("reports correct argument index", func(t *testing.T) {
		t.Parallel()

		err := client.ValidateArgs([]string{"safe", "also-safe", "inject;here"})
		if err == nil {
			t.Error("ValidateArgs() should reject arg with metacharacter")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
		// Error should mention index 2
		if err != nil && !contains(err.Error(), "argument 2") {
			t.Errorf("Error should mention argument 2, got %v", err)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
