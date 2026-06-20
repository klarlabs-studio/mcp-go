package transport

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestSSEWriter_WriteData(t *testing.T) {
	var buf bytes.Buffer
	w := NewSSEWriter(&buf, nil)
	if err := w.WriteData([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("WriteData: %v", err)
	}
	if got, want := buf.String(), "data: {\"a\":1}\n\n"; got != want {
		t.Errorf("WriteData = %q, want %q", got, want)
	}
}

func TestSSEWriter_WriteEvent(t *testing.T) {
	var buf bytes.Buffer
	w := NewSSEWriter(&buf, nil)
	if err := w.WriteEvent("connected", []byte(`{"clientId":"x"}`)); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}
	want := "event: connected\ndata: {\"clientId\":\"x\"}\n\n"
	if got := buf.String(); got != want {
		t.Errorf("WriteEvent = %q, want %q", got, want)
	}
}

func TestSSEReader_RoundTrip(t *testing.T) {
	// Server-formatted stream: a connected event, then two data frames.
	var buf bytes.Buffer
	w := NewSSEWriter(&buf, nil)
	_ = w.WriteEvent("connected", []byte(`{"clientId":"x"}`))
	_ = w.WriteData([]byte(`{"method":"a"}`))
	_ = w.WriteData([]byte(`{"method":"b"}`))

	r := NewSSEReader(strings.NewReader(buf.String()))
	var got []string
	for {
		data, err := r.ReadData()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadData: %v", err)
		}
		got = append(got, string(data))
	}
	// All data frames (including the connected event's data) are returned in
	// order; the caller decides which to act on.
	want := []string{`{"clientId":"x"}`, `{"method":"a"}`, `{"method":"b"}`}
	if len(got) != len(want) {
		t.Fatalf("got %d frames %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d = %q, want %q", i, got[i], want[i])
		}
	}
}
