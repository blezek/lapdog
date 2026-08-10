# Live Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/live` page that answers "is LapDog reading telemetry right now, and what is it seeing", including why driving time is or is not being credited.

**Architecture:** The collector retains a snapshot of the last handled frame behind its existing mutex, plus a derived reason explaining why driving time is not accruing. A new `GET /api/live` endpoint serves that snapshot alongside the session identity and totals. A React page polls it at the collector's own interval and renders five stacked bands, degrading to cleared values when frames go stale and to a single panel when no simulator is present.

**Tech Stack:** Go 1.26 (`CGO_ENABLED=0`), `modernc.org/sqlite`, React 19 + Vite + TypeScript, TanStack Query, react-router-dom.

**Spec:** `docs/superpowers/specs/2026-08-10-lapdog-live-page-design.md`

## Global Constraints

- `CGO_ENABLED=0` everywhere. Never add a dependency requiring cgo.
- Absent and zero are different facts. Nullable telemetry uses pointers in Go and `| null` in TypeScript. A speed of zero is a real reading; an absent speed is not.
- **The reason driving is false must be derived from exactly the values `SampleFrom` uses** — `IsOnTrackCar`, `IsReplayPlaying`, and `CarIdxTrackSurface[driverCarIdx]` as an `irsdk.TrkLoc`. Deriving it from other variables risks an explanation contradicting the boolean it explains.
- Accumulated totals are not stale; instantaneous values are. Totals survive a frame gap, instantaneous values clear.
- Every new test must be mutation-checked: delete the line it covers, confirm it fails, restore. Six non-discriminating tests reached review earlier in this project.
- Dates and numbers in the interface go through `web/src/locale.ts` and `web/src/format.ts`. Never `toLocaleString` directly in a component.
- Run `make ci` before considering any task done.

---

### Task 1: Derive why driving time is not accruing

A pure function over a telemetry row, with no plumbing. It is separated from the collector's state so it can be tested exhaustively against every `TrkLoc` value.

**Files:**
- Modify: `internal/collector/accounting.go` (append after `SampleFrom`, which ends at line 157)
- Test: `internal/collector/accounting_test.go`

**Interfaces:**
- Consumes: `irsdk.Row`, `irsdk.TrkLoc` and its constants (`internal/irsdk/defines.go:85-89`), `Sample` (`internal/collector/accounting.go:45`)
- Produces:
  - `type NotDrivingReason string`
  - Constants `ReasonReplay`, `ReasonNotInCar`, `ReasonPitBox`, `ReasonNotOnTrack`, and `ReasonNone` (the empty string)
  - `func NotDrivingReasonFrom(row irsdk.Row, driverCarIdx int) NotDrivingReason`

- [ ] **Step 1: Write the failing test**

Add to `internal/collector/accounting_test.go`. Look at an existing test in that file first to copy the row-building helper it uses; if there is none, build rows with `internal/synth`'s `Layout()` and `NewRowBuilder` as `internal/collector/collector_test.go` does.

```go
// The reason mirrors Accountant.Add's precedence, because that is what actually
// decides whether time accrues. Replay outranks everything: Add returns before
// crediting even connected time.
func TestNotDrivingReasonFollowsAccountingPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		inCar   bool
		replay  bool
		loc     irsdk.TrkLoc
		want    NotDrivingReason
	}{
		{"driving on track", true, false, irsdk.OnTrack, ReasonNone},
		{"driving off track is still driving", true, false, irsdk.OffTrack, ReasonNone},
		{"approaching pits is still driving", true, false, irsdk.ApproachingPits, ReasonNone},
		{"stationary in the pit box", true, false, irsdk.InPitStall, ReasonPitBox},
		{"not in the world", true, false, irsdk.NotInWorld, ReasonNotOnTrack},
		{"not in the car", false, false, irsdk.OnTrack, ReasonNotInCar},
		// Replay wins even when everything else looks like driving.
		{"replay while apparently driving", true, true, irsdk.OnTrack, ReasonReplay},
		// And replay wins over the more specific location reasons too.
		{"replay in the pit box", true, true, irsdk.InPitStall, ReasonReplay},
		{"replay and not in the car", false, true, irsdk.NotInWorld, ReasonReplay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := rowWith(t, c.inCar, c.replay, c.loc)
			if got := NotDrivingReasonFrom(row, 0); got != c.want {
				t.Errorf("reason = %q, want %q", got, c.want)
			}
		})
	}
}

// The reason must agree with the boolean it explains: empty exactly when the
// accountant would credit driving time, non-empty exactly when it would not.
// A disagreement here is the class of bug this project spent a week fixing.
func TestNotDrivingReasonAgreesWithSample(t *testing.T) {
	for _, loc := range []irsdk.TrkLoc{
		irsdk.NotInWorld, irsdk.OffTrack, irsdk.InPitStall, irsdk.ApproachingPits, irsdk.OnTrack,
	} {
		for _, inCar := range []bool{true, false} {
			row := rowWith(t, inCar, false, loc)
			s, ok := SampleFrom(row, 0)
			if !ok {
				t.Fatalf("SampleFrom refused a complete row (loc %d, inCar %v)", loc, inCar)
			}
			credited := s.InCar && s.Driving
			reason := NotDrivingReasonFrom(row, 0)
			if credited && reason != ReasonNone {
				t.Errorf("loc %d inCar %v: driving time is credited but reason says %q",
					loc, inCar, reason)
			}
			if !credited && reason == ReasonNone {
				t.Errorf("loc %d inCar %v: driving time is not credited but no reason given",
					loc, inCar)
			}
		}
	}
}

// An incomplete row yields no reason rather than a wrong one. The collector
// refuses such a session anyway, so inventing an explanation would be worse
// than declining to give one.
func TestNotDrivingReasonOnAnIncompleteRow(t *testing.T) {
	vh, bufLen := synth.Layout()
	bare := irsdk.NewRow(vh, make([]byte, bufLen))
	// A zeroed row has IsOnTrackCar false, which is a legitimate reading; the
	// out-of-range index is what makes the surface unavailable.
	if got := NotDrivingReasonFrom(bare, 9999); got != ReasonNone {
		t.Errorf("reason = %q for an unreadable surface, want empty", got)
	}
}
```

Add the helper in the same file:

```go
// rowWith builds a telemetry row with only the fields the accounting reads.
func rowWith(t *testing.T, inCar, replay bool, loc irsdk.TrkLoc) irsdk.Row {
	t.Helper()
	vh, bufLen := synth.Layout()
	rb := irsdk.NewRowBuilder(vh, bufLen)
	if err := rb.SetBool("IsOnTrackCar", inCar); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetBool("IsReplayPlaying", replay); err != nil {
		t.Fatal(err)
	}
	// Only the local driver's slot is set, because every call here passes
	// driverCarIdx 0 and nothing reads the others. The remaining entries stay zero,
	// which is OffTrack — harmless, and never consulted.
	if err := rb.SetIntAt("CarIdxTrackSurface", 0, int32(loc)); err != nil {
		t.Fatal(err)
	}
	return irsdk.NewRow(vh, rb.Bytes())
}
```

Import `github.com/blezek/lapdog/internal/irsdk` and `github.com/blezek/lapdog/internal/synth`.

These names are verified against `internal/irsdk/encode.go`: the builder is `NewRowBuilder(vars, bufLen)`, per-index writes use `SetIntAt(name, i, v)` — **there is no `SetIntArray`** — and a row is produced with `irsdk.NewRow(vh, rb.Bytes())` rather than a method on the builder. `internal/synth/sim.go:270` writes `CarIdxTrackSurface` the same way.

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/collector/ -run NotDrivingReason -v`

Expected: FAIL to compile, `undefined: NotDrivingReasonFrom`.

- [ ] **Step 3: Write the implementation**

Append to `internal/collector/accounting.go`:

```go
// NotDrivingReason explains why driving time is not accruing.
//
// It exists because "zero driving seconds" is indistinguishable from a bug
// without it: a real capture recorded 154 seconds in the car and no driving
// time at all, which was correct — the car sat in the pit box — and could only
// be established by reading this file.
type NotDrivingReason string

// NotDrivingReason values. The empty string means driving time is accruing.
const (
	ReasonNone       NotDrivingReason = ""
	ReasonReplay     NotDrivingReason = "watching a replay"
	ReasonNotInCar   NotDrivingReason = "not in the car"
	ReasonPitBox     NotDrivingReason = "in the pit box"
	ReasonNotOnTrack NotDrivingReason = "not on track"
)

// NotDrivingReasonFrom reports why driving time is not accruing for this frame,
// or ReasonNone when it is.
//
// It reads exactly the values SampleFrom reads and follows Accountant.Add's
// precedence, so the explanation cannot contradict the boolean it explains.
// Replay comes first because Add returns before crediting anything at all.
//
// An unreadable row yields ReasonNone rather than a guess: the collector refuses
// such a session, and an invented explanation would be worse than none.
func NotDrivingReasonFrom(row irsdk.Row, driverCarIdx int) NotDrivingReason {
	if replay, ok := row.Bool("IsReplayPlaying"); ok && replay {
		return ReasonReplay
	}
	inCar, ok := row.Bool("IsOnTrackCar")
	if !ok {
		return ReasonNone
	}
	if !inCar {
		return ReasonNotInCar
	}
	surfaces, ok := row.IntArray("CarIdxTrackSurface")
	if !ok || driverCarIdx < 0 || driverCarIdx >= len(surfaces) {
		return ReasonNone
	}
	switch irsdk.TrkLoc(surfaces[driverCarIdx]) {
	case irsdk.InPitStall:
		return ReasonPitBox
	case irsdk.NotInWorld:
		return ReasonNotOnTrack
	default:
		return ReasonNone
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/collector/ -run NotDrivingReason -v`

Expected: PASS, all subtests.

- [ ] **Step 5: Mutation-check the agreement test**

Change `case irsdk.InPitStall:` to `case irsdk.OffTrack:` and run
`CGO_ENABLED=0 go test ./internal/collector/ -run NotDrivingReasonAgreesWithSample`.

Expected: FAIL, reporting that driving time is credited while a reason is given. Restore the line.

This is the check that matters. If the test still passes with the mutation in place, it is not verifying agreement and must be fixed before continuing.

- [ ] **Step 6: Commit**

```bash
git add internal/collector/accounting.go internal/collector/accounting_test.go
git commit -m "Derive why driving time is not accruing"
```

---

### Task 2: Retain the last frame in the collector

**Files:**
- Modify: `internal/collector/collector.go` — `Status` neighbours (line 45), the `Collector` struct (line 65), `handle` (line ~250), `clearActiveStatus` (line ~536)
- Test: `internal/collector/live_test.go` (create)

**Interfaces:**
- Consumes: `NotDrivingReasonFrom` from Task 1; `irsdk.Row`; the existing `c.mu`
- Produces:
  - `type LiveFrame struct { … }` with `At time.Time` and pointer fields
  - `type Live struct { Frame *LiveFrame; … }`
  - `func (c *Collector) Live() Live`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/live_test.go`:

```go
package collector

import (
	"path/filepath"
	"testing"
)

// A handled frame is retained, so the interface can report what the simulator
// last said rather than only what has been accumulated.
func TestLiveReportsTheLastHandledFrame(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "public-practice.lpd"), nil)
	_ = st

	c, src := collectorForFixture(t, "public-practice.lpd")
	defer src.Close()

	if got := c.Live(); got.Frame != nil {
		t.Fatal("a frame was reported before any was handled")
	}

	frame, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handle(frame); err != nil {
		t.Fatal(err)
	}

	live := c.Live()
	if live.Frame == nil {
		t.Fatal("no frame retained after handling one")
	}
	if live.Frame.At.IsZero() {
		t.Error("the frame carries no timestamp, so staleness cannot be computed")
	}
	if live.Frame.Lap == nil {
		t.Error("Lap was not captured")
	}
	if live.Frame.Speed == nil {
		t.Error("Speed was not captured")
	}
}

// Closing the session clears the frame. A finished session must not leave
// instantaneous values behind for the interface to present as current — the
// same rule that made clearActiveStatus necessary.
func TestLiveFrameIsClearedWhenTheSessionCloses(t *testing.T) {
	c, src := collectorForFixture(t, "public-practice.lpd")
	defer src.Close()

	frame, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handle(frame); err != nil {
		t.Fatal(err)
	}
	if c.Live().Frame == nil {
		t.Fatal("no frame retained after handling one")
	}

	c.closeSegment()

	if got := c.Live(); got.Frame != nil {
		t.Errorf("a frame survived the session closing: %+v", got.Frame)
	}
}
```

Add the helper to the same file. Model it on `ingest` in `internal/collector/collector_test.go` (line ~30), which already builds a store, a replay source and a collector — reuse its option values rather than inventing new ones:

```go
// collectorForFixture returns a collector wired to a replay source, without
// running its loop, so individual frames can be handed to handle directly.
func collectorForFixture(t *testing.T, name string) (*Collector, source.Source) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	src, err := source.NewReplay(filepath.Join(fixtureDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{
		Source: src, Store: st,
		Clock:    NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
		Interval: time.Second, MinSession: 0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, src
}
```

Import `io`, `log/slog`, `time`, `github.com/blezek/lapdog/internal/source` and `github.com/blezek/lapdog/internal/store` as needed.

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/collector/ -run TestLive -v`

Expected: FAIL to compile, `c.Live undefined`.

- [ ] **Step 3: Add the types**

In `internal/collector/collector.go`, after the `Status` struct (which ends at line 61):

```go
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
```

Add `lastFrame *LiveFrame` to the `Collector` struct in the `mu`-guarded block alongside `activeCapturePath` (line ~78), with a comment noting it is guarded by `mu`.

- [ ] **Step 4: Capture the frame in `handle`**

In `handle`, immediately after `c.seg.Acct.Add(sample)`, add:

```go
	c.recordLiveFrame(f, sample)
```

And add the method near `clearActiveStatus`:

```go
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
```

Returning a copy of the frame rather than the pointer keeps a caller from observing a later frame's values through a reference it already holds.

- [ ] **Step 5: Clear it when the session closes**

In `clearActiveStatus`, add `c.lastFrame = nil` alongside the fields it already blanks. That function already holds `c.mu`, so no extra locking.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./internal/collector/ -run TestLive -v`

Expected: PASS both tests.

- [ ] **Step 7: Mutation-check the clearing test**

Remove `c.lastFrame = nil` from `clearActiveStatus` and run
`CGO_ENABLED=0 go test ./internal/collector/ -run TestLiveFrameIsCleared`.

Expected: FAIL, reporting a frame survived the session closing. Restore the line.

- [ ] **Step 8: Run the race detector and commit**

```bash
CGO_ENABLED=0 go test -race ./internal/collector/
git add internal/collector/collector.go internal/collector/live_test.go
git commit -m "Retain the last handled frame for a live view"
```

The race run matters here specifically: `Live()` is called from the HTTP goroutine while `handle` writes from the collector's own.

---

### Task 3: Serve `GET /api/live`

**Files:**
- Modify: `internal/api/server.go` — `StatusProvider` (line 27), route table (line ~59)
- Modify: `internal/api/handlers.go` — add `handleLive`
- Modify: `internal/api/api_test.go` — extend `fakeStatus`, add endpoint tests
- Modify: `cmd/lapdogctl/serve.go` if it constructs a `StatusProvider` — grep for `StatusProvider` and `api.New(` to find every construction site

**Interfaces:**
- Consumes: `collector.Live`, `(*Collector).Live()` from Task 2
- Produces: `GET /api/live` returning `liveResponse`

- [ ] **Step 1: Write the failing test**

Add to `internal/api/api_test.go`:

```go
// The endpoint reports the frame and the poll interval, so the interface can
// judge staleness itself rather than being told.
func TestLiveEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	var got liveResponse
	rec := get(t, h, "/api/live", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.IntervalSeconds <= 0 {
		t.Error("no poll interval reported; the interface cannot pace itself or judge staleness")
	}
}

// With no frame handled, the response says so rather than inventing zeroes.
func TestLiveEndpointWithNoFrame(t *testing.T) {
	h, _, _ := newTestServer(t)
	var got liveResponse
	get(t, h, "/api/live", &got)
	if got.Frame != nil {
		t.Errorf("a frame was reported when none had been handled: %+v", got.Frame)
	}
}

// A zero speed is a real reading and must not be confused with an absent one.
func TestLiveEndpointDistinguishesZeroFromAbsent(t *testing.T) {
	h, _, _ := newTestServer(t)
	// fakeStatus is configured with a stationary car: speed present and zero,
	// gear absent entirely.
	var got liveResponse
	get(t, h, "/api/live", &got)
	if got.Frame == nil {
		t.Skip("the fixture provides no frame; covered by TestLiveEndpointWithNoFrame")
	}
	if got.Frame.Speed == nil || *got.Frame.Speed != 0 {
		t.Errorf("Speed = %v, want a present zero", got.Frame.Speed)
	}
	if got.Frame.Gear != nil {
		t.Errorf("Gear = %v, want absent", got.Frame.Gear)
	}
}
```

Extend `fakeStatus` in the same file so it satisfies the widened interface, returning a stationary-car frame:

```go
func (f fakeStatus) Live() collector.Live {
	zero := 0.0
	return collector.Live{
		Status: f.s,
		Frame: &collector.LiveFrame{
			At: time.Now(), InCar: true, Driving: false,
			Reason: collector.ReasonPitBox,
			Speed:  &zero,
		},
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/api/ -run TestLive -v`

Expected: FAIL to compile, `undefined: liveResponse`.

- [ ] **Step 3: Widen the interface and add the route**

In `internal/api/server.go`, change:

```go
// StatusProvider reports the collector's state.
//
// Live is on the same interface rather than an optional one because it is not
// optional: every provider that can report a status can report a frame, and
// there is exactly one implementation plus one test double.
type StatusProvider interface {
	Status() collector.Status
	Live() collector.Live
}
```

Add to the route table, beside `GET /api/status`:

```go
	mux.HandleFunc("GET /api/live", s.handleLive)
```

- [ ] **Step 4: Add the handler**

In `internal/api/handlers.go`:

```go
// liveResponse is the present moment: the last frame, the session it belongs to,
// and the poll interval.
//
// The interval is included so the interface can pace its own polling and decide
// when a frame has gone stale. The server deliberately does not decide staleness
// itself: it does not know when the response will be read.
type liveResponse struct {
	Frame  *collector.LiveFrame `json:"frame"`
	Status collector.Status     `json:"status"`

	IntervalSeconds float64 `json:"intervalSeconds"`

	// Supported reports whether this build can read live telemetry at all, which
	// is a different fact from no simulator being present.
	Supported bool   `json:"supported"`
	Platform  string `json:"platform"`
}

// handleLive serves the collector's view of the present moment.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	var live collector.Live
	if s.sp != nil {
		live = s.sp.Live()
	}
	interval := live.Status.IntervalSeconds
	if interval <= 0 {
		interval = 1
	}
	s.writeJSON(w, liveResponse{
		Frame:           live.Frame,
		Status:          live.Status,
		IntervalSeconds: interval,
		Supported:       runtime.GOOS == "windows",
		Platform:        runtime.GOOS,
	})
}
```

Import `runtime`. If `handleStatus` already computes `Supported` and `Platform`, reuse that helper instead of duplicating the expression — grep for `available` in `handlers.go` first, since the status endpoint reports the same fact under the wording "Supported".

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./internal/api/ -run TestLive -v`

Expected: PASS all three.

- [ ] **Step 6: Fix every other `StatusProvider` implementation**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`

Any type implementing `StatusProvider` now fails to compile. Add a `Live()` method to each. `*collector.Collector` already has one from Task 2.

- [ ] **Step 7: Commit**

```bash
git add internal/api/
git commit -m "Serve the collector's present moment at /api/live"
```

---

### Task 4: The Live page

**Files:**
- Create: `web/src/pages/Live.tsx`
- Create: `web/src/live.ts` — the staleness rule and the display state, as pure functions
- Create: `web/src/live.test.ts`
- Modify: `web/src/api.ts` — types and `api.live()`
- Modify: `web/src/App.tsx` — nav item (line 19) and route (line 68)
- Modify: `web/src/styles.css` — the band and light styles

**Interfaces:**
- Consumes: `GET /api/live` from Task 3
- Produces: `Live` page component; `viewFor(res, now)` returning `'unsupported' | 'idle' | 'stale' | 'live'`

- [ ] **Step 1: Write the failing test for the staleness rule**

The rule is the part worth testing, so it lives in its own module rather than inside the component. Create `web/src/live.test.ts`:

```ts
import { describe, expect, it } from 'vitest'

import { viewFor, staleAfterMs } from './live'
import type { LiveResponse } from './api'

const base: LiveResponse = {
  frame: null,
  status: {
    connected: false, paused: false, intervalSeconds: 1,
    sessionKey: '', sessionLabel: '', trackName: '', carName: '',
    drivingSeconds: 0, laps: 0, missingVars: [], incidentSource: '',
    sessionsRecorded: 0,
  },
  intervalSeconds: 1,
  supported: true,
  platform: 'windows',
}

describe('staleAfterMs', () => {
  it('scales with the poll interval', () => {
    // A thirty-second poll rate must not read as permanently broken, which a
    // fixed threshold would cause.
    expect(staleAfterMs(30)).toBeGreaterThan(30_000)
  })

  it('never drops below two seconds', () => {
    // A quarter-second poll rate would otherwise flap between live and stale on
    // ordinary scheduling jitter.
    expect(staleAfterMs(0.25)).toBeGreaterThanOrEqual(2000)
  })
})

describe('viewFor', () => {
  const now = new Date('2026-08-10T12:00:00Z').getTime()

  it('reports unsupported before anything else', () => {
    // Not being able to read telemetry at all is a different fact from no
    // simulator being present, and it outranks it.
    const res = { ...base, supported: false, platform: 'darwin' }
    expect(viewFor(res, now)).toBe('unsupported')
  })

  it('reports idle when no frame has arrived', () => {
    expect(viewFor(base, now)).toBe('idle')
  })

  it('reports live for a recent frame', () => {
    const res = { ...base, frame: { ...frameAt(now - 500) } }
    expect(viewFor(res, now)).toBe('live')
  })

  it('reports stale once the threshold passes', () => {
    const res = { ...base, frame: { ...frameAt(now - 10_000) } }
    expect(viewFor(res, now)).toBe('stale')
  })

  function frameAt(ms: number) {
    return {
      at: new Date(ms).toISOString(),
      inCar: true, driving: false, replay: false, reason: 'in the pit box',
      lap: 3, lapDistPct: 0.42, lapCurrentTimeS: 98.4,
      lapLastTimeS: 101.9, lapBestTimeS: 98.1,
      speed: 35.6, gear: 4, fuelLevel: 38.2, incidents: 0,
    }
  }
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/live.test.ts`

Expected: FAIL, cannot resolve `./live`.

- [ ] **Step 3: Add the types and client method**

In `web/src/api.ts`, beside the other interfaces:

```ts
/**
 * One frame as the simulator reported it.
 *
 * Every telemetry value is nullable because absent and zero differ: a speed of
 * zero is a stationary car, and a null speed is a variable that was not
 * published or not readable.
 */
export interface LiveFrame {
  at: string
  inCar: boolean
  driving: boolean
  replay: boolean
  /** Why driving time is not accruing; empty when it is. */
  reason: string
  lap: number | null
  lapDistPct: number | null
  lapCurrentTimeS: number | null
  lapLastTimeS: number | null
  lapBestTimeS: number | null
  speed: number | null
  gear: number | null
  fuelLevel: number | null
  incidents: number | null
}

export interface LiveResponse {
  frame: LiveFrame | null
  status: Status
  intervalSeconds: number
  /** Whether this build can read live telemetry at all. */
  supported: boolean
  platform: string
}
```

And in the `api` object: `live: () => get<LiveResponse>('/api/live'),`

- [ ] **Step 4: Write the rule module**

Create `web/src/live.ts`:

```ts
import type { LiveResponse } from './api'

/** What the page should render. */
export type LiveView = 'unsupported' | 'idle' | 'stale' | 'live'

/**
 * How long a frame stays current, in milliseconds.
 *
 * Three poll intervals, floored at two seconds. Derived from the interval rather
 * than fixed because a thirty-second poll rate would otherwise show a permanent
 * and false "not reading"; floored because a quarter-second rate would flap on
 * ordinary scheduling jitter.
 */
export function staleAfterMs(intervalSeconds: number): number {
  return Math.max(2000, intervalSeconds * 3000)
}

/**
 * viewFor decides what the page shows.
 *
 * Order matters. Being unable to read telemetry at all outranks having no
 * simulator, because they are different facts and the remedy differs: one is
 * "this build cannot", the other is "start iRacing".
 */
export function viewFor(res: LiveResponse, now: number): LiveView {
  if (!res.supported) return 'unsupported'
  if (!res.frame) return 'idle'
  const age = now - new Date(res.frame.at).getTime()
  return age > staleAfterMs(res.intervalSeconds) ? 'stale' : 'live'
}

/**
 * pollMs is how often to ask for a new frame.
 *
 * The collector's own interval, clamped. Polling faster returns the frame
 * already held; polling slower than three seconds makes the age counter stop
 * feeling live.
 */
export function pollMs(intervalSeconds: number): number {
  return Math.min(3000, Math.max(500, intervalSeconds * 1000))
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd web && npx vitest run src/live.test.ts`

Expected: PASS.

- [ ] **Step 6: Mutation-check the ordering assertion**

In `viewFor`, swap the first two lines so the frame check precedes the support check. Run the test again.

Expected: FAIL on "reports unsupported before anything else". Restore the order.

- [ ] **Step 7: Build the page**

Create `web/src/pages/Live.tsx`. Five bands for `live` and `stale`, one panel for `idle` and `unsupported`. Read `web/src/pages/Dashboard.tsx` for the `Card`/`Stat` idiom and `web/src/pages/Settings.tsx` for the `setting` row markup; reuse both rather than inventing new components.

Requirements the code must satisfy, each traceable to the spec:

- Poll with `refetchInterval: pollMs(res?.intervalSeconds ?? 1)` and `keepPrevious` from `web/src/query.ts`, so the page does not flash between polls.
- `live`: all five bands populated.
- `stale`: the verdict reads `Not reading` with the frame's age; **instantaneous values render `—`**; **the totals band renders its real values** with a note that they are what was accumulated. This asymmetry is the whole point of the design.
- `idle`: one panel, `Waiting for iRacing`, naming the poll interval, plus `sessionsRecorded` for context.
- `unsupported`: one panel stating this build cannot read telemetry on `platform`, matching the wording already in `Settings.tsx` — grep for `Not supported` there and reuse it verbatim.
- The state band shows three lights and, when `reason` is non-empty, the sentence `Not counting driving time — {reason}.`
- Lap times go through `lapTime()` from `web/src/format.ts`; the lap-distance bar is `lapDistPct * 100` percent wide.
- Every number is `font-variant-numeric: tabular-nums` so values do not jitter as they change.

- [ ] **Step 8: Add the nav item and route**

In `web/src/App.tsx`, add as the **first** entry in the nav array:

```ts
  { to: '/live', label: 'Live', icon: 'steering' },
```

`steering.svg` already exists in `internal/ui/icons/mdi/` and is unused. Add the route above the dashboard route:

```tsx
          <Route path="/live" element={<Live />} />
```

- [ ] **Step 9: Verify it renders**

```bash
make build-ctl && ./dist/lapdogctl serve .dataset.db 47047
```

Open `http://127.0.0.1:47047/live`. On macOS this must show the unsupported panel, not an idle one — the build cannot read telemetry here, and saying "waiting for iRacing" would be false.

- [ ] **Step 10: Commit**

```bash
git add web/src
git commit -m "Add the Live page"
```

---

### Task 5: Verify against the real capture

The page exists to explain a specific real observation. This task proves it does, using the capture that produced it. No simulator required.

**Files:**
- Modify: `README.md` — the page list and the testing section
- Modify: `docs/superpowers/specs/2026-08-10-lapdog-live-page-design.md` — mark it implemented

- [ ] **Step 1: Replay the real capture into a scratch database**

```bash
./dist/lapdogctl ingest ignore/captures /tmp/live.db
sqlite3 /tmp/live.db "SELECT session_type, in_car_seconds, driving_seconds FROM sessions;"
```

Expected: two rows, in-car around 154 and 133 seconds, driving `0.0` for both. This is the observation the page must explain.

- [ ] **Step 2: Confirm the endpoint explains it**

The replay source runs to completion and the collector then closes the segment, so `frame` will be null by the end. To see a mid-session frame, serve the database and check the shape instead:

```bash
./dist/lapdogctl serve /tmp/live.db 47098 &
curl -s localhost:47098/api/live | python3 -m json.tool
```

Expected: valid JSON with `frame: null`, `supported: false` on macOS, and a non-zero `intervalSeconds`. Then open `http://127.0.0.1:47098/live` and confirm the unsupported panel appears.

- [ ] **Step 3: Add a collector test that pins the explanation**

The end-to-end check above cannot assert the pit-box reason, because the frame is cleared when the session closes. Add to `internal/collector/live_test.go`:

```go
// The pit-box case, end to end from a real recording's shape.
//
// This is the observation the Live page exists to explain: time in the car, no
// driving time, and previously no way to see why without reading the accounting
// code. The offline-test fixture drives, so the reason is empty; what is pinned
// here is that a reason is always present exactly when driving time is not
// accruing, on real replayed frames rather than constructed rows.
func TestLiveReasonMatchesAccountingOnRealFrames(t *testing.T) {
	c, src := collectorForFixture(t, "official-race-weekend.lpd")
	defer src.Close()

	for i := 0; i < 200; i++ {
		frame, err := src.Next()
		if err != nil {
			break
		}
		if err := c.handle(frame); err != nil {
			t.Fatal(err)
		}
		live := c.Live()
		if live.Frame == nil {
			continue
		}
		credited := live.Frame.InCar && live.Frame.Driving && !live.Frame.Replay
		if credited && live.Frame.Reason != ReasonNone {
			t.Fatalf("frame %d credits driving time but gives reason %q", i, live.Frame.Reason)
		}
		if !credited && live.Frame.Reason == ReasonNone {
			t.Fatalf("frame %d credits no driving time and gives no reason", i)
		}
	}
}
```

Run: `CGO_ENABLED=0 go test ./internal/collector/ -run TestLiveReason -v`

- [ ] **Step 4: Update the documentation**

In `README.md`, add `/live` to whatever list of pages exists, described as answering whether telemetry is being read right now and why driving time is or is not accruing. In the testing section, note that the Live page's staleness rule is unit-tested in `web/src/live.test.ts` and its agreement with the accounting is pinned in `internal/collector/live_test.go`.

In the spec, change the status line to record that it is implemented, with the commit range.

- [ ] **Step 5: Full check and commit**

```bash
make ci
CGO_ENABLED=0 go test -race ./internal/collector/ ./internal/api/
git add README.md docs/superpowers/specs/2026-08-10-lapdog-live-page-design.md internal/collector/live_test.go
git commit -m "Pin the Live page against a real capture"
```

---

## Self-review

**Spec coverage.** Route and nav — Task 4 step 8. Five bands — Task 4 step 7. Degradation live/stale/idle — Task 4 steps 4 and 7. Unsupported platform — Task 4 steps 4, 7, 9. Frame snapshot — Task 2. Derived reason — Task 1. `GET /api/live` — Task 3. Poll rate — Task 4 step 4 (`pollMs`). Staleness threshold of three intervals floored at two seconds — Task 4 step 4 (`staleAfterMs`), tested. Nullable values — Global Constraints, Task 2 step 3, Task 3 step 1. Deliberate cuts — no lap list and no SSE appear nowhere in the plan, which is the correct treatment for a cut. Verification — Task 5.

**One gap found and closed while reviewing.** Task 5 originally asserted the pit-box reason through the HTTP endpoint after replaying the real capture. That cannot work: the replay runs to completion, the segment closes, and `clearActiveStatus` nulls the frame — so the assertion would have been made against `frame: null` and passed for the wrong reason. It is now a collector-level test over live frames, with the end-to-end check reduced to what it can actually prove. This is exactly the non-discriminating-test failure this project has hit six times, caught before dispatch this once.

**Type consistency.** `NotDrivingReason` and `ReasonPitBox` are used in Tasks 1, 2, 3 and 5 under those names. `LiveFrame` field names match between the Go struct (Task 2), the JSON tags, and the TypeScript interface (Task 4): `at`, `inCar`, `driving`, `replay`, `reason`, `lap`, `lapDistPct`, `lapCurrentTimeS`, `lapLastTimeS`, `lapBestTimeS`, `speed`, `gear`, `fuelLevel`, `incidents`. `Live` in Go carries `Frame` and `Status`; `liveResponse` flattens those plus `intervalSeconds`, `supported` and `platform`, and `LiveResponse` in TypeScript matches that flattened shape rather than the Go `Live`.

**A second gap found and closed while reviewing.** Task 1's helper first used `SetIntArray` and a `Row()` method on the builder. Neither exists: `internal/irsdk/encode.go` offers `SetIntAt(name, i, v)` for per-index writes and no array setter, and a row is built with `irsdk.NewRow(vh, rb.Bytes())`. Every API name in this plan has now been read out of the source rather than recalled, which is why the plan no longer flags anything as unverified.
