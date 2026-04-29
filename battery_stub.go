//go:build !windows

package main

func BatteryStatusTextVAXEE() string {
	return "Battery: N/A"
}

func BatteryStatusLinesVAXEE() (summary string, extrema string, tooltip string) {
	return "电池电量统计: N/A", "最高电量设备: N/A | 最低电量设备: N/A", "暂无可用电量信息"
}

func PrintBatteryVAXEE(dev VaxeeDeviceInfo) {
}

func PrintBatteryAllVAXEE(devs []VaxeeDeviceInfo) {
}
