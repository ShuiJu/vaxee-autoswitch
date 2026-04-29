//go:build !windows

package main

import "errors"

type VaxeeDeviceInfo struct {
	Path         string
	VID          uint16
	PID          uint16
	Manufacturer string
	Product      string
	ContainerID  string
	UsagePage    uint16
	Usage        uint16
	FeatureLen   uint16
}

func EnumerateVaxeeDevices() ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID enumeration is only supported on Windows")
}

func FindOneVaxeeDevice() (VaxeeDeviceInfo, error) {
	return VaxeeDeviceInfo{}, errors.New("HID enumeration is only supported on Windows")
}

func SelectAllVaxeeControlPaths() ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID enumeration is only supported on Windows")
}

func SelectAllVaxeeControlPathsFrom(devs []VaxeeDeviceInfo) ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID enumeration is only supported on Windows")
}

func VaxeeDeviceSetSignature(devs []VaxeeDeviceInfo) string {
	return ""
}

func VaxeePhysicalDeviceNames(devs []VaxeeDeviceInfo, aliases map[string]string) []string {
	return nil
}

func ApplyVaxeeSetting(path string, perf PerfMode, poll PollingRate) error {
	return errors.New("HID feature report is only supported on Windows")
}

func ApplyVaxeeTrajectory(path string, mode TrajectoryMode) error {
	return errors.New("HID feature report is only supported on Windows")
}

func ApplyVaxeeProfileToAll(devs []VaxeeDeviceInfo, perf PerfMode, poll PollingRate, traj TrajectoryMode) ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID feature report is only supported on Windows")
}

func EnumerateAllHidDevices() ([]VaxeeDeviceInfo, error) {
	return nil, errors.New("HID enumeration is only supported on Windows")
}
