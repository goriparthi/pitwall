package tray

// Key is a letter or digit bound with Control+Option (macOS) or Control+Alt (Windows).
type Key rune

var hotkeyHandlers = map[int]func(){}
