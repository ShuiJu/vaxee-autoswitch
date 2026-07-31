//go:build !windows

package main

// MessageBeep sound type constants kept as plain values for portability.
// Renamed with beep prefix to mirror the windows build (avoid clashing with
// MessageBox mb* constants defined elsewhere).
const (
	beepOK              uintptr = 0x00000000
	beepIconHand        uintptr = 0x00000010
	beepIconQuestion    uintptr = 0x00000020
	beepIconExclamation uintptr = 0x00000030
	beepIconAsterisk    uintptr = 0x00000040
)

// playSystemBeep is a no-op on non-Windows platforms.
func playSystemBeep(sound uintptr) {}