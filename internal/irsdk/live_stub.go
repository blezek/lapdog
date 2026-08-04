//go:build !windows

package irsdk

// Conn is a placeholder on platforms without shared-memory telemetry.
type Conn struct{}

// Open always fails off Windows.
//
// Development and testing use the replay source instead, which is why the rest of
// the package needs no stubbing: everything except the mapping itself is portable
// and is exercised by the tests on every platform.
func Open() (*Conn, error) { return nil, ErrUnsupported }

// Snapshot always fails off Windows.
func (c *Conn) Snapshot() (Header, []VarHeader, []byte, []byte, error) {
	return Header{}, nil, nil, nil, ErrUnsupported
}

// Close is a no-op off Windows.
func (c *Conn) Close() error { return nil }
