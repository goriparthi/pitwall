// Package usb opens a vendor-class device's interface 0 and exposes bulk EP1 OUT/IN with timeouts.
// macOS uses libusb (gousb, cgo); Windows uses WinUSB directly (pure Go).
package usb

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("device not found")

type Bulk interface {
	// Write sends the whole buffer on EP 0x01 or fails within timeout.
	Write(buf []byte, timeout time.Duration) error
	// Read returns one transfer from EP 0x81; (0, nil) on a zero-length packet.
	Read(buf []byte, timeout time.Duration) (int, error)
	Close() error
}

// IsTimeout reports whether err is a read/write timeout (expected while polling replies).
func IsTimeout(err error) bool { return errors.Is(err, ErrTimeout) }

var ErrTimeout = errors.New("usb transfer timed out")
