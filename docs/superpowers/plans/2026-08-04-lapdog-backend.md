# LapDog Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go backend for LapDog — a Windows tray application that reads the iRacing telemetry shared-memory API, classifies and records sim sessions to SQLite, and serves a JSON API and CSV/JSON export on `localhost:47047`.

**Architecture:** Single process. A collector goroutine polls a `Source` (live Windows shared memory, or a replay of a captured file) at a configurable interval, runs a session state machine that accumulates three time counters and detects laps and position changes, and upserts rows through a single-writer SQLite connection. An HTTP server reads through a separate connection pool. Only `internal/irsdk` is Windows-specific; every package holding non-trivial logic is tested on macOS against captured fixtures.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (pure Go, no cgo), `fyne.io/systray`, `gopkg.in/yaml.v3`, `golang.org/x/sys/windows`, `github.com/google/uuid`.

**Spec:** `docs/superpowers/specs/2026-08-04-lapdog-design.md`. Section references below (§N) point into it.

**Out of scope for this plan:** The React/ECharts frontend. `internal/web` serves a placeholder page here; Plan 2 replaces it. Central hub upload and `.ibt` import are out of scope entirely (spec §18).

## Global Constraints

Every task's requirements implicitly include this section.

- **Go 1.26.** `go.mod` declares `go 1.26`.
- **`CGO_ENABLED=0` always.** This is load-bearing — it is why `modernc.org/sqlite` was chosen and what allows cross-compiling to Windows from macOS. No dependency may require cgo.
- **`go test ./...` must pass on macOS** with no iRacing and no Windows. Windows-only code sits behind build tags and has a non-Windows stub.
- **Timestamps are RFC3339 UTC strings.** Durations are seconds as `REAL`/`float64`.
- **HTTP server binds `127.0.0.1:47047` only.** The bind address is not configurable — loopback-only binding *is* the security model, so there is no authentication.
- **Poll interval range 0.25 s – 30 s, default 1.0 s.**
- **Capture size cap default 2 GB**, `0` meaning unlimited.
- **`IsReplayPlaying` suppresses all time accounting.** No setting exists for this.
- **Offline testing sessions always count.** No setting exists for this.
- **No anonymisation anywhere.** Opponent names and IDs are stored and exported as-is.
- **All data under `%LOCALAPPDATA%\lapdog\`** on Windows; `$XDG_DATA_HOME/lapdog` or `~/.local/share/lapdog` elsewhere for development.
- **Never squash commits.** One commit per task step where the plan says commit.
- **Godoc comments on every exported identifier.** The repo has a `/doc` convention.

## File Structure

```
go.mod
Makefile

internal/irsdk/
  defines.go            constants, enums, VarType and byte sizes         (Task 2)
  header.go             Header/VarBuf/VarHeader binary parsing            (Task 3)
  buffer.go             torn-read-safe buffer selection                   (Task 3)
  decode.go             typed value extraction from a row                 (Task 4)
  live_windows.go       MapViewOfFile live reader                        (Task 17)
  live_stub.go          non-Windows stub returning ErrUnsupported        (Task 17)

internal/capture/
  format.go             magic, record kinds, record encode/decode         (Task 5)
  writer.go             gzip file writer, size accounting                 (Task 5)
  reader.go             gzip file reader                                  (Task 5)
  ndjson.go             inspect/build codec                              (Task 16)
  prune.go              size-cap retention                                (Task 6)

internal/source/
  source.go             Source interface, Frame type                      (Task 6)
  replay.go             replays a .lpd through the Source interface       (Task 6)
  live.go               wraps internal/irsdk                             (Task 17)

internal/sessionyaml/
  types.go              typed subset structs                              (Task 7)
  parse.go              tolerant parser                                   (Task 7)

internal/classify/
  classify.go           pure function: YAML subset -> type + context      (Task 8)

internal/store/
  migrations/0001_init.sql                                                (Task 9)
  store.go              open, WAL, writer/reader pools, migrations        (Task 9)
  sessions.go           session upsert, session_key derivation           (Task 10)
  laps.go               lap insert                                       (Task 11)
  positions.go          position event insert                            (Task 11)
  queries.go            aggregation queries                              (Task 12)

internal/collector/
  clock.go              Clock interface + real and fake implementations   (Task 13)
  accounting.go         the three counters, poll-gap clamp               (Task 13)
  segment.go            session segment lifecycle, results extraction    (Task 14)
  laps.go               lap detection                                    (Task 15)
  positions.go          position events and cause attribution            (Task 15)
  collector.go          poll loop wiring                                 (Task 15)

internal/config/
  config.go             load/save, defaults, validation, paths            (Task 9)

internal/api/
  server.go             mux, listener, middleware                        (Task 18)
  handlers.go           read endpoints                                   (Task 18)
  export.go             streaming CSV/JSON export                        (Task 19)

internal/web/
  embed.go              embed.FS + placeholder index.html                (Task 18)

cmd/lapdog/main.go      tray app, wires everything                       (Task 20)
cmd/lapdogctl/main.go   dev CLI: inspect, build, reclassify              (Task 16)

testdata/               .lpd fixtures and hand-authored NDJSON
```

Rationale for the split: `irsdk` is divided so that only `live_*.go` is OS-specific and everything else tests anywhere. `collector` is split by concern rather than into one file because each concern (accounting, segments, laps, positions) has its own test cycle and a reviewer could reasonably reject one while accepting the others.

---

### Task 1: Project scaffold

**Files:**
- Create: `go.mod`, `Makefile`, `.gitignore` (append), `internal/version/version.go`
- Test: `internal/version/version_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `version.Version string`, `version.String() string`.

- [ ] **Step 1: Initialise the module and add dependencies**

```bash
cd /Users/MRA9161/Source/lapdog-2
go mod init github.com/blezek/lapdog
go get modernc.org/sqlite@latest
go get gopkg.in/yaml.v3@latest
go get github.com/google/uuid@latest
go get golang.org/x/sys@latest
go get fyne.io/systray@latest
```

- [ ] **Step 2: Write the failing test**

Create `internal/version/version_test.go`:

```go
package version

import "testing"

func TestStringIncludesVersion(t *testing.T) {
	Version = "0.1.0"
	got := String()
	if got != "lapdog 0.1.0" {
		t.Fatalf("String() = %q, want %q", got, "lapdog 0.1.0")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/version/ -v`
Expected: FAIL — build error, `undefined: Version`, `undefined: String`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/version/version.go`:

```go
// Package version reports the application version, set at build time.
package version

import "fmt"

// Version is the application version, overridden at build time with
// -ldflags "-X github.com/blezek/lapdog/internal/version.Version=x.y.z".
var Version = "dev"

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("lapdog %s", Version)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/version/ -v`
Expected: PASS

- [ ] **Step 6: Add the Makefile**

Create `Makefile`. Note the leading tabs are required by make.

```make
VERSION ?= 0.1.0
LDFLAGS := -X github.com/blezek/lapdog/internal/version.Version=$(VERSION) -s -w

.PHONY: test build-windows build-ctl clean

test:
	CGO_ENABLED=0 go test ./...

# The tray app must be linked -H windowsgui so no console window appears.
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
	  go build -ldflags "-H windowsgui $(LDFLAGS)" -o dist/lapdog.exe ./cmd/lapdog

# lapdogctl is a separate binary precisely because a GUI-subsystem
# executable has no console and is useless as a CLI.
build-ctl:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/lapdogctl ./cmd/lapdogctl

clean:
	rm -rf dist
```

- [ ] **Step 7: Append build output to .gitignore**

```bash
printf 'dist/\n*.lpd\n' >> .gitignore
```

- [ ] **Step 8: Verify the whole tree builds and tests**

Run: `make test`
Expected: PASS, with `no test files` for packages that have none yet.

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum Makefile .gitignore internal/version/
git commit -m "Add Go module scaffold and version package"
```

---

### Task 2: irsdk constants and enums

**Files:**
- Create: `internal/irsdk/defines.go`
- Test: `internal/irsdk/defines_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `VarType` (with `VarChar`, `VarBool`, `VarInt`, `VarBitField`, `VarFloat`, `VarDouble`), `VarType.Size() int`, `VarType.String() string`, `TrkLoc` (with `NotInWorld`, `OffTrack`, `InPitStall`, `ApproachingPits`, `OnTrack`), `SessionState` (with `StateInvalid` … `StateCoolDown`), `StatusConnected`, `MemMapFileName`, `DataValidEventName`, `MaxBufs`, `MaxString`, `MaxDesc`, `ExpectedVer`, `UnlimitedLaps`, `UnlimitedTime`.

Source of truth: `documentation/irsdk_1_20/irsdk_defines.h`.

- [ ] **Step 1: Write the failing test**

Create `internal/irsdk/defines_test.go`:

```go
package irsdk

import "testing"

func TestVarTypeSize(t *testing.T) {
	cases := []struct {
		vt   VarType
		want int
	}{
		{VarChar, 1},
		{VarBool, 1},
		{VarInt, 4},
		{VarBitField, 4},
		{VarFloat, 4},
		{VarDouble, 8},
	}
	for _, c := range cases {
		if got := c.vt.Size(); got != c.want {
			t.Errorf("%v.Size() = %d, want %d", c.vt, got, c.want)
		}
	}
}

func TestVarTypeSizeUnknownIsZero(t *testing.T) {
	if got := VarType(99).Size(); got != 0 {
		t.Errorf("VarType(99).Size() = %d, want 0", got)
	}
}

// Enum values are wire format read out of shared memory. If these drift,
// every decoded field silently means something else.
func TestEnumWireValues(t *testing.T) {
	if VarChar != 0 || VarBool != 1 || VarInt != 2 || VarBitField != 3 || VarFloat != 4 || VarDouble != 5 {
		t.Error("VarType wire values do not match irsdk_defines.h")
	}
	if NotInWorld != -1 || OffTrack != 0 || InPitStall != 1 || ApproachingPits != 2 || OnTrack != 3 {
		t.Error("TrkLoc wire values do not match irsdk_defines.h")
	}
	if StateInvalid != 0 || StateRacing != 4 || StateCoolDown != 6 {
		t.Error("SessionState wire values do not match irsdk_defines.h")
	}
}

func TestConstants(t *testing.T) {
	if MaxBufs != 4 || MaxString != 32 || MaxDesc != 64 {
		t.Error("size constants do not match irsdk_defines.h")
	}
	if StatusConnected != 1 {
		t.Errorf("StatusConnected = %d, want 1", StatusConnected)
	}
	if MemMapFileName != `Local\IRSDKMemMapFileName` {
		t.Errorf("MemMapFileName = %q", MemMapFileName)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/irsdk/ -v`
Expected: FAIL — build error, undefined identifiers.

- [ ] **Step 3: Write minimal implementation**

Create `internal/irsdk/defines.go`:

```go
// Package irsdk reads telemetry from the iRacing simulator's shared
// memory-mapped file. Constants and layouts mirror
// documentation/irsdk_1_20/irsdk_defines.h.
package irsdk

// Shared memory and event object names published by the sim.
const (
	MemMapFileName     = `Local\IRSDKMemMapFileName`
	DataValidEventName = `Local\IRSDKDataValidEvent`
)

// Layout limits from irsdk_defines.h.
const (
	MaxBufs   = 4
	MaxString = 32
	MaxDesc   = 64

	// ExpectedVer is IRSDK_VER. A higher value in the header is logged
	// as a warning but is not fatal, since the layout has been stable.
	ExpectedVer = 2

	// UnlimitedLaps and UnlimitedTime are the sim's sentinels for a
	// session with no lap or time limit.
	UnlimitedLaps = 32767
	UnlimitedTime = 604800.0
)

// StatusConnected is the irsdk_stConnected bit in Header.Status.
const StatusConnected = 1

// VarType is the storage type of a telemetry variable.
type VarType int32

// VarType values. These are wire format; do not reorder.
const (
	VarChar VarType = iota
	VarBool
	VarInt
	VarBitField
	VarFloat
	VarDouble
)

// Size returns the width in bytes of a single element of this type, or 0
// if the type is unknown.
func (v VarType) Size() int {
	switch v {
	case VarChar, VarBool:
		return 1
	case VarInt, VarBitField, VarFloat:
		return 4
	case VarDouble:
		return 8
	default:
		return 0
	}
}

// String implements fmt.Stringer.
func (v VarType) String() string {
	switch v {
	case VarChar:
		return "char"
	case VarBool:
		return "bool"
	case VarInt:
		return "int"
	case VarBitField:
		return "bitField"
	case VarFloat:
		return "float"
	case VarDouble:
		return "double"
	default:
		return "unknown"
	}
}

// TrkLoc describes where a car is relative to the track surface. It is
// the element type of the CarIdxTrackSurface array.
type TrkLoc int32

// TrkLoc values. These are wire format; do not reorder.
const (
	NotInWorld      TrkLoc = -1
	OffTrack        TrkLoc = 0
	InPitStall      TrkLoc = 1
	ApproachingPits TrkLoc = 2
	OnTrack         TrkLoc = 3
)

// SessionState is the value of the SessionState telemetry variable.
type SessionState int32

// SessionState values. These are wire format; do not reorder.
const (
	StateInvalid SessionState = iota
	StateGetInCar
	StateWarmup
	StateParadeLaps
	StateRacing
	StateCheckered
	StateCoolDown
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/irsdk/ -v`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/irsdk/
git commit -m "Add irsdk constants and enum definitions"
```

---

### Task 3: irsdk binary layout parsing and buffer selection

**Files:**
- Create: `internal/irsdk/header.go`, `internal/irsdk/buffer.go`
- Test: `internal/irsdk/header_test.go`, `internal/irsdk/buffer_test.go`

**Interfaces:**
- Consumes: Task 2's `VarType`, `MaxBufs`, `MaxString`, `MaxDesc`.
- Produces:
  - `HeaderSize = 112`, `VarHeaderSize = 144` (constants)
  - `type VarBuf struct { TickCount, BufOffset, TickCountBegin int32 }`
  - `type Header struct { Ver, Status, TickRate, SessionInfoUpdate, SessionInfoLen, SessionInfoOffset, NumVars, VarHeaderOffset, NumBuf, BufLen, CurBufTickCount int32; CurBuf uint8; VarBuf [MaxBufs]VarBuf }`
  - `func ParseHeader(b []byte) (Header, error)`
  - `type VarHeader struct { Type VarType; Offset, Count int32; CountAsTime bool; Name, Desc, Unit string }`
  - `func ParseVarHeaders(b []byte, numVars int) ([]VarHeader, error)`
  - `func (h Header) Connected() bool`
  - `func (h Header) LatestBuf() (VarBuf, bool)`
  - `var ErrShortBuffer error`

Byte offsets, derived from `irsdk_defines.h`. `HeaderSize` is 48 bytes of scalars plus `MaxBufs`×16 = 112. `VarHeaderSize` is 16 + 32 + 64 + 32 = 144.

```
Header                      VarHeader
  0  ver            int32     0   type        int32
  4  status         int32     4   offset      int32
  8  tickRate       int32     8   count       int32
 12  sessionInfoUpdate       12   countAsTime bool
 16  sessionInfoLen          13   pad[3]
 20  sessionInfoOffset       16   name[32]
 24  numVars                 48   desc[64]
 28  varHeaderOffset        112   unit[32]
 32  numBuf                 144   = VarHeaderSize
 36  bufLen
 40  curBufTickCount
 44  curBuf         uint8
 45  pad1[3]
 48  varBuf[4] x 16 bytes { tickCount, bufOffset, tickCountBegin, pad }
112  = HeaderSize
```

- [ ] **Step 1: Write the failing header test**

Create `internal/irsdk/header_test.go`:

```go
package irsdk

import (
	"encoding/binary"
	"testing"
)

// buildHeader lays out a synthetic irsdk_header so decoding can be
// tested on any OS without the sim.
func buildHeader(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, HeaderSize)
	put := func(off int, v int32) { binary.LittleEndian.PutUint32(b[off:], uint32(v)) }
	put(0, 2)     // ver
	put(4, 1)     // status: connected
	put(8, 60)    // tickRate
	put(12, 7)    // sessionInfoUpdate
	put(16, 4096) // sessionInfoLen
	put(20, 1024) // sessionInfoOffset
	put(24, 3)    // numVars
	put(28, 112)  // varHeaderOffset
	put(32, 3)    // numBuf
	put(36, 64)   // bufLen
	put(40, 555)  // curBufTickCount
	b[44] = 1     // curBuf
	// varBuf[0..2]: tickCount, bufOffset, tickCountBegin, pad
	put(48, 100)
	put(52, 5000)
	put(56, 100)
	put(64, 300) // varBuf[1] has the highest tickCount
	put(68, 6000)
	put(72, 300)
	put(80, 200)
	put(84, 7000)
	put(88, 200)
	return b
}

func TestParseHeader(t *testing.T) {
	h, err := ParseHeader(buildHeader(t))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.Ver != 2 || h.TickRate != 60 || h.NumVars != 3 || h.BufLen != 64 || h.NumBuf != 3 {
		t.Errorf("scalars decoded wrong: %+v", h)
	}
	if h.SessionInfoLen != 4096 || h.SessionInfoOffset != 1024 || h.SessionInfoUpdate != 7 {
		t.Errorf("session info fields decoded wrong: %+v", h)
	}
	if h.CurBuf != 1 {
		t.Errorf("CurBuf = %d, want 1", h.CurBuf)
	}
	if h.VarBuf[1].TickCount != 300 || h.VarBuf[1].BufOffset != 6000 {
		t.Errorf("VarBuf[1] = %+v", h.VarBuf[1])
	}
	if !h.Connected() {
		t.Error("Connected() = false, want true with status bit set")
	}
}

func TestParseHeaderShortBuffer(t *testing.T) {
	if _, err := ParseHeader(make([]byte, HeaderSize-1)); err == nil {
		t.Fatal("ParseHeader on a short buffer: want error, got nil")
	}
}

func TestConnectedFalseWhenBitClear(t *testing.T) {
	b := buildHeader(t)
	binary.LittleEndian.PutUint32(b[4:], 0)
	h, err := ParseHeader(b)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.Connected() {
		t.Error("Connected() = true, want false with status bit clear")
	}
}

func TestParseVarHeaders(t *testing.T) {
	b := make([]byte, 2*VarHeaderSize)
	put := func(base, off int, v int32) {
		binary.LittleEndian.PutUint32(b[base+off:], uint32(v))
	}
	// Entry 0: a scalar float named Speed, in m/s.
	put(0, 0, int32(VarFloat))
	put(0, 4, 40)
	put(0, 8, 1)
	b[12] = 0
	copy(b[16:], "Speed\x00")
	copy(b[48:], "GPS vehicle speed\x00")
	copy(b[112:], "m/s\x00")
	// Entry 1: a 64-element int array.
	put(VarHeaderSize, 0, int32(VarInt))
	put(VarHeaderSize, 4, 200)
	put(VarHeaderSize, 8, 64)
	b[VarHeaderSize+12] = 1
	copy(b[VarHeaderSize+16:], "CarIdxPosition\x00")

	vh, err := ParseVarHeaders(b, 2)
	if err != nil {
		t.Fatalf("ParseVarHeaders: %v", err)
	}
	if len(vh) != 2 {
		t.Fatalf("len = %d, want 2", len(vh))
	}
	if vh[0].Name != "Speed" || vh[0].Type != VarFloat || vh[0].Offset != 40 || vh[0].Count != 1 {
		t.Errorf("vh[0] = %+v", vh[0])
	}
	if vh[0].Unit != "m/s" || vh[0].Desc != "GPS vehicle speed" {
		t.Errorf("vh[0] strings = %q / %q", vh[0].Desc, vh[0].Unit)
	}
	if vh[0].CountAsTime {
		t.Error("vh[0].CountAsTime = true, want false")
	}
	if vh[1].Name != "CarIdxPosition" || vh[1].Count != 64 || !vh[1].CountAsTime {
		t.Errorf("vh[1] = %+v", vh[1])
	}
}

func TestParseVarHeadersShortBuffer(t *testing.T) {
	if _, err := ParseVarHeaders(make([]byte, VarHeaderSize), 2); err == nil {
		t.Fatal("want error for buffer smaller than numVars*VarHeaderSize")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/irsdk/ -run 'Header' -v`
Expected: FAIL — build error, `undefined: HeaderSize`, `undefined: ParseHeader`.

- [ ] **Step 3: Write the header implementation**

Create `internal/irsdk/header.go`:

```go
package irsdk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Binary sizes of the shared-memory structures, in bytes. See
// irsdk_defines.h: HeaderSize is 48 bytes of scalars plus MaxBufs
// 16-byte varBuf entries; VarHeaderSize is 16 + MaxString + MaxDesc +
// MaxString.
const (
	HeaderSize    = 48 + MaxBufs*16
	VarHeaderSize = 16 + MaxString + MaxDesc + MaxString
)

// ErrShortBuffer indicates the supplied bytes are too small for the
// structure being parsed.
var ErrShortBuffer = errors.New("irsdk: short buffer")

// VarBuf describes one of the sim's triple-buffered variable rows.
// TickCountBegin is written before the sim starts writing the row and
// TickCount after it finishes, which is what makes torn reads
// detectable.
type VarBuf struct {
	TickCount      int32
	BufOffset      int32
	TickCountBegin int32
}

// Header is the irsdk_header at offset 0 of the shared memory region.
type Header struct {
	Ver               int32
	Status            int32
	TickRate          int32
	SessionInfoUpdate int32
	SessionInfoLen    int32
	SessionInfoOffset int32
	NumVars           int32
	VarHeaderOffset   int32
	NumBuf            int32
	BufLen            int32
	CurBufTickCount   int32
	CurBuf            uint8
	VarBuf            [MaxBufs]VarBuf
}

// Connected reports whether the sim has the irsdk_stConnected bit set.
func (h Header) Connected() bool { return h.Status&StatusConnected != 0 }

// LatestBuf returns the variable buffer with the highest TickCount, and
// false if the header declares no usable buffers.
func (h Header) LatestBuf() (VarBuf, bool) {
	n := int(h.NumBuf)
	if n <= 0 {
		return VarBuf{}, false
	}
	if n > MaxBufs {
		n = MaxBufs
	}
	best := 0
	for i := 1; i < n; i++ {
		if h.VarBuf[i].TickCount > h.VarBuf[best].TickCount {
			best = i
		}
	}
	return h.VarBuf[best], true
}

// ParseHeader decodes an irsdk_header from b.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, fmt.Errorf("%w: header needs %d bytes, got %d", ErrShortBuffer, HeaderSize, len(b))
	}
	i32 := func(off int) int32 { return int32(binary.LittleEndian.Uint32(b[off:])) }
	h := Header{
		Ver:               i32(0),
		Status:            i32(4),
		TickRate:          i32(8),
		SessionInfoUpdate: i32(12),
		SessionInfoLen:    i32(16),
		SessionInfoOffset: i32(20),
		NumVars:           i32(24),
		VarHeaderOffset:   i32(28),
		NumBuf:            i32(32),
		BufLen:            i32(36),
		CurBufTickCount:   i32(40),
		CurBuf:            b[44],
	}
	for i := 0; i < MaxBufs; i++ {
		base := 48 + i*16
		h.VarBuf[i] = VarBuf{
			TickCount:      i32(base),
			BufOffset:      i32(base + 4),
			TickCountBegin: i32(base + 8),
		}
	}
	return h, nil
}

// VarHeader describes one telemetry variable: its type, where in a
// variable row it starts, and how many elements it has.
type VarHeader struct {
	Type        VarType
	Offset      int32
	Count       int32
	CountAsTime bool
	Name        string
	Desc        string
	Unit        string
}

// ParseVarHeaders decodes numVars consecutive irsdk_varHeader entries
// from b.
func ParseVarHeaders(b []byte, numVars int) ([]VarHeader, error) {
	need := numVars * VarHeaderSize
	if numVars < 0 || len(b) < need {
		return nil, fmt.Errorf("%w: %d var headers need %d bytes, got %d", ErrShortBuffer, numVars, need, len(b))
	}
	out := make([]VarHeader, numVars)
	for i := range out {
		base := i * VarHeaderSize
		out[i] = VarHeader{
			Type:        VarType(int32(binary.LittleEndian.Uint32(b[base:]))),
			Offset:      int32(binary.LittleEndian.Uint32(b[base+4:])),
			Count:       int32(binary.LittleEndian.Uint32(b[base+8:])),
			CountAsTime: b[base+12] != 0,
			Name:        cstr(b[base+16 : base+16+MaxString]),
			Desc:        cstr(b[base+48 : base+48+MaxDesc]),
			Unit:        cstr(b[base+112 : base+112+MaxString]),
		}
	}
	return out, nil
}

// cstr converts a NUL-padded fixed-width C string to a Go string.
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
```

- [ ] **Step 4: Run the header test to verify it passes**

Run: `go test ./internal/irsdk/ -run 'Header|Connected' -v`
Expected: PASS

- [ ] **Step 5: Write the failing buffer-selection test**

Create `internal/irsdk/buffer_test.go`:

```go
package irsdk

import "testing"

func TestLatestBufPicksHighestTickCount(t *testing.T) {
	h := Header{NumBuf: 3}
	h.VarBuf[0] = VarBuf{TickCount: 100, BufOffset: 1000, TickCountBegin: 100}
	h.VarBuf[1] = VarBuf{TickCount: 300, BufOffset: 2000, TickCountBegin: 300}
	h.VarBuf[2] = VarBuf{TickCount: 200, BufOffset: 3000, TickCountBegin: 200}
	got, ok := h.LatestBuf()
	if !ok {
		t.Fatal("LatestBuf ok = false, want true")
	}
	if got.BufOffset != 2000 {
		t.Errorf("BufOffset = %d, want 2000", got.BufOffset)
	}
}

func TestLatestBufNoBuffers(t *testing.T) {
	if _, ok := (Header{NumBuf: 0}).LatestBuf(); ok {
		t.Error("LatestBuf ok = true with NumBuf 0, want false")
	}
}

// NumBuf larger than MaxBufs must not read past the fixed-size array.
func TestLatestBufClampsToMaxBufs(t *testing.T) {
	h := Header{NumBuf: 99}
	h.VarBuf[MaxBufs-1] = VarBuf{TickCount: 5, BufOffset: 42}
	got, ok := h.LatestBuf()
	if !ok || got.BufOffset != 42 {
		t.Errorf("LatestBuf = %+v, ok = %v", got, ok)
	}
}

func TestIsTorn(t *testing.T) {
	before := VarBuf{TickCount: 100, TickCountBegin: 100}
	if IsTorn(before, VarBuf{TickCount: 100, TickCountBegin: 100}) {
		t.Error("IsTorn = true for a stable buffer, want false")
	}
	if !IsTorn(before, VarBuf{TickCount: 101, TickCountBegin: 101}) {
		t.Error("IsTorn = false when the sim advanced mid-copy, want true")
	}
	// Sim started writing but has not finished.
	if !IsTorn(before, VarBuf{TickCount: 100, TickCountBegin: 101}) {
		t.Error("IsTorn = false when TickCountBegin ran ahead, want true")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/irsdk/ -run 'LatestBuf|IsTorn' -v`
Expected: The `LatestBuf` tests PASS (implemented in Step 3). `TestIsTorn` FAILS with `undefined: IsTorn`.

- [ ] **Step 7: Add the torn-read check**

Create `internal/irsdk/buffer.go`:

```go
package irsdk

// IsTorn reports whether a variable row copied out between two
// observations of its VarBuf may have been overwritten mid-copy.
//
// The sim writes TickCountBegin before starting a row and TickCount
// after finishing it. A caller reads the VarBuf, copies BufLen bytes,
// then re-reads the VarBuf and passes both here. If either counter moved,
// or if TickCountBegin is ahead of TickCount, the copy is not
// self-consistent and must be discarded rather than partially applied.
func IsTorn(before, after VarBuf) bool {
	if before.TickCount != after.TickCount {
		return true
	}
	if before.TickCountBegin != after.TickCountBegin {
		return true
	}
	return after.TickCountBegin != after.TickCount
}
```

- [ ] **Step 8: Run the full package test**

Run: `go test ./internal/irsdk/ -v`
Expected: PASS, all tests.

- [ ] **Step 9: Commit**

```bash
git add internal/irsdk/
git commit -m "Add irsdk header parsing and torn-read detection"
```

---

### Task 4: irsdk typed value decoding

**Files:**
- Create: `internal/irsdk/decode.go`
- Test: `internal/irsdk/decode_test.go`

**Interfaces:**
- Consumes: Task 2's `VarType`; Task 3's `VarHeader`.
- Produces:
  - `type Row struct { ... }` with `func NewRow(vars []VarHeader, data []byte) Row`
  - `func (r Row) Has(name string) bool`
  - `func (r Row) Int(name string) (int32, bool)`
  - `func (r Row) Float(name string) (float64, bool)` — accepts `VarFloat` and `VarDouble`
  - `func (r Row) Bool(name string) (bool, bool)`
  - `func (r Row) BitField(name string) (uint32, bool)`
  - `func (r Row) IntArray(name string) ([]int32, bool)`
  - `func (r Row) FloatArray(name string) ([]float64, bool)`
  - `func (r Row) BoolArray(name string) ([]bool, bool)`
  - `func (r Row) Names() []string`

Every accessor returns `(value, ok)`. `ok` is false when the variable is absent, has the wrong type, or its declared extent exceeds the row — never a panic, because a malformed row must not crash the collector.

- [ ] **Step 1: Write the failing test**

Create `internal/irsdk/decode_test.go`:

```go
package irsdk

import (
	"encoding/binary"
	"math"
	"testing"
)

// testRow builds a row containing one variable of each supported type
// plus two arrays, and returns the decoder for it.
func testRow(t *testing.T) Row {
	t.Helper()
	vars := []VarHeader{
		{Name: "Lap", Type: VarInt, Offset: 0, Count: 1},
		{Name: "Speed", Type: VarFloat, Offset: 4, Count: 1},
		{Name: "SessionTime", Type: VarDouble, Offset: 8, Count: 1},
		{Name: "IsOnTrack", Type: VarBool, Offset: 16, Count: 1},
		{Name: "EngineWarnings", Type: VarBitField, Offset: 17, Count: 1},
		{Name: "CarIdxPosition", Type: VarInt, Offset: 21, Count: 3},
		{Name: "CarIdxOnPitRoad", Type: VarBool, Offset: 33, Count: 3},
		{Name: "CarIdxLapDistPct", Type: VarFloat, Offset: 36, Count: 2},
	}
	data := make([]byte, 44)
	binary.LittleEndian.PutUint32(data[0:], uint32(37))
	binary.LittleEndian.PutUint32(data[4:], math.Float32bits(52.5))
	binary.LittleEndian.PutUint64(data[8:], math.Float64bits(1234.5))
	data[16] = 1
	binary.LittleEndian.PutUint32(data[17:], 0x0021)
	binary.LittleEndian.PutUint32(data[21:], uint32(5))
	binary.LittleEndian.PutUint32(data[25:], uint32(6))
	binary.LittleEndian.PutUint32(data[29:], uint32(7))
	data[33], data[34], data[35] = 0, 1, 0
	binary.LittleEndian.PutUint32(data[36:], math.Float32bits(0.25))
	binary.LittleEndian.PutUint32(data[40:], math.Float32bits(0.75))
	return NewRow(vars, data)
}

func TestRowScalars(t *testing.T) {
	r := testRow(t)

	if v, ok := r.Int("Lap"); !ok || v != 37 {
		t.Errorf("Int(Lap) = %d, %v; want 37, true", v, ok)
	}
	if v, ok := r.Float("Speed"); !ok || math.Abs(v-52.5) > 1e-6 {
		t.Errorf("Float(Speed) = %v, %v; want 52.5, true", v, ok)
	}
	// Float must transparently widen a double.
	if v, ok := r.Float("SessionTime"); !ok || math.Abs(v-1234.5) > 1e-9 {
		t.Errorf("Float(SessionTime) = %v, %v; want 1234.5, true", v, ok)
	}
	if v, ok := r.Bool("IsOnTrack"); !ok || !v {
		t.Errorf("Bool(IsOnTrack) = %v, %v; want true, true", v, ok)
	}
	if v, ok := r.BitField("EngineWarnings"); !ok || v != 0x0021 {
		t.Errorf("BitField = %#x, %v; want 0x21, true", v, ok)
	}
}

func TestRowArrays(t *testing.T) {
	r := testRow(t)

	got, ok := r.IntArray("CarIdxPosition")
	if !ok || len(got) != 3 || got[0] != 5 || got[2] != 7 {
		t.Errorf("IntArray = %v, %v", got, ok)
	}
	gotB, ok := r.BoolArray("CarIdxOnPitRoad")
	if !ok || len(gotB) != 3 || gotB[0] || !gotB[1] || gotB[2] {
		t.Errorf("BoolArray = %v, %v", gotB, ok)
	}
	gotF, ok := r.FloatArray("CarIdxLapDistPct")
	if !ok || len(gotF) != 2 || math.Abs(gotF[1]-0.75) > 1e-6 {
		t.Errorf("FloatArray = %v, %v", gotF, ok)
	}
}

func TestRowMissingVariable(t *testing.T) {
	r := testRow(t)
	if r.Has("PlayerCarMyIncidentCount") {
		t.Error("Has() = true for an absent variable")
	}
	if _, ok := r.Int("PlayerCarMyIncidentCount"); ok {
		t.Error("Int() ok = true for an absent variable")
	}
	if _, ok := r.Float("Nope"); ok {
		t.Error("Float() ok = true for an absent variable")
	}
}

// Asking for the wrong type must fail rather than reinterpret bytes.
func TestRowWrongType(t *testing.T) {
	r := testRow(t)
	if _, ok := r.Int("Speed"); ok {
		t.Error("Int() on a float variable: ok = true, want false")
	}
	if _, ok := r.Bool("Lap"); ok {
		t.Error("Bool() on an int variable: ok = true, want false")
	}
}

// A variable whose declared extent runs past the row must fail, not panic.
func TestRowOutOfBoundsIsNotPanic(t *testing.T) {
	vars := []VarHeader{{Name: "Bad", Type: VarInt, Offset: 100, Count: 1}}
	r := NewRow(vars, make([]byte, 8))
	if _, ok := r.Int("Bad"); ok {
		t.Error("Int() ok = true for an out-of-bounds variable, want false")
	}
	arr := []VarHeader{{Name: "BadArr", Type: VarInt, Offset: 0, Count: 64}}
	r2 := NewRow(arr, make([]byte, 8))
	if _, ok := r2.IntArray("BadArr"); ok {
		t.Error("IntArray() ok = true for an out-of-bounds array, want false")
	}
}

func TestRowNames(t *testing.T) {
	if got := len(testRow(t).Names()); got != 8 {
		t.Errorf("len(Names()) = %d, want 8", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/irsdk/ -run 'Row' -v`
Expected: FAIL — build error, `undefined: Row`, `undefined: NewRow`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/irsdk/decode.go`:

```go
package irsdk

import (
	"encoding/binary"
	"math"
	"sort"
)

// Row decodes telemetry values out of a single variable buffer row,
// looked up by variable name.
//
// Every accessor returns (value, ok). ok is false when the variable is
// absent, is of a different type than requested, or when its declared
// extent runs past the row. A malformed row must never panic, because
// the variable set the sim publishes depends on the car and session and
// is not fully known ahead of time.
type Row struct {
	vars map[string]VarHeader
	data []byte
}

// NewRow builds a decoder over data using vars as the layout. It does
// not copy data, so callers must not mutate it afterwards.
func NewRow(vars []VarHeader, data []byte) Row {
	m := make(map[string]VarHeader, len(vars))
	for _, v := range vars {
		m[v.Name] = v
	}
	return Row{vars: m, data: data}
}

// Names returns the sorted names of every variable in the row's layout.
func (r Row) Names() []string {
	out := make([]string, 0, len(r.vars))
	for n := range r.vars {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Has reports whether the named variable is present in the layout.
func (r Row) Has(name string) bool {
	_, ok := r.vars[name]
	return ok
}

// slice returns the bytes for element i of the named variable, verifying
// the type matches one of want and that the read is in bounds.
func (r Row) slice(name string, i int, want ...VarType) ([]byte, bool) {
	v, ok := r.vars[name]
	if !ok {
		return nil, false
	}
	matched := false
	for _, w := range want {
		if v.Type == w {
			matched = true
			break
		}
	}
	if !matched {
		return nil, false
	}
	sz := v.Type.Size()
	if sz == 0 || i < 0 || int32(i) >= v.Count {
		return nil, false
	}
	start := int(v.Offset) + i*sz
	if start < 0 || start+sz > len(r.data) {
		return nil, false
	}
	return r.data[start : start+sz], true
}

// count returns the declared element count of the named variable, after
// verifying the type matches and the whole extent is in bounds.
func (r Row) count(name string, want ...VarType) (VarHeader, bool) {
	v, ok := r.vars[name]
	if !ok {
		return VarHeader{}, false
	}
	matched := false
	for _, w := range want {
		if v.Type == w {
			matched = true
			break
		}
	}
	if !matched {
		return VarHeader{}, false
	}
	sz := v.Type.Size()
	if sz == 0 || v.Count <= 0 {
		return VarHeader{}, false
	}
	end := int(v.Offset) + int(v.Count)*sz
	if int(v.Offset) < 0 || end > len(r.data) {
		return VarHeader{}, false
	}
	return v, true
}

// Int returns an int variable.
func (r Row) Int(name string) (int32, bool) {
	b, ok := r.slice(name, 0, VarInt)
	if !ok {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(b)), true
}

// BitField returns a bitField variable.
func (r Row) BitField(name string) (uint32, bool) {
	b, ok := r.slice(name, 0, VarBitField)
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b), true
}

// Bool returns a bool variable.
func (r Row) Bool(name string) (bool, bool) {
	b, ok := r.slice(name, 0, VarBool)
	if !ok {
		return false, false
	}
	return b[0] != 0, true
}

// Float returns a float or double variable, widened to float64.
func (r Row) Float(name string) (float64, bool) {
	if b, ok := r.slice(name, 0, VarFloat); ok {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), true
	}
	if b, ok := r.slice(name, 0, VarDouble); ok {
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), true
	}
	return 0, false
}

// IntArray returns every element of an int array variable.
func (r Row) IntArray(name string) ([]int32, bool) {
	v, ok := r.count(name, VarInt)
	if !ok {
		return nil, false
	}
	out := make([]int32, v.Count)
	for i := range out {
		b, ok := r.slice(name, i, VarInt)
		if !ok {
			return nil, false
		}
		out[i] = int32(binary.LittleEndian.Uint32(b))
	}
	return out, true
}

// BoolArray returns every element of a bool array variable.
func (r Row) BoolArray(name string) ([]bool, bool) {
	v, ok := r.count(name, VarBool)
	if !ok {
		return nil, false
	}
	out := make([]bool, v.Count)
	for i := range out {
		b, ok := r.slice(name, i, VarBool)
		if !ok {
			return nil, false
		}
		out[i] = b[0] != 0
	}
	return out, true
}

// FloatArray returns every element of a float or double array variable,
// widened to float64.
func (r Row) FloatArray(name string) ([]float64, bool) {
	v, ok := r.count(name, VarFloat, VarDouble)
	if !ok {
		return nil, false
	}
	out := make([]float64, v.Count)
	for i := range out {
		if v.Type == VarFloat {
			b, ok := r.slice(name, i, VarFloat)
			if !ok {
				return nil, false
			}
			out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
			continue
		}
		b, ok := r.slice(name, i, VarDouble)
		if !ok {
			return nil, false
		}
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	return out, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/irsdk/ -v`
Expected: PASS, all tests including the earlier ones.

- [ ] **Step 5: Verify no cgo crept in**

Run: `CGO_ENABLED=0 go build ./internal/irsdk/`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/irsdk/
git commit -m "Add typed telemetry value decoding"
```

---

### Task 5: Capture file format, writer and reader

**Files:**
- Create: `internal/capture/format.go`, `internal/capture/writer.go`, `internal/capture/reader.go`
- Test: `internal/capture/capture_test.go`

**Interfaces:**
- Consumes: Task 3's `irsdk.VarHeader`, `irsdk.Header`.
- Produces:
  - `Magic = "LPDCAP\x01\x00"` (8 bytes)
  - `type Kind uint8` with `KindHeader Kind = 1`, `KindSession Kind = 2`, `KindVars Kind = 3`
  - `type Meta struct { TickRate, NumVars, BufLen int32; VarHeaders []irsdk.VarHeader }`
  - `type Record struct { Kind Kind; T float64; Update uint32; TickCount uint32; YAML []byte; Vars []byte; Meta *Meta }`
  - `type Writer struct{...}` with `func NewWriter(path string, m Meta) (*Writer, error)`, `(*Writer) WriteSession(t float64, update uint32, yaml []byte) error`, `(*Writer) WriteVars(t float64, tick uint32, row []byte) error`, `(*Writer) Close() error`, `(*Writer) Path() string`
  - `type Reader struct{...}` with `func OpenReader(path string) (*Reader, error)`, `(*Reader) Meta() Meta`, `(*Reader) Next() (Record, error)` returning `io.EOF` at end, `(*Reader) Close() error`
  - `var ErrBadMagic error`

Format, from spec §10.2. The magic sits **outside** the gzip stream so the file is identifiable without decompressing.

```
file   = magic(8) || gzip( record* )
record = kind:uint8 || t:float64LE || len:uint32LE || payload[len]

kind 1  header   payload = JSON-encoded Meta
kind 2  session  payload = update:uint32LE || raw YAML bytes
kind 3  vars     payload = tickCount:uint32LE || raw variable row (BufLen bytes)
```

A `KindHeader` record is always first. Storing the **raw** variable row rather than decoded values is deliberate: replay then feeds bytes through the same decoder as live, so tests cover the binary layout parsing.

- [ ] **Step 1: Write the failing round-trip test**

Create `internal/capture/capture_test.go`:

```go
package capture

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/blezek/lapdog/internal/irsdk"
)

func testMeta() Meta {
	return Meta{
		TickRate: 60,
		NumVars:  2,
		BufLen:   8,
		VarHeaders: []irsdk.VarHeader{
			{Name: "Lap", Type: irsdk.VarInt, Offset: 0, Count: 1},
			{Name: "Speed", Type: irsdk.VarFloat, Offset: 4, Count: 1},
		},
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.lpd")

	w, err := NewWriter(path, testMeta())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteSession(0, 1, []byte("WeekendInfo:\n TrackID: 18\n")); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	if err := w.WriteVars(1.0, 100, []byte{1, 0, 0, 0, 2, 0, 0, 0}); err != nil {
		t.Fatalf("WriteVars: %v", err)
	}
	if err := w.WriteVars(2.0, 160, []byte{3, 0, 0, 0, 4, 0, 0, 0}); err != nil {
		t.Fatalf("WriteVars: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer r.Close()

	m := r.Meta()
	if m.TickRate != 60 || m.BufLen != 8 || len(m.VarHeaders) != 2 {
		t.Fatalf("Meta = %+v", m)
	}
	if m.VarHeaders[1].Name != "Speed" || m.VarHeaders[1].Type != irsdk.VarFloat {
		t.Errorf("VarHeaders[1] = %+v", m.VarHeaders[1])
	}

	rec, err := r.Next()
	if err != nil {
		t.Fatalf("Next 1: %v", err)
	}
	if rec.Kind != KindSession || rec.Update != 1 || string(rec.YAML) != "WeekendInfo:\n TrackID: 18\n" {
		t.Errorf("record 1 = %+v", rec)
	}

	rec, err = r.Next()
	if err != nil {
		t.Fatalf("Next 2: %v", err)
	}
	if rec.Kind != KindVars || rec.T != 1.0 || rec.TickCount != 100 {
		t.Errorf("record 2 = %+v", rec)
	}
	// The raw row must survive byte-for-byte so replay exercises the decoder.
	if got := rec.Vars; len(got) != 8 || got[0] != 1 || got[4] != 2 {
		t.Errorf("record 2 Vars = %v", got)
	}

	rec, err = r.Next()
	if err != nil {
		t.Fatalf("Next 3: %v", err)
	}
	if rec.T != 2.0 || rec.TickCount != 160 {
		t.Errorf("record 3 = %+v", rec)
	}

	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next at end = %v, want io.EOF", err)
	}
}

func TestOpenReaderRejectsBadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.lpd")
	if err := os.WriteFile(path, []byte("NOTLAPDOGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := OpenReader(path)
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("OpenReader on a non-capture file = %v, want ErrBadMagic", err)
	}
}

func TestWriterReportsBytesWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.lpd")
	w, err := NewWriter(path, testMeta())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := w.WriteVars(float64(i), uint32(i), make([]byte, 8)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() < int64(len(Magic)) {
		t.Errorf("file size %d, want at least the magic length", fi.Size())
	}
	if w.Path() != path {
		t.Errorf("Path() = %q, want %q", w.Path(), path)
	}
}

// The magic must be readable without decompressing the body.
func TestMagicIsOutsideGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.lpd")
	w, err := NewWriter(path, testMeta())
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:len(Magic)]) != Magic {
		t.Errorf("first bytes = %q, want %q", b[:len(Magic)], Magic)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capture/ -v`
Expected: FAIL — build error, `undefined: Meta`, `undefined: NewWriter`.

- [ ] **Step 3: Write the format definitions**

Create `internal/capture/format.go`:

```go
// Package capture reads and writes LapDog capture files, which record
// the telemetry frames the collector polled so that a session can be
// replayed later on any operating system.
package capture

import (
	"errors"

	"github.com/blezek/lapdog/internal/irsdk"
)

// Magic identifies a capture file. It is stored uncompressed at offset 0
// so the file is identifiable without decompressing the body.
const Magic = "LPDCAP\x01\x00"

// ErrBadMagic indicates the file is not a LapDog capture.
var ErrBadMagic = errors.New("capture: bad magic")

// Kind identifies a record type within a capture file.
type Kind uint8

// Record kinds. These are wire format; do not reorder.
const (
	// KindHeader carries a JSON-encoded Meta and is always the first record.
	KindHeader Kind = 1
	// KindSession carries a session-info YAML blob, written whenever the
	// sim's sessionInfoUpdate counter changes.
	KindSession Kind = 2
	// KindVars carries one raw variable-buffer row, written once per poll.
	KindVars Kind = 3
)

// Meta describes the variable layout the capture was recorded against.
// It is needed to decode any KindVars record.
type Meta struct {
	TickRate   int32              `json:"tickRate"`
	NumVars    int32              `json:"numVars"`
	BufLen     int32              `json:"bufLen"`
	VarHeaders []irsdk.VarHeader  `json:"varHeaders"`
}

// Record is one decoded capture record. Which fields are populated
// depends on Kind: KindSession sets Update and YAML, KindVars sets
// TickCount and Vars, KindHeader sets Meta.
type Record struct {
	Kind      Kind
	T         float64
	Update    uint32
	TickCount uint32
	YAML      []byte
	Vars      []byte
	Meta      *Meta
}
```

- [ ] **Step 4: Write the writer**

Create `internal/capture/writer.go`:

```go
package capture

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// Writer appends records to a capture file. It is not safe for
// concurrent use; the collector owns one Writer at a time.
type Writer struct {
	path string
	f    *os.File
	bw   *bufio.Writer
	gz   *gzip.Writer
}

// NewWriter creates path and writes the magic and the header record.
func NewWriter(path string, m Meta) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("capture: create %s: %w", path, err)
	}
	bw := bufio.NewWriterSize(f, 64<<10)
	if _, err := bw.WriteString(Magic); err != nil {
		f.Close()
		return nil, fmt.Errorf("capture: write magic: %w", err)
	}
	w := &Writer{path: path, f: f, bw: bw, gz: gzip.NewWriter(bw)}

	payload, err := json.Marshal(m)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("capture: marshal meta: %w", err)
	}
	if err := w.writeRecord(KindHeader, 0, payload); err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

// Path returns the file the Writer is appending to.
func (w *Writer) Path() string { return w.path }

// writeRecord emits one length-prefixed record into the gzip stream.
func (w *Writer) writeRecord(k Kind, t float64, payload []byte) error {
	var hdr [13]byte
	hdr[0] = byte(k)
	binary.LittleEndian.PutUint64(hdr[1:], math.Float64bits(t))
	binary.LittleEndian.PutUint32(hdr[9:], uint32(len(payload)))
	if _, err := w.gz.Write(hdr[:]); err != nil {
		return fmt.Errorf("capture: write record header: %w", err)
	}
	if _, err := w.gz.Write(payload); err != nil {
		return fmt.Errorf("capture: write record payload: %w", err)
	}
	return nil
}

// WriteSession records a session-info YAML blob at time t.
func (w *Writer) WriteSession(t float64, update uint32, yaml []byte) error {
	payload := make([]byte, 4+len(yaml))
	binary.LittleEndian.PutUint32(payload, update)
	copy(payload[4:], yaml)
	return w.writeRecord(KindSession, t, payload)
}

// WriteVars records one raw variable-buffer row at time t.
func (w *Writer) WriteVars(t float64, tick uint32, row []byte) error {
	payload := make([]byte, 4+len(row))
	binary.LittleEndian.PutUint32(payload, tick)
	copy(payload[4:], row)
	return w.writeRecord(KindVars, t, payload)
}

// Close flushes the gzip stream and closes the file.
func (w *Writer) Close() error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if w.gz != nil {
		note(w.gz.Close())
	}
	if w.bw != nil {
		note(w.bw.Flush())
	}
	if w.f != nil {
		note(w.f.Close())
	}
	return firstErr
}
```

- [ ] **Step 5: Verify the writer compiles**

Run: `go build ./internal/capture/`
Expected: no output, exit 0.

- [ ] **Step 6: Write the reader**

Create `internal/capture/reader.go`:

```go
package capture

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// Reader iterates the records of a capture file in write order.
type Reader struct {
	f    *os.File
	gz   *gzip.Reader
	br   *bufio.Reader
	meta Meta
}

// OpenReader opens path, validates the magic, and reads the header
// record so Meta is available before the first Next call.
func OpenReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("capture: open %s: %w", path, err)
	}
	magic := make([]byte, len(Magic))
	if _, err := io.ReadFull(f, magic); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: %s: %v", ErrBadMagic, path, err)
	}
	if string(magic) != Magic {
		f.Close()
		return nil, fmt.Errorf("%w: %s", ErrBadMagic, path)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("capture: gzip %s: %w", path, err)
	}
	r := &Reader{f: f, gz: gz, br: bufio.NewReaderSize(gz, 64<<10)}

	rec, err := r.Next()
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("capture: read header record: %w", err)
	}
	if rec.Kind != KindHeader || rec.Meta == nil {
		r.Close()
		return nil, fmt.Errorf("capture: %s: first record is kind %d, want header", path, rec.Kind)
	}
	r.meta = *rec.Meta
	return r, nil
}

// Meta returns the variable layout the capture was recorded against.
func (r *Reader) Meta() Meta { return r.meta }

// Next returns the next record, or io.EOF when the file is exhausted.
func (r *Reader) Next() (Record, error) {
	var hdr [13]byte
	if _, err := io.ReadFull(r.br, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return Record{}, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Record{}, fmt.Errorf("capture: truncated record header: %w", err)
		}
		return Record{}, err
	}
	rec := Record{
		Kind: Kind(hdr[0]),
		T:    math.Float64frombits(binary.LittleEndian.Uint64(hdr[1:])),
	}
	n := binary.LittleEndian.Uint32(hdr[9:])
	// Guard against a corrupt length allocating unbounded memory.
	const maxPayload = 64 << 20
	if n > maxPayload {
		return Record{}, fmt.Errorf("capture: record payload %d exceeds %d", n, maxPayload)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r.br, payload); err != nil {
		return Record{}, fmt.Errorf("capture: truncated payload: %w", err)
	}

	switch rec.Kind {
	case KindHeader:
		var m Meta
		if err := json.Unmarshal(payload, &m); err != nil {
			return Record{}, fmt.Errorf("capture: unmarshal meta: %w", err)
		}
		rec.Meta = &m
	case KindSession:
		if len(payload) < 4 {
			return Record{}, errors.New("capture: session record shorter than 4 bytes")
		}
		rec.Update = binary.LittleEndian.Uint32(payload)
		rec.YAML = payload[4:]
	case KindVars:
		if len(payload) < 4 {
			return Record{}, errors.New("capture: vars record shorter than 4 bytes")
		}
		rec.TickCount = binary.LittleEndian.Uint32(payload)
		rec.Vars = payload[4:]
	default:
		return Record{}, fmt.Errorf("capture: unknown record kind %d", rec.Kind)
	}
	return rec, nil
}

// Close releases the gzip stream and the underlying file.
func (r *Reader) Close() error {
	var firstErr error
	if r.gz != nil {
		if err := r.gz.Close(); err != nil {
			firstErr = err
		}
	}
	if r.f != nil {
		if err := r.f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/capture/ -v`
Expected: PASS, five tests.

- [ ] **Step 8: Commit**

```bash
git add internal/capture/
git commit -m "Add capture file format, writer and reader"
```

---

### Task 6: Source interface, replay source, and capture pruning

**Files:**
- Create: `internal/source/source.go`, `internal/source/replay.go`, `internal/capture/prune.go`
- Test: `internal/source/replay_test.go`, `internal/capture/prune_test.go`

**Interfaces:**
- Consumes: Task 3–4's `irsdk.Row`, `irsdk.VarHeader`; Task 5's `capture.Reader`, `capture.Meta`.
- Produces:
  - `type Frame struct { T float64; TickCount uint32; Row irsdk.Row; SessionYAML []byte; SessionUpdate uint32; YAMLChanged bool }`
  - `type Source interface { Next() (Frame, error); Meta() capture.Meta; Close() error }`
  - `var ErrDisconnected error`
  - `func NewReplay(path string) (Source, error)`
  - `capture.PruneDir(dir string, maxBytes int64, keep string) (removed int, freed int64, err error)`

`Frame` carries the session YAML on **every** frame, not only when it changes, so the collector never has to remember the last one. `YAMLChanged` tells the collector when re-parsing is worthwhile. The replay source coalesces a `KindSession` record with the following `KindVars` record, since the collector consumes one frame per poll.

`ErrDisconnected` is returned by the live source when the sim is not running. The collector treats it as an expected state, not a failure.

- [ ] **Step 1: Write the failing replay test**

Create `internal/source/replay_test.go`:

```go
package source

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// writeFixture builds a small capture: one YAML blob, then three rows,
// with a second YAML blob before the third row.
func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fix.lpd")
	m := capture.Meta{
		TickRate: 60,
		NumVars:  1,
		BufLen:   4,
		VarHeaders: []irsdk.VarHeader{
			{Name: "Lap", Type: irsdk.VarInt, Offset: 0, Count: 1},
		},
	}
	w, err := capture.NewWriter(path, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSession(0, 1, []byte("WeekendInfo:\n LeagueID: 0\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteVars(1, 60, []byte{1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteVars(2, 120, []byte{2, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSession(2.5, 2, []byte("WeekendInfo:\n LeagueID: 4242\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteVars(3, 180, []byte{3, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayEmitsOneFramePerVarsRecord(t *testing.T) {
	s, err := NewReplay(writeFixture(t))
	if err != nil {
		t.Fatalf("NewReplay: %v", err)
	}
	defer s.Close()

	if got := s.Meta().BufLen; got != 4 {
		t.Errorf("Meta().BufLen = %d, want 4", got)
	}

	f1, err := s.Next()
	if err != nil {
		t.Fatalf("Next 1: %v", err)
	}
	if f1.T != 1 || f1.TickCount != 60 {
		t.Errorf("frame 1 = %+v", f1)
	}
	if lap, ok := f1.Row.Int("Lap"); !ok || lap != 1 {
		t.Errorf("frame 1 Lap = %d, %v; want 1, true", lap, ok)
	}
	// The YAML preceding the first row must be attached, and flagged changed.
	if !f1.YAMLChanged || f1.SessionUpdate != 1 {
		t.Errorf("frame 1 YAMLChanged=%v Update=%d; want true, 1", f1.YAMLChanged, f1.SessionUpdate)
	}
	if string(f1.SessionYAML) != "WeekendInfo:\n LeagueID: 0\n" {
		t.Errorf("frame 1 YAML = %q", f1.SessionYAML)
	}

	f2, err := s.Next()
	if err != nil {
		t.Fatalf("Next 2: %v", err)
	}
	// YAML is still carried, but not flagged as changed.
	if f2.YAMLChanged {
		t.Error("frame 2 YAMLChanged = true, want false")
	}
	if string(f2.SessionYAML) != "WeekendInfo:\n LeagueID: 0\n" {
		t.Errorf("frame 2 must carry the last YAML, got %q", f2.SessionYAML)
	}

	f3, err := s.Next()
	if err != nil {
		t.Fatalf("Next 3: %v", err)
	}
	if !f3.YAMLChanged || f3.SessionUpdate != 2 {
		t.Errorf("frame 3 YAMLChanged=%v Update=%d; want true, 2", f3.YAMLChanged, f3.SessionUpdate)
	}
	if string(f3.SessionYAML) != "WeekendInfo:\n LeagueID: 4242\n" {
		t.Errorf("frame 3 YAML = %q", f3.SessionYAML)
	}
	if lap, ok := f3.Row.Int("Lap"); !ok || lap != 3 {
		t.Errorf("frame 3 Lap = %d, %v", lap, ok)
	}

	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next at end = %v, want io.EOF", err)
	}
}

func TestReplayMissingFile(t *testing.T) {
	if _, err := NewReplay(filepath.Join(t.TempDir(), "nope.lpd")); err == nil {
		t.Fatal("NewReplay on a missing file: want error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/source/ -v`
Expected: FAIL — build error, `undefined: NewReplay`.

- [ ] **Step 3: Write the Source interface**

Create `internal/source/source.go`:

```go
// Package source supplies telemetry frames to the collector, either
// live from the running simulator or replayed from a capture file.
package source

import (
	"errors"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// ErrDisconnected reports that the simulator is not currently running.
// The collector treats this as an expected state rather than a failure:
// iRacing not running is the normal case.
var ErrDisconnected = errors.New("source: sim not connected")

// Frame is one poll's worth of telemetry.
//
// SessionYAML is populated on every frame, not only when it changes, so
// the collector never has to cache the previous value. YAMLChanged
// reports whether it differs from the preceding frame, which is the
// signal to re-parse and re-classify.
type Frame struct {
	T             float64
	TickCount     uint32
	Row           irsdk.Row
	SessionYAML   []byte
	SessionUpdate uint32
	YAMLChanged   bool
}

// Source produces frames in time order. Next returns io.EOF when a
// finite source is exhausted, and ErrDisconnected when a live source
// has no simulator to read.
type Source interface {
	Next() (Frame, error)
	Meta() capture.Meta
	Close() error
}
```

- [ ] **Step 4: Write the replay source**

Create `internal/source/replay.go`:

```go
package source

import (
	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// replay plays a capture file back through the Source interface.
//
// Time advances from the frame timestamps in the file, never from the
// wall clock, so tests can run a ninety-minute race through the
// collector in milliseconds.
type replay struct {
	r        *capture.Reader
	meta     capture.Meta
	yaml     []byte
	update   uint32
	pendYAML bool
}

// NewReplay opens a capture file as a Source.
func NewReplay(path string) (Source, error) {
	r, err := capture.OpenReader(path)
	if err != nil {
		return nil, err
	}
	return &replay{r: r, meta: r.Meta()}, nil
}

// Meta returns the variable layout the capture was recorded against.
func (s *replay) Meta() capture.Meta { return s.meta }

// Next returns the next frame. Session records are folded into the
// following variable record, since the collector consumes one frame per
// poll.
func (s *replay) Next() (Frame, error) {
	for {
		rec, err := s.r.Next()
		if err != nil {
			return Frame{}, err
		}
		switch rec.Kind {
		case capture.KindSession:
			s.yaml = rec.YAML
			s.update = rec.Update
			s.pendYAML = true
		case capture.KindVars:
			f := Frame{
				T:             rec.T,
				TickCount:     rec.TickCount,
				Row:           irsdk.NewRow(s.meta.VarHeaders, rec.Vars),
				SessionYAML:   s.yaml,
				SessionUpdate: s.update,
				YAMLChanged:   s.pendYAML,
			}
			s.pendYAML = false
			return f, nil
		default:
			// A stray header record mid-file is ignored rather than fatal.
			continue
		}
	}
}

// Close releases the underlying capture file.
func (s *replay) Close() error { return s.r.Close() }

// compile-time assertion that replay satisfies Source.
var _ Source = (*replay)(nil)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/source/ -v`
Expected: PASS, two tests.

- [ ] **Step 6: Write the failing prune test**

Create `internal/capture/prune_test.go`:

```go
package capture

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSized creates a file of n bytes with a specific modification time.
func writeSized(t *testing.T, dir, name string, n int, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPruneDirRemovesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	oldest := writeSized(t, dir, "a.lpd", 1000, 3*time.Hour)
	middle := writeSized(t, dir, "b.lpd", 1000, 2*time.Hour)
	newest := writeSized(t, dir, "c.lpd", 1000, 1*time.Hour)

	// Cap of 2500 over 3000 bytes: one file must go, and it must be the oldest.
	removed, freed, err := PruneDir(dir, 2500, "")
	if err != nil {
		t.Fatalf("PruneDir: %v", err)
	}
	if removed != 1 || freed != 1000 {
		t.Errorf("removed=%d freed=%d; want 1, 1000", removed, freed)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Error("oldest file survived, want removed")
	}
	for _, p := range []string{middle, newest} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed, want kept", filepath.Base(p))
		}
	}
}

// The file currently being written must never be deleted.
func TestPruneDirNeverRemovesKeep(t *testing.T) {
	dir := t.TempDir()
	active := writeSized(t, dir, "active.lpd", 5000, 10*time.Hour)
	other := writeSized(t, dir, "other.lpd", 1000, 1*time.Hour)

	removed, _, err := PruneDir(dir, 100, active)
	if err != nil {
		t.Fatalf("PruneDir: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Error("the active file was removed, want kept")
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Error("the non-active file survived, want removed")
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
}

func TestPruneDirZeroMeansUnlimited(t *testing.T) {
	dir := t.TempDir()
	writeSized(t, dir, "a.lpd", 10_000, time.Hour)
	removed, freed, err := PruneDir(dir, 0, "")
	if err != nil {
		t.Fatalf("PruneDir: %v", err)
	}
	if removed != 0 || freed != 0 {
		t.Errorf("removed=%d freed=%d; want 0, 0 with an unlimited cap", removed, freed)
	}
}

func TestPruneDirUnderCapIsNoOp(t *testing.T) {
	dir := t.TempDir()
	writeSized(t, dir, "a.lpd", 100, time.Hour)
	removed, _, err := PruneDir(dir, 1<<20, "")
	if err != nil {
		t.Fatalf("PruneDir: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}

func TestPruneDirIgnoresNonCaptureFiles(t *testing.T) {
	dir := t.TempDir()
	writeSized(t, dir, "notes.txt", 10_000, 5*time.Hour)
	writeSized(t, dir, "a.lpd", 1000, time.Hour)
	removed, _, err := PruneDir(dir, 500, "")
	if err != nil {
		t.Fatalf("PruneDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Error("a non-.lpd file was removed; prune must only touch captures")
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
}

func TestPruneDirMissingDirIsNotAnError(t *testing.T) {
	removed, _, err := PruneDir(filepath.Join(t.TempDir(), "absent"), 100, "")
	if err != nil {
		t.Fatalf("PruneDir on a missing directory = %v, want nil", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test ./internal/capture/ -run Prune -v`
Expected: FAIL — `undefined: PruneDir`.

- [ ] **Step 8: Write the prune implementation**

Create `internal/capture/prune.go`:

```go
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ext is the capture file extension. PruneDir only considers files with
// this suffix, so unrelated files in the directory are never deleted.
const Ext = ".lpd"

// PruneDir enforces a total size cap over the capture files in dir by
// deleting oldest-first until the total is at or below maxBytes.
//
// maxBytes of 0 means unlimited and makes this a no-op. The file named
// by keep is never deleted, which is how the in-progress capture is
// protected. A missing directory is not an error.
//
// It returns the number of files removed and the bytes freed.
func PruneDir(dir string, maxBytes int64, keep string) (int, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("capture: read dir %s: %w", dir, err)
	}
	if maxBytes <= 0 {
		return 0, 0, nil
	}

	type entry struct {
		path  string
		size  int64
		mtime int64
	}
	var files []entry
	var total int64
	keepAbs, _ := filepath.Abs(keep)

	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), Ext) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // vanished between ReadDir and Info; nothing to prune
		}
		p := filepath.Join(dir, e.Name())
		total += fi.Size()
		abs, _ := filepath.Abs(p)
		if keep != "" && abs == keepAbs {
			continue // counts toward the total but is not a deletion candidate
		}
		files = append(files, entry{path: p, size: fi.Size(), mtime: fi.ModTime().UnixNano()})
	}

	if total <= maxBytes {
		return 0, 0, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime < files[j].mtime })

	var removed int
	var freed int64
	for _, f := range files {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, freed, fmt.Errorf("capture: remove %s: %w", f.path, err)
		}
		total -= f.size
		freed += f.size
		removed++
	}
	return removed, freed, nil
}
```

- [ ] **Step 9: Run the full test suite**

Run: `go test ./... -v`
Expected: PASS for `internal/irsdk`, `internal/capture`, `internal/source`, `internal/version`.

- [ ] **Step 10: Commit**

```bash
git add internal/source/ internal/capture/
git commit -m "Add Source interface, replay source and capture pruning"
```

---

### Task 7: Session YAML subset parser

**Files:**
- Create: `internal/sessionyaml/types.go`, `internal/sessionyaml/parse.go`
- Test: `internal/sessionyaml/parse_test.go`, `internal/sessionyaml/testdata/practice_only.yaml`, `internal/sessionyaml/testdata/race_weekend.yaml`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Info struct { WeekendInfo Weekend; SessionInfo Sessions; QualifyResultsInfo QualifyResults; DriverInfo Drivers }`
  - `type Weekend struct { TrackID int; TrackDisplayName, TrackDisplayShortName, TrackConfigName, TrackName string; TrackLength string; SeriesID, SeasonID, SessionID, SubSessionID, LeagueID, Official int; EventType, Category, SimMode string; TeamRacing int }`
  - `type Sessions struct { NumSessions int; Sessions []Session }`
  - `type Session struct { SessionNum int; SessionType string; SessionLaps string; SessionTime string; ResultsOfficial, ResultsLapsComplete int; ResultsPositions []ResultPosition }`
  - `type ResultPosition struct { Position, ClassPosition, CarIdx, Lap, LapsComplete, Incidents, ReasonOutId int; Time, FastestTime float64; FastestLap int }`
  - `type QualifyResults struct { Results []QualifyResult }`
  - `type QualifyResult struct { Position, ClassPosition, CarIdx, FastestLap int; FastestTime float64 }`
  - `type Drivers struct { DriverCarIdx int; DriverCarEstLapTime float64; Drivers []Driver }`
  - `type Driver struct { CarIdx, UserID, CarID, CarClassID, IRating, IsSpectator int; UserName, CarPath, CarScreenName, CarScreenNameShort, CarClassShortName, LicString string; CarIsAI int }`
  - `func Parse(b []byte) (*Info, error)`
  - `func (i *Info) Me() (Driver, bool)`
  - `func (i *Info) SessionByNum(n int) (Session, bool)`
  - `func (i *Info) HasRaceSession() bool`
  - `func (i *Info) TrackLengthKm() float64`
  - `func (i *Info) AIOpponentCount() (count int, fieldPresent bool)`
  - `func (i *Info) MyResult(sessionNum int) (ResultPosition, bool)`
  - `func (i *Info) MyQualifyResult() (QualifyResult, bool)`
  - `func (i *Info) FieldSize(sessionNum int) int`

`SessionLaps`, `SessionTime` and `TrackLength` are strings because the sim emits `"unlimited"` and `"7.20 km"` in those fields, not numbers. Parsing them as `int`/`float64` fails on real data.

The parser must **tolerate unknown and missing keys**. `yaml.v3` does this by default with struct tags; the tests pin that behaviour so a future change to strict mode is caught.

`CarIsAI` is unverified against the bundled documentation — see spec §6.5. `AIOpponentCount` reports `fieldPresent=false` when no driver carries the key, which is the signal for the classifier to use its heuristic.

- [ ] **Step 1: Create the practice-only fixture**

Create `internal/sessionyaml/testdata/practice_only.yaml`:

```yaml
---
WeekendInfo:
 TrackName: watkinsglen
 TrackID: 18
 TrackLength: 5.43 km
 TrackDisplayName: Watkins Glen International
 TrackDisplayShortName: Watkins Glen
 TrackConfigName: Boot
 SeriesID: 0
 SeasonID: 0
 SessionID: 0
 SubSessionID: 0
 LeagueID: 0
 Official: 1
 RaceWeek: 3
 EventType: Practice
 Category: Road
 SimMode: full
 TeamRacing: 0
 SomeFutureKeyWeDoNotKnow: 42
SessionInfo:
 NumSessions: 1
 Sessions:
 - SessionNum: 0
   SessionLaps: unlimited
   SessionTime: unlimited
   SessionType: Open Practice
   ResultsOfficial: 0
   ResultsLapsComplete: -1
   ResultsPositions:
DriverInfo:
 DriverCarIdx: 7
 DriverCarEstLapTime: 102.418
 Drivers:
 - CarIdx: 7
   UserName: Test Driver
   UserID: 123456
   CarID: 173
   CarPath: porsche991rgt3
   CarScreenName: Porsche 911 GT3 R
   CarScreenNameShort: Porsche 911 GT3 R
   CarClassID: 2523
   CarClassShortName: GT3
   IRating: 2100
   LicString: A 3.55
   IsSpectator: 0
 - CarIdx: 9
   UserName: Other Driver
   UserID: 654321
   CarID: 173
   CarScreenName: Porsche 911 GT3 R
   CarClassID: 2523
   IRating: 1800
   IsSpectator: 0
...
```

- [ ] **Step 2: Create the race-weekend fixture**

Create `internal/sessionyaml/testdata/race_weekend.yaml`:

```yaml
---
WeekendInfo:
 TrackName: spa
 TrackID: 341
 TrackLength: 7.00 km
 TrackDisplayName: Circuit de Spa-Francorchamps
 TrackDisplayShortName: Spa
 TrackConfigName: Grand Prix Pits
 SeriesID: 411
 SeasonID: 4703
 SessionID: 221144
 SubSessionID: 55667788
 LeagueID: 0
 Official: 1
 EventType: Race
 Category: Road
 SimMode: full
 TeamRacing: 0
SessionInfo:
 NumSessions: 3
 Sessions:
 - SessionNum: 0
   SessionType: Practice
   SessionLaps: unlimited
   SessionTime: 900.0000 sec
   ResultsOfficial: 0
   ResultsLapsComplete: -1
 - SessionNum: 1
   SessionType: Open Qualify
   SessionLaps: unlimited
   SessionTime: 600.0000 sec
   ResultsOfficial: 1
   ResultsLapsComplete: 2
 - SessionNum: 2
   SessionType: Race
   SessionLaps: 25
   SessionTime: unlimited
   ResultsOfficial: 1
   ResultsLapsComplete: 25
   ResultsPositions:
   - Position: 4
     ClassPosition: 3
     CarIdx: 7
     Lap: 25
     Time: 2841.220
     FastestLap: 12
     FastestTime: 141.882
     LapsComplete: 25
     Incidents: 6
     ReasonOutId: 0
   - Position: 5
     ClassPosition: 4
     CarIdx: 9
     Lap: 25
     LapsComplete: 25
     Incidents: 2
     ReasonOutId: 0
QualifyResultsInfo:
 Results:
 - Position: 6
   ClassPosition: 5
   CarIdx: 7
   FastestLap: 2
   FastestTime: 140.912
 - Position: 7
   ClassPosition: 6
   CarIdx: 9
   FastestLap: 2
   FastestTime: 141.400
DriverInfo:
 DriverCarIdx: 7
 DriverCarEstLapTime: 141.203
 Drivers:
 - CarIdx: 7
   UserName: Test Driver
   UserID: 123456
   CarID: 173
   CarPath: porsche991rgt3
   CarScreenName: Porsche 911 GT3 R
   CarScreenNameShort: Porsche 911 GT3 R
   CarClassID: 2523
   CarClassShortName: GT3
   IRating: 2100
   LicString: A 3.55
   IsSpectator: 0
 - CarIdx: 9
   UserName: Other Driver
   UserID: 654321
   CarID: 173
   CarScreenName: Porsche 911 GT3 R
   CarClassID: 2523
   IsSpectator: 0
...
```

- [ ] **Step 3: Write the failing test**

Create `internal/sessionyaml/parse_test.go`:

```go
package sessionyaml

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) *Info {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	info, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return info
}

func TestParseWeekendInfo(t *testing.T) {
	i := load(t, "race_weekend.yaml")
	w := i.WeekendInfo
	if w.TrackID != 341 || w.TrackDisplayName != "Circuit de Spa-Francorchamps" {
		t.Errorf("track = %d / %q", w.TrackID, w.TrackDisplayName)
	}
	if w.TrackConfigName != "Grand Prix Pits" {
		t.Errorf("TrackConfigName = %q", w.TrackConfigName)
	}
	if w.SubSessionID != 55667788 || w.SeriesID != 411 || w.SeasonID != 4703 {
		t.Errorf("ids = %+v", w)
	}
	if w.LeagueID != 0 || w.Official != 1 || w.SimMode != "full" {
		t.Errorf("context fields = %+v", w)
	}
}

// TrackLength is "7.00 km" in real data, so it must be a string and
// TrackLengthKm must extract the number.
func TestTrackLengthKm(t *testing.T) {
	if got := load(t, "race_weekend.yaml").TrackLengthKm(); math.Abs(got-7.0) > 1e-6 {
		t.Errorf("TrackLengthKm() = %v, want 7.0", got)
	}
	if got := load(t, "practice_only.yaml").TrackLengthKm(); math.Abs(got-5.43) > 1e-6 {
		t.Errorf("TrackLengthKm() = %v, want 5.43", got)
	}
}

// SessionLaps and SessionTime carry "unlimited", so they must be strings.
func TestUnlimitedFieldsParseAsStrings(t *testing.T) {
	i := load(t, "practice_only.yaml")
	s, ok := i.SessionByNum(0)
	if !ok {
		t.Fatal("SessionByNum(0) not found")
	}
	if s.SessionLaps != "unlimited" || s.SessionTime != "unlimited" {
		t.Errorf("laps=%q time=%q", s.SessionLaps, s.SessionTime)
	}
}

// Unknown keys must be ignored, not rejected. iRacing adds fields over time.
func TestUnknownKeysAreIgnored(t *testing.T) {
	if load(t, "practice_only.yaml").WeekendInfo.TrackID != 18 {
		t.Error("a file containing an unknown key failed to parse correctly")
	}
}

func TestSessionByNum(t *testing.T) {
	i := load(t, "race_weekend.yaml")
	s, ok := i.SessionByNum(2)
	if !ok {
		t.Fatal("SessionByNum(2) not found")
	}
	if s.SessionType != "Race" || s.ResultsLapsComplete != 25 {
		t.Errorf("session 2 = %+v", s)
	}
	if _, ok := i.SessionByNum(99); ok {
		t.Error("SessionByNum(99) ok = true, want false")
	}
}

func TestHasRaceSession(t *testing.T) {
	if !load(t, "race_weekend.yaml").HasRaceSession() {
		t.Error("race weekend HasRaceSession() = false, want true")
	}
	if load(t, "practice_only.yaml").HasRaceSession() {
		t.Error("practice-only HasRaceSession() = true, want false")
	}
}

func TestMe(t *testing.T) {
	me, ok := load(t, "race_weekend.yaml").Me()
	if !ok {
		t.Fatal("Me() not found")
	}
	if me.CarIdx != 7 || me.CarScreenName != "Porsche 911 GT3 R" {
		t.Errorf("Me() = %+v", me)
	}
	if me.CarClassShortName != "GT3" || me.CarID != 173 || me.CarClassID != 2523 {
		t.Errorf("Me() car identity = %+v", me)
	}
}

func TestMyResult(t *testing.T) {
	i := load(t, "race_weekend.yaml")
	r, ok := i.MyResult(2)
	if !ok {
		t.Fatal("MyResult(2) not found")
	}
	if r.Position != 4 || r.ClassPosition != 3 || r.Incidents != 6 || r.LapsComplete != 25 {
		t.Errorf("MyResult(2) = %+v", r)
	}
	// A session with no results yet must report absent, not zero values.
	if _, ok := i.MyResult(0); ok {
		t.Error("MyResult(0) ok = true for a session with no ResultsPositions")
	}
}

func TestMyQualifyResult(t *testing.T) {
	q, ok := load(t, "race_weekend.yaml").MyQualifyResult()
	if !ok {
		t.Fatal("MyQualifyResult() not found")
	}
	if q.Position != 6 || q.ClassPosition != 5 {
		t.Errorf("MyQualifyResult() = %+v", q)
	}
	if math.Abs(q.FastestTime-140.912) > 1e-6 {
		t.Errorf("FastestTime = %v, want 140.912", q.FastestTime)
	}
	if _, ok := load(t, "practice_only.yaml").MyQualifyResult(); ok {
		t.Error("MyQualifyResult() ok = true with no qualifying section")
	}
}

func TestFieldSize(t *testing.T) {
	i := load(t, "race_weekend.yaml")
	if got := i.FieldSize(2); got != 2 {
		t.Errorf("FieldSize(2) = %d, want 2 from ResultsPositions", got)
	}
	// With no results, fall back to counting non-spectator drivers.
	if got := i.FieldSize(0); got != 2 {
		t.Errorf("FieldSize(0) = %d, want 2 from the driver list", got)
	}
}

// CarIsAI is absent from both fixtures, so fieldPresent must be false.
// That is the signal for the classifier to fall back to its heuristic.
func TestAIOpponentCountFieldAbsent(t *testing.T) {
	count, present := load(t, "race_weekend.yaml").AIOpponentCount()
	if present {
		t.Error("fieldPresent = true, but no fixture driver carries CarIsAI")
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestAIOpponentCountFieldPresent(t *testing.T) {
	y := []byte(`---
WeekendInfo:
 SubSessionID: 0
 LeagueID: 0
 Official: 0
 SimMode: full
SessionInfo:
 NumSessions: 1
 Sessions:
 - SessionNum: 0
   SessionType: Race
DriverInfo:
 DriverCarIdx: 0
 Drivers:
 - CarIdx: 0
   UserName: Test Driver
   CarIsAI: 0
 - CarIdx: 1
   UserName: Bot One
   CarIsAI: 1
 - CarIdx: 2
   UserName: Bot Two
   CarIsAI: 1
...
`)
	i, err := Parse(y)
	if err != nil {
		t.Fatal(err)
	}
	count, present := i.AIOpponentCount()
	if !present {
		t.Fatal("fieldPresent = false, want true when CarIsAI appears")
	}
	// The player must not be counted even if flagged.
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestParseGarbageIsAnError(t *testing.T) {
	if _, err := Parse([]byte("\tthis: is: not: yaml\n  ][")); err == nil {
		t.Fatal("Parse on malformed YAML: want error, got nil")
	}
}

func TestParseEmptyIsAnError(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("Parse(nil): want error, got nil")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/sessionyaml/ -v`
Expected: FAIL — build error, `undefined: Parse`, `undefined: Info`.

- [ ] **Step 5: Write the type definitions**

Create `internal/sessionyaml/types.go`:

```go
// Package sessionyaml parses the subset of iRacing's session-info YAML
// string that LapDog needs. Unknown and missing keys are tolerated,
// because the sim adds fields over time and the set present depends on
// the session.
package sessionyaml

// Info is the parsed session-info document.
type Info struct {
	WeekendInfo        Weekend        `yaml:"WeekendInfo"`
	SessionInfo        Sessions       `yaml:"SessionInfo"`
	QualifyResultsInfo QualifyResults `yaml:"QualifyResultsInfo"`
	DriverInfo         Drivers        `yaml:"DriverInfo"`
}

// Weekend holds the WeekendInfo section, which carries the identity and
// context fields the classifier depends on.
type Weekend struct {
	TrackName             string `yaml:"TrackName"`
	TrackID               int    `yaml:"TrackID"`
	TrackDisplayName      string `yaml:"TrackDisplayName"`
	TrackDisplayShortName string `yaml:"TrackDisplayShortName"`
	TrackConfigName       string `yaml:"TrackConfigName"`

	// TrackLength is a string because the sim emits "7.00 km".
	TrackLength string `yaml:"TrackLength"`

	SeriesID     int `yaml:"SeriesID"`
	SeasonID     int `yaml:"SeasonID"`
	SessionID    int `yaml:"SessionID"`
	SubSessionID int `yaml:"SubSessionID"`
	LeagueID     int `yaml:"LeagueID"`
	Official     int `yaml:"Official"`

	EventType  string `yaml:"EventType"`
	Category   string `yaml:"Category"`
	SimMode    string `yaml:"SimMode"`
	TeamRacing int    `yaml:"TeamRacing"`
}

// Sessions holds the SessionInfo section.
type Sessions struct {
	NumSessions int       `yaml:"NumSessions"`
	Sessions    []Session `yaml:"Sessions"`
}

// Session is one entry in SessionInfo.Sessions.
type Session struct {
	SessionNum  int    `yaml:"SessionNum"`
	SessionType string `yaml:"SessionType"`

	// SessionLaps and SessionTime are strings because the sim emits
	// "unlimited" and "900.0000 sec".
	SessionLaps string `yaml:"SessionLaps"`
	SessionTime string `yaml:"SessionTime"`

	ResultsOfficial     int              `yaml:"ResultsOfficial"`
	ResultsLapsComplete int              `yaml:"ResultsLapsComplete"`
	ResultsPositions    []ResultPosition `yaml:"ResultsPositions"`
}

// ResultPosition is one car's classified result in a session. These
// fields only populate as the session concludes.
type ResultPosition struct {
	Position      int     `yaml:"Position"`
	ClassPosition int     `yaml:"ClassPosition"`
	CarIdx        int     `yaml:"CarIdx"`
	Lap           int     `yaml:"Lap"`
	Time          float64 `yaml:"Time"`
	FastestLap    int     `yaml:"FastestLap"`
	FastestTime   float64 `yaml:"FastestTime"`
	LapsComplete  int     `yaml:"LapsComplete"`
	Incidents     int     `yaml:"Incidents"`
	ReasonOutId   int     `yaml:"ReasonOutId"`
}

// QualifyResults holds the QualifyResultsInfo section, which is the
// authoritative qualifying result and only populates once qualifying
// has run.
type QualifyResults struct {
	Results []QualifyResult `yaml:"Results"`
}

// QualifyResult is one car's qualifying result.
type QualifyResult struct {
	Position      int     `yaml:"Position"`
	ClassPosition int     `yaml:"ClassPosition"`
	CarIdx        int     `yaml:"CarIdx"`
	FastestLap    int     `yaml:"FastestLap"`
	FastestTime   float64 `yaml:"FastestTime"`
}

// Drivers holds the DriverInfo section.
type Drivers struct {
	DriverCarIdx        int      `yaml:"DriverCarIdx"`
	DriverCarEstLapTime float64  `yaml:"DriverCarEstLapTime"`
	Drivers             []Driver `yaml:"Drivers"`
}

// Driver is one entry in DriverInfo.Drivers.
//
// CarIsAI is a pointer so that "absent" is distinguishable from
// "present and zero". The field is unverified against the bundled SDK
// documentation — see spec section 6.5 — and that distinction is what
// lets the classifier know whether to trust it or fall back.
type Driver struct {
	CarIdx             int    `yaml:"CarIdx"`
	UserName           string `yaml:"UserName"`
	UserID             int    `yaml:"UserID"`
	CarID              int    `yaml:"CarID"`
	CarPath            string `yaml:"CarPath"`
	CarScreenName      string `yaml:"CarScreenName"`
	CarScreenNameShort string `yaml:"CarScreenNameShort"`
	CarClassID         int    `yaml:"CarClassID"`
	CarClassShortName  string `yaml:"CarClassShortName"`
	IRating            int    `yaml:"IRating"`
	LicString          string `yaml:"LicString"`
	IsSpectator        int    `yaml:"IsSpectator"`
	CarIsAI            *int   `yaml:"CarIsAI"`
}
```

- [ ] **Step 6: Write the parser and accessors**

Create `internal/sessionyaml/parse.go`:

```go
package sessionyaml

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse decodes a session-info YAML document. Unknown keys are ignored.
func Parse(b []byte) (*Info, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, errors.New("sessionyaml: empty document")
	}
	var i Info
	if err := yaml.Unmarshal(b, &i); err != nil {
		return nil, fmt.Errorf("sessionyaml: unmarshal: %w", err)
	}
	return &i, nil
}

// Me returns the local driver's entry, matched on DriverCarIdx.
func (i *Info) Me() (Driver, bool) {
	for _, d := range i.DriverInfo.Drivers {
		if d.CarIdx == i.DriverInfo.DriverCarIdx {
			return d, true
		}
	}
	return Driver{}, false
}

// SessionByNum returns the session with the given SessionNum.
func (i *Info) SessionByNum(n int) (Session, bool) {
	for _, s := range i.SessionInfo.Sessions {
		if s.SessionNum == n {
			return s, true
		}
	}
	return Session{}, false
}

// HasRaceSession reports whether any session in the weekend is a race.
// This is what separates race practice from public practice: a practice
// session inside a weekend that also has a race is race practice.
func (i *Info) HasRaceSession() bool {
	for _, s := range i.SessionInfo.Sessions {
		if IsRaceType(s.SessionType) {
			return true
		}
	}
	return false
}

// IsRaceType reports whether a raw SessionType string denotes a race.
// Heats and consolation races count, since they are raced wheel to wheel.
func IsRaceType(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "race", "heat", "consolation":
		return true
	}
	return false
}

// TrackLengthKm extracts the numeric kilometre value from the
// "7.00 km" form the sim emits, returning 0 if it cannot be parsed.
func (i *Info) TrackLengthKm() float64 {
	f := strings.Fields(i.WeekendInfo.TrackLength)
	if len(f) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// AIOpponentCount returns the number of AI opponents excluding the local
// driver, and whether the CarIsAI field was present at all.
//
// fieldPresent false means the field is absent from every driver entry,
// which is the classifier's signal to use its documented heuristic
// instead. See spec section 6.5.
func (i *Info) AIOpponentCount() (int, bool) {
	present := false
	count := 0
	for _, d := range i.DriverInfo.Drivers {
		if d.CarIsAI == nil {
			continue
		}
		present = true
		if *d.CarIsAI != 0 && d.CarIdx != i.DriverInfo.DriverCarIdx {
			count++
		}
	}
	return count, present
}

// MyResult returns the local driver's classified result for a session.
func (i *Info) MyResult(sessionNum int) (ResultPosition, bool) {
	s, ok := i.SessionByNum(sessionNum)
	if !ok {
		return ResultPosition{}, false
	}
	for _, r := range s.ResultsPositions {
		if r.CarIdx == i.DriverInfo.DriverCarIdx {
			return r, true
		}
	}
	return ResultPosition{}, false
}

// MyQualifyResult returns the local driver's qualifying result.
func (i *Info) MyQualifyResult() (QualifyResult, bool) {
	for _, q := range i.QualifyResultsInfo.Results {
		if q.CarIdx == i.DriverInfo.DriverCarIdx {
			return q, true
		}
	}
	return QualifyResult{}, false
}

// FieldSize returns how many cars were classified in a session, falling
// back to the count of non-spectator drivers before results exist.
// Position without field size is misleading: P5 of 6 is not P5 of 40.
func (i *Info) FieldSize(sessionNum int) int {
	if s, ok := i.SessionByNum(sessionNum); ok && len(s.ResultsPositions) > 0 {
		return len(s.ResultsPositions)
	}
	n := 0
	for _, d := range i.DriverInfo.Drivers {
		if d.IsSpectator == 0 {
			n++
		}
	}
	return n
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/sessionyaml/ -v`
Expected: PASS, thirteen tests.

- [ ] **Step 8: Commit**

```bash
git add internal/sessionyaml/
git commit -m "Add session YAML subset parser"
```

---

### Task 8: Session classifier

**Files:**
- Create: `internal/classify/classify.go`
- Test: `internal/classify/classify_test.go`

**Interfaces:**
- Consumes: Task 7's `sessionyaml.Info`, `sessionyaml.IsRaceType`.
- Produces:
  - `type SessionType string` with `TypePractice`, `TypeQualify`, `TypeRace`, `TypeWarmup`, `TypeTimeTrial`, `TypeOfflineTest`, `TypeUnknown`
  - `type EventContext string` with `ContextOfficialRace`, `ContextOfficialPractice`, `ContextLeague`, `ContextHosted`, `ContextOffline`, `ContextTimeTrial`, `ContextAI`, `ContextUnknown`
  - `type AIDetection string` with `AIDetectField`, `AIDetectHeuristic`, `AIDetectNone`
  - `type Result struct { SessionType SessionType; EventContext EventContext; AIOpponentCount int; AIDetection AIDetection; RawSessionType string }`
  - `func Classify(info *sessionyaml.Info, sessionNum int) Result`
  - `func NormaliseSessionType(raw string) SessionType`
  - `func Label(t SessionType, c EventContext) string`

This is the highest-risk logic in the application and gets the densest coverage. Rules are spec §6.1, §6.2, §6.3 and §6.5, reproduced here so the implementer needs no second document.

`session_type` normalisation:

| Raw YAML value (case-insensitive) | SessionType |
|---|---|
| `Practice`, `Open Practice`, `Lone Practice` | `Practice` |
| `Qualify`, `Open Qualify`, `Lone Qualify` | `Qualify` |
| `Race`, `Heat`, `Consolation` | `Race` |
| `Warmup` | `Warmup` |
| `Offline Testing`, `Testing` | `OfflineTest` |
| `Time Trial` | `TimeTrial` |
| anything else | `Unknown` |

`event_context`, first match wins:

1. `LeagueID != 0` → `League`
2. AI opponents present → `AI`
3. `SimMode != "full"` or `session_type == OfflineTest` → `Offline`
4. `session_type == TimeTrial` → `TimeTrial`
5. `Official == 1` and the weekend has a race session → `OfficialRace`
6. `Official == 1` → `OfficialPractice`
7. otherwise → `Hosted`

AI detection, per §6.5. The `CarIsAI` field is **unverified**, so:

- Field present on any driver → count flagged opponents, `AIDetection = "field"`.
- Field absent → heuristic: `session_type == Race` **and** `SubSessionID == 0` **and** `Official == 0` **and** `LeagueID == 0` **and** more than one driver. If it matches, set `AIOpponentCount` to `len(Drivers) - 1` and `AIDetection = "heuristic"`.
- Neither → `AIDetection = "none"`, count 0.

- [ ] **Step 1: Write the failing normalisation test**

Create `internal/classify/classify_test.go`:

```go
package classify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blezek/lapdog/internal/sessionyaml"
)

func TestNormaliseSessionType(t *testing.T) {
	cases := []struct {
		raw  string
		want SessionType
	}{
		{"Practice", TypePractice},
		{"Open Practice", TypePractice},
		{"Lone Practice", TypePractice},
		{"Qualify", TypeQualify},
		{"Open Qualify", TypeQualify},
		{"Lone Qualify", TypeQualify},
		{"Race", TypeRace},
		{"Heat", TypeRace},
		{"Consolation", TypeRace},
		{"Warmup", TypeWarmup},
		{"Offline Testing", TypeOfflineTest},
		{"Testing", TypeOfflineTest},
		{"Time Trial", TypeTimeTrial},
		// Case and whitespace must not matter.
		{"  open qualify  ", TypeQualify},
		{"RACE", TypeRace},
		// An unrecognised value must be Unknown, not silently a Practice.
		{"Sausage Festival", TypeUnknown},
		{"", TypeUnknown},
	}
	for _, c := range cases {
		if got := NormaliseSessionType(c.raw); got != c.want {
			t.Errorf("NormaliseSessionType(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/classify/ -v`
Expected: FAIL — build error, `undefined: NormaliseSessionType`.

- [ ] **Step 3: Write the classifier**

Create `internal/classify/classify.go`:

```go
// Package classify decides what kind of session the sim is running.
//
// Classify is a pure function with no I/O and no state, which is
// deliberate: it is the highest-risk logic in the application, and
// purity makes it exhaustively table-testable.
package classify

import (
	"strings"

	"github.com/blezek/lapdog/internal/sessionyaml"
)

// SessionType is what the driver is doing: practising, qualifying, racing.
type SessionType string

// SessionType values.
const (
	TypePractice    SessionType = "Practice"
	TypeQualify     SessionType = "Qualify"
	TypeRace        SessionType = "Race"
	TypeWarmup      SessionType = "Warmup"
	TypeTimeTrial   SessionType = "TimeTrial"
	TypeOfflineTest SessionType = "OfflineTest"
	TypeUnknown     SessionType = "Unknown"
)

// EventContext is what kind of event the session belongs to. It is
// orthogonal to SessionType, which is what lets "race practice" and
// "public practice" be derived rather than stored as labels.
type EventContext string

// EventContext values.
const (
	ContextOfficialRace     EventContext = "OfficialRace"
	ContextOfficialPractice EventContext = "OfficialPractice"
	ContextLeague           EventContext = "League"
	ContextHosted           EventContext = "Hosted"
	ContextOffline          EventContext = "Offline"
	ContextTimeTrial        EventContext = "TimeTrial"
	ContextAI               EventContext = "AI"
	ContextUnknown          EventContext = "Unknown"
)

// AIDetection records how AI opponents were identified, so that
// heuristically-classified sessions can be found and re-classified once
// the real field is confirmed. See spec section 6.5.
type AIDetection string

// AIDetection values.
const (
	AIDetectField     AIDetection = "field"
	AIDetectHeuristic AIDetection = "heuristic"
	AIDetectNone      AIDetection = "none"
)

// Result is the outcome of classifying one session.
type Result struct {
	SessionType     SessionType
	EventContext    EventContext
	AIOpponentCount int
	AIDetection     AIDetection

	// RawSessionType is the unnormalised YAML string, retained so an
	// Unknown classification can be diagnosed.
	RawSessionType string
}

// NormaliseSessionType maps a raw SessionType string from the session
// YAML onto a SessionType, returning TypeUnknown for anything
// unrecognised rather than guessing.
func NormaliseSessionType(raw string) SessionType {
	switch strings.ToLower(strings.Join(strings.Fields(raw), " ")) {
	case "practice", "open practice", "lone practice":
		return TypePractice
	case "qualify", "open qualify", "lone qualify":
		return TypeQualify
	case "race", "heat", "consolation":
		return TypeRace
	case "warmup":
		return TypeWarmup
	case "offline testing", "testing":
		return TypeOfflineTest
	case "time trial":
		return TypeTimeTrial
	default:
		return TypeUnknown
	}
}

// detectAI reports the AI opponent count and how it was determined.
func detectAI(info *sessionyaml.Info, st SessionType) (int, AIDetection) {
	if count, present := info.AIOpponentCount(); present {
		return count, AIDetectField
	}
	// Heuristic fallback, used only while the CarIsAI field is
	// unverified. It cannot tell an AI race from an offline hosted race
	// with no AI, and it deliberately misses AI practice sessions.
	// Both errors are corrected by reclassify once the field is known.
	w := info.WeekendInfo
	if st == TypeRace &&
		w.SubSessionID == 0 &&
		w.Official == 0 &&
		w.LeagueID == 0 &&
		len(info.DriverInfo.Drivers) > 1 {
		return len(info.DriverInfo.Drivers) - 1, AIDetectHeuristic
	}
	return 0, AIDetectNone
}

// Classify determines the session type and event context for the session
// with the given SessionNum.
func Classify(info *sessionyaml.Info, sessionNum int) Result {
	if info == nil {
		return Result{SessionType: TypeUnknown, EventContext: ContextUnknown, AIDetection: AIDetectNone}
	}

	raw := ""
	if s, ok := info.SessionByNum(sessionNum); ok {
		raw = s.SessionType
	}
	st := NormaliseSessionType(raw)

	aiCount, aiHow := detectAI(info, st)

	res := Result{
		SessionType:     st,
		AIOpponentCount: aiCount,
		AIDetection:     aiHow,
		RawSessionType:  raw,
	}
	res.EventContext = context(info, st, aiCount)
	return res
}

// context applies the ordered event-context rules. First match wins.
func context(info *sessionyaml.Info, st SessionType, aiCount int) EventContext {
	w := info.WeekendInfo

	// A league session never contains AI, so League winning is harmless
	// and keeps league accounting intact.
	if w.LeagueID != 0 {
		return ContextLeague
	}
	// AI is checked before Offline because an AI event is always
	// offline, and "raced against AI" is the more specific fact.
	if aiCount > 0 {
		return ContextAI
	}
	if (w.SimMode != "" && w.SimMode != "full") || st == TypeOfflineTest {
		return ContextOffline
	}
	if st == TypeTimeTrial {
		return ContextTimeTrial
	}
	if w.Official == 1 {
		if info.HasRaceSession() {
			return ContextOfficialRace
		}
		return ContextOfficialPractice
	}
	return ContextHosted
}

// Label renders the pair as the string the UI shows. Labels are computed,
// never stored, so the rules can change without a data migration.
func Label(t SessionType, c EventContext) string {
	if c == ContextOffline {
		return "Offline Testing"
	}
	if t == TypeTimeTrial {
		return "Time Trial"
	}

	var base string
	switch t {
	case TypePractice:
		base = "Practice"
	case TypeQualify:
		base = "Qualifying"
	case TypeRace:
		base = "Race"
	case TypeWarmup:
		base = "Warmup"
	default:
		base = "Unknown"
	}

	switch c {
	case ContextLeague:
		return "League " + base
	case ContextAI:
		return "AI " + base
	case ContextHosted:
		return "Hosted " + base
	case ContextOfficialPractice:
		if t == TypePractice {
			return "Public Practice"
		}
		return base
	case ContextOfficialRace:
		if t == TypePractice {
			return "Race Practice"
		}
		return base
	default:
		return base
	}
}
```

- [ ] **Step 4: Run the normalisation test to verify it passes**

Run: `go test ./internal/classify/ -run Normalise -v`
Expected: PASS

- [ ] **Step 5: Add the context and label tests**

Append to `internal/classify/classify_test.go`:

```go
// build assembles a minimal Info for context testing.
func build(sessionTypes []string, mutate func(*sessionyaml.Info)) *sessionyaml.Info {
	i := &sessionyaml.Info{}
	i.WeekendInfo.SimMode = "full"
	i.WeekendInfo.SubSessionID = 1234
	i.DriverInfo.DriverCarIdx = 0
	i.DriverInfo.Drivers = []sessionyaml.Driver{{CarIdx: 0, UserName: "Me"}}
	for n, st := range sessionTypes {
		i.SessionInfo.Sessions = append(i.SessionInfo.Sessions, sessionyaml.Session{
			SessionNum: n, SessionType: st,
		})
	}
	i.SessionInfo.NumSessions = len(sessionTypes)
	if mutate != nil {
		mutate(i)
	}
	return i
}

func aiFlag(v int) *int { return &v }

func TestClassifyEventContext(t *testing.T) {
	cases := []struct {
		name       string
		types      []string
		sessionNum int
		mutate     func(*sessionyaml.Info)
		wantType   SessionType
		wantCtx    EventContext
		wantLabel  string
	}{
		{
			name: "practice-only official weekend is public practice",
			types: []string{"Open Practice"}, sessionNum: 0,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.Official = 1 },
			wantType: TypePractice, wantCtx: ContextOfficialPractice,
			wantLabel: "Public Practice",
		},
		{
			name: "practice inside a race weekend is race practice",
			types: []string{"Practice", "Open Qualify", "Race"}, sessionNum: 0,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.Official = 1 },
			wantType: TypePractice, wantCtx: ContextOfficialRace,
			wantLabel: "Race Practice",
		},
		{
			name: "qualifying in a race weekend",
			types: []string{"Practice", "Open Qualify", "Race"}, sessionNum: 1,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.Official = 1 },
			wantType: TypeQualify, wantCtx: ContextOfficialRace,
			wantLabel: "Qualifying",
		},
		{
			name: "the race itself",
			types: []string{"Practice", "Open Qualify", "Race"}, sessionNum: 2,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.Official = 1 },
			wantType: TypeRace, wantCtx: ContextOfficialRace,
			wantLabel: "Race",
		},
		{
			name: "league race beats everything else",
			types: []string{"Race"}, sessionNum: 0,
			mutate: func(i *sessionyaml.Info) {
				i.WeekendInfo.LeagueID = 4242
				i.WeekendInfo.Official = 1
			},
			wantType: TypeRace, wantCtx: ContextLeague,
			wantLabel: "League Race",
		},
		{
			name: "league practice",
			types: []string{"Practice", "Race"}, sessionNum: 0,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.LeagueID = 4242 },
			wantType: TypePractice, wantCtx: ContextLeague,
			wantLabel: "League Practice",
		},
		{
			name: "offline testing",
			types: []string{"Offline Testing"}, sessionNum: 0,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.SubSessionID = 0 },
			wantType: TypeOfflineTest, wantCtx: ContextOffline,
			wantLabel: "Offline Testing",
		},
		{
			name: "unofficial non-league is hosted",
			types: []string{"Race"}, sessionNum: 0,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.Official = 0 },
			wantType: TypeRace, wantCtx: ContextHosted,
			wantLabel: "Hosted Race",
		},
		{
			name: "time trial",
			types: []string{"Time Trial"}, sessionNum: 0,
			wantType: TypeTimeTrial, wantCtx: ContextTimeTrial,
			wantLabel: "Time Trial",
		},
		{
			name: "AI race detected by field",
			types: []string{"Race"}, sessionNum: 0,
			mutate: func(i *sessionyaml.Info) {
				i.WeekendInfo.Official = 0
				i.DriverInfo.Drivers = []sessionyaml.Driver{
					{CarIdx: 0, UserName: "Me", CarIsAI: aiFlag(0)},
					{CarIdx: 1, UserName: "Bot", CarIsAI: aiFlag(1)},
				}
			},
			wantType: TypeRace, wantCtx: ContextAI,
			wantLabel: "AI Race",
		},
		{
			name: "AI practice detected by field",
			types: []string{"Practice", "Race"}, sessionNum: 0,
			mutate: func(i *sessionyaml.Info) {
				i.DriverInfo.Drivers = []sessionyaml.Driver{
					{CarIdx: 0, CarIsAI: aiFlag(0)},
					{CarIdx: 1, CarIsAI: aiFlag(1)},
				}
			},
			wantType: TypePractice, wantCtx: ContextAI,
			wantLabel: "AI Practice",
		},
		{
			name: "league wins over AI when both would match",
			types: []string{"Race"}, sessionNum: 0,
			mutate: func(i *sessionyaml.Info) {
				i.WeekendInfo.LeagueID = 99
				i.DriverInfo.Drivers = []sessionyaml.Driver{
					{CarIdx: 0, CarIsAI: aiFlag(0)},
					{CarIdx: 1, CarIsAI: aiFlag(1)},
				}
			},
			wantType: TypeRace, wantCtx: ContextLeague,
			wantLabel: "League Race",
		},
		{
			name: "unknown session type does not crash or guess",
			types: []string{"Sausage Festival"}, sessionNum: 0,
			mutate:   func(i *sessionyaml.Info) { i.WeekendInfo.Official = 1 },
			wantType: TypeUnknown, wantCtx: ContextOfficialPractice,
			wantLabel: "Unknown",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(build(c.types, c.mutate), c.sessionNum)
			if got.SessionType != c.wantType {
				t.Errorf("SessionType = %q, want %q", got.SessionType, c.wantType)
			}
			if got.EventContext != c.wantCtx {
				t.Errorf("EventContext = %q, want %q", got.EventContext, c.wantCtx)
			}
			if label := Label(got.SessionType, got.EventContext); label != c.wantLabel {
				t.Errorf("Label = %q, want %q", label, c.wantLabel)
			}
		})
	}
}

func TestClassifyAIDetectionHeuristic(t *testing.T) {
	// Offline race, unofficial, no league, several drivers, CarIsAI absent.
	i := build([]string{"Race"}, func(i *sessionyaml.Info) {
		i.WeekendInfo.SubSessionID = 0
		i.WeekendInfo.Official = 0
		i.DriverInfo.Drivers = []sessionyaml.Driver{
			{CarIdx: 0, UserName: "Me"},
			{CarIdx: 1, UserName: "Bot One"},
			{CarIdx: 2, UserName: "Bot Two"},
		}
	})
	got := Classify(i, 0)
	if got.AIDetection != AIDetectHeuristic {
		t.Errorf("AIDetection = %q, want %q", got.AIDetection, AIDetectHeuristic)
	}
	if got.AIOpponentCount != 2 {
		t.Errorf("AIOpponentCount = %d, want 2", got.AIOpponentCount)
	}
	if got.EventContext != ContextAI {
		t.Errorf("EventContext = %q, want %q", got.EventContext, ContextAI)
	}
}

func TestClassifyAIDetectionFieldWins(t *testing.T) {
	// The field is present and says no AI; the heuristic conditions would
	// otherwise fire. The field must win.
	i := build([]string{"Race"}, func(i *sessionyaml.Info) {
		i.WeekendInfo.SubSessionID = 0
		i.WeekendInfo.Official = 0
		i.DriverInfo.Drivers = []sessionyaml.Driver{
			{CarIdx: 0, CarIsAI: aiFlag(0)},
			{CarIdx: 1, CarIsAI: aiFlag(0)},
		}
	})
	got := Classify(i, 0)
	if got.AIDetection != AIDetectField {
		t.Errorf("AIDetection = %q, want %q", got.AIDetection, AIDetectField)
	}
	if got.AIOpponentCount != 0 {
		t.Errorf("AIOpponentCount = %d, want 0", got.AIOpponentCount)
	}
	if got.EventContext != ContextHosted {
		t.Errorf("EventContext = %q, want %q", got.EventContext, ContextHosted)
	}
}

func TestClassifyAIDetectionNoneForOnlineRace(t *testing.T) {
	i := build([]string{"Race"}, func(i *sessionyaml.Info) {
		i.WeekendInfo.SubSessionID = 998877
		i.WeekendInfo.Official = 1
		i.DriverInfo.Drivers = []sessionyaml.Driver{{CarIdx: 0}, {CarIdx: 1}}
	})
	got := Classify(i, 0)
	if got.AIDetection != AIDetectNone {
		t.Errorf("AIDetection = %q, want %q", got.AIDetection, AIDetectNone)
	}
	if got.EventContext != ContextOfficialRace {
		t.Errorf("EventContext = %q, want %q", got.EventContext, ContextOfficialRace)
	}
}

func TestClassifyNilInfo(t *testing.T) {
	got := Classify(nil, 0)
	if got.SessionType != TypeUnknown || got.EventContext != ContextUnknown {
		t.Errorf("Classify(nil) = %+v, want Unknown/Unknown", got)
	}
}

// A session number with no matching entry must not panic.
func TestClassifyMissingSessionNum(t *testing.T) {
	got := Classify(build([]string{"Race"}, nil), 99)
	if got.SessionType != TypeUnknown {
		t.Errorf("SessionType = %q, want Unknown", got.SessionType)
	}
}

// Classification must work against the real fixtures too, not only
// synthetic Info values.
func TestClassifyAgainstFixtures(t *testing.T) {
	read := func(name string) *sessionyaml.Info {
		b, err := os.ReadFile(filepath.Join("..", "sessionyaml", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		i, err := sessionyaml.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}

	got := Classify(read("practice_only.yaml"), 0)
	if got.SessionType != TypePractice || got.EventContext != ContextOfficialPractice {
		t.Errorf("practice_only = %+v, want Practice/OfficialPractice", got)
	}
	if l := Label(got.SessionType, got.EventContext); l != "Public Practice" {
		t.Errorf("label = %q, want Public Practice", l)
	}

	got = Classify(read("race_weekend.yaml"), 0)
	if got.EventContext != ContextOfficialRace {
		t.Errorf("race_weekend session 0 context = %q, want OfficialRace", got.EventContext)
	}
	if l := Label(got.SessionType, got.EventContext); l != "Race Practice" {
		t.Errorf("label = %q, want Race Practice", l)
	}

	got = Classify(read("race_weekend.yaml"), 2)
	if got.SessionType != TypeRace || got.EventContext != ContextOfficialRace {
		t.Errorf("race_weekend session 2 = %+v", got)
	}
}
```

- [ ] **Step 6: Run the full classify test suite**

Run: `go test ./internal/classify/ -v`
Expected: PASS. The `TestClassifyEventContext` subtests should each report individually.

- [ ] **Step 7: Verify the whole tree still passes**

Run: `make test`
Expected: PASS for all packages.

- [ ] **Step 8: Commit**

```bash
git add internal/classify/
git commit -m "Add session classifier with AI detection"
```

---

### Task 9: Configuration and data paths

**Files:**
- Create: `internal/config/config.go`, `internal/config/paths.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Config struct { PollIntervalSeconds, MinSessionSeconds float64; CaptureEnabled bool; CaptureMaxBytes int64; Port int; StartWithWindows bool; Units, Theme string }`
  - `func Default() Config`
  - `func (c Config) Validate() error`
  - `func (c *Config) Normalise()` — clamps out-of-range values to the nearest legal value
  - `func (c Config) PollInterval() time.Duration`
  - `func Load(path string) (Config, error)` — returns `Default()` if the file does not exist
  - `func Save(path string, c Config) error` — atomic write via temp file plus rename
  - `func DataDir() (string, error)`
  - `func ConfigPath(dir string) string`, `func DBPath(dir string) string`, `func CapturesDir(dir string) string`, `func LogPath(dir string) string`
  - `func CheckLocalFilesystem(dir string) error`
  - `var ErrNetworkPath error`
  - Constants: `MinPollSeconds = 0.25`, `MaxPollSeconds = 30.0`, `DefaultPort = 47047`, `DefaultCaptureMaxBytes = 2 << 30`

`Load` returning `Default()` for a missing file is deliberate: first run must not be an error. A *corrupt* file **is** an error, because silently reverting a user's settings is worse than refusing to start.

`CheckLocalFilesystem` exists because SQLite WAL requires a real local filesystem — the `-shm` file misbehaves on SMB shares and under file-sync tools. On Windows it rejects UNC paths (`\\server\share`) and mapped network drives; elsewhere it is a no-op, since development machines are local.

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	d := Default()
	if err := d.Validate(); err != nil {
		t.Fatalf("Default() is not valid: %v", err)
	}
	if d.PollIntervalSeconds != 1.0 {
		t.Errorf("PollIntervalSeconds = %v, want 1.0", d.PollIntervalSeconds)
	}
	if d.MinSessionSeconds != 30 {
		t.Errorf("MinSessionSeconds = %v, want 30", d.MinSessionSeconds)
	}
	if !d.CaptureEnabled {
		t.Error("CaptureEnabled = false, want true — capture defaults on")
	}
	if d.CaptureMaxBytes != DefaultCaptureMaxBytes {
		t.Errorf("CaptureMaxBytes = %d, want %d", d.CaptureMaxBytes, DefaultCaptureMaxBytes)
	}
	if d.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", d.Port, DefaultPort)
	}
	if !d.StartWithWindows {
		t.Error("StartWithWindows = false, want true")
	}
	if d.Units != "metric" || d.Theme != "system" {
		t.Errorf("Units=%q Theme=%q", d.Units, d.Theme)
	}
}

func TestPollInterval(t *testing.T) {
	c := Config{PollIntervalSeconds: 0.25}
	if got := c.PollInterval(); got != 250*time.Millisecond {
		t.Errorf("PollInterval() = %v, want 250ms", got)
	}
}

func TestValidateRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"poll below minimum", func(c *Config) { c.PollIntervalSeconds = 0.1 }},
		{"poll above maximum", func(c *Config) { c.PollIntervalSeconds = 31 }},
		{"negative min session", func(c *Config) { c.MinSessionSeconds = -1 }},
		{"negative capture cap", func(c *Config) { c.CaptureMaxBytes = -1 }},
		{"port zero", func(c *Config) { c.Port = 0 }},
		{"port too large", func(c *Config) { c.Port = 70000 }},
		{"unknown units", func(c *Config) { c.Units = "furlongs" }},
		{"unknown theme", func(c *Config) { c.Theme = "chartreuse" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mut(&c)
			if err := c.Validate(); err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

// Zero is the documented "unlimited" value for the capture cap.
func TestValidateAcceptsZeroCaptureCap(t *testing.T) {
	c := Default()
	c.CaptureMaxBytes = 0
	if err := c.Validate(); err != nil {
		t.Errorf("CaptureMaxBytes 0 must mean unlimited, got error: %v", err)
	}
}

func TestNormaliseClampsRatherThanFailing(t *testing.T) {
	c := Config{PollIntervalSeconds: 100, MinSessionSeconds: -5, Port: 0, CaptureMaxBytes: -3}
	c.Normalise()
	if c.PollIntervalSeconds != MaxPollSeconds {
		t.Errorf("PollIntervalSeconds = %v, want clamped to %v", c.PollIntervalSeconds, MaxPollSeconds)
	}
	if c.MinSessionSeconds != 0 {
		t.Errorf("MinSessionSeconds = %v, want clamped to 0", c.MinSessionSeconds)
	}
	if c.Port != DefaultPort {
		t.Errorf("Port = %d, want reset to %d", c.Port, DefaultPort)
	}
	if c.CaptureMaxBytes != 0 {
		t.Errorf("CaptureMaxBytes = %d, want clamped to 0", c.CaptureMaxBytes)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Normalise() must produce a valid config, got %v", err)
	}

	low := Config{PollIntervalSeconds: 0.001}
	low.Normalise()
	if math.Abs(low.PollIntervalSeconds-MinPollSeconds) > 1e-9 {
		t.Errorf("PollIntervalSeconds = %v, want clamped to %v", low.PollIntervalSeconds, MinPollSeconds)
	}
}

// A missing file is first run, not an error.
func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load on a missing file = %v, want nil", err)
	}
	if got != Default() {
		t.Errorf("Load = %+v, want Default()", got)
	}
}

// A corrupt file IS an error — silently reverting settings is worse.
func TestLoadCorruptFileIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("Load on a corrupt file = nil, want an error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	want := Default()
	want.PollIntervalSeconds = 2.5
	want.CaptureEnabled = false
	want.CaptureMaxBytes = 500 << 20
	want.Theme = "dark"

	if err := Save(p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// Save must not leave a truncated file behind if it is interrupted, so it
// writes to a temp file and renames. Verify no stray temp files remain.
func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := Save(p, Default()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v, want only config.json", names)
	}
}

// An out-of-range value in the file is clamped on load rather than
// rejected, so hand-editing cannot brick the application.
func TestLoadNormalisesOutOfRangeValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"pollIntervalSeconds": 999, "port": 47047, "units":"metric","theme":"system"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.PollIntervalSeconds != MaxPollSeconds {
		t.Errorf("PollIntervalSeconds = %v, want clamped to %v", got.PollIntervalSeconds, MaxPollSeconds)
	}
}

func TestPaths(t *testing.T) {
	dir := filepath.Join("C:", "Users", "test", "AppData", "Local", "lapdog")
	if got := ConfigPath(dir); got != filepath.Join(dir, "config.json") {
		t.Errorf("ConfigPath = %q", got)
	}
	if got := DBPath(dir); got != filepath.Join(dir, "lapdog.db") {
		t.Errorf("DBPath = %q", got)
	}
	if got := CapturesDir(dir); got != filepath.Join(dir, "captures") {
		t.Errorf("CapturesDir = %q", got)
	}
	if got := LogPath(dir); got != filepath.Join(dir, "lapdog.log") {
		t.Errorf("LogPath = %q", got)
	}
}

func TestDataDirIsAbsolute(t *testing.T) {
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("DataDir() = %q, want an absolute path", got)
	}
}

func TestCheckLocalFilesystemAcceptsTempDir(t *testing.T) {
	if err := CheckLocalFilesystem(t.TempDir()); err != nil {
		t.Errorf("CheckLocalFilesystem on a temp dir = %v, want nil", err)
	}
}

func TestCheckLocalFilesystemRejectsUNC(t *testing.T) {
	err := CheckLocalFilesystem(`\\fileserver\share\lapdog`)
	if !errors.Is(err, ErrNetworkPath) {
		t.Errorf("CheckLocalFilesystem on a UNC path = %v, want ErrNetworkPath", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — build error, `undefined: Default`, `undefined: Config`.

- [ ] **Step 3: Write the config type**

Create `internal/config/config.go`:

```go
// Package config loads and saves LapDog's user settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Poll interval bounds, in seconds. Below the minimum the collector
// burns CPU for no extra fidelity; above the maximum, time accounting
// and lap attribution get too coarse to be useful.
const (
	MinPollSeconds = 0.25
	MaxPollSeconds = 30.0
)

// DefaultPort is the fixed web UI port. The bind address is always
// loopback and is deliberately not configurable.
const DefaultPort = 47047

// DefaultCaptureMaxBytes is the default capture retention cap, 2 GiB.
// A value of 0 means unlimited.
const DefaultCaptureMaxBytes int64 = 2 << 30

// Config is the persisted user settings.
type Config struct {
	PollIntervalSeconds float64 `json:"pollIntervalSeconds"`
	MinSessionSeconds   float64 `json:"minSessionSeconds"`
	CaptureEnabled      bool    `json:"captureEnabled"`
	CaptureMaxBytes     int64   `json:"captureMaxBytes"`
	Port                int     `json:"port"`
	StartWithWindows    bool    `json:"startWithWindows"`
	Units               string  `json:"units"`
	Theme               string  `json:"theme"`
}

// Default returns the settings a fresh install starts with.
func Default() Config {
	return Config{
		PollIntervalSeconds: 1.0,
		MinSessionSeconds:   30,
		CaptureEnabled:      true,
		CaptureMaxBytes:     DefaultCaptureMaxBytes,
		Port:                DefaultPort,
		StartWithWindows:    true,
		Units:               "metric",
		Theme:               "system",
	}
}

// PollInterval returns PollIntervalSeconds as a duration.
func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalSeconds * float64(time.Second))
}

// Validate reports whether every field holds a legal value.
func (c Config) Validate() error {
	if c.PollIntervalSeconds < MinPollSeconds || c.PollIntervalSeconds > MaxPollSeconds {
		return fmt.Errorf("config: pollIntervalSeconds %v outside [%v, %v]",
			c.PollIntervalSeconds, MinPollSeconds, MaxPollSeconds)
	}
	if c.MinSessionSeconds < 0 {
		return fmt.Errorf("config: minSessionSeconds %v is negative", c.MinSessionSeconds)
	}
	if c.CaptureMaxBytes < 0 {
		return fmt.Errorf("config: captureMaxBytes %d is negative (0 means unlimited)", c.CaptureMaxBytes)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d outside [1, 65535]", c.Port)
	}
	switch c.Units {
	case "metric", "imperial":
	default:
		return fmt.Errorf("config: units %q must be metric or imperial", c.Units)
	}
	switch c.Theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("config: theme %q must be system, light or dark", c.Theme)
	}
	return nil
}

// Normalise clamps out-of-range values to the nearest legal value so a
// hand-edited file cannot prevent the application from starting.
func (c *Config) Normalise() {
	if c.PollIntervalSeconds < MinPollSeconds {
		c.PollIntervalSeconds = MinPollSeconds
	}
	if c.PollIntervalSeconds > MaxPollSeconds {
		c.PollIntervalSeconds = MaxPollSeconds
	}
	if c.MinSessionSeconds < 0 {
		c.MinSessionSeconds = 0
	}
	if c.CaptureMaxBytes < 0 {
		c.CaptureMaxBytes = 0
	}
	if c.Port < 1 || c.Port > 65535 {
		c.Port = DefaultPort
	}
	switch c.Units {
	case "metric", "imperial":
	default:
		c.Units = "metric"
	}
	switch c.Theme {
	case "system", "light", "dark":
	default:
		c.Theme = "system"
	}
}

// Load reads the config at path.
//
// A missing file returns Default with no error, because that is a first
// run rather than a fault. A file that exists but cannot be decoded IS
// an error: silently reverting a user's settings is worse than refusing
// to continue. Values that decode but are out of range are clamped.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	c := Default()
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	c.Normalise()
	return c, nil
}

// Save writes c to path atomically, so an interrupted write cannot leave
// a truncated config behind.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create directory: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: rename into place: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Write the path helpers**

Create `internal/config/paths.go`:

```go
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNetworkPath indicates the data directory is on a network
// filesystem, where SQLite WAL is unsafe.
var ErrNetworkPath = errors.New("config: data directory is on a network path")

// DataDir returns the directory holding the database, config, log and
// captures.
//
// On Windows this is %LOCALAPPDATA%\lapdog, which is deliberate:
// LOCALAPPDATA is not synced by OneDrive, and SQLite WAL requires a real
// local filesystem.
func DataDir() (string, error) {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "lapdog"), nil
		}
	}
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return filepath.Join(base, "lapdog"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: locate home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "lapdog"), nil
}

// ConfigPath returns the settings file path within dir.
func ConfigPath(dir string) string { return filepath.Join(dir, "config.json") }

// DBPath returns the SQLite database path within dir.
func DBPath(dir string) string { return filepath.Join(dir, "lapdog.db") }

// CapturesDir returns the capture directory within dir.
func CapturesDir(dir string) string { return filepath.Join(dir, "captures") }

// LogPath returns the log file path within dir.
func LogPath(dir string) string { return filepath.Join(dir, "lapdog.log") }

// CheckLocalFilesystem reports whether dir is safe to hold a WAL-mode
// SQLite database.
//
// The WAL shared-memory file misbehaves on SMB shares and under
// file-sync tools, so a network path must be refused loudly rather than
// producing intermittent corruption. On non-Windows platforms this is a
// no-op, since development machines are local.
func CheckLocalFilesystem(dir string) error {
	if runtime.GOOS != "windows" {
		if strings.HasPrefix(dir, `\\`) {
			return fmt.Errorf("%w: %s", ErrNetworkPath, dir)
		}
		return nil
	}
	if strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, "//") {
		return fmt.Errorf("%w: %s", ErrNetworkPath, dir)
	}
	if drive := filepath.VolumeName(dir); len(drive) == 2 && drive[1] == ':' {
		if remote, err := isRemoteDrive(drive); err == nil && remote {
			return fmt.Errorf("%w: %s is a mapped network drive", ErrNetworkPath, drive)
		}
	}
	return nil
}
```

- [ ] **Step 5: Add the Windows drive-type probe and its stub**

Create `internal/config/drive_windows.go`:

```go
//go:build windows

package config

import "golang.org/x/sys/windows"

// driveRemote is DRIVE_REMOTE from the Win32 GetDriveType API.
const driveRemote = 4

// isRemoteDrive reports whether a drive letter such as "Z:" refers to a
// mapped network drive.
func isRemoteDrive(drive string) (bool, error) {
	root, err := windows.UTF16PtrFromString(drive + `\`)
	if err != nil {
		return false, err
	}
	return windows.GetDriveType(root) == driveRemote, nil
}
```

Create `internal/config/drive_other.go`:

```go
//go:build !windows

package config

// isRemoteDrive is a no-op off Windows, where drive letters do not exist.
func isRemoteDrive(string) (bool, error) { return false, nil }
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Verify the Windows build still compiles**

Run: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./internal/config/`
Expected: no output, exit 0. This is the first task with build-tagged files, so confirming both sides compile matters.

- [ ] **Step 8: Commit**

```bash
git add internal/config/
git commit -m "Add configuration loading and data path resolution"
```

---

### Task 10: Store — schema, migrations, WAL and the writer/reader split

**Files:**
- Create: `internal/store/migrations/0001_init.sql`, `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Store struct { ... }`
  - `func Open(path string) (*Store, error)`
  - `func (s *Store) Close() error`
  - `func (s *Store) Writer() *sql.DB` — `MaxOpenConns(1)`
  - `func (s *Store) Reader() *sql.DB` — pooled
  - `func (s *Store) SchemaVersion() (int, error)`
  - `func (s *Store) Path() string`
  - `var ErrSchemaTooNew error`
  - `const CurrentSchemaVersion = 1`

The writer/reader split is the whole point of this task. SQLite permits one writer and many concurrent readers in WAL mode. Serialising every write through a single connection owned by the collector means `SQLITE_BUSY` cannot occur by construction, rather than being retried away.

Driver import is `_ "modernc.org/sqlite"`, driver name `"sqlite"`. Pragmas are passed in the DSN.

- [ ] **Step 1: Write the migration**

Create `internal/store/migrations/0001_init.sql`. This is spec §11 verbatim.

```sql
CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version (version) VALUES (1);

CREATE TABLE sessions (
  id                     INTEGER PRIMARY KEY,
  uuid                   TEXT    NOT NULL UNIQUE,
  session_key            TEXT    NOT NULL UNIQUE,
  subsession_id          INTEGER NOT NULL DEFAULT 0,
  session_num            INTEGER NOT NULL,
  session_type           TEXT    NOT NULL,
  event_context          TEXT    NOT NULL,
  league_id              INTEGER NOT NULL DEFAULT 0,
  series_id              INTEGER NOT NULL DEFAULT 0,
  season_id              INTEGER NOT NULL DEFAULT 0,
  official               INTEGER NOT NULL DEFAULT 0,
  track_id               INTEGER,
  track_name             TEXT,
  track_config           TEXT,
  track_length_km        REAL,
  car_id                 INTEGER,
  car_name               TEXT,
  car_class_id           INTEGER,
  car_class_name         TEXT,
  started_at             TEXT    NOT NULL,
  ended_at               TEXT,
  connected_seconds      REAL    NOT NULL DEFAULT 0,
  in_car_seconds         REAL    NOT NULL DEFAULT 0,
  driving_seconds        REAL    NOT NULL DEFAULT 0,
  laps_completed         INTEGER NOT NULL DEFAULT 0,
  incidents              INTEGER NOT NULL DEFAULT 0,
  best_lap_time_s        REAL,
  starting_position      INTEGER,
  finish_position        INTEGER,
  finish_class_position  INTEGER,
  qualify_position       INTEGER,
  qualify_class_position INTEGER,
  qualify_best_time_s    REAL,
  field_size             INTEGER,
  ai_opponent_count      INTEGER NOT NULL DEFAULT 0,
  ai_detection           TEXT,
  incident_source        TEXT    NOT NULL DEFAULT 'yaml',
  classify_source_json   TEXT    NOT NULL,
  capture_file           TEXT,
  created_at             TEXT    NOT NULL,
  updated_at             TEXT    NOT NULL,
  uploaded_at            TEXT
);

CREATE INDEX idx_sessions_started  ON sessions(started_at);
CREATE INDEX idx_sessions_type_ctx ON sessions(session_type, event_context);
CREATE INDEX idx_sessions_track    ON sessions(track_id);
CREATE INDEX idx_sessions_car      ON sessions(car_id);
CREATE INDEX idx_sessions_upload   ON sessions(uploaded_at);
CREATE INDEX idx_sessions_ai       ON sessions(ai_detection);

CREATE TABLE laps (
  id               INTEGER PRIMARY KEY,
  uuid             TEXT    NOT NULL UNIQUE,
  session_id       INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  lap_number       INTEGER NOT NULL,
  lap_time_s       REAL,
  delta_to_best_s  REAL,
  fuel_used_l      REAL,
  fuel_level_end_l REAL,
  incidents_on_lap INTEGER NOT NULL DEFAULT 0,
  is_pit_lap       INTEGER NOT NULL DEFAULT 0,
  position         INTEGER,
  class_position   INTEGER,
  recorded_at      TEXT    NOT NULL,
  uploaded_at      TEXT,
  UNIQUE(session_id, lap_number)
);

CREATE INDEX idx_laps_session ON laps(session_id, lap_number);
CREATE INDEX idx_laps_time    ON laps(lap_time_s);

CREATE TABLE position_events (
  id               INTEGER PRIMARY KEY,
  uuid             TEXT    NOT NULL UNIQUE,
  session_id       INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  lap_number       INTEGER NOT NULL,
  session_time_s   REAL    NOT NULL,
  from_position    INTEGER NOT NULL,
  to_position      INTEGER NOT NULL,
  is_class         INTEGER NOT NULL DEFAULT 0,
  opponent_car_idx INTEGER,
  opponent_name    TEXT,
  cause            TEXT    NOT NULL,
  recorded_at      TEXT    NOT NULL,
  uploaded_at      TEXT
);

CREATE INDEX idx_pos_session ON position_events(session_id, lap_number);
CREATE INDEX idx_pos_cause   ON position_events(cause);
```

- [ ] **Step 2: Write the failing test**

Create `internal/store/store_test.go`:

```go
package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// openTemp opens a store in a temp directory and closes it on cleanup.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := openTemp(t)
	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", v, CurrentSchemaVersion)
	}
}

func TestOpenCreatesAllTables(t *testing.T) {
	s := openTemp(t)
	for _, table := range []string{"schema_version", "sessions", "laps", "position_events"} {
		var name string
		err := s.Reader().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestWALIsEnabled(t *testing.T) {
	s := openTemp(t)
	var mode string
	if err := s.Reader().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	s := openTemp(t)
	// Inserting a lap against a non-existent session must fail, otherwise
	// ON DELETE CASCADE is decorative.
	_, err := s.Writer().Exec(
		`INSERT INTO laps (uuid, session_id, lap_number, recorded_at) VALUES ('u1', 9999, 1, '2026-01-01T00:00:00Z')`,
	)
	if err == nil {
		t.Fatal("insert with a dangling session_id succeeded; foreign keys are not enforced")
	}
}

// The writer must be limited to a single connection: that is what makes
// SQLITE_BUSY impossible rather than merely unlikely.
func TestWriterIsSingleConnection(t *testing.T) {
	s := openTemp(t)
	if got := s.Writer().Stats().MaxOpenConnections; got != 1 {
		t.Errorf("writer MaxOpenConnections = %d, want 1", got)
	}
}

func TestReaderAllowsConcurrentReads(t *testing.T) {
	s := openTemp(t)
	if got := s.Reader().Stats().MaxOpenConnections; got <= 1 {
		t.Errorf("reader MaxOpenConnections = %d, want more than 1", got)
	}
}

// Readers must not block while a write is in flight. In WAL mode they
// read the last committed snapshot instead.
func TestConcurrentWriteAndReadDoNotDeadlock(t *testing.T) {
	s := openTemp(t)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, err := s.Writer().Exec(
				`INSERT INTO sessions
				  (uuid, session_key, session_num, session_type, event_context,
				   started_at, classify_source_json, created_at, updated_at)
				 VALUES (?, ?, 0, 'Practice', 'Hosted', '2026-01-01T00:00:00Z', '{}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
				uuidish(i), keyish(i),
			)
			if err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			var n int
			if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
				t.Errorf("read %d: %v", i, err)
				return
			}
		}
	}()

	wg.Wait()

	var n int
	if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Errorf("session count = %d, want 50", n)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open on an existing database: %v", err)
	}
	defer s2.Close()
	v, err := s2.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("SchemaVersion after reopen = %d, want %d", v, CurrentSchemaVersion)
	}
}

// A database written by a newer build must be refused, not silently used.
func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Writer().Exec(`UPDATE schema_version SET version = ?`, CurrentSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Open(path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open on a newer schema = %v, want ErrSchemaTooNew", err)
	}
}

func TestPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Path() != path {
		t.Errorf("Path() = %q, want %q", s.Path(), path)
	}
}

func uuidish(i int) string { return "uuid-" + itoa(i) }
func keyish(i int) string  { return "key-" + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -v`
Expected: FAIL — build error, `undefined: Open`, `undefined: Store`.

- [ ] **Step 4: Write the store**

Create `internal/store/store.go`:

```go
// Package store persists sessions, laps and position events to SQLite.
//
// Concurrency model: SQLite in WAL mode permits one writer and many
// concurrent readers. This package therefore exposes two *sql.DB
// handles. The writer is capped at a single connection and is owned by
// the collector, which means two writes can never race and SQLITE_BUSY
// cannot occur by construction rather than being retried away. The
// reader is a pool used by the HTTP API; in WAL mode readers see the
// last committed snapshot and are never blocked by the writer.
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// CurrentSchemaVersion is the schema this build understands.
const CurrentSchemaVersion = 1

// ErrSchemaTooNew indicates the database was written by a newer build.
// Downgrade is not supported, so this is refused rather than risked.
var ErrSchemaTooNew = errors.New("store: database schema is newer than this build")

// Store owns the database connections.
type Store struct {
	path   string
	writer *sql.DB
	reader *sql.DB
}

// dsn builds a connection string with the pragmas LapDog depends on.
//
// busy_timeout is set even though single-writer discipline should make
// it unnecessary: if it ever fires, that is a genuine anomaly worth
// surviving rather than crashing on.
func dsn(path string, readOnly bool) string {
	s := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)"
	if readOnly {
		s += "&mode=ro"
	}
	return s
}

// Open opens or creates the database at path and applies any pending
// migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: create directory: %w", err)
	}

	writer, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("store: open writer: %w", err)
	}
	// Exactly one writer connection. This is the core of the concurrency
	// model, not a tuning knob.
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	if err := writer.Ping(); err != nil {
		writer.Close()
		return nil, fmt.Errorf("store: ping writer: %w", err)
	}

	s := &Store{path: path, writer: writer}
	if err := s.migrate(); err != nil {
		writer.Close()
		return nil, err
	}

	// The reader pool is opened after migration so it never observes a
	// half-built schema.
	reader, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("store: open reader: %w", err)
	}
	reader.SetMaxOpenConns(4)
	reader.SetMaxIdleConns(4)
	if err := reader.Ping(); err != nil {
		writer.Close()
		reader.Close()
		return nil, fmt.Errorf("store: ping reader: %w", err)
	}
	s.reader = reader
	return s, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Writer returns the single-connection write handle.
func (s *Store) Writer() *sql.DB { return s.writer }

// Reader returns the pooled read handle.
func (s *Store) Reader() *sql.DB { return s.reader }

// Close releases both connection pools.
func (s *Store) Close() error {
	var firstErr error
	if s.reader != nil {
		if err := s.reader.Close(); err != nil {
			firstErr = err
		}
	}
	if s.writer != nil {
		if err := s.writer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SchemaVersion returns the schema version recorded in the database, or
// 0 if the schema_version table does not exist yet.
func (s *Store) SchemaVersion() (int, error) {
	var name string
	err := s.writer.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'`,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: probe schema_version: %w", err)
	}
	var v int
	if err := s.writer.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}

// migrate applies every migration newer than the recorded version, each
// inside its own transaction. Downgrade is not supported.
func (s *Store) migrate() error {
	have, err := s.SchemaVersion()
	if err != nil {
		return err
	}
	if have > CurrentSchemaVersion {
		return fmt.Errorf("%w: database is version %d, this build understands %d",
			ErrSchemaTooNew, have, CurrentSchemaVersion)
	}
	if have == CurrentSchemaVersion {
		return nil
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}
	for i, name := range names {
		version := i + 1
		if version <= have {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		tx, err := s.writer.Begin()
		if err != nil {
			return fmt.Errorf("store: begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		// 0001 seeds schema_version itself; later migrations update it.
		if version > 1 {
			if _, err := tx.Exec(`UPDATE schema_version SET version = ?`, version); err != nil {
				tx.Rollback()
				return fmt.Errorf("store: record version %d: %w", version, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", name, err)
		}
	}
	return nil
}

// migrationNames returns the embedded migration filenames in apply order.
func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: list migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS, ten tests. `TestConcurrentWriteAndReadDoNotDeadlock` is the important one — if it hangs rather than fails, WAL is not active.

- [ ] **Step 6: Verify no cgo and that Windows still builds**

```bash
CGO_ENABLED=0 go build ./internal/store/
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./internal/store/
```

Expected: no output from either, exit 0. If the second fails, a cgo-requiring dependency has crept in and the driver choice needs revisiting.

- [ ] **Step 7: Run the whole suite with the race detector**

Run: `go test -race ./...`
Expected: PASS with no race reports. This is the first task with concurrent access, so it is worth checking here rather than at the end.

- [ ] **Step 8: Commit**

```bash
git add internal/store/
git commit -m "Add SQLite store with WAL, migrations and writer/reader split"
```

---

### Task 11: Store — session upsert and session_key identity

**Files:**
- Create: `internal/store/sessions.go`
- Test: `internal/store/sessions_test.go`

**Interfaces:**
- Consumes: Task 10's `Store`.
- Produces:
  - `type Session struct { ... }` — one field per `sessions` column, nullable columns as pointers
  - `func SessionKey(subsessionID, sessionNum int, startedAt time.Time) string`
  - `func (s *Store) UpsertSession(rec *Session) (int64, error)` — sets `rec.ID` and returns it
  - `func (s *Store) SessionByKey(key string) (*Session, error)`
  - `func (s *Store) SessionByID(id int64) (*Session, error)`
  - `func (s *Store) DeleteSession(id int64) error`
  - `var ErrNotFound error`
  - `func Now() string` and `func FormatTime(t time.Time) string` — RFC3339 UTC

The identity rule matters and is easy to get wrong. Offline sessions report `SubSessionID = 0`, so `(subsession_id, session_num)` is **not** unique across them — two offline test sessions on the same day would collide and the second would overwrite the first. Hence:

- `subsession_id != 0` → `"<subsession_id>/<session_num>"`
- `subsession_id == 0` → `"offline/<session_num>/<started_at RFC3339>"`

`UpsertSession` must **preserve** `id`, `uuid` and `created_at` on update while overwriting the counters and results. It generates a UUID on first insert only. This is what lets the collector flush the same session every ten seconds without churning identity.

Nullable columns are pointers rather than zero values because "finished P0" and "did not finish" are different facts, and a practice session legitimately has no finish position at all.

- [ ] **Step 1: Write the failing key-derivation test**

Create `internal/store/sessions_test.go`:

```go
package store

import (
	"errors"
	"testing"
	"time"
)

func TestSessionKeyOnline(t *testing.T) {
	at := time.Date(2026, 8, 4, 19, 30, 0, 0, time.UTC)
	if got := SessionKey(55667788, 2, at); got != "55667788/2" {
		t.Errorf("SessionKey = %q, want %q", got, "55667788/2")
	}
}

// Offline sessions all report SubSessionID 0, so the key must include the
// start time or two offline tests would collide and overwrite each other.
func TestSessionKeyOfflineIncludesStartTime(t *testing.T) {
	a := time.Date(2026, 8, 4, 19, 30, 0, 0, time.UTC)
	b := time.Date(2026, 8, 4, 21, 15, 0, 0, time.UTC)

	ka := SessionKey(0, 0, a)
	kb := SessionKey(0, 0, b)
	if ka == kb {
		t.Fatalf("two offline sessions produced the same key %q", ka)
	}
	if ka != "offline/0/2026-08-04T19:30:00Z" {
		t.Errorf("SessionKey = %q", ka)
	}
}

// The key must be stable for the same inputs, since it is the upsert target.
func TestSessionKeyIsStable(t *testing.T) {
	at := time.Date(2026, 8, 4, 19, 30, 0, 0, time.UTC)
	if SessionKey(0, 1, at) != SessionKey(0, 1, at) {
		t.Error("SessionKey is not deterministic")
	}
}

// Local time in must produce UTC out, or the same session keyed from two
// timezones would produce two rows.
func TestSessionKeyNormalisesToUTC(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*3600)
	local := time.Date(2026, 8, 4, 14, 30, 0, 0, loc)
	utc := local.UTC()
	if SessionKey(0, 0, local) != SessionKey(0, 0, utc) {
		t.Error("SessionKey depends on the input timezone; it must normalise to UTC")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run SessionKey -v`
Expected: FAIL — `undefined: SessionKey`.

- [ ] **Step 3: Write the session type and key derivation**

Create `internal/store/sessions.go`:

```go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound indicates no row matched the lookup.
var ErrNotFound = errors.New("store: not found")

// FormatTime renders t as an RFC3339 string in UTC, which is the only
// timestamp format stored in the database.
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Now returns the current time in the stored format.
func Now() string { return FormatTime(time.Now()) }

// Session is one row of the sessions table.
//
// Nullable columns are pointers because absent and zero mean different
// things: a practice session has no finish position at all, which is not
// the same as finishing in position zero.
type Session struct {
	ID           int64
	UUID         string
	SessionKey   string
	SubsessionID int
	SessionNum   int
	SessionType  string
	EventContext string

	LeagueID int
	SeriesID int
	SeasonID int
	Official int

	TrackID       *int
	TrackName     *string
	TrackConfig   *string
	TrackLengthKm *float64

	CarID        *int
	CarName      *string
	CarClassID   *int
	CarClassName *string

	StartedAt string
	EndedAt   *string

	ConnectedSeconds float64
	InCarSeconds     float64
	DrivingSeconds   float64

	LapsCompleted int
	Incidents     int
	BestLapTimeS  *float64

	StartingPosition     *int
	FinishPosition       *int
	FinishClassPosition  *int
	QualifyPosition      *int
	QualifyClassPosition *int
	QualifyBestTimeS     *float64
	FieldSize            *int

	AIOpponentCount int
	AIDetection     *string
	IncidentSource  string

	ClassifySourceJSON string
	CaptureFile        *string

	CreatedAt  string
	UpdatedAt  string
	UploadedAt *string
}

// SessionKey derives the stable identity of a session segment.
//
// Online sessions are identified by subsession and session number. Offline
// sessions all report SubSessionID 0, so that pair is not unique among
// them and the start time is folded in — otherwise two offline test
// sessions would collide and the second would overwrite the first.
func SessionKey(subsessionID, sessionNum int, startedAt time.Time) string {
	if subsessionID != 0 {
		return strconv.Itoa(subsessionID) + "/" + strconv.Itoa(sessionNum)
	}
	return "offline/" + strconv.Itoa(sessionNum) + "/" + FormatTime(startedAt)
}
```

- [ ] **Step 4: Run the key test to verify it passes**

Run: `go test ./internal/store/ -run SessionKey -v`
Expected: PASS, four tests.

- [ ] **Step 5: Write the failing upsert test**

Append to `internal/store/sessions_test.go`:

```go
// minimalSession returns a Session with only the NOT NULL columns set.
func minimalSession(key string) *Session {
	return &Session{
		SessionKey:         key,
		SubsessionID:       55667788,
		SessionNum:         2,
		SessionType:        "Race",
		EventContext:       "OfficialRace",
		StartedAt:          "2026-08-04T19:30:00Z",
		ClassifySourceJSON: `{"WeekendInfo":{"LeagueID":0}}`,
		IncidentSource:     "yaml",
	}
}

func intp(v int) *int          { return &v }
func f64p(v float64) *float64  { return &v }
func strp(v string) *string    { return &v }

func TestUpsertSessionInsertsAndAssignsIdentity(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")

	id, err := s.UpsertSession(rec)
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if id == 0 {
		t.Fatal("UpsertSession returned id 0")
	}
	if rec.ID != id {
		t.Errorf("rec.ID = %d, want %d — UpsertSession must write the id back", rec.ID, id)
	}
	if rec.UUID == "" {
		t.Error("rec.UUID is empty; a UUID must be generated on first insert")
	}
	if rec.CreatedAt == "" || rec.UpdatedAt == "" {
		t.Errorf("timestamps not set: created=%q updated=%q", rec.CreatedAt, rec.UpdatedAt)
	}
}

// The collector flushes the same session every ten seconds. Identity must
// survive that, and the counters must be overwritten.
func TestUpsertSessionUpdatePreservesIdentity(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	firstID, firstUUID, firstCreated := rec.ID, rec.UUID, rec.CreatedAt

	rec.ConnectedSeconds = 1200
	rec.InCarSeconds = 900
	rec.DrivingSeconds = 840
	rec.LapsCompleted = 24
	rec.Incidents = 6
	rec.FinishPosition = intp(4)
	rec.BestLapTimeS = f64p(141.882)
	rec.EndedAt = strp("2026-08-04T20:18:00Z")

	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatalf("second UpsertSession: %v", err)
	}
	if rec.ID != firstID {
		t.Errorf("ID changed on update: %d -> %d", firstID, rec.ID)
	}
	if rec.UUID != firstUUID {
		t.Errorf("UUID changed on update: %q -> %q", firstUUID, rec.UUID)
	}
	if rec.CreatedAt != firstCreated {
		t.Errorf("CreatedAt changed on update: %q -> %q", firstCreated, rec.CreatedAt)
	}

	got, err := s.SessionByKey("55667788/2")
	if err != nil {
		t.Fatalf("SessionByKey: %v", err)
	}
	if got.DrivingSeconds != 840 || got.LapsCompleted != 24 || got.Incidents != 6 {
		t.Errorf("counters not updated: %+v", got)
	}
	if got.FinishPosition == nil || *got.FinishPosition != 4 {
		t.Errorf("FinishPosition = %v, want 4", got.FinishPosition)
	}
	if got.EndedAt == nil || *got.EndedAt != "2026-08-04T20:18:00Z" {
		t.Errorf("EndedAt = %v", got.EndedAt)
	}

	// Exactly one row: the upsert must not have inserted a duplicate.
	var n int
	if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("session count = %d, want 1", n)
	}
}

func TestUpsertSessionRoundTripsAllFields(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	rec.LeagueID = 4242
	rec.SeriesID = 411
	rec.SeasonID = 4703
	rec.Official = 1
	rec.TrackID = intp(341)
	rec.TrackName = strp("Circuit de Spa-Francorchamps")
	rec.TrackConfig = strp("Grand Prix Pits")
	rec.TrackLengthKm = f64p(7.0)
	rec.CarID = intp(173)
	rec.CarName = strp("Porsche 911 GT3 R")
	rec.CarClassID = intp(2523)
	rec.CarClassName = strp("GT3")
	rec.StartingPosition = intp(6)
	rec.FinishClassPosition = intp(3)
	rec.QualifyPosition = intp(6)
	rec.QualifyClassPosition = intp(5)
	rec.QualifyBestTimeS = f64p(140.912)
	rec.FieldSize = intp(40)
	rec.AIOpponentCount = 0
	rec.AIDetection = strp("none")
	rec.CaptureFile = strp("2026-08-04T193000Z-55667788-2.lpd")

	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionByID(rec.ID)
	if err != nil {
		t.Fatalf("SessionByID: %v", err)
	}
	if got.LeagueID != 4242 || got.Official != 1 {
		t.Errorf("context = %+v", got)
	}
	if got.TrackName == nil || *got.TrackName != "Circuit de Spa-Francorchamps" {
		t.Errorf("TrackName = %v", got.TrackName)
	}
	if got.CarName == nil || *got.CarName != "Porsche 911 GT3 R" {
		t.Errorf("CarName = %v", got.CarName)
	}
	if got.QualifyPosition == nil || *got.QualifyPosition != 6 {
		t.Errorf("QualifyPosition = %v", got.QualifyPosition)
	}
	if got.QualifyBestTimeS == nil || *got.QualifyBestTimeS != 140.912 {
		t.Errorf("QualifyBestTimeS = %v", got.QualifyBestTimeS)
	}
	if got.FieldSize == nil || *got.FieldSize != 40 {
		t.Errorf("FieldSize = %v", got.FieldSize)
	}
	if got.AIDetection == nil || *got.AIDetection != "none" {
		t.Errorf("AIDetection = %v", got.AIDetection)
	}
	if got.CaptureFile == nil {
		t.Error("CaptureFile is nil")
	}
	if got.ClassifySourceJSON != rec.ClassifySourceJSON {
		t.Errorf("ClassifySourceJSON = %q", got.ClassifySourceJSON)
	}
}

// Nullable columns must come back nil, not zero, when never set.
func TestUpsertSessionNullsStayNull(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionByID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FinishPosition != nil {
		t.Errorf("FinishPosition = %v, want nil for a session with no result", got.FinishPosition)
	}
	if got.EndedAt != nil {
		t.Errorf("EndedAt = %v, want nil for an in-progress session", got.EndedAt)
	}
	if got.UploadedAt != nil {
		t.Errorf("UploadedAt = %v, want nil — nothing writes it in this version", got.UploadedAt)
	}
	if got.BestLapTimeS != nil {
		t.Errorf("BestLapTimeS = %v, want nil", got.BestLapTimeS)
	}
}

// Two offline sessions must produce two rows, not one overwritten row.
func TestUpsertSessionOfflineSessionsDoNotCollide(t *testing.T) {
	s := openTemp(t)
	a := time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)
	b := time.Date(2026, 8, 4, 21, 0, 0, 0, time.UTC)

	for _, at := range []time.Time{a, b} {
		rec := minimalSession(SessionKey(0, 0, at))
		rec.SubsessionID = 0
		rec.SessionNum = 0
		rec.SessionType = "OfflineTest"
		rec.EventContext = "Offline"
		rec.StartedAt = FormatTime(at)
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatalf("UpsertSession(%v): %v", at, err)
		}
	}

	var n int
	if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("session count = %d, want 2 — offline sessions collided", n)
	}
}

func TestSessionByKeyNotFound(t *testing.T) {
	s := openTemp(t)
	if _, err := s.SessionByKey("nope/0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionByKey on a missing key = %v, want ErrNotFound", err)
	}
}

func TestSessionByIDNotFound(t *testing.T) {
	s := openTemp(t)
	if _, err := s.SessionByID(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionByID on a missing id = %v, want ErrNotFound", err)
	}
}

func TestDeleteSessionCascades(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Writer().Exec(
		`INSERT INTO laps (uuid, session_id, lap_number, recorded_at) VALUES ('lap-uuid', ?, 1, '2026-08-04T19:35:00Z')`,
		rec.ID,
	); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(rec.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	var n int
	if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM laps WHERE session_id = ?`, rec.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("lap count after delete = %d, want 0 — cascade did not fire", n)
	}
}

func TestDeleteSessionNotFound(t *testing.T) {
	s := openTemp(t)
	if err := s.DeleteSession(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteSession on a missing id = %v, want ErrNotFound", err)
	}
}

func TestUpdatedAtAdvancesOnUpdate(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	// updated_at is written by the store, so force a distinguishable value
	// to prove the update path rewrites it.
	if _, err := s.Writer().Exec(`UPDATE sessions SET updated_at = '2000-01-01T00:00:00Z' WHERE id = ?`, rec.ID); err != nil {
		t.Fatal(err)
	}
	rec.DrivingSeconds = 10
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionByID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdatedAt == "2000-01-01T00:00:00Z" {
		t.Error("updated_at was not advanced on update; it is the sync cursor")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'Upsert|SessionBy|DeleteSession|UpdatedAt' -v`
Expected: FAIL — `undefined: (*Store).UpsertSession`.

- [ ] **Step 7: Write the upsert and readers**

Append to `internal/store/sessions.go`:

```go
// sessionColumns is the column list used by every session read, in the
// order scanSession expects.
const sessionColumns = `
	id, uuid, session_key, subsession_id, session_num, session_type, event_context,
	league_id, series_id, season_id, official,
	track_id, track_name, track_config, track_length_km,
	car_id, car_name, car_class_id, car_class_name,
	started_at, ended_at,
	connected_seconds, in_car_seconds, driving_seconds,
	laps_completed, incidents, best_lap_time_s,
	starting_position, finish_position, finish_class_position,
	qualify_position, qualify_class_position, qualify_best_time_s, field_size,
	ai_opponent_count, ai_detection, incident_source,
	classify_source_json, capture_file,
	created_at, updated_at, uploaded_at`

// UpsertSession inserts rec, or updates it if a row with the same
// session_key already exists.
//
// On update, id, uuid and created_at are preserved and everything else is
// overwritten. That is what lets the collector flush the same session
// every few seconds without churning its identity.
func (s *Store) UpsertSession(rec *Session) (int64, error) {
	if rec == nil {
		return 0, errors.New("store: UpsertSession called with nil record")
	}
	if rec.SessionKey == "" {
		return 0, errors.New("store: UpsertSession requires a SessionKey")
	}

	existing, err := s.SessionByKey(rec.SessionKey)
	switch {
	case err == nil:
		rec.ID = existing.ID
		rec.UUID = existing.UUID
		rec.CreatedAt = existing.CreatedAt
	case errors.Is(err, ErrNotFound):
		if rec.UUID == "" {
			rec.UUID = uuid.NewString()
		}
		if rec.CreatedAt == "" {
			rec.CreatedAt = Now()
		}
	default:
		return 0, err
	}
	rec.UpdatedAt = Now()
	if rec.IncidentSource == "" {
		rec.IncidentSource = "yaml"
	}

	const q = `
INSERT INTO sessions (
	uuid, session_key, subsession_id, session_num, session_type, event_context,
	league_id, series_id, season_id, official,
	track_id, track_name, track_config, track_length_km,
	car_id, car_name, car_class_id, car_class_name,
	started_at, ended_at,
	connected_seconds, in_car_seconds, driving_seconds,
	laps_completed, incidents, best_lap_time_s,
	starting_position, finish_position, finish_class_position,
	qualify_position, qualify_class_position, qualify_best_time_s, field_size,
	ai_opponent_count, ai_detection, incident_source,
	classify_source_json, capture_file,
	created_at, updated_at
) VALUES (
	?, ?, ?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?,
	?, ?,
	?, ?
)
ON CONFLICT(session_key) DO UPDATE SET
	subsession_id = excluded.subsession_id,
	session_num = excluded.session_num,
	session_type = excluded.session_type,
	event_context = excluded.event_context,
	league_id = excluded.league_id,
	series_id = excluded.series_id,
	season_id = excluded.season_id,
	official = excluded.official,
	track_id = excluded.track_id,
	track_name = excluded.track_name,
	track_config = excluded.track_config,
	track_length_km = excluded.track_length_km,
	car_id = excluded.car_id,
	car_name = excluded.car_name,
	car_class_id = excluded.car_class_id,
	car_class_name = excluded.car_class_name,
	started_at = excluded.started_at,
	ended_at = excluded.ended_at,
	connected_seconds = excluded.connected_seconds,
	in_car_seconds = excluded.in_car_seconds,
	driving_seconds = excluded.driving_seconds,
	laps_completed = excluded.laps_completed,
	incidents = excluded.incidents,
	best_lap_time_s = excluded.best_lap_time_s,
	starting_position = excluded.starting_position,
	finish_position = excluded.finish_position,
	finish_class_position = excluded.finish_class_position,
	qualify_position = excluded.qualify_position,
	qualify_class_position = excluded.qualify_class_position,
	qualify_best_time_s = excluded.qualify_best_time_s,
	field_size = excluded.field_size,
	ai_opponent_count = excluded.ai_opponent_count,
	ai_detection = excluded.ai_detection,
	incident_source = excluded.incident_source,
	classify_source_json = excluded.classify_source_json,
	capture_file = excluded.capture_file,
	updated_at = excluded.updated_at
RETURNING id`

	var id int64
	err = s.writer.QueryRow(q,
		rec.UUID, rec.SessionKey, rec.SubsessionID, rec.SessionNum, rec.SessionType, rec.EventContext,
		rec.LeagueID, rec.SeriesID, rec.SeasonID, rec.Official,
		rec.TrackID, rec.TrackName, rec.TrackConfig, rec.TrackLengthKm,
		rec.CarID, rec.CarName, rec.CarClassID, rec.CarClassName,
		rec.StartedAt, rec.EndedAt,
		rec.ConnectedSeconds, rec.InCarSeconds, rec.DrivingSeconds,
		rec.LapsCompleted, rec.Incidents, rec.BestLapTimeS,
		rec.StartingPosition, rec.FinishPosition, rec.FinishClassPosition,
		rec.QualifyPosition, rec.QualifyClassPosition, rec.QualifyBestTimeS, rec.FieldSize,
		rec.AIOpponentCount, rec.AIDetection, rec.IncidentSource,
		rec.ClassifySourceJSON, rec.CaptureFile,
		rec.CreatedAt, rec.UpdatedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: upsert session %s: %w", rec.SessionKey, err)
	}
	rec.ID = id
	return id, nil
}

// scanSession reads one session row in sessionColumns order.
func scanSession(sc interface{ Scan(...any) error }) (*Session, error) {
	var r Session
	err := sc.Scan(
		&r.ID, &r.UUID, &r.SessionKey, &r.SubsessionID, &r.SessionNum, &r.SessionType, &r.EventContext,
		&r.LeagueID, &r.SeriesID, &r.SeasonID, &r.Official,
		&r.TrackID, &r.TrackName, &r.TrackConfig, &r.TrackLengthKm,
		&r.CarID, &r.CarName, &r.CarClassID, &r.CarClassName,
		&r.StartedAt, &r.EndedAt,
		&r.ConnectedSeconds, &r.InCarSeconds, &r.DrivingSeconds,
		&r.LapsCompleted, &r.Incidents, &r.BestLapTimeS,
		&r.StartingPosition, &r.FinishPosition, &r.FinishClassPosition,
		&r.QualifyPosition, &r.QualifyClassPosition, &r.QualifyBestTimeS, &r.FieldSize,
		&r.AIOpponentCount, &r.AIDetection, &r.IncidentSource,
		&r.ClassifySourceJSON, &r.CaptureFile,
		&r.CreatedAt, &r.UpdatedAt, &r.UploadedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SessionByKey looks up a session by its session_key.
func (s *Store) SessionByKey(key string) (*Session, error) {
	row := s.writer.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE session_key = ?`, key)
	rec, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: session_key %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("store: read session %s: %w", key, err)
	}
	return rec, nil
}

// SessionByID looks up a session by its primary key.
func (s *Store) SessionByID(id int64) (*Session, error) {
	row := s.reader.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	rec, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: session id %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("store: read session %d: %w", id, err)
	}
	return rec, nil
}

// DeleteSession removes a session and, by cascade, its laps and position
// events.
func (s *Store) DeleteSession(id int64) error {
	res, err := s.writer.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete session %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete session %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: session id %d", ErrNotFound, id)
	}
	return nil
}
```

Note on `SessionByKey` using the writer handle: the upsert path calls it to decide insert-versus-update, and reading through the same single connection the write will use avoids observing a stale WAL snapshot mid-flush.

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS, all tests.

- [ ] **Step 9: Commit**

```bash
git add internal/store/
git commit -m "Add session upsert with stable session_key identity"
```
