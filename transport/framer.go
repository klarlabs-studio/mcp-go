package transport

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// maxFrameSize bounds a single newline-delimited JSON message. bufio's 64KB
// default is too small for large tool payloads (e.g. a browser-automation
// annotated_screenshot at ~66KB+), so the reader is raised to 16MB. Both the
// client and the server use this single value so the two sides agree on the
// maximum message size.
const maxFrameSize = 16 * 1024 * 1024

// readChunk sizes the buffered reader used by ReadMessage. It is the largest
// slice ReadMessage accumulates per underlying read; a modest value keeps the
// common (small message) path cheap while still round-tripping messages up to
// maxFrameSize by looping.
const readChunk = 64 * 1024

// ErrFrameTooLarge is returned by ReadMessage when a single frame exceeds
// maxFrameSize. It is recoverable: the framer drains the remainder of the
// oversized line so the NEXT ReadMessage resynchronizes on the following frame.
// Read loops should treat it as "skip this message" rather than a fatal error,
// keeping the transport alive instead of wedging on one malformed frame.
var ErrFrameTooLarge = errors.New("mcp-go/transport: frame exceeds maximum size")

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
	reader *bufio.Reader
	buf    []byte // reused accumulation buffer for the current line

	mu sync.Mutex
	w  io.Writer
}

// NewNewlineFramer constructs a framer over the given reader and writer. Either
// may be nil when only one direction is needed.
func NewNewlineFramer(r io.Reader, w io.Writer) *NewlineFramer {
	f := &NewlineFramer{w: w}
	if r != nil {
		f.reader = bufio.NewReaderSize(r, readChunk)
	}
	return f
}

// ReadMessage reads one newline-delimited JSON message and returns its raw
// bytes (without the trailing newline). It returns io.EOF when the underlying
// reader is exhausted, and ErrFrameTooLarge when a single frame exceeds
// maxFrameSize — in the latter case it has already drained the oversized line,
// so a subsequent ReadMessage resynchronizes on the next frame. The returned
// slice is only valid until the next ReadMessage call; callers that retain it
// must copy.
func (f *NewlineFramer) ReadMessage() ([]byte, error) {
	if f.reader == nil {
		return nil, io.EOF
	}

	f.buf = f.buf[:0]
	overflow := false // current line already exceeded maxFrameSize; drain and skip

	for {
		chunk, err := f.reader.ReadSlice('\n')

		switch {
		case err == nil:
			// A complete line terminated by '\n' (chunk includes it). The
			// delimiter itself does not count toward the frame size, so measure
			// content only (chunk always holds at least the trailing '\n').
			if overflow || len(f.buf)+len(chunk)-1 > maxFrameSize {
				f.buf = f.buf[:0]
				return nil, ErrFrameTooLarge
			}
			f.buf = append(f.buf, chunk...)
			return trimLineEnding(f.buf), nil

		case errors.Is(err, bufio.ErrBufferFull):
			// Partial line: the delimiter was not found within the buffer.
			// Accumulate unless we have already overflowed, in which case we
			// keep reading to drain the rest of the oversized line.
			if !overflow {
				if len(f.buf)+len(chunk) > maxFrameSize {
					overflow = true
					f.buf = f.buf[:0]
				} else {
					f.buf = append(f.buf, chunk...)
				}
			}
			continue

		default:
			// io.EOF or a genuine read error. A trailing partial line without a
			// final newline is still a valid frame (bufio.Scanner returned it),
			// so preserve it — unless it, too, exceeds the cap.
			if errors.Is(err, io.EOF) {
				if overflow || len(f.buf)+len(chunk) > maxFrameSize {
					return nil, ErrFrameTooLarge
				}
				if len(chunk) > 0 {
					f.buf = append(f.buf, chunk...)
				}
				if len(f.buf) > 0 {
					return trimLineEnding(f.buf), nil
				}
				return nil, io.EOF
			}
			return nil, err
		}
	}
}

// trimLineEnding drops a single trailing "\n" or "\r\n" from a framed line.
func trimLineEnding(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
	}
	return line
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
