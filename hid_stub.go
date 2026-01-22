//go:build !windows

package main

import "errors"

type VaxeeDeviceInfo struct {
	Path         string
	VID          uint16
	PID          uint16
	Manufacturer string
	Product      string
}

func EnumerateVaxeeDevices() ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID enumeration is only supported on Windows")
}

func FindOneVaxeeDevice() (VaxeeDeviceInfo, error) {
	return VaxeeDeviceInfo{}, errors.New("HID enumeration is only supported on Windows")
}

func ApplyVaxeeSetting(path string, perf PerfMode, poll PollingRate) error {
	return errors.New("HID feature report is only supported on Windows")
}

func EnumerateAllHidDevices() ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID enumeration is only supported on Windows")
}
