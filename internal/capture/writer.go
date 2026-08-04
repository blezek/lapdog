package capture

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// Writer appends records to a capture file. It is not safe for concurrent use;
// the collector owns one Writer at a time.
type Writer struct {
	path string
	f    *os.File
	bw   *bufio.Writer
	gz   *gzip.Writer
}

// NewWriter creates path and writes the magic and the header record.
func NewWriter(path string, m Meta) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("capture: create %s: %w", path, err)
	}
	bw := bufio.NewWriterSize(f, 64<<10)
	if _, err := bw.WriteString(Magic); err != nil {
		f.Close()
		return nil, fmt.Errorf("capture: write magic: %w", err)
	}
	w := &Writer{path: path, f: f, bw: bw, gz: gzip.NewWriter(bw)}

	payload, err := json.Marshal(m)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("capture: marshal meta: %w", err)
	}
	if err := w.writeRecord(KindHeader, 0, payload); err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

// Path returns the file the Writer is appending to.
func (w *Writer) Path() string { return w.path }

// writeRecord emits one length-prefixed record into the gzip stream.
func (w *Writer) writeRecord(k Kind, t float64, payload []byte) error {
	var hdr [13]byte
	hdr[0] = byte(k)
	binary.LittleEndian.PutUint64(hdr[1:], math.Float64bits(t))
	binary.LittleEndian.PutUint32(hdr[9:], uint32(len(payload)))
	if _, err := w.gz.Write(hdr[:]); err != nil {
		return fmt.Errorf("capture: write record header: %w", err)
	}
	if _, err := w.gz.Write(payload); err != nil {
		return fmt.Errorf("capture: write record payload: %w", err)
	}
	return nil
}

// WriteSession records a session-info YAML blob at time t.
func (w *Writer) WriteSession(t float64, update uint32, yaml []byte) error {
	payload := make([]byte, 4+len(yaml))
	binary.LittleEndian.PutUint32(payload, update)
	copy(payload[4:], yaml)
	return w.writeRecord(KindSession, t, payload)
}

// WriteVars records one raw variable-buffer row at time t.
func (w *Writer) WriteVars(t float64, tick uint32, row []byte) error {
	payload := make([]byte, 4+len(row))
	binary.LittleEndian.PutUint32(payload, tick)
	copy(payload[4:], row)
	return w.writeRecord(KindVars, t, payload)
}

// Close flushes the gzip stream and closes the file.
func (w *Writer) Close() error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if w.gz != nil {
		note(w.gz.Close())
		w.gz = nil
	}
	if w.bw != nil {
		note(w.bw.Flush())
		w.bw = nil
	}
	if w.f != nil {
		note(w.f.Close())
		w.f = nil
	}
	return firstErr
}
