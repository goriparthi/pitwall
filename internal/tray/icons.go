package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
)

// icon draws an anti-aliased ring with a filled center in c; PNG on macOS, PNG-in-ICO on Windows.
func icon(c color.NRGBA) []byte {
	const n = 32
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := math.Hypot(float64(x)-15.5, float64(y)-15.5)
			a := 0.0
			switch {
			case d <= 6.5:
				a = 1
			case d <= 7.5:
				a = 7.5 - d
			case d >= 11 && d <= 13:
				a = 1
			case d > 10 && d < 11:
				a = d - 10
			case d > 13 && d < 14:
				a = 14 - d
			}
			if a > 0 {
				img.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8(float64(c.A) * a)})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	if runtime.GOOS != "windows" {
		return buf.Bytes()
	}
	// ICONDIR + one ICONDIRENTRY pointing at the PNG (supported since Windows Vista)
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1})
	ico.Write([]byte{n, n, 0, 0})
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})
	_ = binary.Write(&ico, binary.LittleEndian, []uint32{uint32(buf.Len()), 22})
	ico.Write(buf.Bytes())
	return ico.Bytes()
}

var (
	iconCalm    = icon(color.NRGBA{150, 160, 176, 255})
	iconWorking = icon(color.NRGBA{76, 184, 255, 255})
	iconWaiting = icon(color.NRGBA{255, 178, 36, 255})
	iconFailed  = icon(color.NRGBA{255, 93, 98, 255})
	iconOff     = icon(color.NRGBA{110, 116, 128, 160})
)
