//go:build ignore

package main

import (
	"fmt"
	"sort"
	"strings"
)

func main() {
	devs, err := EnumerateVaxeeDevices()
	if err != nil {
		fmt.Printf("EnumerateVaxeeDevices failed: %v\n", err)
		return
	}

	fmt.Printf("VAXEE logical HID interfaces: %d\n", len(devs))
	fmt.Printf("VAXEE physical devices: %d\n\n", CountUniqueVaxeeDevices(devs))
	fmt.Printf("Built-in display names: %s\n\n", strings.Join(VaxeePhysicalDeviceNames(devs, nil), ", "))

	groups := map[string][]VaxeeDeviceInfo{}
	var keys []string
	for _, dev := range devs {
		key := vaxeeDeviceKey(dev)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], dev)
	}
	sort.Strings(keys)

	controls, controlErr := SelectAllVaxeeControlPathsFrom(devs)
	controlByKey := map[string]VaxeeDeviceInfo{}
	for _, dev := range controls {
		controlByKey[vaxeeDeviceKey(dev)] = dev
	}

	for i, key := range keys {
		items := groups[key]
		control, hasControl := controlByKey[key]

		fmt.Printf("=== Physical device #%d ===\n", i+1)
		fmt.Printf("GroupKey: %s\n", key)
		if len(items) > 0 {
			first := items[0]
			fmt.Printf("Manufacturer: %q\n", first.Manufacturer)
			fmt.Printf("Product:      %q\n", first.Product)
			fmt.Printf("VID/PID:      0x%04x/0x%04x\n", first.VID, first.PID)
			fmt.Printf("ContainerID:  %s\n", first.ContainerID)
			fmt.Printf("NameGuess:    %s\n", vaxeeBatteryDeviceName(first, i))
		}

		if hasControl {
			pct, charging, ok := ReadBatteryVAXEE(control)
			fmt.Printf("ControlPath:  %s\n", control.Path)
			fmt.Printf("ControlUsage: page=0x%04x usage=0x%04x featureLen=%d\n", control.UsagePage, control.Usage, control.FeatureLen)
			if ok {
				fmt.Printf("Battery:      %d%% (%s)\n", pct, batteryStateText(charging))
			} else {
				fmt.Printf("Battery:      N/A\n")
			}
		} else {
			fmt.Printf("ControlPath:  N/A\n")
		}

		fmt.Printf("Logical interfaces (%d):\n", len(items))
		for j, dev := range items {
			fmt.Printf("  #%02d usagePage=0x%04x usage=0x%04x featureLen=%d product=%q path=%s\n",
				j+1, dev.UsagePage, dev.Usage, dev.FeatureLen, dev.Product, compactHIDPath(dev.Path))
		}
		fmt.Println()
	}

	if controlErr != nil {
		fmt.Printf("Control selection warning: %v\n", controlErr)
	}
}

func compactHIDPath(path string) string {
	parts := strings.Split(path, "#")
	if len(parts) <= 2 {
		return path
	}
	return strings.Join(parts[:len(parts)-1], "#")
}