package transport

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// maxFrameSize bounds a single newline-delimited JSON message. bufio's 64KB
// default is too small for large tool payloads (e.g. a browser-automation
// annotated_screenshot at ~66KB+), so the reader is raised to 16MB. Both the
// client and the server use this single value so the two sides agree on the
// maximum message size.
const maxFrameSize = 16 * 1024 * 1024

// NewlineFramer reads and writes JSON-RPC messages as newline-delimited JSON
// (one compact JSON value per line). It is the single framing primitive shared
// by the stdio client and the stdio server so the wire format and buffering
// limits never drift between the two sides.
//
// A NewlineFramer may be used for reading only, writing only, or both. Writes
// are serialized so the framer is safe for concurrent WriteMessage calls;
// reads are not safe for concurrent ReadMessage calls and are expected to run
// from a single goroutine.
type NewlineFramer struct {
	scanner *bufio.Scanner

	mu sync.Mutex
	w  io.Writer
}

// NewNewlineFramer constructs a framer over the given reader and writer. Either
// may be nil when only one direction is needed.
func NewNewlineFramer(r io.Reader, w io.Writer) *NewlineFramer {
	f := &NewlineFramer{w: w}
	if r != nil {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), maxFrameSize)
		f.scanner = sc
	}
	return f
}

// ReadMessage reads one newline-delimited JSON message and returns its raw
// bytes. It returns io.EOF when the underlying reader is exhausted. The
// returned slice is only valid until the next ReadMessage call; callers that
// retain it must copy.
func (f *NewlineFramer) ReadMessage() ([]byte, error) {
	if f.scanner == nil {
		return nil, io.EOF
	}
	if !f.scanner.Scan() {
		if err := f.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return f.scanner.Bytes(), nil
}

// WriteMessage marshals v to compact JSON and writes it followed by a newline.
// It is safe for concurrent use; concurrent messages never interleave.
func (f *NewlineFramer) WriteMessage(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return f.WriteRaw(data)
}

// WriteRaw writes pre-marshaled JSON bytes followed by a newline. It is safe
// for concurrent use.
func (f *NewlineFramer) WriteRaw(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Write the payload and the delimiter in one call so a concurrent writer
	// cannot interleave between them.
	buf := make([]byte, 0, len(data)+1)
	buf = append(buf, data...)
	buf = append(buf, '\n')
	_, err := f.w.Write(buf)
	return err
}
