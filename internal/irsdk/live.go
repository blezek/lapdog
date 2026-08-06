package irsdk

import (
	"errors"
	"fmt"
)

// ErrUnsupported indicates live telemetry is not available on this platform.
// Only Windows can read the sim's shared memory.
var ErrUnsupported = errors.New("irsdk: live telemetry is only available on Windows")

// ErrNotRunning indicates the simulator is not currently running or has not
// published a connected status. This is an expected state, not a fault.
var ErrNotRunning = errors.New("irsdk: simulator not running")

// Trace receives a description of each step of an open or a read.
//
// The mapping code only runs on Windows, on a machine with a simulator attached and
// no development environment, so a failure there cannot be stepped through — the log
// is the only instrument. Every step reports what it observed, not merely that it
// failed, because "could not open" and "opened, mapped 1.2 MB, header says 0 vars"
// lead to entirely different conclusions.
//
// A nil Trace disables tracing, so the quiet path costs one nil check.
type Trace func(step string, kv ...any)

// note calls t when it is non-nil.
func (t Trace) note(step string, kv ...any) {
	if t != nil {
		t(step, kv...)
	}
}

// maxTornRetries bounds how many times a single poll re-reads a buffer that the
// sim overwrote mid-copy. Beyond this the poll is abandoned rather than spinning;
// the next poll will try again.
const maxTornRetries = 3

// snapshotFrom extracts the layout, the newest self-consistent variable row and
// the session YAML from a mapped region.
//
// This is deliberately separate from the mapping code so that all the byte-level
// reasoning is testable on any operating system. Only the mapping itself requires
// Windows, which keeps the code that needs a simulator to verify down to a few
// dozen lines.
func snapshotFrom(mapping []byte) (Header, []VarHeader, []byte, []byte, error) {
	return snapshotTraced(mapping, nil)
}

// snapshotTraced is snapshotFrom with a step trace, for diagnosing a live read.
func snapshotTraced(mapping []byte, tr Trace) (Header, []VarHeader, []byte, []byte, error) {
	hdr, err := ParseHeader(mapping)
	if err != nil {
		tr.note("header parse failed", "mappedBytes", len(mapping), "err", err)
		return Header{}, nil, nil, nil, err
	}
	// Every header field, because a wrong one explains everything downstream and
	// there is no way to inspect them on the affected machine.
	tr.note("header parsed",
		"mappedBytes", len(mapping),
		"ver", hdr.Ver, "status", hdr.Status, "connected", hdr.Connected(),
		"tickRate", hdr.TickRate, "numVars", hdr.NumVars,
		"varHeaderOffset", hdr.VarHeaderOffset, "numBuf", hdr.NumBuf,
		"bufLen", hdr.BufLen, "curBufTickCount", hdr.CurBufTickCount,
		"sessionInfoLen", hdr.SessionInfoLen,
		"sessionInfoOffset", hdr.SessionInfoOffset,
		"sessionInfoUpdate", hdr.SessionInfoUpdate)

	if !hdr.Connected() {
		// This is the ordinary state at the simulator's menus, and the single most
		// likely explanation for a connected-looking build recording nothing, so it
		// says what the bit actually was.
		tr.note("simulator present but not connected",
			"status", hdr.Status, "wantBitSet", StatusConnected)
		return hdr, nil, nil, nil, ErrNotRunning
	}

	// Variable headers.
	vhStart := int(hdr.VarHeaderOffset)
	vhLen := int(hdr.NumVars) * VarHeaderSize
	if vhStart < 0 || vhLen < 0 || vhStart > len(mapping)-vhLen {
		return hdr, nil, nil, nil, fmt.Errorf(
			"%w: var headers at %d+%d exceed the %d byte mapping",
			ErrShortBuffer, vhStart, vhLen, len(mapping))
	}
	vh, err := ParseVarHeaders(mapping[vhStart:vhStart+vhLen], int(hdr.NumVars))
	if err != nil {
		tr.note("variable headers unparseable", "at", vhStart, "len", vhLen, "err", err)
		return hdr, nil, nil, nil, err
	}
	tr.note("variable headers parsed", "count", len(vh))

	// Newest variable row, copied out and then verified untorn.
	row, err := readRowTraced(mapping, hdr, tr)
	if err != nil {
		tr.note("row read failed", "err", err)
		return hdr, vh, nil, nil, err
	}

	// Session YAML.
	//
	// A YAML string that does not fit the mapping is not fatal: the row is still
	// good, and the collector carries the previous YAML forward. Truncating it
	// would be worse than having none, because a half-parsed session would
	// classify wrongly rather than visibly not at all.
	yStart := int(hdr.SessionInfoOffset)
	yLen := int(hdr.SessionInfoLen)
	var yaml []byte
	if yStart >= 0 && yLen > 0 && yStart <= len(mapping)-yLen {
		yaml = make([]byte, yLen)
		copy(yaml, mapping[yStart:yStart+yLen])
		// The sim NUL-terminates the string inside the declared length.
		yaml = trimNul(yaml)
	}
	// The YAML is what classification depends on, so its absence is worth a line even
	// though it is not fatal: the row is still good and the collector carries the
	// previous document forward.
	if len(yaml) == 0 {
		tr.note("no usable session YAML",
			"declaredOffset", yStart, "declaredLen", yLen, "mappedBytes", len(mapping))
	} else {
		tr.note("session YAML read", "bytes", len(yaml), "update", hdr.SessionInfoUpdate)
	}
	return hdr, vh, row, yaml, nil
}

// readRow copies the newest variable row, retrying while the sim overwrites it
// mid-copy.
//
// The sim writes TickCountBegin before a row and TickCount after it, which is what
// makes a torn copy detectable. A torn frame is discarded entirely rather than
// partially applied: a row that mixes two ticks can hold a lap number from one
// and a track position from the next, which is worse than no sample at all.
func readRow(mapping []byte, hdr Header) ([]byte, error) {
	return readRowTraced(mapping, hdr, nil)
}

// readRowTraced is readRow with a step trace.
func readRowTraced(mapping []byte, hdr Header, tr Trace) ([]byte, error) {
	bufLen := int(hdr.BufLen)
	if bufLen <= 0 {
		return nil, fmt.Errorf("irsdk: header declares bufLen %d", bufLen)
	}

	for attempt := 0; attempt < maxTornRetries; attempt++ {
		before, ok := hdr.LatestBuf()
		if !ok {
			return nil, errors.New("irsdk: header declares no variable buffers")
		}
		start := int(before.BufOffset)
		if start < 0 || start > len(mapping)-bufLen {
			return nil, fmt.Errorf(
				"%w: row at %d+%d exceeds the %d byte mapping",
				ErrShortBuffer, start, bufLen, len(mapping))
		}

		row := make([]byte, bufLen)
		copy(row, mapping[start:start+bufLen])

		// Re-read the header to see whether the sim moved underneath us.
		after, err := ParseHeader(mapping)
		if err != nil {
			return nil, err
		}
		afterBuf, ok := after.LatestBuf()
		if !ok {
			return nil, errors.New("irsdk: header declares no variable buffers")
		}
		if !IsTorn(before, afterBuf) {
			if attempt > 0 {
				tr.note("row read settled after a retry", "attempts", attempt+1)
			}
			return row, nil
		}
		// A rotation between the two header reads is ordinary at sixty hertz; it is
		// only worth reporting because persistent tearing looks identical until the
		// retries run out.
		tr.note("row looked torn, retrying",
			"attempt", attempt+1,
			"beforeTick", before.TickCount, "afterTick", afterBuf.TickCount,
			"afterBegin", afterBuf.TickCountBegin)
		hdr = after
	}
	return nil, errors.New("irsdk: could not obtain an untorn variable row")
}

// trimNul cuts a byte slice at its first NUL, if any.
func trimNul(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}
