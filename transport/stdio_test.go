package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

func TestNewStdio(t *testing.T) {
	t.Run("creates stdio transport with defaults", func(t *testing.T) {
		transport := NewStdio()

		if transport == nil {
			t.Fatal("expected transport to be created")
		}

		if transport.Addr() != "stdio" {
			t.Errorf("Addr() = %q, want %q", transport.Addr(), "stdio")
		}
	})

	t.Run("creates stdio transport with custom streams", func(t *testing.T) {
		in := &bytes.Buffer{}
		out := &bytes.Buffer{}
		errOut := &bytes.Buffer{}

		transport := NewStdio(
			WithStdin(in),
			WithStdout(out),
			WithStderr(errOut),
		)

		if transport.in != in {
			t.Error("expected custom stdin to be used")
		}
		if transport.out != out {
			t.Error("expected custom stdout to be used")
		}
		if transport.errOut != errOut {
			t.Error("expected custom stderr to be used")
		}
	})
}

func TestStdio_Serve(t *testing.T) {
	t.Run("processes single request", func(t *testing.T) {
		req := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test/method",
		}
		reqBytes, _ := json.Marshal(req)

		in := bytes.NewBuffer(append(reqBytes, '\n'))
		out := &bytes.Buffer{}

		transport := NewStdio(
			WithStdin(in),
			WithStdout(out),
		)

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "success"), nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Serve will exit when stdin is exhausted
		_ = transport.Serve(ctx, handler)

		// Check output
		output := out.String()
		if !strings.Contains(output, `"result":"success"`) {
			t.Errorf("output = %q, expected to contain success result", output)
		}
	})

	t.Run("handles multiple requests", func(t *testing.T) {
		req1 := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "method1",
		}
		req2 := protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`2`),
			Method:  "method2",
		}

		req1Bytes, _ := json.Marshal(req1)
		req2Bytes, _ := json.Marshal(req2)

		input := string(req1Bytes) + "\n" + string(req2Bytes) + "\n"
		in := bytes.NewBufferString(input)
		out := &bytes.Buffer{}

		transport := NewStdio(
			WithStdin(in),
			WithStdout(out),
		)

		callCount := 0
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			callCount++
			return protocol.NewResponse(req.ID, req.Method), nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_ = transport.Serve(ctx, handler)

		if callCount != 2 {
			t.Errorf("handler called %d times, want 2", callCount)
		}
	})

	t.Run("returns error response for invalid JSON", func(t *testing.T) {
		in := bytes.NewBufferString("{invalid json}\n")
		out := &bytes.Buffer{}

		transport := NewStdio(
			WithStdin(in),
			WithStdout(out),
		)

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			t.Error("handler should not be called for invalid JSON")
			return nil, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_ = transport.Serve(ctx, handler)

		output := out.String()
		if !strings.Contains(output, `"error"`) {
			t.Errorf("expected error response, got %q", output)
		}
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		// Use a reader that blocks forever
		in := &blockingReader{}
		out := &bytes.Buffer{}

		transport := NewStdio(
			WithStdin(in),
			WithStdout(out),
		)

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- transport.Serve(ctx, handler)
		}()

		// Cancel after a short delay
		time.Sleep(10 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			if err != context.Canceled {
				t.Errorf("expected context.Canceled, got %v", err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("Serve did not stop after context cancellation")
		}
	})

	t.Run("skips notifications (no response)", func(t *testing.T) {
		notification := protocol.Request{
			JSONRPC: "2.0",
			Method:  "notifications/test",
			// No ID = notification
		}
		notifBytes, _ := json.Marshal(notification)

		in := bytes.NewBuffer(append(notifBytes, '\n'))
		out := &bytes.Buffer{}

		transport := NewStdio(
			WithStdin(in),
			WithStdout(out),
		)

		handlerCalled := false
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			handlerCalled = true
			return nil, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_ = transport.Serve(ctx, handler)

		if !handlerCalled {
			t.Error("handler should be called for notifications")
		}

		// Notifications should not produce output
		if out.Len() > 0 {
			t.Errorf("expected no output for notification, got %q", out.String())
		}
	})
}

// interleaveProneWriter models an unsynchronized sink such as os.Stdout: a
// single Write is performed as several smaller underlying operations with a
// goroutine yield between them, so two concurrent Write calls can interleave
// their bytes byte-for-byte. The final bytes are recorded under a mutex so the
// test can inspect the result, but the per-Write splitting deliberately does NOT
// hold that mutex across the whole Write — interleaving protection must come
// from the caller serializing Writes through a single framer.
type interleaveProneWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *interleaveProneWriter) Write(p []byte) (int, error) {
	// Split each Write into individual bytes with a yield between them. If the
	// caller does not serialize Writes, concurrent calls interleave and produce
	// corrupted, unparseable frames.
	for i := 0; i < len(p); i++ {
		w.mu.Lock()
		w.buf.WriteByte(p[i])
		w.mu.Unlock()
		runtime.Gosched()
	}
	return len(p), nil
}

func (w *interleaveProneWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestStdio_ConcurrentSendNotificationBeforeServe verifies that out-of-Serve
// writes (SendNotification fired before Serve is active) are serialized through
// a single shared framer. Two concurrent senders sharing the same output must
// not interleave their bytes, and every emitted line must be a well-formed,
// complete NDJSON frame. Run with -race to catch the data race directly.
func TestStdio_ConcurrentSendNotificationBeforeServe(t *testing.T) {
	out := &interleaveProneWriter{}
	transport := NewStdio(WithStdout(out))

	const senders = 16
	const perSender = 50

	var wg sync.WaitGroup
	wg.Add(senders)
	for s := 0; s < senders; s++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perSender; i++ {
				if err := transport.SendNotification("notifications/progress", map[string]int{
					"sender": id,
					"seq":    i,
				}); err != nil {
					t.Errorf("SendNotification: %v", err)
					return
				}
			}
		}(s)
	}
	wg.Wait()

	// Every line must be a complete, parseable JSON-RPC notification. If two
	// writes interleaved, at least one line would fail to unmarshal.
	output := out.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if got, want := len(lines), senders*perSender; got != want {
		t.Fatalf("got %d lines, want %d (interleaving splits or merges frames)", got, want)
	}
	for i, line := range lines {
		var n Notification
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			t.Fatalf("line %d is not a well-formed frame: %v\nline=%q", i, err, line)
		}
		if n.Method != "notifications/progress" {
			t.Fatalf("line %d method = %q, want notifications/progress", i, n.Method)
		}
	}
}

// blockingReader is a reader that blocks until context is done
type blockingReader struct{}

func (r *blockingReader) Read(p []byte) (n int, err error) {
	// Block forever (will be interrupted by context)
	select {}
}
