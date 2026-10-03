package usb

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GUID_DEVINTERFACE_USB_DEVICE: every USB device exposes it; WinUSB opens it when winusb.sys is the function driver.
var guidUSBDevice = windows.GUID{Data1: 0xA5DCBF10, Data2: 0x6530, Data3: 0x11D2, Data4: [8]byte{0x90, 0x1F, 0x00, 0xC0, 0x4F, 0xB9, 0x51, 0xED}}

var (
	winusb          = windows.NewLazySystemDLL("winusb.dll")
	procInitialize  = winusb.NewProc("WinUsb_Initialize")
	procFree        = winusb.NewProc("WinUsb_Free")
	procWritePipe   = winusb.NewProc("WinUsb_WritePipe")
	procReadPipe    = winusb.NewProc("WinUsb_ReadPipe")
	procSetPipePol  = winusb.NewProc("WinUsb_SetPipePolicy")
	procAbortPipe   = winusb.NewProc("WinUsb_AbortPipe")
)

const pipeTransferTimeout = 0x03

type winBulk struct {
	file windows.Handle
	h    uintptr
	tout map[byte]time.Duration
}

func Open(vid, pid uint16) (Bulk, error) {
	paths, err := windows.CM_Get_Device_Interface_List("", &guidUSBDevice, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, fmt.Errorf("enumerate USB devices: %w", err)
	}
	want := fmt.Sprintf("vid_%04x&pid_%04x", vid, pid)
	for _, p := range paths {
		if !strings.Contains(strings.ToLower(p), want) {
			continue
		}
		name, err := windows.UTF16PtrFromString(p)
		if err != nil {
			continue
		}
		f, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED, 0)
		if err != nil {
			return nil, fmt.Errorf("open %s (is L-Connect using it?): %w", want, err)
		}
		var h uintptr
		if r, _, e := procInitialize.Call(uintptr(f), uintptr(unsafe.Pointer(&h))); r == 0 {
			windows.CloseHandle(f)
			return nil, fmt.Errorf("WinUSB is not the driver for %s: %w", want, e)
		}
		return &winBulk{file: f, h: h, tout: map[byte]time.Duration{}}, nil
	}
	return nil, ErrNotFound
}

func (b *winBulk) setTimeout(pipe byte, d time.Duration) {
	if b.tout[pipe] == d {
		return
	}
	ms := uint32(d.Milliseconds())
	procSetPipePol.Call(b.h, uintptr(pipe), pipeTransferTimeout, 4, uintptr(unsafe.Pointer(&ms)))
	b.tout[pipe] = d
}

func (b *winBulk) Write(buf []byte, timeout time.Duration) error {
	if len(buf) == 0 {
		return nil
	}
	b.setTimeout(0x01, timeout)
	var n uint32
	if r, _, e := procWritePipe.Call(b.h, 0x01, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)), 0); r == 0 {
		if e == windows.ERROR_SEM_TIMEOUT {
			return ErrTimeout
		}
		return e
	}
	if int(n) != len(buf) {
		return fmt.Errorf("short write %d/%d", n, len(buf))
	}
	return nil
}

func (b *winBulk) Read(buf []byte, timeout time.Duration) (int, error) {
	b.setTimeout(0x81, timeout)
	var n uint32
	if r, _, e := procReadPipe.Call(b.h, 0x81, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)), 0); r == 0 {
		if e == windows.ERROR_SEM_TIMEOUT {
			return 0, ErrTimeout
		}
		return 0, e
	}
	return int(n), nil
}

func (b *winBulk) Close() error {
	procAbortPipe.Call(b.h, 0x01)
	procAbortPipe.Call(b.h, 0x81)
	procFree.Call(b.h)
	return windows.CloseHandle(b.file)
}
