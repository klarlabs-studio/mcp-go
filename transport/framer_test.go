package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
)

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
