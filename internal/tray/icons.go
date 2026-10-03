package tray

import (
	"bytes"
	"embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"runtime"
	"sync"
)

// Brand assets (assets/icons): the macOS template glyph is black on transparent so the OS tints it;
// Windows uses the full-color icon with a status dot, since its tray shows no title text.
var (
	//go:embed assets/menu-template-32.png
	menuTemplate []byte
	//go:embed assets/icon-32.png
	brandIcon32 []byte
	// macOS menu bar dial by screen state (scripts/brand/menu-icons.sh); colored, so not template images
	//go:embed assets/dial-connected-32.png
	dialConnected []byte
	//go:embed assets/dial-disconnected-32.png
	dialDisconnected []byte
	// Menu glyphs rendered from web/js/icons.js by scripts/brand/menu-icons.sh, black on transparent.
	//go:embed assets/menu/*.png
	menuGlyphs embed.FS
)

// withDot overlays a status dot (bottom right) on the brand icon and wraps it as a PNG-in-ICO.
func withDot(c *color.NRGBA) []byte {
	src, err := png.Decode(bytes.NewReader(brandIcon32))
	if err != nil {
		return nil
	}
	img := image.NewNRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	if c != nil {
		cx, cy, r := 24.5, 24.5, 6.5
		for y := 16; y < 32; y++ {
			for x := 16; x < 32; x++ {
				d := math.Hypot(float64(x)-cx, float64(y)-cy)
				switch {
				case d <= r:
					img.SetNRGBA(x, y, *c)
				case d <= r+1.5: // dark ring so the dot reads on any taskbar
					img.SetNRGBA(x, y, color.NRGBA{11, 16, 20, 255})
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return pngToICO(buf.Bytes(), 32)
}

// pngToICO wraps one PNG in an ICO container, which Windows needs for tray and menu icons.
func pngToICO(p []byte, size byte) []byte {
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1})
	ico.Write([]byte{size, size, 0, 0})
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})
	_ = binary.Write(&ico, binary.LittleEndian, []uint32{uint32(len(p)), 22})
	ico.Write(p)
	return ico.Bytes()
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	if runtime.GOOS == "windows" {
		return pngToICO(buf.Bytes(), 32)
	}
	return buf.Bytes()
}

// menuGrey is the glyph color where the OS cannot tint a template image (Windows menus, light or dark).
var menuGrey = color.NRGBA{0x6B, 0x7A, 0x85, 255}

type glyphPair struct{ template, regular []byte }

var (
	glyphMu    sync.Mutex
	glyphCache = map[string]glyphPair{}
)

// menuGlyph returns a menu icon as a macOS template PNG plus a grey copy for platforms without template images.
func menuGlyph(name string) (template, regular []byte) {
	glyphMu.Lock()
	defer glyphMu.Unlock()
	if g, ok := glyphCache[name]; ok {
		return g.template, g.regular
	}
	t, err := menuGlyphs.ReadFile("assets/menu/" + name + ".png")
	if err != nil {
		return nil, nil
	}
	src, err := png.Decode(bytes.NewReader(t))
	if err != nil {
		return nil, nil
	}
	b := src.Bounds()
	grey := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			grey.SetNRGBA(x, y, color.NRGBA{menuGrey.R, menuGrey.G, menuGrey.B, uint8(a >> 8)})
		}
	}
	g := glyphPair{t, encode(grey)}
	glyphCache[name] = g
	return g.template, g.regular
}

// statusDot is a colored, antialiased dot for agent rows; status colors are the semantic tokens.
func statusDot(c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	const cx, cy, r = 16.0, 16.0, 8.0
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			if cov := r + 0.5 - d; cov > 0 {
				a := c.A
				if cov < 1 {
					a = uint8(float64(c.A) * cov)
				}
				img.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, a})
			}
		}
	}
	return encode(img)
}

var statusColor = map[string]color.NRGBA{
	"working":   {0x75, 0xC7, 0xFF, 255},
	"waiting":   {0xFF, 0xD0, 0x76, 255},
	"failed":    {0xFF, 0x8E, 0x87, 255},
	"completed": {0x77, 0xD9, 0xA0, 255},
	"idle":      {0x83, 0x95, 0x9F, 255},
	"unknown":   {0x83, 0x95, 0x9F, 255},
	"ok":        {0x77, 0xD9, 0xA0, 255},
	"warn":      {0xFF, 0xD0, 0x76, 255},
	"crit":      {0xFF, 0x8E, 0x87, 255},
}

var dots = func() map[string][]byte {
	m := map[string][]byte{}
	for k, c := range statusColor {
		m[k] = statusDot(c)
	}
	return m
}()

// Status colors are the semantic tokens, never the brand accent.
var (
	icoCalm    = withDot(nil)
	icoWorking = withDot(&color.NRGBA{0x75, 0xC7, 0xFF, 255})
	icoWaiting = withDot(&color.NRGBA{0xFF, 0xD0, 0x76, 255})
	icoFailed  = withDot(&color.NRGBA{0xFF, 0x8E, 0x87, 255})
)
