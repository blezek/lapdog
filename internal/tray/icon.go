package tray

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"runtime"

	"github.com/blezek/lapdog/internal/ui/icons"
	xdraw "golang.org/x/image/draw"
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

//go:embed lapdog-tray.png
var trayMarkPNG []byte

var trayMark, trayMarkErr = png.Decode(bytes.NewReader(trayMarkPNG))

// icon renders the tray icon for a state.
//
// It scales the simplified helmeted-dog mark rather than the full car illustration:
// the notification area can be only 16 pixels high, where the wheels, number and
// cockpit collapse into noise. A small status badge preserves the existing state
// colours without recolouring the multicolour identity mark.
//
// Windows wants an .ico; the other platforms accept a PNG. Both are produced by
// pure-Go rasterisation, so this needs no build tag and is testable anywhere.
func icon(s iconState) []byte {
	if runtime.GOOS == "windows" {
		return windowsIcon(s)
	}
	img, err := renderIcon(s, 32)
	if err != nil {
		return nil
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err == nil {
		return b.Bytes()
	}
	return nil
}

func windowsIcon(s iconState) []byte {
	// 16 for the tray at standard DPI, 32 for scaled displays.
	images := make([]image.Image, 0, 2)
	for _, size := range []int{16, 32} {
		img, err := renderIcon(s, size)
		if err != nil {
			return nil
		}
		images = append(images, img)
	}
	b, err := icons.EncodeICO(images...)
	if err != nil {
		return nil
	}
	return b
}

func renderIcon(s iconState, size int) (*image.RGBA, error) {
	if trayMarkErr != nil {
		return nil, fmt.Errorf("tray: decode identity mark: %w", trayMarkErr)
	}
	if size <= 0 {
		return nil, fmt.Errorf("tray: icon size %d must be positive", size)
	}

	out := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(out, out.Bounds(), trayMark, trayMark.Bounds(), draw.Over, nil)
	drawBadge(out, tintFor(s))
	return out, nil
}

func drawBadge(img *image.RGBA, tint color.Color) {
	size := img.Bounds().Dx()
	radius := size / 6
	if radius < 2 {
		radius = 2
	}
	cx, cy := size-radius-1, size-radius-1
	fillCircle(img, cx, cy, radius, color.White)
	fillCircle(img, cx, cy, radius-1, tint)
}

func fillCircle(img *image.RGBA, cx, cy, radius int, fill color.Color) {
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= radius*radius {
				img.Set(x, y, fill)
			}
		}
	}
}
