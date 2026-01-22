package main

import (
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

// exeDir 获取可执行文件所在目录
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// u32ptrFromI32 将 int32 转换为 uintptr
func u32ptrFromI32(v int32) uintptr {
	return uintptr(uint32(v))
}

// ==================== 打印函数 ====================

// printBanner 打印程序横幅
func printBanner(cfgPath string) {
	log.Printf("========================================")
	log.Printf(" VAXEE AutoSwitch (Console)")
	log.Printf(" Config: %s", cfgPath)
	log.Printf("========================================")
}

// printConfig 打印配置信息
func printConfig(cfg *Config) {
	log.Printf("[CFG] interval=%s", cfg.Interval)
	log.Printf("[CFG] hit    : mode=%s poll=%dHz", perfName(cfg.HitMode), cfg.HitPoll)
	log.Printf("[CFG] default: mode=%s poll=%dHz", perfName(cfg.DefaultMode), cfg.DefaultPoll)
	log.Printf("[CFG] whitelist(%d): %s", len(cfg.Whitelist), strings.Join(cfg.Whitelist, ", "))
}

// waitForever 等待程序退出
func waitForever() {
	log.Printf("按 Ctrl+C 退出。")
	select {}
}

// ==================== Windows 优先级设置 ====================

// setLowPriorityDefaults 设置低优先级默认值
func setLowPriorityDefaults(enableBackgroundMode bool, enableEcoQoS bool) {
	// 获取当前进程和线程句柄
	hProc, _, _ := procGetCurrentProcess.Call()
	hThread, _, _ := procGetCurrentThread.Call()

	// 1. 设置进程优先级为 BELOW_NORMAL
	if r, _, e := procSetPriorityClass.Call(hProc, uintptr(BELOW_NORMAL_PRIORITY_CLASS)); r == 0 {
		log.Printf("[PRIO] SetPriorityClass(BELOW_NORMAL) failed: %v", e)
	} else {
		log.Printf("[PRIO] Process priority set to BELOW_NORMAL.")
	}

	// 2. 设置线程优先级为 LOWEST
	if r, _, e := procSetThreadPriority.Call(hThread, uintptr(u32ptrFromI32(THREAD_PRIORITY_LOWEST))); r == 0 {
		log.Printf("[PRIO] SetThreadPriority(LOWEST) failed: %v", e)
	} else {
		log.Printf("[PRIO] Thread priority set to LOWEST.")
	}

	// 3. 可选：启用后台处理模式
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

	// 4. 可选：启用 EcoQoS/执行速度节流
	if enableEcoQoS {
		setProcessPowerThrottling(hProc)
		setThreadPowerThrottling(hThread)
	}
}

// setProcessPowerThrottling 设置进程电源节流
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

// setThreadPowerThrottling 设置线程电源节流
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
	// 线程侧失败也无所谓，不影响主流程
}

// ==================== 主逻辑函数 ====================

// tickOnce 执行一次检查并切换
func tickOnce(cfg *Config, last *Applied) (switchMsg string, errStr string) {
	// 获取前台进程名
	proc, err := ForegroundProcessName()
	if err != nil {
		return "", ""
	}
	proc = strings.ToLower(filepath.Base(proc))

	// 检查是否在白名单中
	_, hit := cfg.WhitelistSet[proc]
	wantPerf := cfg.DefaultMode
	wantPoll := cfg.DefaultPoll

	if hit {
		wantPerf = cfg.HitMode
		wantPoll = cfg.HitPoll
	}

	// 如果设置没有变化，直接返回
	if last.ok && last.perf == wantPerf && last.poll == wantPoll {
		return "", ""
	}

	// 查找 VAXEE 设备
	dev, findErr := FindOneVaxeeDevice()
	if findErr != nil {
		return "", "未找到可用 VAXEE 设备：" + findErr.Error()
	}

	// 应用设置
	if err := ApplyVaxeeSetting(dev.Path, wantPerf, wantPoll); err != nil {
		return "", "应用设置失败：" + err.Error()
	}

	// 更新记录
	*last = Applied{perf: wantPerf, poll: wantPoll, ok: true}

	// 返回切换信息
	if hit {
		return fmt.Sprintf("[SWITCH] 命中白名单(%s) -> %s + %dHz", proc, perfName(wantPerf), wantPoll), ""
	}
	return fmt.Sprintf("[SWITCH] 未命中白名单(%s) -> %s + %dHz", proc, perfName(wantPerf), wantPoll), ""
}

// ==================== 主函数 ====================

func main() {
	log.SetFlags(log.LstdFlags)

	// 配置文件路径
	cfgPath := filepath.Join(exeDir(), configFileName)

	// 确保配置文件存在
	if err := ensureConfigExists(cfgPath); err != nil {
		log.Printf("[ERR] 无法创建配置文件：%v", err)
		log.Printf("程序不会退出（窗口保留）。请检查权限/路径：%s", cfgPath)
		waitForever()
	}

	// 加载配置
	cfg, modTime, err := loadConfig(cfgPath)
	if err != nil {
		log.Printf("[ERR] 读取配置失败：%v", err)
		log.Printf("程序不会退出（窗口保留）。请修复配置后保存：%s", cfgPath)
		waitForever()
	}

	// 打印横幅和配置
	printBanner(cfgPath)
	printConfig(cfg)

	// 枚举 VAXEE 设备
	enumerateDevices()

	// 设置低优先级
	setLowPriorityDefaults(true, false)
	log.Printf("开始后台监控：每 %s 检查一次前台进程。", cfg.Interval)

	// 启动定时器
	// ticker := time.NewTicker(cfg.Interval)
	// defer ticker.Stop()

	var last Applied
	var lastErr string

	// 主循环
	for {
		// 热加载配置
		reloadConfigIfChanged(cfgPath, &cfg, &modTime)

		// 执行一次检查
		switchMsg, errStr := tickOnce(cfg, &last)
		if switchMsg != "" {
			log.Print(switchMsg)
		}

		// 处理错误信息
		handleError(&lastErr, errStr)

		// 等待下一次检查
		// <-ticker.C
		time.Sleep(cfg.Interval)

	}

}

// ==================== 辅助函数 ====================

// enumerateDevices 枚举并显示设备信息
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
			log.Printf("  #%d Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
				i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
		}
	}
}

// enumerateAllHidDevices 枚举所有 HID 设备
func enumerateAllHidDevices() {
	all, errAll := EnumerateAllHidDevices()
	if errAll != nil {
		log.Printf("[DEV] 枚举全部 HID 设备失败：%v", errAll)
		return
	}

	log.Printf("[DEV] 系统 HID 设备总数（可读取字符串/属性的接口）：%d", len(all))
	for i, d := range all {
		// 过滤掉完全空字符串的设备，减少噪音
		if d.Manufacturer == "" && d.Product == "" {
			continue
		}
		log.Printf("  [HID #%d] Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
			i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
	}
	log.Printf("[DEV] 提示：如果你在列表里看到了目标鼠标但字符串不含 VAXEE，后续可以改成按 VID/PID 固定匹配。")
}

// reloadConfigIfChanged 检查并重新加载配置
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

// handleError 处理错误信息
func handleError(lastErr *string, errStr string) {
	if errStr != "" && errStr != *lastErr {
		*lastErr = errStr
		log.Printf("[ERR] %s", errStr)
	} else if errStr == "" {
		*lastErr = ""
	}
}

// package main

// import (
// 	"fmt"
// 	"log"
// 	"os"
// 	"path/filepath"
// 	"strings"
// 	"syscall"
// 	"time"
// 	"unsafe"
// )

// type Applied struct {
// 	perf PerfMode
// 	poll PollingRate
// 	ok   bool
// }

// func exeDir() string {
// 	exe, err := os.Executable()
// 	if err != nil {
// 		return "."
// 	}
// 	return filepath.Dir(exe)
// }

// func printBanner(cfgPath string) {
// 	log.Printf("========================================")
// 	log.Printf(" VAXEE AutoSwitch (Console)")
// 	log.Printf(" Config: %s", cfgPath)
// 	log.Printf("========================================")
// }

// func printConfig(cfg *Config) {
// 	log.Printf("[CFG] interval=%s", cfg.Interval)
// 	log.Printf("[CFG] hit    : mode=%s poll=%dHz", perfName(cfg.HitMode), cfg.HitPoll)
// 	log.Printf("[CFG] default: mode=%s poll=%dHz", perfName(cfg.DefaultMode), cfg.DefaultPoll)
// 	log.Printf("[CFG] whitelist(%d): %s", len(cfg.Whitelist), strings.Join(cfg.Whitelist, ", "))
// }

// func waitForever() {
// 	log.Printf("按 Ctrl+C 退出。")
// 	select {}
// }

// func u32ptrFromI32(v int32) uintptr {
// 	return uintptr(uint32(v)) // 把负数按补码映射到 uint32，再转 uintptr
// }

// var (
// 	k32prio               = syscall.NewLazyDLL("kernel32.dll")
// 	procGetCurrentProcess = k32prio.NewProc("GetCurrentProcess")
// 	procGetCurrentThread  = k32prio.NewProc("GetCurrentThread")
// 	procSetPriorityClass  = k32prio.NewProc("SetPriorityClass")
// 	procSetThreadPriority = k32prio.NewProc("SetThreadPriority")

// 	// 可选：SetProcessInformation (EcoQoS / Power throttling)
// 	procSetProcessInformation = k32prio.NewProc("SetProcessInformation")
// 	// 可选：SetThreadInformation (thread power throttling)
// 	procSetThreadInformation = k32prio.NewProc("SetThreadInformation")
// )

// const (
// 	// SetPriorityClass dwPriorityClass
// 	IDLE_PRIORITY_CLASS           = 0x00000040
// 	BELOW_NORMAL_PRIORITY_CLASS   = 0x00004000
// 	PROCESS_MODE_BACKGROUND_BEGIN = 0x00100000 // 后台处理模式 begin [1](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setpriorityclass)

// 	// SetThreadPriority nPriority
// 	THREAD_PRIORITY_LOWEST       = -2
// 	THREAD_PRIORITY_IDLE         = -15
// 	THREAD_MODE_BACKGROUND_BEGIN = 0x00010000 // 线程后台处理模式 begin [5](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setthreadpriority)

// 	// SetProcessInformation ProcessInformationClass
// 	ProcessPowerThrottling = 4 // PROCESS_INFORMATION_CLASS: ProcessPowerThrottling [2](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setprocessinformation)
// 	// SetThreadInformation ThreadInformationClass（值在 WinSDK 里，Go 没内置；不同版本可能有差异）
// 	// 如果你不想碰这个，线程侧 EcoQoS 直接不启用即可。
// 	ThreadPowerThrottling = 5

// 	// PROCESS/THREAD_POWER_THROTTLING_STATE
// 	PROCESS_POWER_THROTTLING_CURRENT_VERSION = 1
// 	PROCESS_POWER_THROTTLING_EXECUTION_SPEED = 0x1 // [3](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/ns-processthreadsapi-process_power_throttling_state)

// 	THREAD_POWER_THROTTLING_CURRENT_VERSION = 1
// 	THREAD_POWER_THROTTLING_EXECUTION_SPEED = 0x1 // [6](https://learn.microsoft.com/zh-cn/windows/win32/api/processthreadsapi/ns-processthreadsapi-thread_power_throttling_state)
// )

// type PROCESS_POWER_THROTTLING_STATE struct {
// 	Version     uint32
// 	ControlMask uint32
// 	StateMask   uint32
// } // [3](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/ns-processthreadsapi-process_power_throttling_state)

// type THREAD_POWER_THROTTLING_STATE struct {
// 	Version     uint32
// 	ControlMask uint32
// 	StateMask   uint32
// } // [6](https://learn.microsoft.com/zh-cn/windows/win32/api/processthreadsapi/ns-processthreadsapi-thread_power_throttling_state)

// func setLowPriorityDefaults(enableBackgroundMode bool, enableEcoQoS bool) {
// 	// 当前进程/线程伪句柄
// 	hProc, _, _ := procGetCurrentProcess.Call()
// 	hThread, _, _ := procGetCurrentThread.Call()

// 	// 1) 进程优先级：BELOW_NORMAL（温和，不至于“饿死”自己）
// 	if r, _, e := procSetPriorityClass.Call(hProc, uintptr(BELOW_NORMAL_PRIORITY_CLASS)); r == 0 {
// 		log.Printf("[PRIO] SetPriorityClass(BELOW_NORMAL) failed: %v", e)
// 	} else {
// 		log.Printf("[PRIO] Process priority set to BELOW_NORMAL.")
// 	} // [1](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setpriorityclass)

// 	// 2) 线程优先级：LOWEST
// 	if r, _, e := procSetThreadPriority.Call(hThread, uintptr(u32ptrFromI32(THREAD_PRIORITY_LOWEST))); r == 0 {
// 		log.Printf("[PRIO] SetThreadPriority(LOWEST) failed: %v", e)
// 	} else {
// 		log.Printf("[PRIO] Thread priority set to LOWEST.")
// 	} // [5](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setthreadpriority)

// 	// 3) 可选：后台处理模式（更明确告诉系统“我做后台活”）
// 	if enableBackgroundMode {
// 		if r, _, e := procSetPriorityClass.Call(hProc, uintptr(PROCESS_MODE_BACKGROUND_BEGIN)); r == 0 {
// 			log.Printf("[PRIO] PROCESS_MODE_BACKGROUND_BEGIN failed: %v", e)
// 		} else {
// 			log.Printf("[PRIO] Process background mode enabled.")
// 		} // [1](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setpriorityclass)

// 		if r, _, e := procSetThreadPriority.Call(hThread, uintptr(THREAD_MODE_BACKGROUND_BEGIN)); r == 0 {
// 			log.Printf("[PRIO] THREAD_MODE_BACKGROUND_BEGIN failed: %v", e)
// 		} else {
// 			log.Printf("[PRIO] Thread background mode enabled.")
// 		} // [5](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setthreadpriority)
// 	}

// 	// 4) 可选：EcoQoS/执行速度节流（Power throttling）
// 	if enableEcoQoS {
// 		st := PROCESS_POWER_THROTTLING_STATE{
// 			Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
// 			ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
// 			StateMask:   PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
// 		} // [3](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/ns-processthreadsapi-process_power_throttling_state)

// 		r, _, e := procSetProcessInformation.Call(
// 			hProc,
// 			uintptr(ProcessPowerThrottling),
// 			uintptr(unsafe.Pointer(&st)),
// 			unsafe.Sizeof(st),
// 		)
// 		if r == 0 {
// 			log.Printf("[PRIO] Process EcoQoS/PowerThrottling failed: %v", e)
// 		} else {
// 			log.Printf("[PRIO] Process EcoQoS/PowerThrottling enabled.")
// 		} // [2](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-setprocessinformation)[3](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/ns-processthreadsapi-process_power_throttling_state)

// 		// 线程侧可选（如果你担心 ThreadInformationClass 值兼容性，这段可以先不启用）
// 		ts := THREAD_POWER_THROTTLING_STATE{
// 			Version:     THREAD_POWER_THROTTLING_CURRENT_VERSION,
// 			ControlMask: THREAD_POWER_THROTTLING_EXECUTION_SPEED,
// 			StateMask:   THREAD_POWER_THROTTLING_EXECUTION_SPEED,
// 		} // [6](https://learn.microsoft.com/zh-cn/windows/win32/api/processthreadsapi/ns-processthreadsapi-thread_power_throttling_state)
// 		_, _, _ = procSetThreadInformation.Call(
// 			hThread,
// 			uintptr(ThreadPowerThrottling),
// 			uintptr(unsafe.Pointer(&ts)),
// 			unsafe.Sizeof(ts),
// 		)
// 		// 线程侧失败也无所谓，不影响主流程
// 	}
// }

// func main() {
// 	log.SetFlags(log.LstdFlags)

// 	cfgPath := filepath.Join(exeDir(), configFileName)

// 	if err := ensureConfigExists(cfgPath); err != nil {
// 		log.Printf("[ERR] 无法创建配置文件：%v", err)
// 		log.Printf("程序不会退出（窗口保留）。请检查权限/路径：%s", cfgPath)
// 		waitForever()
// 	}

// 	cfg, modTime, err := loadConfig(cfgPath)
// 	if err != nil {
// 		log.Printf("[ERR] 读取配置失败：%v", err)
// 		log.Printf("程序不会退出（窗口保留）。请修复配置后保存：%s", cfgPath)
// 		waitForever()
// 	}

// 	printBanner(cfgPath)

// 	infos, enumErr := EnumerateVaxeeDevices()
// 	if enumErr != nil {
// 		log.Printf("[DEV] 枚举 HID 设备失败：%v", enumErr)
// 	} else if len(infos) == 0 {
// 		log.Printf("[DEV] 未发现 VAXEE 设备（Manufacturer/Product 不包含 vaxee）。")
// 		log.Printf("[DEV] 程序将继续运行，每次尝试切换时会重新查找设备。")

// 		// 新增：启动时找不到 VAXEE → 额外枚举一次所有 HID 设备并打印信息（仅一次）
// 		all, errAll := EnumerateAllHidDevices()
// 		if errAll != nil {
// 			log.Printf("[DEV] 枚举全部 HID 设备失败：%v", errAll)
// 		} else {
// 			log.Printf("[DEV] 系统 HID 设备总数（可读取字符串/属性的接口）：%d", len(all))
// 			for i, d := range all {
// 				// 过滤掉完全空字符串的设备，减少噪音（你也可以删掉这个 if）
// 				if d.Manufacturer == "" && d.Product == "" {
// 					continue
// 				}
// 				log.Printf("  [HID #%d] Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
// 					i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
// 			}
// 			log.Printf("[DEV] 提示：如果你在列表里看到了目标鼠标但字符串不含 VAXEE，后续可以改成按 VID/PID 固定匹配。")
// 		}
// 	} else {
// 		log.Printf("[DEV] 发现 %d 个 VAXEE HID 设备：", len(infos))
// 		for i, d := range infos {
// 			log.Printf("  #%d Manufacturer=%q Product=%q VID=0x%04x PID=0x%04x Path=%s",
// 				i+1, d.Manufacturer, d.Product, d.VID, d.PID, d.Path)
// 		}
// 	}

// 	printConfig(cfg)
// 	setLowPriorityDefaults(true, false) // (后台模式=开, EcoQoS=开)；不想冒险就 setLowPriorityDefaults(true, false)
// 	log.Printf("开始后台监控：每 %s 检查一次前台进程。", cfg.Interval)

// 	ticker := time.NewTicker(cfg.Interval)
// 	defer ticker.Stop()

// 	var last Applied
// 	var lastErr string

// 	for {
// 		// 热加载配置
// 		if fi, e := os.Stat(cfgPath); e == nil && fi.ModTime().After(modTime) {
// 			if nc, mt, e2 := loadConfig(cfgPath); e2 == nil {
// 				cfg, modTime = nc, mt
// 				log.Printf("[CFG] 检测到配置文件变更，已重新加载。")
// 				printConfig(cfg)
// 			} else {
// 				log.Printf("[ERR] 配置文件变更但重载失败：%v", e2)
// 			}
// 		}

// 		switchMsg, errStr := tickOnce(cfg, &last)
// 		if switchMsg != "" {
// 			log.Print(switchMsg)
// 		}
// 		if errStr != "" && errStr != lastErr {
// 			lastErr = errStr
// 			log.Printf("[ERR] %s", errStr)
// 		} else if errStr == "" {
// 			lastErr = ""
// 		}

// 		<-ticker.C
// 	}
// }

// func tickOnce(cfg *Config, last *Applied) (switchMsg string, errStr string) {
// 	proc, err := ForegroundProcessName()
// 	if err != nil {
// 		return "", ""
// 	}
// 	proc = strings.ToLower(filepath.Base(proc))

// 	_, hit := cfg.WhitelistSet[proc]
// 	wantPerf := cfg.DefaultMode
// 	wantPoll := cfg.DefaultPoll
// 	if hit {
// 		wantPerf = cfg.HitMode
// 		wantPoll = cfg.HitPoll
// 	}

// 	if last.ok && last.perf == wantPerf && last.poll == wantPoll {
// 		return "", ""
// 	}

// 	dev, findErr := FindOneVaxeeDevice()
// 	if findErr != nil {
// 		return "", "未找到可用 VAXEE 设备：" + findErr.Error()
// 	}

// 	if err := ApplyVaxeeSetting(dev.Path, wantPerf, wantPoll); err != nil {
// 		return "", "应用设置失败：" + err.Error()
// 	}

// 	*last = Applied{perf: wantPerf, poll: wantPoll, ok: true}

// 	if hit {
// 		return fmt.Sprintf("[SWITCH] 命中白名单(%s) -> %s + %dHz", proc, perfName(wantPerf), wantPoll), ""
// 	}
// 	return fmt.Sprintf("[SWITCH] 未命中白名单(%s) -> %s + %dHz", proc, perfName(wantPerf), wantPoll), ""
// }
