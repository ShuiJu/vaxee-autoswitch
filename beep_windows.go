//go:build windows

package main

import (
	"syscall"
)

var (
	user32Beep      = syscall.NewLazyDLL("user32.dll")
	procMessageBeep = user32Beep.NewProc("MessageBeep")
)

// MessageBeep sound type constants (Windows standard system sounds).
// Renamed with beep prefix to avoid clashing with MessageBox mb* constants
// defined in gui_tray_windows.go.
// https://learn.microsoft.com/windows/win32/api/winuser/nf-winuser-messagebeep
const (
	beepOK              uintptr = 0x00000000
	beepIconHand        uintptr = 0x00000010
	beepIconQuestion    uintptr = 0x00000020
	beepIconExclamation uintptr = 0x00000030
	beepIconAsterisk    uintptr = 0x00000040
)

// playSystemBeep plays a built-in Windows system sound (no file path needed).
// Use beepOK for a generic success/notification chime available on every Windows.
func playSystemBeep(sound uintptr) {
	procMessageBeep.Call(sound)
}