//go:build windows

package irsdk

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mappingSize is how much of the shared memory region to map.
//
// The sim does not publish the region's size, so a fixed window is mapped. 2 MiB
// comfortably covers the header, the variable headers, the triple buffers and a
// large session YAML string; the SDK's own samples take the same approach.
//
// Mapping more than the region contains is safe because the pages are only read
// where the header says data lives, and every one of those offsets is bounds
// checked against this length before use.
const mappingSize = 2 << 20

// OpenFileMappingW, declared here because golang.org/x/sys/windows does not wrap
// it.
//
// CreateFileMapping is not a substitute. It would *create* the region when it is
// absent and so succeed with no simulator running, which would turn the one signal
// that says "iRacing is not up" into a mapping full of zeroes.
var (
	modkernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procOpenFileMappingW = modkernel32.NewProc("OpenFileMappingW")
)

// openFileMapping opens an existing named section for reading.
func openFileMapping(access uint32, name *uint16) (windows.Handle, error) {
	r, _, e := procOpenFileMappingW.Call(
		uintptr(access),
		0, // bInheritHandle
		uintptr(unsafe.Pointer(name)),
	)
	if r == 0 {
		if e != nil && e != windows.ERROR_SUCCESS {
			return 0, e
		}
		return 0, windows.ERROR_FILE_NOT_FOUND
	}
	return windows.Handle(r), nil
}

// Conn is an open, read-only view of the simulator's shared memory.
//
// It holds no lock and does no synchronisation with the sim. Consistency comes
// from the tick counters instead: see readRow.
type Conn struct {
	handle windows.Handle
	view   uintptr
	buf    []byte
}

// Open maps the simulator's telemetry region.
//
// It returns ErrNotRunning when the sim is not running, which the collector treats
// as the normal idle state rather than a failure. The mapping only exists while
// iRacing is up, so failing to open it is the ordinary case, not an error worth
// surfacing to the user.
func Open() (*Conn, error) {
	name, err := windows.UTF16PtrFromString(MemMapFileName)
	if err != nil {
		return nil, fmt.Errorf("irsdk: encode mapping name: %w", err)
	}
	h, err := openFileMapping(windows.FILE_MAP_READ, name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	view, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, mappingSize)
	if err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("irsdk: map view: %w", err)
	}
	// `go vet` reports "possible misuse of unsafe.Pointer" on the conversion below,
	// and it is a false positive here rather than something to work around.
	//
	// The check exists because a uintptr is not a reference the garbage collector
	// tracks, so converting one back to a pointer is normally a way to resurrect an
	// address that has already moved. This address never moves: it names a mapped
	// view outside the Go heap, and it stays valid until UnmapViewOfFile. The real
	// hazard is using the slice after Close, which is why Close clears buf and
	// Snapshot rejects a nil one.
	//
	// It is why CI vets for the host only. Do not "fix" this by switching to
	// CreateFileMapping, which would fabricate an empty region when the sim is
	// absent and silently report a connected simulator publishing zeroes.
	return &Conn{
		handle: h,
		view:   view,
		buf:    unsafe.Slice((*byte)(unsafe.Pointer(view)), mappingSize),
	}, nil
}

// Snapshot reads the header, the variable layout, the newest untorn variable row
// and the session YAML.
//
// All the parsing happens in snapshotFrom, which has no build tag and is tested on
// every platform. This method exists only to hand it the mapped bytes.
func (c *Conn) Snapshot() (Header, []VarHeader, []byte, []byte, error) {
	if c == nil || c.buf == nil {
		return Header{}, nil, nil, nil, ErrNotRunning
	}
	return snapshotFrom(c.buf)
}

// Close unmaps the view and releases the handle.
//
// Both steps are attempted even if the first fails, so a failed unmap cannot leak
// the handle as well. Close is safe to call more than once.
func (c *Conn) Close() error {
	var firstErr error
	if c.view != 0 {
		if err := windows.UnmapViewOfFile(c.view); err != nil {
			firstErr = err
		}
		c.view = 0
		c.buf = nil
	}
	if c.handle != 0 {
		if err := windows.CloseHandle(c.handle); err != nil && firstErr == nil {
			firstErr = err
		}
		c.handle = 0
	}
	return firstErr
}
