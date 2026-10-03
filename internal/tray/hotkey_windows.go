package tray

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procRegister   = user32.NewProc("RegisterHotKey")
	procGetMessage = user32.NewProc("GetMessageW")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modNoRepeat = 0x4000
	wmHotkey    = 0x0312
)

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// registerHotkeys binds Ctrl+Alt+key on a dedicated OS thread that owns the message loop.
func registerHotkeys(bind map[Key]func()) []string {
	result := make(chan []string, 1)
	go func() {
		runtime.LockOSThread()
		var failed []string
		id := 1
		for k, fn := range bind {
			if r, _, _ := procRegister.Call(0, uintptr(id), modControl|modAlt|modNoRepeat, uintptr(k)); r == 0 {
				failed = append(failed, fmt.Sprintf("Ctrl+Alt+%c", k))
				continue
			}
			hotkeyHandlers[id] = fn
			id++
		}
		result <- failed
		var m msg
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			if m.message == wmHotkey {
				if fn := hotkeyHandlers[int(m.wParam)]; fn != nil {
					go fn()
				}
			}
		}
	}()
	return <-result
}

const HotkeyLabel = "Ctrl+Alt+"
