// Package source supplies telemetry frames to the collector, either live from
// the running simulator or replayed from a capture file.
package source

import (
	"errors"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// ErrDisconnected reports that the simulator is not currently running.
//
// The collector treats this as an expected state rather than a failure: iRacing
// not running is the normal case, and the tray and web interface must stay alive
// through it.
var ErrDisconnected = errors.New("source: sim not connected")

// Frame is one poll's worth of telemetry.
//
// SessionYAML is populated on every frame, not only when it changes, so the
// collector never has to cache the previous value. YAMLChanged reports whether it
// differs from the preceding frame, which is the signal to re-parse and
// re-classify.
type Frame struct {
	// T is monotonic seconds since the source started. It is deliberately not the
	// sim's SessionTime, which resets between sessions and would make elapsed-time
	// arithmetic wrong at every segment boundary.
	T             float64
	TickCount     uint32
	Row           irsdk.Row
	SessionYAML   []byte
	SessionUpdate uint32
	YAMLChanged   bool
}

// Source produces frames in time order.
//
// Next returns io.EOF when a finite source is exhausted, and ErrDisconnected
// when a live source has no simulator to read.
type Source interface {
	Next() (Frame, error)
	Meta() capture.Meta
	Close() error
}

// Paced is implemented by sources whose Next blocks to achieve a poll rate.
//
// The live source implements it; the replay source does not, which is what lets a
// captured race run through the collector as fast as the CPU allows with no
// special test mode.
type Paced interface {
	SetInterval(time.Duration)
}
