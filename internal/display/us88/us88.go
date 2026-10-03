// Package us88 drives the Lian Li 8.8" Universal Screen (USB 1cbe:a088): DES-headed JPEG frames over bulk EP1.
// Protocol per github.com/sgtaziz/lian-li-linux (winusb/lcd/core.rs, crypto.rs). Nothing is written to device storage.
package us88

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/goriparthi/pitwall/internal/usb"
)

const (
	VendorID   = 0x1cbe
	ProductID  = 0xa088
	Width      = 480 // native portrait
	Height     = 1920
	MaxPayload = 512_000

	cmdGetVer     = 0x0a
	cmdBrightness = 0x0e
	cmdFrameRate  = 0x0f
	cmdSetClock   = 0x33
	cmdStopClock  = 0x34
	cmdPushJPG    = 0x65
	cmdQueryBlock = 0x7a
	cmdStopPlay   = 0x7b
)

var key = []byte("slv3tuzx")

// Header builds the 512-byte command: 500-byte plaintext [cmd,0,0x1A,0x6D,ts u32le,params] -> DES-CBC/PKCS7 (504 B),
// zero gap, trailer A1 1A.
func Header(cmd byte, params []byte, ts uint32) []byte {
	plain := make([]byte, 504)
	plain[0], plain[2], plain[3] = cmd, 0x1a, 0x6d
	binary.LittleEndian.PutUint32(plain[4:8], ts)
	copy(plain[8:500], params)
	for i := 500; i < 504; i++ {
		plain[i] = 4 // PKCS7 padding of 500 -> 504
	}
	block, _ := des.NewCipher(key)
	out := make([]byte, 512)
	cipher.NewCBCEncrypter(block, key).CryptBlocks(out[:504], plain)
	out[510], out[511] = 0xa1, 0x1a
	return out
}

type Screen struct {
	bulk     usb.Bulk
	mu       sync.Mutex // one command or frame on the wire at a time
	t0       time.Time
	lastTS   uint32
	replies  chan []byte
	stop     chan struct{}
	done     chan struct{}
	Firmware string
}

func Open() (*Screen, error) {
	b, err := usb.Open(VendorID, ProductID)
	if err != nil {
		return nil, err
	}
	s := &Screen{bulk: b, t0: time.Now(), replies: make(chan []byte, 16), stop: make(chan struct{}), done: make(chan struct{})}
	go s.readLoop()
	if err := s.init(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// readLoop keeps one read pending; zero-length packets are dropped and replies are matched by opcode later.
func (s *Screen) readLoop() {
	defer close(s.done)
	buf := make([]byte, 512)
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		n, err := s.bulk.Read(buf, 250*time.Millisecond)
		if err != nil && !usb.IsTimeout(err) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if n > 0 {
			select {
			case s.replies <- append([]byte(nil), buf[:n]...):
			default: // nobody waiting; stale reply
			}
		}
	}
}

func (s *Screen) ts() uint32 {
	t := uint32(time.Since(s.t0).Milliseconds())
	if t <= s.lastTS {
		t = s.lastTS + 1
	}
	s.lastTS = t
	return t
}

// await returns the next reply echoing cmd in byte 0, discarding stale ones; nil on timeout.
func (s *Screen) await(cmd byte, d time.Duration) []byte {
	deadline := time.After(d)
	for {
		select {
		case r := <-s.replies:
			if r[0] == cmd {
				return r
			}
		case <-deadline:
			return nil
		}
	}
}

func (s *Screen) drain() {
	for {
		select {
		case <-s.replies:
		default:
			return
		}
	}
}

func (s *Screen) command(cmd byte, params []byte) ([]byte, error) {
	s.drain()
	if err := s.bulk.Write(Header(cmd, params, s.ts()), 2*time.Second); err != nil {
		return nil, err
	}
	return s.await(cmd, 2*time.Second), nil
}

// init mirrors slv3.rs do_init: stop playback, firmware, frame rate, clock sync then stop clock.
func (s *Screen) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.command(cmdStopPlay, nil); err != nil {
		return fmt.Errorf("screen did not accept commands: %w", err)
	}
	if r, _ := s.command(cmdGetVer, nil); len(r) > 8 {
		fw := r[8:min(40, len(r))]
		if i := bytes.IndexByte(fw, 0); i >= 0 {
			fw = fw[:i]
		}
		s.Firmware = string(fw)
	}
	_, _ = s.command(cmdFrameRate, []byte{30})
	n := time.Now()
	y := n.Year()
	_, _ = s.command(cmdSetClock, []byte{byte(y >> 8), byte(y), byte(n.Month()), byte(n.Day()), byte(n.Hour()), byte(n.Minute()), byte(n.Second()), 2})
	_, err := s.command(cmdStopClock, []byte{0})
	return err
}

func (s *Screen) SetBrightness(pct int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.command(cmdBrightness, []byte{byte(max(0, min(100, pct)))})
	return err
}

// PushJPEG sends one native-orientation (480x1920) JPEG; waits for the panel buffer when it reports > 3.
func (s *Screen) PushJPEG(jpeg []byte) (int, error) {
	if len(jpeg) > MaxPayload {
		return 0, fmt.Errorf("frame %d B exceeds %d B", len(jpeg), MaxPayload)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(jpeg)))
	s.drain()
	if err := s.bulk.Write(append(Header(cmdPushJPG, size, s.ts()), jpeg...), 4*time.Second); err != nil {
		return 0, err
	}
	level := -1
	if r := s.await(cmdPushJPG, 2*time.Second); len(r) > 8 {
		level = int(r[8])
	}
	for i := 0; level > 3 && i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		r, err := s.command(cmdQueryBlock, nil)
		if err != nil {
			return level, err
		}
		if len(r) > 8 {
			level = int(r[8])
		} else {
			break
		}
	}
	return level, nil
}

// Close waits for any in-flight frame (so the panel is never left mid-JPEG), then releases the device.
func (s *Screen) Close() error {
	locked := make(chan struct{})
	go func() { s.mu.Lock(); close(locked) }()
	select {
	case <-locked:
		defer s.mu.Unlock()
	case <-time.After(5 * time.Second):
		return errors.New("frame still in flight after 5 s; leaving device open")
	}
	close(s.stop)
	<-s.done
	return s.bulk.Close()
}
