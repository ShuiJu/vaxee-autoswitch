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

type Applied struct {
	perf    PerfMode
	poll    PollingRate
	traj    TrajectoryMode
	devices string
	ok      bool
}

var (
	kernel32DLL = syscall.NewLazyDLL("kernel32.dll")

	procGetCurrentProcess     = kernel32DLL.NewProc("GetCurrentProcess")
	procGetCurrentThread      = kernel32DLL.NewProc("GetCurrentThread")
	procSetPriorityClass      = kernel32DLL.NewProc("SetPriorityClass")
	procSetThreadPriority     = kernel32DLL.NewProc("SetThreadPriority")
	procSetProcessInformation = kernel32DLL.NewProc("SetProcessInformation")
	procSetThreadInformation  = kernel32DLL.NewProc("SetThreadInformation")
)

const (
	IDLE_PRIORITY_CLASS           = 0x00000040
	BELOW_NORMAL_PRIORITY_CLASS   = 0x00004000
	PROCESS_MODE_BACKGROUND_BEGIN = 0x00100000

	THREAD_PRIORITY_LOWEST       = -2
	THREAD_PRIORITY_IDLE         = -15
	THREAD_MODE_BACKGROUND_BEGIN = 0x00010000

	ProcessPowerThrottling = 4
	ThreadPowerThrottling  = 5

	PROCESS_POWER_THROTTLING_CURRENT_VERSION = 1
	PROCESS_POWER_THROTTLING_EXECUTION_SPEED = 0x1

	THREAD_POWER_THROTTLING_CURRENT_VERSION = 1
	THREAD_POWER_THROTTLING_EXECUTION_SPEED = 0x1
)

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

func printBanner(cfgPath string) {
	log.Printf("========================================")
	log.Printf(" %s (Console)", appDisplayName)
	log.Printf(" Config: %s", cfgPath)
	log.Printf("========================================")
}

func printConfig(cfg *Config) {
	log.Printf("[CFG] interval=%s auto_switch=%v", cfg.Interval, cfg.AutoSwitchEnabled)
	log.Printf("[CFG] hit    : mode=%s poll=%dHz traj=%s", perfName(cfg.HitMode), cfg.HitPoll, trajName(cfg.HitTraj))
	log.Printf("[CFG] default: mode=%s poll=%dHz traj=%s", perfName(cfg.DefaultMode), cfg.DefaultPoll, trajName(cfg.DefaultTraj))
	log.Printf("[CFG] whitelist(%d): %s", len(cfg.Whitelist), strings.Join(cfg.Whitelist, ", "))
}

func waitForever() {
	log.Printf("Press Ctrl+C to exit.")
	select {}
}

func setLowPriorityDefaults(enableBackgroundMode bool, enableEcoQoS bool) {
	hProc, _, _ := procGetCurrentProcess.Call()
	hThread, _, _ := procGetCurrentThread.Call()

	_, _, _ = procSetPriorityClass.Call(hProc, uintptr(BELOW_NORMAL_PRIORITY_CLASS))
	_, _, _ = procSetThreadPriority.Call(hThread, uintptr(u32ptrFromI32(THREAD_PRIORITY_LOWEST)))

	if enableBackgroundMode {
		_, _, _ = procSetPriorityClass.Call(hProc, uintptr(PROCESS_MODE_BACKGROUND_BEGIN))
		_, _, _ = procSetThreadPriority.Call(hThread, uintptr(THREAD_MODE_BACKGROUND_BEGIN))
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
	_, _, _ = procSetProcessInformation.Call(
		hProc,
		uintptr(ProcessPowerThrottling),
		uintptr(unsafe.Pointer(&state)),
		unsafe.Sizeof(state),
	)
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

func tickOnce(cfg *Config, last *Applied) (switchMsg string, errStr string, devCount int, logicalCount int, switched bool, appliedPerf PerfMode, appliedPoll PollingRate, appliedTraj TrajectoryMode) {
	proc, err := ForegroundProcessName()
	if err != nil {
		return "", "", 0, 0, false, 0, 0, 0
	}
	proc = normalizeProcessName(proc)

	_, hit := cfg.WhitelistSet[proc]

	wantPerf := cfg.DefaultMode
	wantPoll := cfg.DefaultPoll
	wantTraj := cfg.DefaultTraj
	if hit {
		wantPerf = cfg.HitMode
		wantPoll = cfg.HitPoll
		wantTraj = cfg.HitTraj
	}

	devs := FindAllVaxeeDevices()
	logicalCount = len(devs)
	devCount = CountUniqueVaxeeDevices(devs)
	deviceSig := VaxeeDeviceSetSignature(devs)

	// 自动切换总开关关闭：后台仅轮询设备状态，不推送设置到设备
	if !cfg.AutoSwitchEnabled {
		return "", "", devCount, logicalCount, false, wantPerf, wantPoll, wantTraj
	}

	if last.ok && last.perf == wantPerf && last.poll == wantPoll && last.traj == wantTraj && last.devices == deviceSig {
		return "", "", devCount, logicalCount, false, wantPerf, wantPoll, wantTraj
	}

	applied, err := ApplyVaxeeProfileToAll(devs, wantPerf, wantPoll, wantTraj)
	if err != nil {
		return "", "应用设置失败: " + err.Error(), devCount, logicalCount, false, wantPerf, wantPoll, wantTraj
	}

	*last = Applied{perf: wantPerf, poll: wantPoll, traj: wantTraj, devices: deviceSig, ok: true}
	PrintBatteryAllVAXEE(applied)

	if hit {
		return fmt.Sprintf("[SWITCH] 命中白名单(%s) -> %s + %dHz + traj=%s",
				proc, perfName(wantPerf), wantPoll, trajName(wantTraj)), "", devCount, logicalCount, true, wantPerf, wantPoll, wantTraj
	}
	return fmt.Sprintf("[SWITCH] 未命中白名单(%s) -> %s + %dHz + traj=%s",
		proc, perfName(wantPerf), wantPoll, trajName(wantTraj)), "", devCount, logicalCount, true, wantPerf, wantPoll, wantTraj
}

func enumerateDevices() {
	infos, enumErr := EnumerateVaxeeDevices()
	if enumErr != nil {
		log.Printf("[DEV] 枚举 HID 设备失败: %v", enumErr)
		return
	}
	if len(infos) == 0 {
		log.Printf("[DEV] 未发现 VAXEE 设备(Manufacturer/Product 不包含 vaxee)。")
		log.Printf("[DEV] 程序将继续运行，每次尝试切换时会重新查找设备。")
		enumerateAllHidDevices()
		return
	}

	log.Printf("[DEV] 发现 %d 个 VAXEE HID 设备:", len(infos))
	for i, d := range infos {
		log.Printf(" #%d Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
			i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
	}
}

func enumerateAllHidDevices() {
	all, errAll := EnumerateAllHidDevices()
	if errAll != nil {
		log.Printf("[DEV] 枚举全部 HID 设备失败: %v", errAll)
		return
	}
	log.Printf("[DEV] 系统 HID 设备总数(可读取字符串属性的接口): %d", len(all))
	for i, d := range all {
		if d.Manufacturer == "" && d.Product == "" {
			continue
		}
		log.Printf(" [HID #%d] Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
			i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
	}
}

func reloadConfigIfChanged(cfgPath string, cfg **Config, modTime *time.Time) {
	if fi, e := os.Stat(cfgPath); e == nil && fi.ModTime().After(*modTime) {
		if nc, mt, e2 := loadConfig(cfgPath); e2 == nil {
			*cfg = nc
			*modTime = mt
			log.Printf("[CFG] 检测到配置文件变更，已重新加载。")
			printConfig(*cfg)
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
