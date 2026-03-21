package client_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/felixgeelhaar/mcp-go/client"
)

func TestStdioTransport(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("spawns and communicates with subprocess", func(t *testing.T) {
		// Use a simple echo-like program that reads JSON and responds
		// We'll use a Go test helper for this
		transport, err := client.NewStdioTransport("go", "run", "./testdata/echoserver/main.go")
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		defer transport.Close()

		c := client.New(transport, client.WithTimeout(30*time.Second))

		// Initialize should work
		info, err := c.Initialize(context.Background())
		if err != nil {
			t.Fatalf("initialize failed: %v", err)
		}

		if info.Name != "echo-server" {
			t.Errorf("server name = %q, want %q", info.Name, "echo-server")
		}
	})

	t.Run("handles process not found", func(t *testing.T) {
		_, err := client.NewStdioTransport("nonexistent-command-that-should-not-exist")
		if err == nil {
			t.Fatal("expected error for nonexistent command")
		}
		// Validation now catches this earlier with ErrInvalidCommand
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})
}

func TestStdioTransport_Close(t *testing.T) {
	// Create a cat process that just echoes
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}

	transport, err := client.NewStdioTransport("cat")
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	// Close should not panic
	if err := transport.Close(); err != nil {
		// cat will exit with signal, which is expected
		t.Logf("close returned (expected): %v", err)
	}

	// Close again should be safe
	if err := transport.Close(); err != nil {
		t.Errorf("second close returned error: %v", err)
	}
}

func TestNewStdioTransport_RejectsInjection(t *testing.T) {
	t.Parallel()

	t.Run("rejects command with shell metacharacters", func(t *testing.T) {
		t.Parallel()

		_, err := client.NewStdioTransport("cat;rm -rf /")
		if err == nil {
			t.Fatal("expected error for command with metacharacters")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects args with shell metacharacters", func(t *testing.T) {
		t.Parallel()

		_, err := client.NewStdioTransport("cat", "--flag;inject")
		if err == nil {
			t.Fatal("expected error for args with metacharacters")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})

	t.Run("rejects non-existent command", func(t *testing.T) {
		t.Parallel()

		_, err := client.NewStdioTransport("nonexistent-command-that-should-not-exist")
		if err == nil {
			t.Fatal("expected error for nonexistent command")
		}
		if !errors.Is(err, client.ErrInvalidCommand) {
			t.Errorf("Error should wrap ErrInvalidCommand, got %v", err)
		}
	})
}

func TestNewUnsafeStdioTransport(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}

	t.Run("bypasses validation", func(t *testing.T) {
		transport, err := client.NewUnsafeStdioTransport("cat")
		if err != nil {
			t.Fatalf("NewUnsafeStdioTransport() unexpected error: %v", err)
		}
		defer transport.Close()
	})
}

func TestMain(m *testing.M) {
	// Create test server directory
	os.MkdirAll("testdata/echoserver", 0755)

	// Write a simple echo server for testing
	echoServer := `package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type Request struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      json.RawMessage ` + "`json:\"id\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params,omitempty\"`" + `
}

type Response struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      json.RawMessage ` + "`json:\"id\"`" + `
	Result  any             ` + "`json:\"result,omitempty\"`" + `
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]any{
					"name":    "echo-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]any{},
			}
		case "ping":
			result = map[string]any{}
		default:
			result = map[string]any{}
		}

		resp := Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
		data, _ := json.Marshal(resp)
		os.Stdout.Write(data)
		os.Stdout.Write([]byte("\n"))
	}
}
`
	os.WriteFile("testdata/echoserver/main.go", []byte(echoServer), 0644)

	code := m.Run()

	// Cleanup
	os.RemoveAll("testdata")

	os.Exit(code)
}
