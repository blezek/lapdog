// Package capture reads and writes LapDog capture files, which record the
// telemetry frames the collector polled so that a session can be replayed later
// on any operating system.
package capture

import (
	"errors"

	"github.com/blezek/lapdog/internal/irsdk"
)

// Magic identifies a capture file. It is stored uncompressed at offset 0 so the
// file is identifiable without decompressing the body.
const Magic = "LPDCAP\x01\x00"

// Ext is the capture file extension.
const Ext = ".lpd"

// ErrBadMagic indicates the file is not a LapDog capture.
var ErrBadMagic = errors.New("capture: bad magic")

// Kind identifies a record type within a capture file.
type Kind uint8

// Record kinds. These are wire format; do not reorder.
const (
	// KindHeader carries a JSON-encoded Meta and is always the first record.
	KindHeader Kind = 1
	// KindSession carries a session-info YAML blob, written whenever the sim's
	// sessionInfoUpdate counter changes.
	KindSession Kind = 2
	// KindVars carries one raw variable-buffer row, written once per poll.
	KindVars Kind = 3
)

// Meta describes the variable layout the capture was recorded against. It is
// needed to decode any KindVars record.
type Meta struct {
	TickRate   int32              `json:"tickRate"`
	NumVars    int32              `json:"numVars"`
	BufLen     int32              `json:"bufLen"`
	VarHeaders []irsdk.VarHeader  `json:"varHeaders"`
}

// Record is one decoded capture record. Which fields are populated depends on
// Kind: KindSession sets Update and YAML, KindVars sets TickCount and Vars,
// KindHeader sets Meta.
type Record struct {
	Kind      Kind
	T         float64
	Update    uint32
	TickCount uint32
	YAML      []byte
	Vars      []byte
	Meta      *Meta
}
