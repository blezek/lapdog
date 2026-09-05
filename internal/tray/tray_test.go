package tray

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"runtime"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/updater"
)

func TestUpdateTitleIncludesMeasuredDownloadProgress(t *testing.T) {
	total := int64(100)
	s := updater.Snapshot{
		State:     updater.Downloading,
		Available: &updater.Release{Version: "v1.2.0"},
		Download:  &updater.DownloadProgress{Phase: updater.DownloadArchive, DownloadedBytes: 42, TotalBytes: &total},
	}
	if got := updateTitle(s); got != "Downloading update v1.2.0 (42%)" {
		t.Fatalf("title=%q", got)
	}
}

// The three icons must all render and must differ from one another, or the tray
// would report the same thing whatever the collector was doing.
func TestIconsRenderAndDiffer(t *testing.T) {
	states := map[string]iconState{
		"disconnected": stateDisconnected,
		"connected":    stateConnected,
		"paused":       statePaused,
	}
	seen := make(map[string][]byte, len(states))
	for name, s := range states {
		b := icon(s)
		if len(b) == 0 {
			t.Errorf("%s icon did not render", name)
			continue
		}
		seen[name] = b
	}
	for a, ba := range seen {
		for b, bb := range seen {
			if a < b && bytes.Equal(ba, bb) {
				t.Errorf("the %s and %s icons are byte-identical", a, b)
			}
		}
	}
}

func TestRenderedIconKeepsBrandColoursAndStateBadge(t *testing.T) {
	states := map[string]iconState{
		"disconnected": stateDisconnected,
		"connected":    stateConnected,
		"paused":       statePaused,
	}
	for name, state := range states {
		t.Run(name, func(t *testing.T) {
			img, err := renderIcon(state, 16)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
				t.Fatalf("bounds = %v, want 16x16", img.Bounds())
			}

			// A multicolour palette distinguishes the branded dog from the old
			// single-tint helmet even after the source is reduced to tray size.
			palette := make(map[color.RGBA]struct{})
			for y := 0; y < 16; y++ {
				for x := 0; x < 8; x++ { // stay clear of the state badge
					pixel := img.RGBAAt(x, y)
					if pixel.A > 0 {
						palette[pixel] = struct{}{}
					}
				}
			}
			if len(palette) < 8 {
				t.Errorf("left half has only %d opaque colours, want a multicolour brand mark", len(palette))
			}

			radius := 16 / 6
			center := 16 - radius - 1
			want := color.RGBAModel.Convert(tintFor(state)).(color.RGBA)
			if got := img.RGBAAt(center, center); got != want {
				t.Errorf("badge centre = %v, want state tint %v", got, want)
			}
		})
	}
}

func TestWindowsIconCarriesStandardAndScaledImages(t *testing.T) {
	b := windowsIcon(stateConnected)
	if len(b) < 6+16*2 {
		t.Fatalf("Windows icon is only %d bytes, too small for two entries", len(b))
	}
	if count := binary.LittleEndian.Uint16(b[4:]); count != 2 {
		t.Fatalf("Windows icon entry count = %d, want 2", count)
	}
	if got := []byte{b[6], b[22]}; !bytes.Equal(got, []byte{16, 32}) {
		t.Errorf("Windows icon sizes = %v, want [16 32]", got)
	}
}

// Paused outranks connected. Someone who paused recording needs to see that
// nothing is being recorded even while the sim is running.
func TestStateForPrefersPausedOverConnected(t *testing.T) {
	cases := []struct {
		name string
		s    collector.Status
		want iconState
		text string
	}{
		{"idle", collector.Status{}, stateDisconnected, "Not connected"},
		{"driving", collector.Status{Connected: true}, stateConnected, "Connected"},
		{"paused while connected", collector.Status{Connected: true, Paused: true}, statePaused, "Paused"},
		{"paused while disconnected", collector.Status{Paused: true}, statePaused, "Paused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stateFor(c.s); got != c.want {
				t.Errorf("stateFor = %v, want %v", got, c.want)
			}
			if got := stateText(c.s); got != c.text {
				t.Errorf("stateText = %q, want %q", got, c.text)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0, "0:00"},
		{59, "0:00"},
		{60, "0:01"},
		{3600, "1:00"},
		{3660, "1:01"},
		// Past a day it keeps counting hours rather than wrapping, because total
		// driving time is the point and "1:30" for thirty-one hours would be wrong.
		{90000, "25:00"},
	}
	for _, c := range cases {
		if got := formatDuration(c.sec); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.sec, got, c.want)
		}
	}
}

func TestOpenTitleNamesFallbackPort(t *testing.T) {
	if got := openTitle(Options{PreferredPort: 47047, InterfacePort: 53124}); got != "Open LapDog (port 53124)" {
		t.Errorf("fallback openTitle = %q", got)
	}
	if got := openTitle(Options{PreferredPort: 47047, InterfacePort: 47047}); got != "Open LapDog" {
		t.Errorf("preferred openTitle = %q", got)
	}
	if got := openTitle(Options{PreferredPort: 47047, InterfaceError: "no interface"}); got != "Open LapDog" {
		t.Errorf("error openTitle = %q", got)
	}
}

func TestIdleDetailShowsInterfaceNotice(t *testing.T) {
	const notice = "Interface on port 53124; 47047 unavailable"
	if got := idleDetail(Options{InterfaceNotice: notice}); got != notice {
		t.Errorf("idleDetail with notice = %q, want %q", got, notice)
	}
	if got := idleDetail(Options{}); got != "No active session" {
		t.Errorf("idleDetail without notice = %q", got)
	}
}

// Run must return when Done closes, including when Done is already closed before
// Run is called.
//
// That second case is the bug this pins. The tray used to install its own signal
// handler, so an interrupt arriving before Run started was consumed by the
// caller's handler and Run then waited forever for a signal already delivered —
// Ctrl-C during start-up left a process that only SIGKILL could stop. Nothing
// fails visibly when this regresses; the program simply stops exiting.
func TestRunReturnsWhenDoneIsAlreadyClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows tray needs a message loop and a desktop session")
	}
	done := make(chan struct{})
	close(done)

	quit := false
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		Run(Options{Done: done, Quit: func() { quit = true }})
	}()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return although Done was already closed")
	}
	_ = quit
}

// Closing Done while Run is blocked must also release it.
func TestRunReturnsWhenDoneClosesLater(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows tray needs a message loop and a desktop session")
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		Run(Options{Done: done})
	}()

	select {
	case <-finished:
		t.Fatal("Run returned before Done closed")
	case <-time.After(100 * time.Millisecond):
	}

	close(done)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Done closed")
	}
}

// A nil Done would block forever, which is the same hang by another route.
func TestRunRefusesANilDoneChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows tray needs a message loop and a desktop session")
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		Run(Options{})
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Run blocked on a nil Done channel instead of returning")
	}
}
