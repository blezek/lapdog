package irsdk

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Off Windows, Open must fail cleanly with ErrUnsupported rather than panicking
// or blocking. This is what lets the rest of the test suite run on macOS.
func TestOpenOffWindowsIsUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this asserts the non-Windows stub")
	}
	c, err := Open()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Open() = %v, want ErrUnsupported", err)
	}
	if c != nil {
		t.Error("Open() returned a non-nil Conn alongside an error")
	}
}

// On Windows without iRacing running, Open must report ErrNotRunning rather than
// a generic failure, so the collector can treat it as the normal idle state.
func TestOpenOnWindowsWithoutSim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	c, err := Open()
	if err == nil {
		// iRacing is running on this machine; that is a valid outcome.
		c.Close()
		t.Skip("iRacing appears to be running; nothing to assert")
	}
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("Open() without the sim = %v, want ErrNotRunning", err)
	}
}

// putI writes a little-endian int32, the only integer encoding the header uses.
func putI(b []byte, off int, v int32) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
	b[off+2] = byte(v >> 16)
	b[off+3] = byte(v >> 24)
}

// syntheticMapping lays out a complete shared-memory region: header, variable
// headers, session YAML and two variable buffers, with the newest holding Lap 42.
func syntheticMapping(yamlText string) []byte {
	const (
		numVars  = 2
		bufLen   = 8
		vhOffset = HeaderSize
		buf0Off  = 4096
		buf1Off  = 8192
	)
	yamlOff := vhOffset + numVars*VarHeaderSize
	mapping := make([]byte, 12288)

	putI(mapping, 0, 2)               // ver
	putI(mapping, 4, StatusConnected) // status
	putI(mapping, 8, 60)              // tickRate
	putI(mapping, 12, 3)              // sessionInfoUpdate
	putI(mapping, 16, int32(len(yamlText)))
	putI(mapping, 20, int32(yamlOff))
	putI(mapping, 24, numVars)
	putI(mapping, 28, vhOffset)
	putI(mapping, 32, 2) // numBuf
	putI(mapping, 36, bufLen)
	putI(mapping, 40, 200) // curBufTickCount

	// varBuf[0] is the older generation, varBuf[1] the newest. The stride is 16
	// rather than 12: the struct is padded, which HeaderSize already accounts for.
	putI(mapping, 48, 100)
	putI(mapping, 52, buf0Off)
	putI(mapping, 56, 100)
	putI(mapping, 64, 200)
	putI(mapping, 68, buf1Off)
	putI(mapping, 72, 200)

	putI(mapping, vhOffset, int32(VarInt))
	putI(mapping, vhOffset+4, 0)
	putI(mapping, vhOffset+8, 1)
	copy(mapping[vhOffset+16:], "Lap\x00")
	putI(mapping, vhOffset+VarHeaderSize, int32(VarFloat))
	putI(mapping, vhOffset+VarHeaderSize+4, 4)
	putI(mapping, vhOffset+VarHeaderSize+8, 1)
	copy(mapping[vhOffset+VarHeaderSize+16:], "Speed\x00")

	copy(mapping[yamlOff:], yamlText)

	putI(mapping, buf0Off, 7)
	putI(mapping, buf1Off, 42)
	return mapping
}

// snapshotFrom is the pure part of the read path: given raw mapped bytes, extract
// the layout, the newest row and the YAML. Testing it here means the only code
// untested on macOS is the memory mapping itself.
func TestSnapshotFromSyntheticMapping(t *testing.T) {
	const yamlText = "WeekendInfo:\n TrackID: 18\n"
	mapping := syntheticMapping(yamlText)

	hdr, vh, row, yaml, err := snapshotFrom(mapping)
	if err != nil {
		t.Fatalf("snapshotFrom: %v", err)
	}
	if !hdr.Connected() || hdr.TickRate != 60 {
		t.Errorf("header = %+v", hdr)
	}
	if len(vh) != 2 || vh[0].Name != "Lap" || vh[1].Name != "Speed" {
		t.Errorf("var headers = %+v", vh)
	}
	if string(yaml) != yamlText {
		t.Errorf("yaml = %q, want %q", yaml, yamlText)
	}
	if len(row) != 8 {
		t.Fatalf("row length = %d, want 8", len(row))
	}
	// The newest buffer must win. Reading the older one would silently serve
	// stale telemetry, which is indistinguishable from the sim being paused.
	if lap, ok := NewRow(vh, row).Int("Lap"); !ok || lap != 42 {
		t.Errorf("decoded Lap = %d, %v; want 42 from the newest buffer", lap, ok)
	}
}

func TestSnapshotFromRejectsTruncatedMapping(t *testing.T) {
	if _, _, _, _, err := snapshotFrom(make([]byte, HeaderSize-1)); err == nil {
		t.Error("snapshotFrom on a mapping smaller than the header = nil, want an error")
	}
}

// A header pointing past the end of the mapping must error rather than panic on a
// slice out of range. The mapping is a fixed window over memory the sim owns, so
// its contents are not to be trusted for bounds.
func TestSnapshotFromRejectsOutOfRangeOffsets(t *testing.T) {
	mapping := make([]byte, HeaderSize+64)
	putI(mapping, 0, 2)
	putI(mapping, 4, StatusConnected)
	putI(mapping, 24, 1)       // numVars
	putI(mapping, 28, 1000000) // varHeaderOffset well past the end
	putI(mapping, 32, 1)
	putI(mapping, 36, 8)
	putI(mapping, 48, 10)
	putI(mapping, 52, 900000) // bufOffset past the end
	putI(mapping, 56, 10)

	if _, _, _, _, err := snapshotFrom(mapping); err == nil {
		t.Error("snapshotFrom with out-of-range offsets = nil, want an error")
	}
}

// A row offset that is in range but whose end is not must also be rejected.
func TestSnapshotFromRejectsRowRunningPastTheEnd(t *testing.T) {
	mapping := syntheticMapping("x")
	// Point the newest buffer four bytes short of the end with an eight byte row.
	putI(mapping, 68, int32(len(mapping)-4))
	if _, _, _, _, err := snapshotFrom(mapping); err == nil {
		t.Error("snapshotFrom with a row overrunning the mapping = nil, want an error")
	}
}

func TestSnapshotFromDisconnectedStatus(t *testing.T) {
	mapping := make([]byte, 4096)
	// ver set, status bit clear.
	mapping[0] = 2
	if _, _, _, _, err := snapshotFrom(mapping); !errors.Is(err, ErrNotRunning) {
		t.Errorf("snapshotFrom with the connected bit clear = %v, want ErrNotRunning", err)
	}
}

// A torn read must be reported rather than returned as data. The row is copied
// between the two header reads, so moving the buffer generation underneath it is
// exactly what the tick counters exist to detect.
func TestReadRowGivesUpOnPersistentTearing(t *testing.T) {
	mapping := syntheticMapping("x")
	hdr, err := ParseHeader(mapping)
	if err != nil {
		t.Fatal(err)
	}
	// TickCountBegin ahead of TickCount means the sim is mid-write, on every
	// attempt, so the bounded retry has to give up instead of spinning.
	putI(mapping, 64, 200)
	putI(mapping, 72, 201)

	if _, err := readRow(mapping, hdr); err == nil {
		t.Error("readRow on a permanently torn buffer = nil, want an error")
	}
}

// The YAML is optional; a header whose string does not fit must still yield a
// usable row rather than failing the whole poll.
func TestSnapshotFromToleratesUnusableYAML(t *testing.T) {
	mapping := syntheticMapping("x")
	putI(mapping, 16, 999999) // sessionInfoLen far larger than the mapping

	_, vh, row, yaml, err := snapshotFrom(mapping)
	if err != nil {
		t.Fatalf("snapshotFrom: %v", err)
	}
	if len(yaml) != 0 {
		t.Errorf("yaml = %q, want empty when the declared length does not fit", yaml)
	}
	if lap, ok := NewRow(vh, row).Int("Lap"); !ok || lap != 42 {
		t.Errorf("row was lost along with the YAML: Lap = %d, %v", lap, ok)
	}
}

// A live simulator completes a new buffer while a row is being copied. That is not a
// torn read, and the reader must recover rather than reporting failure.
//
// This documents behaviour that was checked because the Windows build recorded nothing
// while connected, and this path had never run against a live simulator. The
// hypothesis was that a newly-completed buffer would be mistaken for tearing and every
// read would fail. It does not: the first attempt does see a tick change, but the
// retry re-reads the header and reads the now-latest buffer successfully. At sixty hertz
// a retry takes microseconds against a sixteen millisecond tick, so it converges. The
// test is kept because it pins that convergence, which is the property the live path
// depends on and which no other test covers.
func TestReadRowRecoversWhenANewerBufferCompletes(t *testing.T) {
	mapping := syntheticMapping("x")

	// Parse the header while buffer 1 is newest, which is what the caller does.
	hdr, err := ParseHeader(mapping)
	if err != nil {
		t.Fatal(err)
	}
	before, ok := hdr.LatestBuf()
	if !ok || before.BufOffset != 8192 {
		t.Fatalf("expected buffer 1 at 8192 to be newest, got %+v", before)
	}

	// The sim finishes buffer 0 with a higher tick, so it becomes latest. Buffer 1 is
	// untouched and still self-consistent — this is the ordinary live case, not a tear.
	putI(mapping, 48, 300)
	putI(mapping, 56, 300)

	row, err := readRow(mapping, hdr)
	if err != nil {
		t.Fatalf("readRow failed on an ordinary buffer rotation: %v", err)
	}
	// It returns the buffer that is latest once it settles, which is buffer 0 holding
	// Lap 7 — the freshest complete row, which is what a poll wants.
	lap, ok := NewRow(mustVarHeaders(t, mapping), row).Int("Lap")
	if !ok {
		t.Fatal("returned row does not decode")
	}
	if lap != 7 {
		t.Errorf("decoded Lap = %d, want 7 from the buffer that was latest on settling", lap)
	}
}

// A genuine tear — the chosen buffer being rewritten mid-copy — must still be caught.
func TestReadRowStillDetectsTheChosenBufferChanging(t *testing.T) {
	mapping := syntheticMapping("x")
	hdr, err := ParseHeader(mapping)
	if err != nil {
		t.Fatal(err)
	}

	// TickCountBegin ahead of TickCount is what a write in progress looks like, and it
	// stays that way on every retry.
	putI(mapping, 64, 200)
	putI(mapping, 72, 201)

	if _, err := readRow(mapping, hdr); err == nil {
		t.Error("readRow accepted a row from a buffer that was being rewritten")
	}
}

// mustVarHeaders parses the variable layout out of a synthetic mapping.
func mustVarHeaders(t *testing.T, mapping []byte) []VarHeader {
	t.Helper()
	hdr, err := ParseHeader(mapping)
	if err != nil {
		t.Fatal(err)
	}
	start := int(hdr.VarHeaderOffset)
	vh, err := ParseVarHeaders(mapping[start:start+int(hdr.NumVars)*VarHeaderSize], int(hdr.NumVars))
	if err != nil {
		t.Fatal(err)
	}
	return vh
}

// The trace reports what it observed, not merely that something failed.
//
// This is the only diagnostic channel for the Windows read path: that machine has no
// development environment, so a log line saying "read failed" is worth very little
// while one carrying the header's own fields is usually conclusive. The test pins the
// fields that have actually been needed to diagnose a fault.
func TestTraceReportsTheHeaderFields(t *testing.T) {
	var steps []string
	tr := Trace(func(step string, kv ...any) {
		line := step
		for i := 0; i+1 < len(kv); i += 2 {
			line += fmt.Sprintf(" %v=%v", kv[i], kv[i+1])
		}
		steps = append(steps, line)
	})

	if _, _, _, _, err := snapshotTraced(syntheticMapping("WeekendInfo:\n TrackID: 18\n"), tr); err != nil {
		t.Fatalf("snapshotTraced: %v", err)
	}
	all := strings.Join(steps, "\n")

	// Each of these has been the answer to a real question about why nothing recorded.
	for _, want := range []string{
		"header parsed",   // the step reached at all
		"connected=true",  // the bit that is clear at the simulator's menus
		"numVars=",        // zero here would explain every missing-variable refusal
		"bufLen=",         // a zero row length fails the read outright
		"sessionInfoLen=", // no YAML means no classification
		"mappedBytes=",    // a short mapping was the original Windows bug
		"variable headers parsed",
		"session YAML read",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("trace omits %q; it reads:\n%s", want, all)
		}
	}
}

// A simulator sitting at its menus is the most likely explanation for a build that
// looks connected and records nothing, so the trace must name it rather than reporting
// a generic failure.
func TestTraceExplainsTheDisconnectedBit(t *testing.T) {
	var steps []string
	tr := Trace(func(step string, kv ...any) { steps = append(steps, step) })

	mapping := syntheticMapping("x")
	putI(mapping, 4, 0) // clear the connected bit

	if _, _, _, _, err := snapshotTraced(mapping, tr); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v, want ErrNotRunning", err)
	}
	if !strings.Contains(strings.Join(steps, "\n"), "not connected") {
		t.Errorf("trace does not explain the cleared connected bit: %v", steps)
	}
}

// A nil Trace must be safe, since every non-traced call passes one.
func TestNilTraceIsSafe(t *testing.T) {
	var tr Trace
	tr.note("this must not panic", "k", "v")
	if _, _, _, _, err := snapshotTraced(syntheticMapping("x"), nil); err != nil {
		t.Fatalf("snapshotTraced with a nil trace: %v", err)
	}
}
