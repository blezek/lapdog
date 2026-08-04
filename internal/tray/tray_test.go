package tray

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/collector"
)

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
