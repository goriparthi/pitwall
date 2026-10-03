package usb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/gousb"
)

var (
	ctxOnce sync.Once
	usbCtx  *gousb.Context
)

type darwinBulk struct {
	dev  *gousb.Device
	cfg  *gousb.Config
	intf *gousb.Interface
	out  *gousb.OutEndpoint
	in   *gousb.InEndpoint
}

func Open(vid, pid uint16) (Bulk, error) {
	ctxOnce.Do(func() { usbCtx = gousb.NewContext() })
	dev, err := usbCtx.OpenDeviceWithVIDPID(gousb.ID(vid), gousb.ID(pid))
	if err != nil {
		return nil, fmt.Errorf("open %04x:%04x: %w", vid, pid, err)
	}
	if dev == nil {
		return nil, ErrNotFound
	}
	// no SetAutoDetach: no kernel driver binds these vendor-class devices, and detaching needs an entitlement on macOS
	cfg, err := dev.Config(1)
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("select configuration: %w", err)
	}
	intf, err := cfg.Interface(0, 0)
	if err != nil {
		cfg.Close()
		dev.Close()
		return nil, fmt.Errorf("claim interface: %w", err)
	}
	out, err1 := intf.OutEndpoint(1)
	in, err2 := intf.InEndpoint(1)
	if err := errors.Join(err1, err2); err != nil {
		intf.Close()
		cfg.Close()
		dev.Close()
		return nil, err
	}
	return &darwinBulk{dev, cfg, intf, out, in}, nil
}

func (b *darwinBulk) Write(buf []byte, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	n, err := b.out.WriteContext(ctx, buf)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("short write %d/%d", n, len(buf))
	}
	return nil
}

func (b *darwinBulk) Read(buf []byte, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	n, err := b.in.ReadContext(ctx, buf)
	if err != nil && ctx.Err() != nil {
		return n, ErrTimeout
	}
	return n, err
}

func (b *darwinBulk) Close() error {
	b.intf.Close()
	err := b.cfg.Close()
	return errors.Join(err, b.dev.Close())
}
