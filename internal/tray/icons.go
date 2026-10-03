package tray

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// Brand assets (assets/icons): the macOS template glyph is black on transparent so the OS tints it;
// Windows uses the full-color icon with a status dot, since its tray shows no title text.
var (
	//go:embed assets/menu-template-32.png
	menuTemplate []byte
	//go:embed assets/icon-32.png
	brandIcon32 []byte
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
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1})
	ico.Write([]byte{32, 32, 0, 0})
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})
	_ = binary.Write(&ico, binary.LittleEndian, []uint32{uint32(buf.Len()), 22})
	ico.Write(buf.Bytes())
	return ico.Bytes()
}

// Status colors are the semantic tokens, never the brand accent.
var (
	icoCalm    = withDot(nil)
	icoWorking = withDot(&color.NRGBA{0x75, 0xC7, 0xFF, 255})
	icoWaiting = withDot(&color.NRGBA{0xFF, 0xD0, 0x76, 255})
	icoFailed  = withDot(&color.NRGBA{0xFF, 0x8E, 0x87, 255})
)
