//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32FG = syscall.NewLazyDLL("user32.dll")
	k32FG    = syscall.NewLazyDLL("kernel32.dll")

	procGetForegroundWindowFG      = user32FG.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessIdFG = user32FG.NewProc("GetWindowThreadProcessId")
	procOpenProcessFG              = k32FG.NewProc("OpenProcess")
	procCloseHandleFG              = k32FG.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = k32FG.NewProc("QueryFullProcessImageNameW")
)

const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

func ForegroundProcessName() (string, error) {
	hwnd, _, _ := procGetForegroundWindowFG.Call()
	if hwnd == 0 {
		return "", syscall.EINVAL
	}

	var pid uint32
	procGetWindowThreadProcessIdFG.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return "", syscall.EINVAL
	}

	hProc, _, err := procOpenProcessFG.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if hProc == 0 {
		return "", err
	}
	defer procCloseHandleFG.Call(hProc)

	buf := make([]uint16, 4096)
	size := uint32(len(buf))
	r1, _, err := procQueryFullProcessImageNameW.Call(
		hProc,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return "", err
	}

	full := syscall.UTF16ToString(buf[:size])
	base := filepath.Base(full)
	return strings.ToLower(base), nil
}
