//go:build windows

package main

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"unsafe"
)

//go:embed vaxee-icon.ico
var embeddedAppIconICO []byte

var (
	procCreateIconFromResourceEx = user32GUI.NewProc("CreateIconFromResourceEx")
	procDestroyIconHandle        = user32GUI.NewProc("DestroyIcon")
	procGetSystemMetrics         = user32GUI.NewProc("GetSystemMetrics")
)

const (
	iconResourceVersion = 0x00030000

	smCxIcon      = 11
	smCyIcon      = 12
	smCxSmallIcon = 49
	smCySmallIcon = 50
)

func loadEmbeddedAppIcons() (uintptr, uintptr, error) {
	largeW := getSystemMetric(smCxIcon, 32)
	largeH := getSystemMetric(smCyIcon, 32)
	smallW := getSystemMetric(smCxSmallIcon, 16)
	smallH := getSystemMetric(smCySmallIcon, 16)

	largeIcon, err := createIconFromICO(embeddedAppIconICO, largeW, largeH)
	if err != nil {
		return 0, 0, err
	}

	smallIcon, err := createIconFromICO(embeddedAppIconICO, smallW, smallH)
	if err != nil {
		destroyIconHandle(largeIcon)
		return 0, 0, err
	}

	return largeIcon, smallIcon, nil
}

func destroyIconHandle(icon uintptr) {
	if icon != 0 {
		procDestroyIconHandle.Call(icon)
	}
}

func getSystemMetric(metric int32, fallback int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(metric))
	if int32(r) <= 0 {
		return fallback
	}
	return int32(r)
}

func createIconFromICO(data []byte, width int32, height int32) (uintptr, error) {
	if len(data) < 6 {
		return 0, fmt.Errorf("invalid embedded icon: header too small")
	}

	if binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return 0, fmt.Errorf("invalid embedded icon: bad ico header")
	}

	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 {
		return 0, fmt.Errorf("invalid embedded icon: no image entries")
	}

	bestOffset := -1
	bestSize := 0
	bestScore := int(^uint(0) >> 1)
	bestPixels := 0

	for i := 0; i < count; i++ {
		entryOffset := 6 + i*16
		if entryOffset+16 > len(data) {
			break
		}

		entryWidth := int32(data[entryOffset])
		if entryWidth == 0 {
			entryWidth = 256
		}

		entryHeight := int32(data[entryOffset+1])
		if entryHeight == 0 {
			entryHeight = 256
		}

		bytesInRes := int(binary.LittleEndian.Uint32(data[entryOffset+8 : entryOffset+12]))
		imageOffset := int(binary.LittleEndian.Uint32(data[entryOffset+12 : entryOffset+16]))
		if bytesInRes <= 0 || imageOffset <= 0 || imageOffset+bytesInRes > len(data) {
			continue
		}

		score := abs32(entryWidth-width) + abs32(entryHeight-height)
		pixels := int(entryWidth * entryHeight)
		if score < bestScore || (score == bestScore && pixels > bestPixels) {
			bestOffset = imageOffset
			bestSize = bytesInRes
			bestScore = score
			bestPixels = pixels
		}
	}

	if bestOffset < 0 || bestSize == 0 {
		return 0, fmt.Errorf("invalid embedded icon: no usable image entry")
	}

	r, _, err := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&data[bestOffset])),
		uintptr(bestSize),
		1,
		iconResourceVersion,
		uintptr(width),
		uintptr(height),
		0,
	)
	if r == 0 {
		return 0, err
	}

	return r, nil
}

func abs32(v int32) int {
	if v < 0 {
		return int(-v)
	}
	return int(v)
}
