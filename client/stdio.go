// Package client provides an MCP client for connecting to MCP servers.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"go.klarlabs.de/mcp/protocol"
)

// StdioTransport connects to an MCP server via subprocess stdio.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu       sync.Mutex
	respChan map[int64]chan *protocol.Response
	scanner  *bufio.Scanner
	closed   bool

	readWG sync.WaitGroup
}

// StdioTransportOption configures a StdioTransport.
type StdioTransportOption func(*StdioTransport)

// NewStdioTransport creates a transport that spawns a subprocess.
// The command and arguments are validated against shell metacharacters
// and the command is resolved to an absolute path before execution.
// Use [NewUnsafeStdioTransport] to bypass validation for trusted contexts.
func NewStdioTransport(command string, args ...string) (*StdioTransport, error) {
	resolved, err := ValidateCommand(command)
	if err != nil {
		return nil, fmt.Errorf("validate command: %w", err)
	}

	if err := ValidateArgs(args); err != nil {
		return nil, fmt.Errorf("validate args: %w", err)
	}

	return newStdioTransport(resolved, args)
}

// NewUnsafeStdioTransport creates a transport without command validation.
// Use this only when the command and arguments come from a trusted source
// (e.g., hard-coded in application code).
func NewUnsafeStdioTransport(command string, args ...string) (*StdioTransport, error) {
	return newStdioTransport(command, args)
}

func newStdioTransport(command string, args []string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	t := &StdioTransport{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		respChan: make(map[int64]chan *protocol.Response),
		scanner:  bufio.NewScanner(stdout),
	}

	// Start reading responses
	t.readWG.Add(1)
	go t.readResponses()

	return t, nil
}

// Send sends a request and waits for a response.
func (t *StdioTransport) Send(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}

	// Get request ID
	var id int64
	if err := json.Unmarshal(req.ID, &id); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("invalid request ID: %w", err)
	}

	// Create response channel
	respCh := make(chan *protocol.Response, 1)
	t.respChan[id] = respCh
	t.mu.Unlock()

	// Clean up on return
	defer func() {
		t.mu.Lock()
		delete(t.respChan, id)
		t.mu.Unlock()
	}()

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	t.mu.Lock()
	_, err = t.stdin.Write(append(data, '\n'))
	t.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

// Close closes the transport and terminates the subprocess.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	// Close stdin to signal EOF
	_ = t.stdin.Close()

	// Wait for read goroutine
	t.readWG.Wait()

	// Kill process if still running (ignoring error as process may have exited)
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill() //nolint:errcheck // Process may have already exited
	}

	return t.cmd.Wait()
}

func (t *StdioTransport) readResponses() {
	defer t.readWG.Done()

	for t.scanner.Scan() {
		line := t.scanner.Text()

		var resp protocol.Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue // Skip malformed responses
		}

		// Get response ID
		var id int64
		if err := json.Unmarshal(resp.ID, &id); err != nil {
			continue
		}

		// Dispatch to waiting caller
		t.mu.Lock()
		if ch, ok := t.respChan[id]; ok {
			ch <- &resp
		}
		t.mu.Unlock()
	}
}

// Stderr returns the stderr reader for the subprocess.
func (t *StdioTransport) Stderr() io.Reader {
	return t.stderr
}
