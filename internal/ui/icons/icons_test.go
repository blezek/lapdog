package icons

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strings"
	"testing"
)

// Every named icon must actually be vendored, so a rename or a missed download
// fails the build rather than leaving a blank space in the interface.
func TestAllNamedIconsAreVendored(t *testing.T) {
	for _, name := range All {
		b, err := SVG(name)
		if err != nil {
			t.Errorf("icon %q is referenced but not vendored: %v", name, err)
			continue
		}
		if !bytes.Contains(b, []byte("<svg")) || !bytes.Contains(b, []byte("<path")) {
			t.Errorf("icon %q does not look like an SVG path document", name)
		}
	}
}

func TestSVGUnknownIcon(t *testing.T) {
	if _, err := SVG("no-such-icon"); err == nil {
		t.Error("SVG on an unknown name returned nil error")
	}
}

// The licence must ship inside the binary. Apache 2.0 requires the licence to
// travel with the redistributed work, and the settings screen shows it.
func TestLicenseIsEmbedded(t *testing.T) {
	lic := License()
	if lic == "" {
		t.Fatal("License() is empty; the licence must be embedded with the icons")
	}
	if !strings.Contains(lic, "Apache 2.0") {
		t.Errorf("licence text does not mention Apache 2.0:\n%s", lic)
	}
	if !strings.Contains(lic, "Pictogrammers") {
		t.Error("licence text does not attribute Pictogrammers")
	}
}

func TestRasterizeProducesOpaquePixels(t *testing.T) {
	img, err := Rasterize(RacingHelmet, 32, TintConnected)
	if err != nil {
		t.Fatalf("Rasterize: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("bounds = %v, want 32x32", b)
	}

	// A rendered icon must actually mark some pixels, and they must carry the
	// tint rather than the SVG's own fill.
	var painted int
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				painted++
			}
		}
	}
	if painted == 0 {
		t.Fatal("the rasterised icon is entirely transparent")
	}
	if painted == 32*32 {
		t.Error("every pixel is painted; the icon has no transparent margin, which suggests the mask was ignored")
	}
}

func TestRasterizeRejectsBadSize(t *testing.T) {
	if _, err := Rasterize(RacingHelmet, 0, TintConnected); err == nil {
		t.Error("Rasterize with size 0 returned nil error")
	}
}

func TestPNGDecodes(t *testing.T) {
	b, err := PNG(FlagCheckered, 16, TintDisconnected)
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the produced bytes are not a valid PNG: %v", err)
	}
	if img.Bounds().Dx() != 16 {
		t.Errorf("decoded width = %d, want 16", img.Bounds().Dx())
	}
}

// The ICO container is written by hand, so its structure gets a test rather than
// trust. A malformed header shows up as a blank tray icon on Windows, which is
// awkward to diagnose from a Mac.
func TestICOStructure(t *testing.T) {
	b, err := ICO(RacingHelmet, TintConnected, 16, 32)
	if err != nil {
		t.Fatalf("ICO: %v", err)
	}
	if len(b) < 6+16*2 {
		t.Fatalf("ICO is only %d bytes, too small for its own directory", len(b))
	}

	reserved := binary.LittleEndian.Uint16(b[0:])
	kind := binary.LittleEndian.Uint16(b[2:])
	count := binary.LittleEndian.Uint16(b[4:])
	if reserved != 0 {
		t.Errorf("reserved = %d, want 0", reserved)
	}
	if kind != 1 {
		t.Errorf("type = %d, want 1 for an icon", kind)
	}
	if count != 2 {
		t.Fatalf("image count = %d, want 2", count)
	}

	// Walk each directory entry and confirm its declared slice is in range and
	// decodes as a PNG of the declared size.
	for i := 0; i < int(count); i++ {
		e := b[6+16*i:]
		wantDim := int(e[0])
		size := binary.LittleEndian.Uint32(e[8:])
		offset := binary.LittleEndian.Uint32(e[12:])

		if int(offset)+int(size) > len(b) {
			t.Fatalf("entry %d declares bytes %d..%d, past the %d byte file",
				i, offset, int(offset)+int(size), len(b))
		}
		img, err := png.Decode(bytes.NewReader(b[offset : uint32(offset)+size]))
		if err != nil {
			t.Errorf("entry %d does not decode as PNG: %v", i, err)
			continue
		}
		if img.Bounds().Dx() != wantDim {
			t.Errorf("entry %d declares %d pixels but the PNG is %d", i, wantDim, img.Bounds().Dx())
		}
	}
}

func TestICORejectsOversizeEntry(t *testing.T) {
	if _, err := ICO(RacingHelmet, TintConnected, 512); err == nil {
		t.Error("ICO accepted a 512 pixel entry; the container cannot express it")
	}
}

// The three tray tints must be distinguishable, since the icon is the only
// always-visible indicator of recording state.
func TestTrayTintsDiffer(t *testing.T) {
	tints := map[string][4]uint32{}
	for name, c := range map[string]interface {
		RGBA() (uint32, uint32, uint32, uint32)
	}{
		"connected":    TintConnected,
		"paused":       TintPaused,
		"disconnected": TintDisconnected,
	} {
		r, g, b, a := c.RGBA()
		tints[name] = [4]uint32{r, g, b, a}
	}
	if tints["connected"] == tints["paused"] ||
		tints["connected"] == tints["disconnected"] ||
		tints["paused"] == tints["disconnected"] {
		t.Errorf("tray tints are not all distinct: %v", tints)
	}
}

// FS must expose the icons for the HTTP layer, so the frontend does not need a
// second copy of them in its bundle.
func TestFSServesIcons(t *testing.T) {
	f, err := FS().Open(RacingHelmet + ".svg")
	if err != nil {
		t.Fatalf("FS does not serve %s.svg: %v", RacingHelmet, err)
	}
	f.Close()
}
