//go:build windows

package irsdk

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fallbackMappingSize bounds the view when the region's true size cannot be
// determined.
//
// It is a floor for safety, not a guess at the real size: every offset read out of
// the mapping is bounds checked against whatever length is in use, so a smaller
// bound can only truncate, never overrun. The header, the variable headers and the
// triple buffers all sit well inside it.
const fallbackMappingSize = 1 << 20

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
	// A length of zero maps the whole section, which is what the reference SDK does
	// (irsdk_utils.cpp: MapViewOfFile(h, FILE_MAP_READ, 0, 0, 0)).
	//
	// Passing an explicit length here was a real bug and the reason a Windows build
	// connected to iRacing and recorded nothing: MapViewOfFile fails outright when
	// asked for more bytes than the section contains, and the section the simulator
	// creates is computed from its own limits rather than being a round number. The
	// request was 2 MiB, the call returned ERROR_ACCESS_DENIED, and every poll then
	// reported the simulator as absent.
	view, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("irsdk: map view of %s: %w", MemMapFileName, err)
	}

	// Ask the kernel how large the mapped region actually is, rather than assuming.
	// The slice must not extend past the section, or a header offset near the end
	// would read unmapped memory instead of being rejected by the bounds checks.
	size := regionSize(view)
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
		buf:    unsafe.Slice((*byte)(unsafe.Pointer(view)), size),
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

// regionSize reports how many bytes are mapped at view.
//
// VirtualQuery is asked rather than trusting a constant, because the simulator sizes
// its section from its own limits and those have changed across versions. If the
// query fails the fallback applies, which can only under-report: every offset taken
// from the mapping is bounds checked against the length in use, so a short bound
// rejects a read that a correct bound would have allowed, rather than reading memory
// that is not there.
func regionSize(view uintptr) int {
	var mbi windows.MemoryBasicInformation
	if err := windows.VirtualQuery(view, &mbi, unsafe.Sizeof(mbi)); err != nil {
		return fallbackMappingSize
	}
	if mbi.RegionSize < uintptr(HeaderSize) {
		return fallbackMappingSize
	}
	return int(mbi.RegionSize)
}
