package tray

import (
	"image/color"
	"runtime"

	"github.com/blezek/lapdog/internal/ui/icons"
)

// iconState is the connection state the tray icon reflects.
type iconState int

// Icon states.
const (
	stateDisconnected iconState = iota
	stateConnected
	statePaused
)

// State tints.
//
// These are the status colours from the interface palette, so the tray and the web
// interface agree on what "recording" looks like. Colour is the only difference
// between the three icons, which is acceptable here and only here: a tray icon is
// 16 pixels and cannot carry a glyph as well, and the state is always available as
// text in the tooltip and the menu header. Nothing depends on the colour alone.
var (
	tintConnected    = color.RGBA{R: 0x0c, G: 0xa3, B: 0x0c, A: 0xff}
	tintPaused       = color.RGBA{R: 0xfa, G: 0xb2, B: 0x19, A: 0xff}
	tintDisconnected = color.RGBA{R: 0x89, G: 0x87, B: 0x81, A: 0xff}
)

// tintFor returns the colour for a state.
func tintFor(s iconState) color.Color {
	switch s {
	case stateConnected:
		return tintConnected
	case statePaused:
		return tintPaused
	default:
		return tintDisconnected
	}
}

// icon renders the tray icon for a state.
//
// It rasterises the vendored racing-helmet icon rather than drawing a shape here,
// so the tray carries the same mark as the rest of the product and there is one
// icon set to license and maintain. The plan called for a generated circle; the
// icon set landed afterwards and is a better answer, since a coloured dot in the
// notification area says nothing about which application it belongs to.
//
// Windows wants an .ico; the other platforms accept a PNG. Both are produced by
// pure-Go rasterisation, so this needs no build tag and is testable anywhere.
func icon(s iconState) []byte {
	tint := tintFor(s)
	if runtime.GOOS == "windows" {
		// 16 for the tray at standard DPI, 32 for scaled displays.
		if b, err := icons.ICO(icons.RacingHelmet, tint, 16, 32); err == nil {
			return b
		}
		return nil
	}
	if b, err := icons.PNG(icons.RacingHelmet, 32, tint); err == nil {
		return b
	}
	return nil
}
