//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"
)

type VaxeeBatteryInfo struct {
	Name     string
	Percent  int
	Charging bool
	OK       bool
}

const vaxeeBattDebug = false // 若仍异常可设 true 打印页回包头部

// 查询页：0e a5 page 01 01 00 ...（抓包显示这种“01 01 00”结构会触发 GET_REPORT 返回）[1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)
func buildQueryPageReportSized(total int, page byte) []byte {
	if total < 6 {
		total = 64
	}
	buf := make([]byte, total)
	buf[0] = 0x0e
	buf[1] = 0xa5
	buf[2] = page
	buf[3] = 0x01
	buf[4] = 0x01
	buf[5] = 0x00
	return buf
}

func vaxeeReadPage(dev VaxeeDeviceInfo, page byte) ([]byte, bool) {
	flen := int(dev.FeatureLen)
	if flen <= 0 {
		flen = 64
	}

	// SET_REPORT（Feature）
	if err := sendFeatureReport(dev.Path, buildQueryPageReportSized(flen, page)); err != nil {
		return nil, false
	}
	time.Sleep(10 * time.Millisecond)

	// GET_REPORT（Feature）
	buf, err := getFeature(dev.Path, 0x0e, flen)
	if err != nil || len(buf) < 6 {
		return nil, false
	}

	// 严格校验头：0e a5 page
	if buf[0] == 0x0e && buf[1] == 0xa5 && buf[2] == page {
		return buf, true
	}
	return nil, false
}

// 电量解析：优先按“byte5*5”的离散段规则；若不合理则 fallback 寻找 0..100 的候选
func parsePercentFromPage(buf []byte) int {
	if len(buf) < 6 {
		return -1
	}
	// 常见段值：0..20
	raw := int(buf[5])
	pct := raw * 5
	if pct >= 0 && pct <= 100 {
		return pct
	}
	// fallback：寻找像百分比的字节
	for _, v := range buf {
		if v <= 100 {
			// 忽略 0（避免充电时误判 0%）
			if v != 0 {
				return int(v)
			}
		}
	}
	return -1
}

func ReadBatteryVAXEE(dev VaxeeDeviceInfo) (percent int, charging bool, ok bool) {
	if dev.Path == "" {
		return 0, false, false
	}

	// page 0x0B：电量
	var b0b []byte
	for i := 0; i < 6; i++ {
		if buf, ok := vaxeeReadPage(dev, 0x0b); ok {
			b0b = buf
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if len(b0b) < 6 {
		return 0, false, false
	}
	pct := parsePercentFromPage(b0b)
	if pct < 0 {
		pct = 0
	}
	percent = pct

	// page 0x10：充电标志（尽量使用 byte5==1）
	var b10 []byte
	for i := 0; i < 6; i++ {
		if buf, ok := vaxeeReadPage(dev, 0x10); ok {
			b10 = buf
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	charging = len(b10) >= 6 && b10[5] == 0x01

	if vaxeeBattDebug {
		fmt.Printf("[BATDBG] 0B head=% x\n", b0b[:min(24, len(b0b))])
		fmt.Printf("[BATDBG] 10 head=% x\n", b10[:min(24, len(b10))])
	}

	return percent, charging, true
}

func BatteryStatusTextVAXEE() string {
	line, _, _ := BatteryStatusLinesVAXEE()
	return line
}

func BatteryStatusLinesVAXEE() (summary string, extrema string, tooltip string) {
	devs, err := SelectAllVaxeeControlPaths()
	if err != nil && len(devs) == 0 {
		return "电池电量统计: N/A", "最高电量设备: N/A | 最低电量设备: N/A", "暂无可用电量信息"
	}
	return BatteryStatusLinesForDevicesVAXEE(devs)
}

func BatteryStatusTextForDevicesVAXEE(devs []VaxeeDeviceInfo) string {
	summary, _, _ := BatteryStatusLinesForDevicesVAXEE(devs)
	return summary
}

func BatteryStatusLinesForDevicesVAXEE(devs []VaxeeDeviceInfo) (summary string, extrema string, tooltip string) {
	if len(devs) == 0 {
		return "电池电量统计: N/A", "最高电量设备: N/A | 最低电量设备: N/A", "暂无可用电量信息"
	}

	infos := ReadBatteryDetailsVAXEE(devs)
	tipParts := make([]string, 0, len(infos))
	var maxInfo VaxeeBatteryInfo
	var minInfo VaxeeBatteryInfo
	found := false

	for _, info := range infos {
		if !info.OK {
			tipParts = append(tipParts, fmt.Sprintf("%s现在是 N/A", info.Name))
			continue
		}
		tipParts = append(tipParts, fmt.Sprintf("%s现在是%d%%", info.Name, info.Percent))

		if !found || info.Percent > maxInfo.Percent {
			maxInfo = info
		}
		if !found || info.Percent < minInfo.Percent {
			minInfo = info
		}
		found = true
	}

	if !found {
		extrema = "最高电量设备: N/A | 最低电量设备: N/A"
	} else {
		extrema = fmt.Sprintf("最高电量设备: %d%% (%s) | 最低电量设备: %d%% (%s)",
			maxInfo.Percent, batteryStateText(maxInfo.Charging),
			minInfo.Percent, batteryStateText(minInfo.Charging))
	}
	return "电池电量统计:", extrema, strings.Join(tipParts, "\r\n")
}

func ReadBatteryDetailsVAXEE(devs []VaxeeDeviceInfo) []VaxeeBatteryInfo {
	infos := make([]VaxeeBatteryInfo, 0, len(devs))
	for i, dev := range devs {
		pct, chg, ok := ReadBatteryVAXEE(dev)
		infos = append(infos, VaxeeBatteryInfo{
			Name:     vaxeeBatteryDeviceName(dev, i),
			Percent:  pct,
			Charging: chg,
			OK:       ok,
		})
	}
	return infos
}

func batteryStateText(charging bool) string {
	if charging {
		return "充电中"
	}
	return "未充电"
}

func vaxeeBatteryDeviceName(dev VaxeeDeviceInfo, idx int) string {
	name := strings.TrimSpace(dev.Product)
	if name == "" {
		name = strings.TrimSpace(dev.Manufacturer)
	}
	if name == "" {
		return fmt.Sprintf("D%d", idx+1)
	}

	name = strings.TrimSpace(stripLeadingFold(name, "VAXEE"))
	name = strings.TrimSpace(stripLeadingFold(name, "ZYGEN"))
	name = strings.NewReplacer(
		"NP-01S", "NP01S",
		"NP-01", "NP01",
		"np-01s", "NP01S",
		"np-01", "NP01",
	).Replace(name)
	if name == "" {
		return fmt.Sprintf("D%d", idx+1)
	}
	return name
}

func stripLeadingFold(s string, prefix string) string {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s
	}
	return strings.TrimSpace(s[len(prefix):])
}

func PrintBatteryVAXEE(dev VaxeeDeviceInfo) {
	pct, chg, ok := ReadBatteryVAXEE(dev)
	if !ok {
		fmt.Printf("🔋 VAXEE Battery: N/A\n")
		return
	}
	state := "discharging"
	if chg {
		state = "charging"
	}
	fmt.Printf("🔋 VAXEE Battery: %d%% (%s)\n", pct, state)
}

func PrintBatteryAllVAXEE(devs []VaxeeDeviceInfo) {
	if len(devs) == 0 {
		fmt.Printf("[BAT] VAXEE Battery: N/A\n")
		return
	}
	for i, dev := range devs {
		pct, chg, ok := ReadBatteryVAXEE(dev)
		if !ok {
			fmt.Printf("[BAT] VAXEE #%d Battery: N/A\n", i+1)
			continue
		}
		state := "discharging"
		if chg {
			state = "charging"
		}
		fmt.Printf("[BAT] VAXEE #%d Battery: %d%% (%s)\n", i+1, pct, state)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
