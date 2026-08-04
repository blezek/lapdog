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
	hdr, err := ParseHeader(mapping)
	if err != nil {
		return Header{}, nil, nil, nil, err
	}
	if !hdr.Connected() {
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
		return hdr, nil, nil, nil, err
	}

	// Newest variable row, copied out and then verified untorn.
	row, err := readRow(mapping, hdr)
	if err != nil {
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
			return row, nil
		}
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
