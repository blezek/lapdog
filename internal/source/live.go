package source

import (
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
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

	// lastFailure is the most recent non-idle failure, retained so the interface can
	// say why nothing is being read instead of only that nothing is.
	lastFailure string

	// log receives the step trace. Never nil after NewLive.
	log *slog.Logger

	// polls counts calls to Next, so repeated identical outcomes can be summarised
	// rather than written once a second forever. A 1 Hz loop left unthrottled fills a
	// four megabyte log with the same line and buries the one that matters.
	polls int64
	// loggedVars records whether the variable list has been dumped. It is written once
	// per connection because it is long and only changes when the sim's layout does.
	loggedVars bool
}

// noteFailure records, or clears, the reason live reading is failing.
func (s *live) noteFailure(err error) {
	s.mu.Lock()
	if err == nil {
		s.lastFailure = ""
	} else {
		s.lastFailure = err.Error()
	}
	s.mu.Unlock()
}

// LastFailure returns the most recent non-idle failure, or an empty string.
//
// The simulator being absent is not a failure and never appears here; this is for the
// case where the mapping exists but cannot be read, which is otherwise silent.
func (s *live) LastFailure() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastFailure
}

// NewLive returns a Source reading the running simulator.
//
// It does not fail when the sim is absent: connecting is attempted lazily in Next,
// because the sim not running is the normal state and must not stop the tray or
// the web interface from starting.
func NewLive() (Source, error) { return NewLiveWithLogger(nil) }

// NewLiveWithLogger returns a live Source that narrates its read path.
//
// The logger is not optional in practice. This is the only code that cannot be
// exercised on the development machine, and the machine that runs it has no debugger,
// so the trace is the sole means of finding out why a read failed. A nil logger is
// accepted so tests need not supply one, and discards the trace.
func NewLiveWithLogger(log *slog.Logger) (Source, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &live{interval: time.Second, now: time.Now, log: log}, nil
}

// discardLog swallows output, for a source constructed without a logger.
var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// logger returns the source's logger, or a discarding one.
//
// Nil-safe on purpose: the tests construct a live source as a struct literal, which is
// a legitimate way to exercise it, and a logger requirement should not be the reason
// that breaks.
func (s *live) logger() *slog.Logger {
	if s.log == nil {
		return discardLog
	}
	return s.log
}

// trace returns an irsdk.Trace that writes to the source's logger at debug level.
func (s *live) trace() irsdk.Trace {
	return func(step string, kv ...any) {
		s.logger().Debug("telemetry: "+step, kv...)
	}
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

	s.polls++

	if s.conn == nil {
		// Tracing every open at 1 Hz would fill the log while iRacing is closed, which
		// is most of the time. The first few attempts are traced in full, then one in
		// sixty — about once a minute — which is frequent enough to show the state and
		// sparse enough to leave room for the session that matters.
		tr := s.trace()
		if s.polls > 3 && s.polls%60 != 0 {
			tr = nil
		}
		conn, err := irsdk.OpenTraced(tr)
		if err != nil {
			// The simulator not running is the ordinary case and stays quiet. Any
			// other failure — a mapping that exists but cannot be opened or viewed —
			// is reported, because flattening everything into ErrDisconnected is what
			// made a real mapping bug indistinguishable from an idle simulator: the
			// interface said "not connected" and the log said nothing at all.
			if !errors.Is(err, irsdk.ErrNotRunning) {
				s.noteFailure(err)
			}
			return Frame{}, ErrDisconnected
		}
		s.noteFailure(nil)
		s.conn = conn
		s.loggedVars = false
		// Deliberately not logged as a connection here. Opening the mapping proves only
		// that the shared-memory section exists, which is true whenever iRacing is
		// running — including at its menus, where the connected bit is clear and the
		// header is all zeroes. This line used to claim a connection at that point, and
		// because the mapping is reopened every poll it claimed one once a second: a real
		// log showed 190 of them alternating with 189 "not connected", 59% of the file,
		// each contradicted by the line beneath it. The claim now waits for a header that
		// actually reports connected, below.
	}

	// The first read after connecting is traced in full; afterwards once a minute, so
	// a long session does not bury its own beginning.
	readTrace := s.trace()
	if s.loggedVars && s.polls%60 != 0 {
		readTrace = nil
	}
	hdr, vh, row, yamlBytes, err := s.conn.SnapshotTraced(readTrace)
	if err != nil {
		// A failed read after the mapping existed usually means the sim exited.
		// Drop the mapping so the next poll reopens it rather than reading a
		// region that no longer belongs to a running simulator.
		//
		// ErrNotRunning here means the connected bit is clear, which is what the
		// simulator reports at its menus — ordinary, and not worth recording. Anything
		// else is a real read failure and is kept for the interface to show.
		if !errors.Is(err, irsdk.ErrNotRunning) {
			s.noteFailure(err)
		}
		s.conn.Close()
		s.conn = nil
		if s.loggedVars {
			// Only worth a line if a connection was actually established; otherwise
			// this is the ordinary idle path.
			s.logger().Info("telemetry: connection lost", "err", err)
		}
		s.loggedVars = false
		return Frame{}, ErrDisconnected
	}
	s.noteFailure(nil)

	// The connection, and the full variable list, once per connection.
	//
	// This is reached only after a header parsed and reported the connected bit set, so
	// it is the first point at which "connected" is a true statement rather than "the
	// section exists". loggedVars doubles as the transition flag: a second one would be
	// two pieces of state to keep in agreement for no gain.
	//
	// The variable list is the single most useful line in the log. The collector refuses
	// a session outright when a required variable is absent, and the names it needs were
	// taken from 2015 documentation — so when a session is refused, the question is
	// always "what does this build of the simulator actually publish?". Answering it
	// from a log beats guessing, and there is no other way to see it on that machine.
	if !s.loggedVars {
		s.logger().Info("telemetry: connected to the simulator",
			"tickRate", hdr.TickRate, "numVars", hdr.NumVars, "bufLen", hdr.BufLen)

		names := make([]string, 0, len(vh))
		for _, v := range vh {
			names = append(names, v.Name)
		}
		sort.Strings(names)
		s.logger().Info("telemetry: variables published by the simulator",
			"count", len(names), "tickRate", hdr.TickRate, "bufLen", hdr.BufLen,
			"names", strings.Join(names, ","))
		s.loggedVars = true
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
