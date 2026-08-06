//go:build !windows

package irsdk

// Conn is a placeholder on platforms without shared-memory telemetry.
type Conn struct{}

// Open always fails off Windows.
//
// Development and testing use the replay source instead, which is why the rest of
// the package needs no stubbing: everything except the mapping itself is portable
// and is exercised by the tests on every platform.
func Open() (*Conn, error) { return OpenTraced(nil) }

// OpenTraced always fails off Windows, and says so through the trace as well as the
// error, so a log from a development machine reads the same way as one from Windows.
func OpenTraced(tr Trace) (*Conn, error) {
	tr.note("live telemetry is unavailable on this platform",
		"name", MemMapFileName,
		"meaning", "only Windows can read the simulator's shared memory")
	return nil, ErrUnsupported
}

// Snapshot always fails off Windows.
func (c *Conn) Snapshot() (Header, []VarHeader, []byte, []byte, error) {
	return c.SnapshotTraced(nil)
}

// SnapshotTraced always fails off Windows.
func (c *Conn) SnapshotTraced(tr Trace) (Header, []VarHeader, []byte, []byte, error) {
	tr.note("snapshot is unavailable on this platform")
	return Header{}, nil, nil, nil, ErrUnsupported
}

// Close is a no-op off Windows.
func (c *Conn) Close() error { return nil }
