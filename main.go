//go:build legacyconsole

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Applied 记录当前应用的设置
type Applied struct {
	perf PerfMode
	poll PollingRate
	traj TrajectoryMode
	ok   bool
}

// Windows API 相关常量和变量
var (
	kernel32DLL = syscall.NewLazyDLL("kernel32.dll")
	// Windows API 函数
	procGetCurrentProcess     = kernel32DLL.NewProc("GetCurrentProcess")
	procGetCurrentThread      = kernel32DLL.NewProc("GetCurrentThread")
	procSetPriorityClass      = kernel32DLL.NewProc("SetPriorityClass")
	procSetThreadPriority     = kernel32DLL.NewProc("SetThreadPriority")
	procSetProcessInformation = kernel32DLL.NewProc("SetProcessInformation")
	procSetThreadInformation  = kernel32DLL.NewProc("SetThreadInformation")
)

// Windows 优先级常量
const (
	// SetPriorityClass dwPriorityClass
	IDLE_PRIORITY_CLASS           = 0x00000040
	BELOW_NORMAL_PRIORITY_CLASS   = 0x00004000
	PROCESS_MODE_BACKGROUND_BEGIN = 0x00100000

	// SetThreadPriority nPriority
	THREAD_PRIORITY_LOWEST       = -2
	THREAD_PRIORITY_IDLE         = -15
	THREAD_MODE_BACKGROUND_BEGIN = 0x00010000

	// SetProcessInformation ProcessInformationClass
	ProcessPowerThrottling = 4
	// SetThreadInformation ThreadInformationClass
	ThreadPowerThrottling = 5

	// PROCESS/THREAD_POWER_THROTTLING_STATE
	PROCESS_POWER_THROTTLING_CURRENT_VERSION = 1
	PROCESS_POWER_THROTTLING_EXECUTION_SPEED = 0x1

	THREAD_POWER_THROTTLING_CURRENT_VERSION = 1
	THREAD_POWER_THROTTLING_EXECUTION_SPEED = 0x1
)

// Windows 结构体定义
type PROCESS_POWER_THROTTLING_STATE struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}
type THREAD_POWER_THROTTLING_STATE struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}

// ==================== 工具函数 ====================

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func u32ptrFromI32(v int32) uintptr {
	return uintptr(uint32(v))
}

// ==================== 打印函数 ====================

func printBanner(cfgPath string) {
	log.Printf("========================================")
	log.Printf(" VAXEE AutoSwitch (Console)")
	log.Printf(" Config: %s", cfgPath)
	log.Printf("========================================")
}

func printConfig(cfg *Config) {
	log.Printf("[CFG] interval=%s", cfg.Interval)
	log.Printf("[CFG] hit    : mode=%s poll=%dHz traj=%s", perfName(cfg.HitMode), cfg.HitPoll, trajName(cfg.HitTraj))
	log.Printf("[CFG] default: mode=%s poll=%dHz traj=%s", perfName(cfg.DefaultMode), cfg.DefaultPoll, trajName(cfg.DefaultTraj))
	log.Printf("[CFG] whitelist(%d): %s", len(cfg.Whitelist), strings.Join(cfg.Whitelist, ", "))
}

func waitForever() {
	log.Printf("按 Ctrl+C 退出。")
	select {}
}

// ==================== Windows 优先级设置 ====================

func setLowPriorityDefaults(enableBackgroundMode bool, enableEcoQoS bool) {
	hProc, _, _ := procGetCurrentProcess.Call()
	hThread, _, _ := procGetCurrentThread.Call()

	if r, _, e := procSetPriorityClass.Call(hProc, uintptr(BELOW_NORMAL_PRIORITY_CLASS)); r == 0 {
		log.Printf("[PRIO] SetPriorityClass(BELOW_NORMAL) failed: %v", e)
	} else {
		log.Printf("[PRIO] Process priority set to BELOW_NORMAL.")
	}

	if r, _, e := procSetThreadPriority.Call(hThread, uintptr(u32ptrFromI32(THREAD_PRIORITY_LOWEST))); r == 0 {
		log.Printf("[PRIO] SetThreadPriority(LOWEST) failed: %v", e)
	} else {
		log.Printf("[PRIO] Thread priority set to LOWEST.")
	}

	if enableBackgroundMode {
		if r, _, e := procSetPriorityClass.Call(hProc, uintptr(PROCESS_MODE_BACKGROUND_BEGIN)); r == 0 {
			log.Printf("[PRIO] PROCESS_MODE_BACKGROUND_BEGIN failed: %v", e)
		} else {
			log.Printf("[PRIO] Process background mode enabled.")
		}
		if r, _, e := procSetThreadPriority.Call(hThread, uintptr(THREAD_MODE_BACKGROUND_BEGIN)); r == 0 {
			log.Printf("[PRIO] THREAD_MODE_BACKGROUND_BEGIN failed: %v", e)
		} else {
			log.Printf("[PRIO] Thread background mode enabled.")
		}
	}

	if enableEcoQoS {
		setProcessPowerThrottling(hProc)
		setThreadPowerThrottling(hThread)
	}
}

func setProcessPowerThrottling(hProc uintptr) {
	state := PROCESS_POWER_THROTTLING_STATE{
		Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
	}
	r, _, e := procSetProcessInformation.Call(
		hProc,
		uintptr(ProcessPowerThrottling),
		uintptr(unsafe.Pointer(&state)),
		unsafe.Sizeof(state),
	)
	if r == 0 {
		log.Printf("[PRIO] Process EcoQoS/PowerThrottling failed: %v", e)
	} else {
		log.Printf("[PRIO] Process EcoQoS/PowerThrottling enabled.")
	}
}

func setThreadPowerThrottling(hThread uintptr) {
	state := THREAD_POWER_THROTTLING_STATE{
		Version:     THREAD_POWER_THROTTLING_CURRENT_VERSION,
		ControlMask: THREAD_POWER_THROTTLING_EXECUTION_SPEED,
		StateMask:   THREAD_POWER_THROTTLING_EXECUTION_SPEED,
	}
	_, _, _ = procSetThreadInformation.Call(
		hThread,
		uintptr(ThreadPowerThrottling),
		uintptr(unsafe.Pointer(&state)),
		unsafe.Sizeof(state),
	)
}

// ==================== 主逻辑函数 ====================

func tickOnce(cfg *Config, last *Applied) (switchMsg string, errStr string) {
	proc, err := ForegroundProcessName()
	if err != nil {
		return "", ""
	}
	proc = strings.ToLower(filepath.Base(proc))

	_, hit := cfg.WhitelistSet[proc]

	wantPerf := cfg.DefaultMode
	wantPoll := cfg.DefaultPoll
	wantTraj := cfg.DefaultTraj
	if hit {
		wantPerf = cfg.HitMode
		wantPoll = cfg.HitPoll
		wantTraj = cfg.HitTraj
	}

	if last.ok && last.perf == wantPerf && last.poll == wantPoll && last.traj == wantTraj {
		return "", ""
	}

	dev, findErr := FindOneVaxeeDevice()
	if findErr != nil {
		return "", "未找到可用 VAXEE 设备：" + findErr.Error()
	}

	if err := ApplyVaxeeSetting(dev.Path, wantPerf, wantPoll); err != nil {
		return "", "应用设置失败：" + err.Error()
	}

	// 追踪轨迹
	if err := ApplyVaxeeTrajectory(dev.Path, wantTraj); err != nil {
		return "", "应用追踪轨迹失败：" + err.Error()
	}

	*last = Applied{perf: wantPerf, poll: wantPoll, traj: wantTraj, ok: true}

	// 每次切换成功后打印一次电量
	PrintBatteryVAXEE(dev)

	if hit {
		return fmt.Sprintf("[SWITCH] 命中白名单(%s) -> %s + %dHz + traj=%s",
			proc, perfName(wantPerf), wantPoll, trajName(wantTraj)), ""
	}
	return fmt.Sprintf("[SWITCH] 未命中白名单(%s) -> %s + %dHz + traj=%s",
		proc, perfName(wantPerf), wantPoll, trajName(wantTraj)), ""
}

// ==================== 主函数 ====================

func main() {
	log.SetFlags(log.LstdFlags)

	cfgPath := filepath.Join(exeDir(), configFileName)

	if err := ensureConfigExists(cfgPath); err != nil {
		log.Printf("[ERR] 无法创建配置文件：%v", err)
		log.Printf("程序不会退出（窗口保留）。请检查权限/路径：%s", cfgPath)
		waitForever()
	}

	cfg, modTime, err := loadConfig(cfgPath)
	if err != nil {
		log.Printf("[ERR] 读取配置失败：%v", err)
		log.Printf("程序不会退出（窗口保留）。请修复配置后保存：%s", cfgPath)
		waitForever()
	}

	printBanner(cfgPath)
	printConfig(cfg)

	enumerateDevices()

	setLowPriorityDefaults(true, true)

	log.Printf("开始后台监控：每 %s 检查一次前台进程。", cfg.Interval)

	var last Applied
	var lastErr string

	for {
		reloadConfigIfChanged(cfgPath, &cfg, &modTime)

		switchMsg, errStr, _, _ := tickOnce(cfg, &last)
		if switchMsg != "" {
			log.Print(switchMsg)
		}

		handleError(&lastErr, errStr)

		time.Sleep(cfg.Interval)
	}
}

// ==================== 辅助函数 ====================

func enumerateDevices() {
	infos, enumErr := EnumerateVaxeeDevices()
	if enumErr != nil {
		log.Printf("[DEV] 枚举 HID 设备失败：%v", enumErr)
		return
	}
	if len(infos) == 0 {
		log.Printf("[DEV] 未发现 VAXEE 设备（Manufacturer/Product 不包含 vaxee）。")
		log.Printf("[DEV] 程序将继续运行，每次尝试切换时会重新查找设备。")
		enumerateAllHidDevices()
	} else {
		log.Printf("[DEV] 发现 %d 个 VAXEE HID 设备：", len(infos))
		for i, d := range infos {
			log.Printf(" #%d Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
				i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
		}
	}
}

func enumerateAllHidDevices() {
	all, errAll := EnumerateAllHidDevices()
	if errAll != nil {
		log.Printf("[DEV] 枚举全部 HID 设备失败：%v", errAll)
		return
	}
	log.Printf("[DEV] 系统 HID 设备总数（可读取字符串/属性的接口）：%d", len(all))
	for i, d := range all {
		if d.Manufacturer == "" && d.Product == "" {
			continue
		}
		log.Printf(" [HID #%d] Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
			i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
	}
	log.Printf("[DEV] 提示：如果你在列表里看到了目标鼠标但字符串不含 VAXEE，后续可以改成按 VID/PID 固定匹配。")
}

func reloadConfigIfChanged(cfgPath string, cfg **Config, modTime *time.Time) {
	if fi, e := os.Stat(cfgPath); e == nil && fi.ModTime().After(*modTime) {
		if nc, mt, e2 := loadConfig(cfgPath); e2 == nil {
			*cfg = nc
			*modTime = mt
			log.Printf("[CFG] 检测到配置文件变更，已重新加载。")
			printConfig(*cfg)
		} else {
			log.Printf("[ERR] 配置文件变更但重载失败：%v", e2)
		}
	}
}

func handleError(lastErr *string, errStr string) {
	if errStr != "" && errStr != *lastErr {
		*lastErr = errStr
		log.Printf("[ERR] %s", errStr)
	} else if errStr == "" {
		*lastErr = ""
	}
}

func runConsoleApp() {
	log.SetFlags(log.LstdFlags)

	cfgPath := filepath.Join(exeDir(), configFileName)

	app, err := NewAutoSwitchApp(cfgPath)
	if err != nil {
		log.Printf("[ERR] %v", err)
		waitForever()
	}

	printBanner(cfgPath)
	printConfig(app.CurrentConfig())
	enumerateDevices()

	if err := app.Run(context.Background()); err != nil {
		log.Printf("[ERR] %v", err)
		waitForever()
	}
}
