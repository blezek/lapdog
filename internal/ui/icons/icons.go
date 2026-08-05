// Package icons provides LapDog's iconography.
//
// The icons are Material Design Icons from the Pictogrammers project, vendored
// under mdi/. They are released under the Apache 2.0 licence, which permits
// commercial use in a closed-source application and imposes no in-application
// attribution requirement — see mdi/LICENSE, which is redistributed alongside
// them as the licence requires.
//
// Vendoring rather than fetching at build time keeps the build hermetic: a
// release can be reproduced years later without depending on a CDN still being
// up or still serving the same bytes.
package icons

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed mdi/*.svg mdi/LICENSE
var mdiFS embed.FS

// Named icons. Referring to icons through constants rather than raw strings
// means a rename or a removed file is a compile error rather than a blank space
// in the interface.
const (
	// RacingHelmet is the application's identity mark, used for the tray icon
	// and the window title.
	RacingHelmet = "racing-helmet"

	FlagCheckered = "flag-checkered"
	CarSports     = "car-sports"
	GoKart        = "go-kart"
	Steering      = "steering"
	Tire          = "tire"
	Podium        = "podium"
	Trophy        = "trophy"
	Speedometer   = "speedometer"
	Timer         = "timer-outline"
	// RoadVariant marks the Tracks page. The set had no road or map glyph.
	RoadVariant = "road-variant"

	ChartLine     = "chart-line"
	ChartBar      = "chart-bar"
	CalendarMonth = "calendar-month"
	TableLarge    = "table-large"
	FilterVariant = "filter-variant"
	Magnify       = "magnify"

	Cog        = "cog"
	Download   = "download"
	FolderOpen = "folder-open"
	OpenInNew  = "open-in-new"
	Pause      = "pause"
	Dot        = "circle-slice-8"

	AlertCircle = "alert-circle-outline"
	CheckCircle = "check-circle-outline"
	InfoCircle  = "information-outline"
)

// All is every vendored icon name, used by the UI build to emit a sprite sheet
// and by tests to assert nothing referenced is missing.
var All = []string{
	RacingHelmet, FlagCheckered, CarSports, GoKart, Steering, Tire,
	Podium, Trophy, Speedometer, Timer, RoadVariant,
	ChartLine, ChartBar, CalendarMonth, TableLarge, FilterVariant, Magnify,
	Cog, Download, FolderOpen, OpenInNew, Pause, Dot,
	AlertCircle, CheckCircle, InfoCircle,
}

// SVG returns the raw SVG document for a named icon.
func SVG(name string) ([]byte, error) {
	b, err := mdiFS.ReadFile("mdi/" + name + ".svg")
	if err != nil {
		return nil, fmt.Errorf("icons: no icon named %q: %w", name, err)
	}
	return b, nil
}

// FS returns the embedded icon set, so the HTTP layer can serve the SVGs
// directly to the browser without a second copy of them in the frontend bundle.
func FS() fs.FS {
	sub, err := fs.Sub(mdiFS, "mdi")
	if err != nil {
		// The directory is embedded at compile time, so this cannot fail at run
		// time.
		panic("icons: mdi directory missing from the binary: " + err.Error())
	}
	return sub
}

// License returns the vendored licence text, which the settings screen shows so
// the attribution lives in the product rather than only in the repository.
func License() string {
	b, err := mdiFS.ReadFile("mdi/LICENSE")
	if err != nil {
		return ""
	}
	return string(b)
}
