package tray

/*
#cgo LDFLAGS: -framework Carbon
#include <Carbon/Carbon.h>

extern void goHotkey(int id);

static OSStatus hkHandler(EventHandlerCallRef next, EventRef ev, void *data) {
	EventHotKeyID hk;
	GetEventParameter(ev, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof(hk), NULL, &hk);
	goHotkey((int)hk.id);
	return noErr;
}

static int installHandler(void) {
	EventTypeSpec spec = { kEventClassKeyboard, kEventHotKeyPressed };
	return (int)InstallApplicationEventHandler(&hkHandler, 1, &spec, NULL, NULL);
}

// Control+Option, signature 'PTWL'. Returns 0 on success.
static int registerKey(int keycode, int id) {
	EventHotKeyID hk = { 'PTWL', (UInt32)id };
	EventHotKeyRef ref;
	return (int)RegisterEventHotKey((UInt32)keycode, controlKey | optionKey, hk, GetApplicationEventTarget(), 0, &ref);
}
*/
import "C"

import "fmt"

// macOS virtual key codes (kVK_ANSI_*) for the keys we bind.
var macKeycodes = map[Key]int{
	'F': 0x03, 'H': 0x04, 'R': 0x0F, 'K': 0x28, 'D': 0x02,
	'1': 0x12, '2': 0x13, '3': 0x14, '4': 0x15, '5': 0x17, '6': 0x16, '7': 0x1A, '8': 0x1C, '9': 0x19,
}

//export goHotkey
func goHotkey(id C.int) {
	if fn := hotkeyHandlers[int(id)]; fn != nil {
		go fn()
	}
}

// registerHotkeys must run on the main thread after the app run loop exists (systray onReady).
func registerHotkeys(bind map[Key]func()) []string {
	var failed []string
	if C.installHandler() != 0 {
		return []string{"handler"}
	}
	id := 1
	for k, fn := range bind {
		code, ok := macKeycodes[k]
		if !ok {
			continue
		}
		if C.registerKey(C.int(code), C.int(id)) != 0 {
			failed = append(failed, fmt.Sprintf("⌃⌥%c", k))
			continue
		}
		hotkeyHandlers[id] = fn
		id++
	}
	return failed
}

const HotkeyLabel = "⌃⌥"
