package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/sessionyaml"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
)

// FlushIntervalSeconds is how much frame time may accumulate before the active
// session is written to the database. A crash therefore loses at most this much
// accounted time.
const FlushIntervalSeconds = 10

// Options configures a Collector.
type Options struct {
	Source source.Source
	Store  *store.Store
	Clock  Clock

	Interval   time.Duration
	MinSession time.Duration

	CaptureEnabled  bool
	CaptureDir      string
	CaptureMaxBytes int64

	Logger *slog.Logger
}

// Status is a snapshot of what the collector is doing, for the tray and the
// settings screen.
type Status struct {
	Connected       bool    `json:"connected"`
	Paused          bool    `json:"paused"`
	IntervalSeconds float64 `json:"intervalSeconds"`
	SessionKey      string  `json:"sessionKey"`
	SessionLabel    string  `json:"sessionLabel"`
	TrackName       string  `json:"trackName"`
	CarName         string  `json:"carName"`

	// All three accounted totals, not driving alone. Driving on its own cannot be
	// interpreted: zero driving seconds is either a bug or a car sitting in the pit
	// box, and only the connected and in-car figures beside it say which. That is
	// the exact ambiguity the 2026-08-06 test left unresolved for a week.
	ConnectedSeconds float64 `json:"connectedSeconds"`
	InCarSeconds     float64 `json:"inCarSeconds"`
	DrivingSeconds   float64 `json:"drivingSeconds"`

	Laps           int      `json:"laps"`
	MissingVars    []string `json:"missingVars"`
	IncidentSource string   `json:"incidentSource"`

	// SessionsRecorded counts segments written since start-up, which makes an
	// ingestion run observable without querying the database.
	SessionsRecorded int `json:"sessionsRecorded"`
}

// LiveFrame is what the simulator reported on the most recently handled frame.
//
// Every telemetry value is a pointer because absent and zero are different
// facts: a speed of zero is a real reading from a stationary car, and an absent
// speed means the variable was not published or not readable.
//
// At is the wall-clock time the frame was handled, so the interface can decide
// for itself whether the values are still current. Staleness is a question about
// now, and the server does not know when the interface will read this.
type LiveFrame struct {
	At time.Time `json:"at"`

	InCar   bool             `json:"inCar"`
	Driving bool             `json:"driving"`
	Replay  bool             `json:"replay"`
	Reason  NotDrivingReason `json:"reason"`

	Lap             *int     `json:"lap"`
	LapDistPct      *float64 `json:"lapDistPct"`
	LapCurrentTimeS *float64 `json:"lapCurrentTimeS"`
	LapLastTimeS    *float64 `json:"lapLastTimeS"`
	LapBestTimeS    *float64 `json:"lapBestTimeS"`

	Speed     *float64 `json:"speed"`
	Gear      *int     `json:"gear"`
	FuelLevel *float64 `json:"fuelLevel"`
	Incidents *int     `json:"incidents"`
}

// Live is the collector's view of the present moment.
//
// Frame is nil when no frame has been handled, or when the session that produced
// it has closed. Status carries the session identity and the accumulated totals,
// which remain meaningful after frames stop: they record what happened rather
// than what is happening.
type Live struct {
	Frame  *LiveFrame `json:"frame"`
	Status Status     `json:"status"`
}

// Collector polls a telemetry source and records sessions, laps and position
// events.
type Collector struct {
	src   source.Source
	st    *store.Store
	clock Clock
	log   *slog.Logger

	captureEnabled  bool
	captureDir      string
	captureMaxBytes int64

	mu         sync.Mutex
	interval   time.Duration
	minSession time.Duration
	// activeCapturePath is protected by mu so settings changes can prune old
	// captures without deleting the file the collector is still writing.
	activeCapturePath string
	paused            bool
	status            Status
	// lastFrame is guarded by mu, since Live is read from the HTTP goroutine
	// while handle writes it from the collector's own.
	lastFrame *LiveFrame

	// activeMu serializes the active segment state. Pause can arrive from the
	// tray/API goroutine, while Run handles frames on the collector goroutine.
	activeMu sync.Mutex

	// Active segment state. A nil segment means nothing is being recorded. Fields
	// in this block are protected by activeMu.
	seg        *Segment
	lapDet     *LapDetector
	posDet     *PositionDetector
	info       *sessionyaml.Info
	capWriter  *capture.Writer
	refused    bool
	lastFlushT float64
}

// New validates the options and returns a Collector.
func New(opts Options) (*Collector, error) {
	if opts.Source == nil {
		return nil, errors.New("collector: Source is required")
	}
	if opts.Store == nil {
		return nil, errors.New("collector: Store is required")
	}
	if opts.Clock == nil {
		opts.Clock = RealClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}

	c := &Collector{
		src:             opts.Source,
		st:              opts.Store,
		clock:           opts.Clock,
		log:             opts.Logger,
		captureEnabled:  opts.CaptureEnabled,
		captureDir:      opts.CaptureDir,
		captureMaxBytes: opts.CaptureMaxBytes,
		interval:        opts.Interval,
		minSession:      opts.MinSession,
		lapDet:          NewLapDetector(),
		posDet:          NewPositionDetector(),
	}
	c.status.IntervalSeconds = opts.Interval.Seconds()
	c.applyIntervalToSource(opts.Interval)
	return c, nil
}

// SetInterval changes the poll rate, taking effect on the next poll.
func (c *Collector) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	c.interval = d
	c.status.IntervalSeconds = d.Seconds()
	c.mu.Unlock()
	c.applyIntervalToSource(d)
}

// SetMinSession changes the minimum segment length used when closing sessions.
func (c *Collector) SetMinSession(d time.Duration) {
	if d < 0 {
		return
	}
	c.mu.Lock()
	c.minSession = d
	c.mu.Unlock()
}

// SetCapture changes capture recording and retention without restarting.
//
// Disabling capture closes the active capture from the collector loop on the
// next handled frame, so a settings request never closes the writer while it is
// being used.
func (c *Collector) SetCapture(enabled bool, maxBytes int64) {
	if maxBytes < 0 {
		return
	}
	c.mu.Lock()
	c.captureEnabled = enabled
	c.captureMaxBytes = maxBytes
	dir := c.captureDir
	keep := c.activeCapturePath
	c.mu.Unlock()

	c.pruneCapturesWith(dir, maxBytes, keep)
}

// applyIntervalToSource forwards the poll rate to sources that pace themselves.
// The replay source does not, which is what lets a captured race run through as
// fast as the CPU allows.
func (c *Collector) applyIntervalToSource(d time.Duration) {
	if p, ok := c.src.(source.Paced); ok {
		p.SetInterval(d)
	}
}

// SetPaused stops or resumes recording without exiting.
func (c *Collector) SetPaused(p bool) {
	c.mu.Lock()
	c.paused = p
	c.status.Paused = p
	c.mu.Unlock()
	if p {
		c.closeSegment()
	}
}

// Status returns a snapshot of the collector's state.
func (c *Collector) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.status
	s.MissingVars = append([]string(nil), c.status.MissingVars...)
	return s
}

// Run polls the source until it is exhausted or ctx is cancelled.
//
// Pacing is the source's responsibility: the live source blocks for the poll
// interval inside Next, and the replay source returns immediately. Run therefore
// contains no timer.
func (c *Collector) Run(ctx context.Context) error {
	defer c.closeSegment()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		frame, err := c.next(ctx)
		switch {
		case errors.Is(err, context.Canceled):
			return nil
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, source.ErrDisconnected):
			// The sim not running is the normal state, not a failure.
			c.setConnected(false)
			c.closeSegment()
			continue
		case err != nil:
			c.log.Warn("telemetry read failed", "err", err)
			continue
		}
		c.setConnected(true)

		if c.isPaused() {
			continue
		}
		if err := c.handle(frame); err != nil {
			c.log.Error("frame handling failed", "err", err)
		}
	}
}

func (c *Collector) next(ctx context.Context) (source.Frame, error) {
	if s, ok := c.src.(source.ContextSource); ok {
		return s.NextContext(ctx)
	}
	return c.src.Next()
}

// handle processes one frame.
func (c *Collector) handle(f source.Frame) error {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()

	if f.YAMLChanged || c.info == nil {
		if info, err := sessionyaml.Parse(f.SessionYAML); err == nil {
			c.info = info
		} else if c.info == nil {
			// Without any session YAML there is nothing to classify or key on, so
			// this frame cannot be attributed to a session.
			c.log.Warn("session YAML unparseable and none cached", "err", err)
			return nil
		}
	}

	sessionNum := 0
	if v, ok := f.Row.Int("SessionNum"); ok {
		sessionNum = int(v)
	}
	subsession := c.info.WeekendInfo.SubSessionID

	// A change in either identity component starts a new segment. Both matter:
	// SessionNum advances through practice, qualify and race within one subsession,
	// and SubSessionID changes when the driver joins a different event.
	if c.seg != nil && (c.seg.SessionNum != sessionNum || c.seg.SubsessionID != subsession) {
		c.closeSegmentLocked()
	}

	if c.seg == nil {
		if err := c.openSegment(f, sessionNum); err != nil {
			return err
		}
	}
	if c.refused {
		return nil
	}
	if f.YAMLChanged {
		c.seg.ApplyInfo(c.info)
	}
	captureStarted := c.syncCapture(f)

	sample, ok := SampleFrom(f.Row, c.info.DriverInfo.DriverCarIdx)
	if !ok {
		return nil
	}
	sample.T = f.T
	c.seg.Acct.Add(sample)
	c.recordLiveFrame(f, sample)

	if c.capWriter != nil {
		if err := c.writeCapture(f, captureStarted); err != nil {
			// Capture must never cost session data, so disable it and carry on
			// recording to the database.
			c.log.Warn("capture write failed, disabling capture for this run", "err", err)
			c.closeCapture()
			c.disableCaptureForRun()
		}
	}

	c.observeIncidents(f)
	c.observeLaps(f)
	c.observePositions(f)
	c.observeStartingPosition(f)

	if f.T-c.lastFlushT >= FlushIntervalSeconds {
		c.lastFlushT = f.T
		if err := c.flush(); err != nil {
			return err
		}
	}
	c.refreshStatus()
	return nil
}

// syncCapture applies the live capture setting to the active segment.
func (c *Collector) syncCapture(f source.Frame) bool {
	captureEnabled, captureDir, _ := c.captureSettings()
	if !captureEnabled {
		c.closeCapture()
		return false
	}
	if c.capWriter != nil || captureDir == "" {
		return false
	}
	if err := c.openCapture(c.seg, captureDir); err != nil {
		c.log.Warn("could not start capture, continuing without it", "err", err)
		return false
	}
	if err := c.capWriter.WriteSession(f.T, f.SessionUpdate, f.SessionYAML); err != nil {
		c.log.Warn("capture write failed, disabling capture for this run", "err", err)
		c.closeCapture()
		c.disableCaptureForRun()
		return false
	}
	return true
}

// openSegment begins recording a new session segment.
func (c *Collector) openSegment(f source.Frame, sessionNum int) error {
	c.refused = false
	c.lapDet.Reset()
	c.posDet.Reset()
	c.lastFlushT = f.T

	seg := NewSegment(c.info, sessionNum, c.clock.Now(), c.pollInterval())

	// Refuse the session outright if a required variable is absent, rather than
	// recording data that would be wrong.
	need := append([]string(nil), RequiredCoreVars...)
	if seg.IsRace() {
		need = append(need, RequiredRaceVars...)
	}
	if missing := MissingVars(f.Row, need); len(missing) > 0 {
		c.refused = true
		c.mu.Lock()
		c.status.MissingVars = missing
		c.mu.Unlock()
		// The names it needed and the names the simulator actually published, both, so
		// a refusal can be diagnosed from one log line rather than by guessing which
		// spelling is wrong. The required names came from 2015 documentation.
		published := f.Row.Names()
		sort.Strings(published)
		c.log.Error("refusing to record session: required telemetry variables absent",
			"missing", strings.Join(missing, ","),
			"missingCount", len(missing),
			"required", strings.Join(need, ","),
			"publishedCount", len(published),
			"published", strings.Join(published, ","),
			"session", seg.Key,
			"isRace", seg.IsRace())
		return nil
	}

	c.mu.Lock()
	c.status.MissingVars = nil
	c.mu.Unlock()
	c.log.Info("recording session",
		"session", seg.Key, "type", seg.Class.SessionType, "context", seg.Class.EventContext,
		"track", seg.trackName, "car", seg.carName, "isRace", seg.IsRace(),
		"pollIntervalSeconds", c.pollInterval().Seconds())

	// The live incident variable is preferred when present because it updates
	// continuously rather than only when the YAML does.
	seg.SetIncidentSource(f.Row.Has(OptionalIncidentVar))
	c.seg = seg

	c.log.Info("recording session", "key", seg.Key, "label", seg.Label())
	return nil
}

// observeIncidents updates the segment's incident count from the live variable,
// when the sim publishes one.
func (c *Collector) observeIncidents(f source.Frame) {
	if v, ok := f.Row.Int(OptionalIncidentVar); ok {
		c.seg.NoteIncidents(int(v))
	}
}

// observeLaps records any lap crossed on this frame.
func (c *Collector) observeLaps(f source.Frame) {
	var best *float64
	if v, ok := c.seg.BestLapTimeS(); ok {
		best = &v
	}

	lap, ok := c.lapDet.Observe(f.Row, c.seg.Incidents(), best)
	if !ok || lap == nil {
		return
	}
	// The segment must exist in the database before a lap can reference it.
	if err := c.flush(); err != nil {
		c.log.Error("flush before lap insert failed", "err", err)
		return
	}
	lap.SessionID = c.seg.StoreID
	if _, err := c.st.InsertLap(lap); err != nil {
		c.log.Error("lap insert failed", "lap", lap.LapNumber, "err", err)
		return
	}
	t := 0.0
	if lap.LapTimeS != nil {
		t = *lap.LapTimeS
	}
	c.seg.NoteLap(lap.LapNumber, t)
}

// observePositions records position changes, in races only.
func (c *Collector) observePositions(f source.Frame) {
	if !c.seg.IsRace() {
		return
	}
	ev, ok := c.posDet.Observe(f.Row, c.info.DriverInfo.DriverCarIdx, f.T, c.info)
	if !ok || ev == nil {
		return
	}
	if err := c.flush(); err != nil {
		c.log.Error("flush before position insert failed", "err", err)
		return
	}
	ev.SessionID = c.seg.StoreID
	if _, err := c.st.InsertPositionEvent(ev); err != nil {
		c.log.Error("position event insert failed", "err", err)
	}
}

// observeStartingPosition captures the grid slot at the green flag, which differs
// from the qualifying position after a pit-lane start or a penalty.
func (c *Collector) observeStartingPosition(f source.Frame) {
	if !c.seg.IsRace() {
		return
	}
	state, ok := f.Row.Int("SessionState")
	if !ok || state < 4 { // irsdk_StateRacing
		return
	}
	if p, ok := f.Row.Int("PlayerCarPosition"); ok {
		c.seg.NoteStartingPosition(int(p))
	}
}

// flush writes the active segment to the database.
func (c *Collector) flush() error {
	if c.seg == nil || c.refused {
		return nil
	}
	rec := c.seg.ToStore()
	id, err := c.st.UpsertSession(rec)
	if err != nil {
		return fmt.Errorf("collector: upsert session %s: %w", c.seg.Key, err)
	}
	c.seg.StoreID = id
	return nil
}

// closeSegment ends the active segment, discarding it if it is too short.
func (c *Collector) closeSegment() {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	c.closeSegmentLocked()
}

func (c *Collector) closeSegmentLocked() {
	c.closeCapture()
	if c.seg == nil {
		c.clearActiveStatus()
		return
	}
	seg := c.seg
	c.seg = nil
	defer c.clearActiveStatus()

	if c.refused {
		c.refused = false
		return
	}

	seg.End(c.clock.Now())
	if seg.TooShort(c.minSessionLen()) {
		// Below the minimum length: an accidental join, not a session. If it was
		// already flushed, remove it.
		if seg.StoreID != 0 {
			if err := c.st.DeleteSession(seg.StoreID); err != nil {
				c.log.Warn("could not remove a too-short session", "err", err)
			}
		}
		c.log.Info("discarding session below the minimum length",
			"key", seg.Key, "connectedSeconds", seg.Acct.Connected)
		return
	}

	c.seg = seg
	if err := c.flush(); err != nil {
		c.log.Error("final flush failed", "err", err)
	}
	c.seg = nil

	c.mu.Lock()
	c.status.SessionsRecorded++
	c.mu.Unlock()

	if seg.Acct.Clamped > 0 {
		c.log.Warn("poll gaps were clamped during this session",
			"key", seg.Key, "count", seg.Acct.Clamped)
	}
}

func (c *Collector) clearActiveStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.SessionKey = ""
	c.status.SessionLabel = ""
	c.status.TrackName = ""
	c.status.CarName = ""
	c.status.ConnectedSeconds = 0
	c.status.InCarSeconds = 0
	c.status.DrivingSeconds = 0
	c.status.Laps = 0
	c.status.IncidentSource = ""
	// A finished session must not leave instantaneous values behind for the
	// live interface to present as though they were still current.
	c.lastFrame = nil
}

// recordLiveFrame retains what this frame reported, for the live interface.
//
// Read from the row rather than from the segment, because these are readings at
// an instant and the segment holds accumulations. A value the row does not carry
// is stored as absent rather than zero.
func (c *Collector) recordLiveFrame(f source.Frame, sample Sample) {
	idx := c.info.DriverInfo.DriverCarIdx
	lf := &LiveFrame{
		At:      time.Now(),
		InCar:   sample.InCar,
		Driving: sample.Driving,
		Replay:  sample.Replay,
		Reason:  NotDrivingReasonFrom(f.Row, idx),
	}
	if v, ok := f.Row.Int("Lap"); ok {
		n := int(v)
		lf.Lap = &n
	}
	if v, ok := f.Row.Int("Gear"); ok {
		n := int(v)
		lf.Gear = &n
	}
	if v, ok := f.Row.Int(OptionalIncidentVar); ok {
		n := int(v)
		lf.Incidents = &n
	}
	for _, p := range []struct {
		name string
		dst  **float64
	}{
		{"LapDistPct", &lf.LapDistPct},
		{"LapCurrentLapTime", &lf.LapCurrentTimeS},
		{"LapLastLapTime", &lf.LapLastTimeS},
		{"LapBestLapTime", &lf.LapBestTimeS},
		{"Speed", &lf.Speed},
		{"FuelLevel", &lf.FuelLevel},
	} {
		if v, ok := f.Row.Float(p.name); ok {
			val := v
			*p.dst = &val
		}
	}

	c.mu.Lock()
	c.lastFrame = lf
	c.mu.Unlock()
}

// Live returns the present moment: the last frame handled, and the session
// totals that outlive it.
func (c *Collector) Live() Live {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := Live{Status: c.status}
	out.Status.MissingVars = append([]string(nil), c.status.MissingVars...)
	if c.lastFrame != nil {
		f := *c.lastFrame
		out.Frame = &f
	}
	return out
}

// openCapture starts a capture file for the segment.
func (c *Collector) openCapture(seg *Segment, captureDir string) error {
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		return fmt.Errorf("collector: create capture directory: %w", err)
	}
	// Colons are illegal in Windows filenames, so the timestamp is flattened
	// rather than used verbatim.
	stamp := strings.NewReplacer(":", "", "-", "").Replace(store.FormatTime(seg.StartedAt))
	name := fmt.Sprintf("%s-%d-%d%s", stamp, seg.SubsessionID, seg.SessionNum, capture.Ext)
	path := filepath.Join(captureDir, name)

	w, err := capture.NewWriter(path, c.src.Meta())
	if err != nil {
		return err
	}
	c.capWriter = w
	c.mu.Lock()
	c.activeCapturePath = path
	c.mu.Unlock()
	seg.SetCaptureFile(name)
	return nil
}

// closeCapture stops writing the active capture, if any.
func (c *Collector) closeCapture() {
	if c.capWriter == nil {
		return
	}
	if err := c.capWriter.Close(); err != nil {
		c.log.Warn("capture close failed", "err", err)
	}
	c.capWriter = nil
	c.mu.Lock()
	c.activeCapturePath = ""
	c.mu.Unlock()
	c.pruneCaptures()
}

// writeCapture records this frame into the active capture file.
func (c *Collector) writeCapture(f source.Frame, sessionAlreadyWritten bool) error {
	if f.YAMLChanged && !sessionAlreadyWritten {
		if err := c.capWriter.WriteSession(f.T, f.SessionUpdate, f.SessionYAML); err != nil {
			return err
		}
	}
	return c.capWriter.WriteVars(f.T, f.TickCount, f.Row.Raw())
}

// pruneCaptures enforces the capture retention cap.
func (c *Collector) pruneCaptures() {
	_, dir, maxBytes := c.captureSettings()
	c.pruneCapturesWith(dir, maxBytes, "")
}

func (c *Collector) pruneCapturesWith(dir string, maxBytes int64, keep string) {
	if dir == "" || maxBytes <= 0 {
		return
	}
	removed, freed, err := capture.PruneDir(dir, maxBytes, keep)
	if err != nil {
		c.log.Warn("capture pruning failed", "err", err)
		return
	}
	if removed > 0 {
		c.log.Info("pruned old captures", "files", removed, "bytes", freed)
	}
}

// refreshStatus updates the snapshot the interface reads.
func (c *Collector) refreshStatus() {
	if c.seg == nil {
		return
	}
	rec := c.seg.ToStore()
	label := c.seg.Label()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.SessionKey = c.seg.Key
	c.status.SessionLabel = label
	c.status.ConnectedSeconds = c.seg.Acct.Connected
	c.status.InCarSeconds = c.seg.Acct.InCar
	c.status.DrivingSeconds = c.seg.Acct.Driving
	c.status.Laps = rec.LapsCompleted
	c.status.IncidentSource = rec.IncidentSource
	c.status.TrackName = ""
	if rec.TrackName != nil {
		c.status.TrackName = *rec.TrackName
	}
	c.status.CarName = ""
	if rec.CarName != nil {
		c.status.CarName = *rec.CarName
	}
}

func (c *Collector) setConnected(v bool) {
	c.mu.Lock()
	c.status.Connected = v
	c.mu.Unlock()
}

func (c *Collector) isPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

func (c *Collector) pollInterval() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.interval
}

func (c *Collector) minSessionLen() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.minSession
}

func (c *Collector) captureSettings() (bool, string, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.captureEnabled, c.captureDir, c.captureMaxBytes
}

func (c *Collector) disableCaptureForRun() {
	c.mu.Lock()
	c.captureEnabled = false
	c.mu.Unlock()
}
