package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"time"

	"github.com/goriparthi/pitwall/internal/display/ledring"
	"github.com/goriparthi/pitwall/internal/display/us88"
)

// probeCmd pushes an orientation frame (green top-left block, orange bottom-right block, rotated to native)
// or runs a short LED test. Neither is saved to the device.
func probeCmd(args []string) int {
	if len(args) > 0 && args[0] == "led" {
		r, err := ledring.Open()
		if err != nil {
			fmt.Fprintln(os.Stderr, "LED ring:", err)
			return 1
		}
		defer r.Close()
		_ = r.Fill(ledring.RGB{90, 55, 0})
		time.Sleep(3 * time.Second)
		_ = r.Fill(ledring.RGB{})
		fmt.Println("LED ring: 3 s amber, then off")
		return 0
	}
	s, err := us88.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "screen:", err)
		return 1
	}
	defer s.Close()
	land := image.NewRGBA(image.Rect(0, 0, 1920, 480))
	fill := func(r image.Rectangle, c color.RGBA) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				land.SetRGBA(x, y, c)
			}
		}
	}
	fill(land.Bounds(), color.RGBA{11, 14, 20, 255})
	fill(image.Rect(20, 20, 420, 200), color.RGBA{61, 220, 151, 255})
	fill(image.Rect(1500, 300, 1900, 460), color.RGBA{245, 165, 36, 255})
	// rotate 90° clockwise into the panel's native 480x1920
	native := image.NewRGBA(image.Rect(0, 0, 480, 1920))
	for y := 0; y < 480; y++ {
		for x := 0; x < 1920; x++ {
			native.Set(479-y, x, land.At(x, y))
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, native, &jpeg.Options{Quality: 90})
	level, err := s.PushJPEG(buf.Bytes())
	if err != nil {
		fmt.Fprintln(os.Stderr, "push:", err)
		return 1
	}
	fmt.Printf("firmware %s; pushed %d B; buffer level %d\nexpect: green block top-left, orange block bottom-right\n", s.Firmware, buf.Len(), level)
	return 0
}
