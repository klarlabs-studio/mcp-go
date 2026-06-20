package transport

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
)

// sseMaxFrameSize bounds a single SSE data line on the reader. SSE frames carry
// JSON-RPC notifications, which are small, but the buffer is raised above
// bufio's 64KB default for headroom.
const sseMaxFrameSize = 1024 * 1024

// SSEWriter writes Server-Sent Events. It is the server-side half of the SSE
// wire format shared with SSEReader (the client-side half), so the event
// grammar (the "data: " prefix and the blank-line frame separator) lives in
// one place rather than being duplicated across client and server.
//
// WriteData/WriteEvent are safe for concurrent use. When the underlying writer
// implements http.Flusher (passed as flusher), each frame is flushed so the
// client receives it promptly.
type SSEWriter struct {
	mu      sync.Mutex
	w       io.Writer
	flusher interface{ Flush() }
}

// NewSSEWriter constructs an SSEWriter over w. flusher may be nil; when non-nil
// it is flushed after every frame (pass the http.ResponseWriter, which
// implements http.Flusher).
func NewSSEWriter(w io.Writer, flusher interface{ Flush() }) *SSEWriter {
	return &SSEWriter{w: w, flusher: flusher}
}

// WriteData writes a single SSE data frame: "data: <payload>\n\n".
func (s *SSEWriter) WriteData(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", payload); err != nil {
		return err
	}
	s.flush()
	return nil
}

// WriteEvent writes a named SSE event: "event: <name>\ndata: <payload>\n\n".
func (s *SSEWriter) WriteEvent(name string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		return err
	}
	s.flush()
	return nil
}

func (s *SSEWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// SSEReader reads Server-Sent Events, returning the payload of each data frame.
// It is the client-side half of the SSE wire format shared with SSEWriter.
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader constructs an SSEReader over r.
func NewSSEReader(r io.Reader) *SSEReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), sseMaxFrameSize)
	return &SSEReader{scanner: sc}
}

// ReadData returns the payload of the next SSE data frame, skipping event
// lines, comments, and blank separators. It returns io.EOF when the stream
// ends. The returned slice is valid until the next ReadData call.
func (s *SSEReader) ReadData() ([]byte, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "" {
			continue // event lines, comments, blank separators
		}
		return []byte(data), nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}
