package capture

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// maxPayload guards against a corrupt length prefix allocating unbounded memory.
const maxPayload = 64 << 20

// Reader iterates the records of a capture file in write order.
type Reader struct {
	f    *os.File
	gz   *gzip.Reader
	br   *bufio.Reader
	meta Meta
}

// OpenReader opens path, validates the magic, and reads the header record so
// Meta is available before the first Next call.
func OpenReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("capture: open %s: %w", path, err)
	}
	magic := make([]byte, len(Magic))
	if _, err := io.ReadFull(f, magic); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: %s: %v", ErrBadMagic, path, err)
	}
	if string(magic) != Magic {
		f.Close()
		return nil, fmt.Errorf("%w: %s", ErrBadMagic, path)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("capture: gzip %s: %w", path, err)
	}
	r := &Reader{f: f, gz: gz, br: bufio.NewReaderSize(gz, 64<<10)}

	rec, err := r.Next()
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("capture: read header record: %w", err)
	}
	if rec.Kind != KindHeader || rec.Meta == nil {
		r.Close()
		return nil, fmt.Errorf("capture: %s: first record is kind %d, want header", path, rec.Kind)
	}
	r.meta = *rec.Meta
	return r, nil
}

// Meta returns the variable layout the capture was recorded against.
func (r *Reader) Meta() Meta { return r.meta }

// Next returns the next record, or io.EOF when the file is exhausted.
func (r *Reader) Next() (Record, error) {
	var hdr [13]byte
	if _, err := io.ReadFull(r.br, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return Record{}, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Record{}, fmt.Errorf("capture: truncated record header: %w", err)
		}
		return Record{}, err
	}
	rec := Record{
		Kind: Kind(hdr[0]),
		T:    math.Float64frombits(binary.LittleEndian.Uint64(hdr[1:])),
	}
	n := binary.LittleEndian.Uint32(hdr[9:])
	if n > maxPayload {
		return Record{}, fmt.Errorf("capture: record payload %d exceeds %d", n, maxPayload)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r.br, payload); err != nil {
		return Record{}, fmt.Errorf("capture: truncated payload: %w", err)
	}

	switch rec.Kind {
	case KindHeader:
		var m Meta
		if err := json.Unmarshal(payload, &m); err != nil {
			return Record{}, fmt.Errorf("capture: unmarshal meta: %w", err)
		}
		rec.Meta = &m
	case KindSession:
		if len(payload) < 4 {
			return Record{}, errors.New("capture: session record shorter than 4 bytes")
		}
		rec.Update = binary.LittleEndian.Uint32(payload)
		rec.YAML = payload[4:]
	case KindVars:
		if len(payload) < 4 {
			return Record{}, errors.New("capture: vars record shorter than 4 bytes")
		}
		rec.TickCount = binary.LittleEndian.Uint32(payload)
		rec.Vars = payload[4:]
	default:
		return Record{}, fmt.Errorf("capture: unknown record kind %d", rec.Kind)
	}
	return rec, nil
}

// Close releases the gzip stream and the underlying file.
func (r *Reader) Close() error {
	var firstErr error
	if r.gz != nil {
		if err := r.gz.Close(); err != nil {
			firstErr = err
		}
		r.gz = nil
	}
	if r.f != nil {
		if err := r.f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		r.f = nil
	}
	return firstErr
}
