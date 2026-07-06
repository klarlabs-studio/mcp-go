package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestNewlineFramer_OverCapFrameSkippedAndResync(t *testing.T) {
	// A frame larger than maxFrameSize must not wedge the reader. It should
	// surface ErrFrameTooLarge (recoverable) and then resynchronize on the next
	// newline-delimited frame instead of dying or corrupting the stream.
	oversized := strings.Repeat("A", maxFrameSize+1024)
	good := `{"method":"after"}`
	stream := oversized + "\n" + good + "\n"

	r := NewNewlineFramer(strings.NewReader(stream), nil)

	// First read: the oversized frame is reported as a recoverable skip.
	if _, err := r.ReadMessage(); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("first ReadMessage err = %v, want ErrFrameTooLarge", err)
	}

	// Second read: the reader has drained the oversized line and resynced.
	line, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("second ReadMessage: %v", err)
	}
	if string(line) != good {
		t.Fatalf("after skip got %q, want %q", line, good)
	}

	if _, err := r.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF at end, got %v", err)
	}
}

func TestNewlineFramer_AtCapFrameStillReads(t *testing.T) {
	// A frame exactly at the cap must round-trip (boundary must not over-reject).
	// Build a JSON object whose serialized length is exactly maxFrameSize.
	prefix := `{"data":"`
	suffix := `"}`
	payload := prefix + strings.Repeat("x", maxFrameSize-len(prefix)-len(suffix)) + suffix
	if len(payload) != maxFrameSize {
		t.Fatalf("payload len = %d, want %d", len(payload), maxFrameSize)
	}
	r := NewNewlineFramer(strings.NewReader(payload+"\n"), nil)
	line, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage at cap: %v", err)
	}
	if len(line) != maxFrameSize {
		t.Fatalf("line len = %d, want %d", len(line), maxFrameSize)
	}
}

func TestNewlineFramer_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	f := NewNewlineFramer(nil, &buf)

	type msg struct {
		Method string `json:"method"`
	}
	if err := f.WriteMessage(msg{Method: "ping"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := f.WriteMessage(msg{Method: "pong"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// Each message is newline-delimited JSON.
	got := buf.String()
	want := `{"method":"ping"}` + "\n" + `{"method":"pong"}` + "\n"
	if got != want {
		t.Errorf("framed output = %q, want %q", got, want)
	}

	// Read them back.
	r := NewNewlineFramer(strings.NewReader(got), nil)
	for _, wantMethod := range []string{"ping", "pong"} {
		line, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		var m msg
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if m.Method != wantMethod {
			t.Errorf("method = %q, want %q", m.Method, wantMethod)
		}
	}
	if _, err := r.ReadMessage(); err != io.EOF {
		t.Errorf("expected io.EOF at end, got %v", err)
	}
}

func TestNewlineFramer_LargeMessage(t *testing.T) {
	// A payload well above bufio's 64KB default must round-trip (regression for
	// the screenshot-overflow bug the 16MB buffer fixes).
	big := strings.Repeat("x", 1<<20) // 1 MiB
	var buf bytes.Buffer
	w := NewNewlineFramer(nil, &buf)
	if err := w.WriteMessage(map[string]string{"data": big}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	r := NewNewlineFramer(&buf, nil)
	line, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["data"] != big {
		t.Errorf("large payload not preserved (len got %d, want %d)", len(m["data"]), len(big))
	}
}

func TestNewlineFramer_ConcurrentWrites(t *testing.T) {
	// WriteMessage must be safe for concurrent use: lines must not interleave.
	var buf bytes.Buffer
	f := NewNewlineFramer(nil, &buf)

	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.WriteMessage(map[string]int{"i": 1})
		}()
	}
	wg.Wait()

	r := NewNewlineFramer(&buf, nil)
	count := 0
	for {
		line, err := r.ReadMessage()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		var m map[string]int
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("interleaved/corrupt line %q: %v", line, err)
		}
		count++
	}
	if count != n {
		t.Errorf("read %d messages, want %d", count, n)
	}
}
