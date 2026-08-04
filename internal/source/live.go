package source

import (
	"sync"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// live reads frames from the running simulator.
//
// Pacing lives here rather than in the collector: Next blocks for the poll
// interval. That is what lets the collector's loop be timer-free and lets the
// replay source run as fast as the CPU allows with no special test mode.
type live struct {
	mu       sync.Mutex
	interval time.Duration
	meta     capture.Meta

	// The fields below are touched only by Next, which the collector calls from a
	// single goroutine. SetInterval and Meta are the only methods other goroutines
	// call, and they take the mutex.
	conn       *irsdk.Conn
	started    time.Time
	lastUpdate uint32
	lastYAML   []byte
	haveYAML   bool

	// now is swappable so the timestamp behaviour can be tested without sleeping.
	now func() time.Time
}

// NewLive returns a Source reading the running simulator.
//
// It does not fail when the sim is absent: connecting is attempted lazily in Next,
// because the sim not running is the normal state and must not stop the tray or
// the web interface from starting.
func NewLive() (Source, error) {
	return &live{interval: time.Second, now: time.Now}, nil
}

// SetInterval changes the poll rate, satisfying Paced.
func (s *live) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	s.mu.Lock()
	s.interval = d
	s.mu.Unlock()
}

// Meta returns the variable layout most recently observed. It is empty until the
// first successful read, because the layout is the sim's to declare.
func (s *live) Meta() capture.Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta
}

// Next waits one poll interval and then reads a frame.
//
// It returns ErrDisconnected when the sim is not running. The collector treats that
// as an expected state, so the tray stays alive and the interface keeps serving
// historical data while iRacing is closed.
func (s *live) Next() (Frame, error) {
	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()
	time.Sleep(interval)

	if s.conn == nil {
		conn, err := irsdk.Open()
		if err != nil {
			return Frame{}, ErrDisconnected
		}
		s.conn = conn
	}

	hdr, vh, row, yamlBytes, err := s.conn.Snapshot()
	if err != nil {
		// A failed read after the mapping existed usually means the sim exited.
		// Drop the mapping so the next poll reopens it rather than reading a
		// region that no longer belongs to a running simulator.
		s.conn.Close()
		s.conn = nil
		return Frame{}, ErrDisconnected
	}

	s.mu.Lock()
	s.meta = capture.Meta{
		TickRate:   hdr.TickRate,
		NumVars:    hdr.NumVars,
		BufLen:     hdr.BufLen,
		VarHeaders: vh,
	}
	s.mu.Unlock()

	// YAML is re-parsed only when the sim bumps its update counter, and only when
	// it actually handed us a string. Reporting a change without new content would
	// make the collector re-classify the same session on every poll.
	update := uint32(hdr.SessionInfoUpdate)
	changed := false
	if len(yamlBytes) > 0 && (!s.haveYAML || update != s.lastUpdate) {
		s.lastYAML = yamlBytes
		s.lastUpdate = update
		s.haveYAML = true
		changed = true
	}

	// Frame time is monotonic seconds since this source started, not the sim's
	// SessionTime.
	//
	// SessionTime restarts at each session in a weekend, so an interval taken
	// across that boundary would be negative — and the accountant credits nothing
	// for a negative interval, so practice time would silently vanish at exactly
	// the moment a session changed. A clock that only moves forward makes every
	// interval meaningful, and it is what Frame.T is documented to be.
	if s.started.IsZero() {
		s.started = s.now()
	}
	t := s.now().Sub(s.started).Seconds()

	return Frame{
		T:             t,
		TickCount:     uint32(hdr.CurBufTickCount),
		Row:           irsdk.NewRow(vh, row),
		SessionYAML:   s.lastYAML,
		SessionUpdate: s.lastUpdate,
		YAMLChanged:   changed,
	}, nil
}

// Close releases the shared memory mapping.
func (s *live) Close() error {
	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	return nil
}

// compile-time assertions that live satisfies both interfaces.
var (
	_ Source = (*live)(nil)
	_ Paced  = (*live)(nil)
)
