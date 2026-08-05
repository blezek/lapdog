package icons

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// Rasterize renders a named icon at the given pixel size, tinted with tint.
//
// The vendored SVGs are monochrome path data, so tinting is applied by drawing
// the rendered alpha channel through a solid colour rather than by rewriting the
// SVG fill. That keeps one source file per icon regardless of how many states
// the interface needs it in.
func Rasterize(name string, size int, tint color.Color) (*image.RGBA, error) {
	if size <= 0 {
		return nil, fmt.Errorf("icons: size %d must be positive", size)
	}
	raw, err := SVG(name)
	if err != nil {
		return nil, err
	}

	svg, err := oksvg.ReadIconStream(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("icons: parse %s: %w", name, err)
	}
	svg.SetTarget(0, 0, float64(size), float64(size))

	// Render to a mask first. Only the alpha channel matters, since the tint
	// supplies the colour.
	mask := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, mask, mask.Bounds())
	svg.Draw(rasterx.NewDasher(size, size, scanner), 1.0)

	out := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.DrawMask(out, out.Bounds(), image.NewUniform(tint), image.Point{}, mask, image.Point{}, draw.Over)
	return out, nil
}

// PNG renders a named icon to PNG bytes.
func PNG(name string, size int, tint color.Color) ([]byte, error) {
	img, err := Rasterize(name, size, tint)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("icons: encode %s as PNG: %w", name, err)
	}
	return buf.Bytes(), nil
}

// ICO renders a named icon as a Windows .ico containing the given sizes.
//
// The Windows tray wants an .ico, and Vista and later accept PNG-compressed
// entries inside the container. Writing the container by hand avoids a
// dependency for what is a twenty-two byte header per image.
func ICO(name string, tint color.Color, sizes ...int) ([]byte, error) {
	if len(sizes) == 0 {
		// 16 for the tray at standard DPI, 32 for scaled displays.
		sizes = []int{16, 32}
	}

	type entry struct {
		size int
		png  []byte
	}
	entries := make([]entry, 0, len(sizes))
	for _, s := range sizes {
		b, err := PNG(name, s, tint)
		if err != nil {
			return nil, err
		}
		if s > 256 {
			return nil, fmt.Errorf("icons: ICO entries cannot exceed 256 pixels, got %d", s)
		}
		entries = append(entries, entry{size: s, png: b})
	}

	var buf bytes.Buffer
	// ICONDIR: reserved, type 1 (icon), image count.
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))

	// Image data begins after the directory.
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		// A dimension of 256 is encoded as zero, which is why the cap above
		// exists.
		dim := byte(e.size)
		if e.size == 256 {
			dim = 0
		}
		buf.WriteByte(dim)                                          // width
		buf.WriteByte(dim)                                          // height
		buf.WriteByte(0)                                            // palette size
		buf.WriteByte(0)                                            // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))          // colour planes
		binary.Write(&buf, binary.LittleEndian, uint16(32))         // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(e.png))) // byte size
		binary.Write(&buf, binary.LittleEndian, uint32(offset))     // byte offset
		offset += len(e.png)
	}
	for _, e := range entries {
		buf.Write(e.png)
	}
	return buf.Bytes(), nil
}

// Tray state colours.
//
// These come from the project's status palette so the tray agrees with the
// interface. Colour alone never carries the meaning: the tray tooltip and the
// menu header always state the connection state in words as well.
var (
	// TintConnected is status-good.
	TintConnected = color.RGBA{R: 0x0c, G: 0xa3, B: 0x0c, A: 0xff}
	// TintPaused is status-warning.
	TintPaused = color.RGBA{R: 0xfa, G: 0xb2, B: 0x19, A: 0xff}
	// TintDisconnected is the muted ink used for inactive chrome.
	TintDisconnected = color.RGBA{R: 0x89, G: 0x87, B: 0x81, A: 0xff}
)
