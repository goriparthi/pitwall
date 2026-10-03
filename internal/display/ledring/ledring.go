// Package ledring drives the Universal Screen LED ring (USB 0416:8050): 60 RGB LEDs as 64-byte bulk packets
// [0x11, offset, 0, 0, 20 x RGB] per lian-li-linux winusb/led.rs. Stream only; nothing is saved to the device.
package ledring

import (
	"sync"
	"time"

	"github.com/goriparthi/pitwall/internal/usb"
)

const (
	VendorID  = 0x0416
	ProductID = 0x8050
	Count     = 60
	perPacket = 20
)

type RGB [3]byte

func Packets(colors []RGB) [][]byte {
	var out [][]byte
	for start := 0; start < Count; start += perPacket {
		p := make([]byte, 64)
		p[0], p[1] = 0x11, byte(start)
		for i := 0; i < perPacket; i++ {
			if c := start + i; c < len(colors) {
				copy(p[4+i*3:], colors[c][:])
			}
		}
		out = append(out, p)
	}
	return out
}

type Ring struct {
	mu   sync.Mutex
	bulk usb.Bulk
}

func Open() (*Ring, error) {
	b, err := usb.Open(VendorID, ProductID)
	if err != nil {
		return nil, err
	}
	return &Ring{bulk: b}, nil
}

func (r *Ring) Show(colors []RGB) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range Packets(colors) {
		if err := r.bulk.Write(p, time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (r *Ring) Fill(c RGB) error {
	all := make([]RGB, Count)
	for i := range all {
		all[i] = c
	}
	return r.Show(all)
}

func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bulk.Close()
}
