//go:build ignore

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

type guiState struct {
	app    *AutoSwitchApp
	cancel context.CancelFunc

	hInstance uintptr
	mainHwnd  uintptr
	cfgHwnd   uintptr
	statusHW  uintptr

	currentProfile ProfileKind

	profileButtons map[ProfileKind]uintptr
	perfButtons    map[PerfMode]uintptr
	pollButtons    map[PollingRate]uintptr
	trajButtons    map[TrajectoryMode]uintptr

	trayIcon NOTIFYICONDATA
}

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type POINT struct {
	X int32
	Y int32
}

type MSG struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

type NOTIFYICONDATA struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         syscall.GUID
	HBalloonIcon     uintptr
}

var (
	user32GUI   = syscall.NewLazyDLL("user32.dll")
	kernel32GUI = syscall.NewLazyDLL("kernel32.dll")
	shell32GUI  = syscall.NewLazyDLL("shell32.dll")

	procRegisterClassExW = user32GUI.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32GUI.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32GUI.NewProc("DefWindowProcW")
	procDestroyWindow    = user32GUI.NewProc("DestroyWindow")
	procShowWindow       = user32GUI.NewProc("ShowWindow")
	procIsWindowVisible  = user32GUI.NewProc("IsWindowVisible")
	procUpdateWindow     = user32GUI.NewProc("UpdateWindow")
	procGetMessageW      = user32GUI.NewProc("GetMessageW")
	procTranslateMessage = user32GUI.NewProc("TranslateMessage")
	procDispatchMessageW = user32GUI.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32GUI.NewProc("PostQuitMessage")
	procLoadCursorW      = user32GUI.NewProc("LoadCursorW")
	procLoadIconW        = user32GUI.NewProc("LoadIconW")
	procSendMessageW     = user32GUI.NewProc("SendMessageW")
	procSetWindowTextW   = user32GUI.NewProc("SetWindowTextW")
	procSetForegroundWin = user32GUI.NewProc("SetForegroundWindow")
	procGetCursorPos     = user32GUI.NewProc("GetCursorPos")
	procCreatePopupMenu  = user32GUI.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32GUI.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32GUI.NewProc("TrackPopupMenu")
	procMessageBoxW      = user32GUI.NewProc("MessageBoxW")

	procGetModuleHandleW = kernel32GUI.NewProc("GetModuleHandleW")

	procShellNotifyIconW = shell32GUI.NewProc("Shell_NotifyIconW")
)

var globalGUI *guiState

const (
	classMain = "VaxeeAutoSwitchTrayWindow"
	classCfg  = "VaxeeAutoSwitchConfigWindow"

	wmAppTray = 0x8000 + 1

	wmCommand = 0x0111
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmCreate  = 0x0001

	wmLButtonUp  = 0x0202
	wmLButtonDbl = 0x0203
	wmRButtonUp  = 0x0205

	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000
	wsGroup       = 0x00020000
	wsTabStop     = 0x00010000
	wsMinimizeBox = 0x00020000

	bsAutoradio = 0x00000009

	swHide = 0
	swShow = 5

	cwUseDefault = 0x80000000

	colorWindow = 5

	mbOK          = 0x00000000
	mbIconError   = 0x00000010
	mbIconWarning = 0x00000030

	bmSetCheck   = 0x00F1
	bstChecked   = 1
	bstUnchecked = 0

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	notifyIconVersion4 = 4

	mfString = 0x00000000

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002

	idiApplication = 32512
	idcArrow       = 32512

	idProfileHit     = 1001
	idProfileDefault = 1002

	idPerfStandardMSOff  = 1101
	idPerfCompetitiveOff = 1102
	idPerfCompetitiveOn  = 1103
	idPerfStandardMSOn   = 1104

	idPoll1000 = 1201
	idPoll2000 = 1202
	idPoll4000 = 1203

	idTrajSmooth = 1301
	idTrajStable = 1302

	idMenuOpen = 9001
	idMenuExit = 9002
)

func runGUIApp() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cfgPath := filepath.Join(exeDir(), configFileName)
	app, err := NewAutoSwitchApp(cfgPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go app.Run(ctx)

	gui := &guiState{
		app:            app,
		cancel:         cancel,
		currentProfile: ProfileHit,
		profileButtons: map[ProfileKind]uintptr{},
		perfButtons:    map[PerfMode]uintptr{},
		pollButtons:    map[PollingRate]uintptr{},
		trajButtons:    map[TrajectoryMode]uintptr{},
	}
	globalGUI = gui

	if err := gui.init(); err != nil {
		cancel()
		return err
	}

	gui.showConfigWindow()
	gui.messageLoop()
	cancel()
	return nil
}

func (g *guiState) init() error {
	instance, _, _ := procGetModuleHandleW.Call(0)
	g.hInstance = instance

	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wndProc := syscall.NewCallback(guiWndProc)

	if err := registerWindowClass(classMain, instance, icon, cursor, wndProc); err != nil {
		return err
	}
	if err := registerWindowClass(classCfg, instance, icon, cursor, wndProc); err != nil {
		return err
	}

	mainHwnd, err := createTopLevelWindow(classMain, "VAXEE AutoSwitch", instance, 0, 0)
	if err != nil {
		return err
	}
	g.mainHwnd = mainHwnd

	cfgHwnd, err := createTopLevelWindow(classCfg, "VAXEE AutoSwitch", instance, 430, 360)
	if err != nil {
		return err
	}
	g.cfgHwnd = cfgHwnd
	g.buildControls()
	g.syncControls()

	if err := g.addTrayIcon(icon); err != nil {
		return err
	}
	return nil
}

func registerWindowClass(name string, instance uintptr, icon uintptr, cursor uintptr, wndProc uintptr) error {
	className := syscall.StringToUTF16Ptr(name)
	wc := WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		LpfnWndProc:   wndProc,
		HInstance:     instance,
		HIcon:         icon,
		HCursor:       cursor,
		HbrBackground: uintptr(colorWindow + 1),
		LpszClassName: className,
		HIconSm:       icon,
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}
	return nil
}

func createTopLevelWindow(className string, title string, instance uintptr, width int32, height int32) (uintptr, error) {
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	r, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(className))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		style,
		uintptr(cwUseDefault),
		uintptr(cwUseDefault),
		uintptr(width),
		uintptr(height),
		0,
		0,
		instance,
		0,
	)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func (g *guiState) buildControls() {
	createLabel(g.cfgHwnd, g.hInstance, 18, 18, 360, 18, "编辑目标")

	g.profileButtons[ProfileHit] = createRadio(g.cfgHwnd, g.hInstance, 18, 44, 150, 22, idProfileHit, "Hit 命中配置", true)
	g.profileButtons[ProfileDefault] = createRadio(g.cfgHwnd, g.hInstance, 190, 44, 180, 22, idProfileDefault, "Miss 默认配置", false)

	createLabel(g.cfgHwnd, g.hInstance, 18, 84, 300, 18, "性能模式")
	g.perfButtons[PerfCompetitiveMSOff] = createRadio(g.cfgHwnd, g.hInstance, 18, 110, 180, 22, idPerfCompetitiveOff, "competitive_ms_off", true)
	g.perfButtons[PerfStandardMSOff] = createRadio(g.cfgHwnd, g.hInstance, 210, 110, 170, 22, idPerfStandardMSOff, "standard_ms_off", false)
	g.perfButtons[PerfCompetitiveMSOn] = createRadio(g.cfgHwnd, g.hInstance, 18, 136, 180, 22, idPerfCompetitiveOn, "competitive_ms_on", false)
	g.perfButtons[PerfStandardMSOn] = createRadio(g.cfgHwnd, g.hInstance, 210, 136, 170, 22, idPerfStandardMSOn, "standard_ms_on", false)

	createLabel(g.cfgHwnd, g.hInstance, 18, 176, 300, 18, "回报率")
	g.pollButtons[Poll1000] = createRadio(g.cfgHwnd, g.hInstance, 18, 202, 110, 22, idPoll1000, "1000 Hz", true)
	g.pollButtons[Poll2000] = createRadio(g.cfgHwnd, g.hInstance, 140, 202, 110, 22, idPoll2000, "2000 Hz", false)
	g.pollButtons[Poll4000] = createRadio(g.cfgHwnd, g.hInstance, 262, 202, 110, 22, idPoll4000, "4000 Hz", false)

	createLabel(g.cfgHwnd, g.hInstance, 18, 242, 300, 18, "轨迹模式")
	g.trajButtons[TrajSmoothSensitive] = createRadio(g.cfgHwnd, g.hInstance, 18, 268, 180, 22, idTrajSmooth, "smooth_sensitive", true)
	g.trajButtons[TrajStableControl] = createRadio(g.cfgHwnd, g.hInstance, 210, 268, 170, 22, idTrajStable, "stable_control", false)

	g.statusHW = createLabel(g.cfgHwnd, g.hInstance, 18, 308, 380, 18, "")
}

func createLabel(parent uintptr, instance uintptr, x, y, w, h int32, text string) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(wsChild|wsVisible),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent,
		0,
		instance,
		0,
	)
	return hwnd
}

func createRadio(parent uintptr, instance uintptr, x, y, w, h int32, id int, text string, group bool) uintptr {
	style := uintptr(wsChild | wsVisible | wsTabStop | bsAutoradio)
	if group {
		style |= wsGroup
	}
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent,
		uintptr(id),
		instance,
		0,
	)
	return hwnd
}

func (g *guiState) addTrayIcon(icon uintptr) error {
	g.trayIcon = NOTIFYICONDATA{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATA{})),
		HWnd:             g.mainHwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmAppTray,
		HIcon:            icon,
		UVersion:         notifyIconVersion4,
	}
	copyUTF16(g.trayIcon.SzTip[:], "VAXEE AutoSwitch")

	r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&g.trayIcon)))
	if r == 0 {
		return err
	}
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&g.trayIcon)))
	return nil
}

func (g *guiState) removeTrayIcon() {
	if g.trayIcon.HWnd != 0 {
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&g.trayIcon)))
	}
}

func (g *guiState) messageLoop() {
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func guiWndProc(hwnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	if globalGUI == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	}

	switch msg {
	case wmCreate:
		return 0
	case wmCommand:
		globalGUI.handleCommand(hwnd, controlID(wParam))
		return 0
	case wmClose:
		if hwnd == globalGUI.cfgHwnd {
			procShowWindow.Call(hwnd, swHide)
			return 0
		}
	case wmDestroy:
		if hwnd == globalGUI.mainHwnd {
			globalGUI.removeTrayIcon()
			procPostQuitMessage.Call(0)
			return 0
		}
	case wmAppTray:
		switch uint32(lParam) {
		case wmLButtonUp, wmLButtonDbl:
			globalGUI.toggleConfigWindow()
		case wmRButtonUp:
			globalGUI.showTrayMenu()
		}
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func (g *guiState) handleCommand(hwnd uintptr, id int) {
	switch id {
	case idProfileHit:
		g.currentProfile = ProfileHit
		g.syncControls()
	case idProfileDefault:
		g.currentProfile = ProfileDefault
		g.syncControls()
	case idMenuOpen:
		g.showConfigWindow()
	case idMenuExit:
		g.close()
	case idPerfStandardMSOff:
		g.applySelection(PerfStandardMSOff, 0, 0)
	case idPerfCompetitiveOff:
		g.applySelection(PerfCompetitiveMSOff, 0, 0)
	case idPerfCompetitiveOn:
		g.applySelection(PerfCompetitiveMSOn, 0, 0)
	case idPerfStandardMSOn:
		g.applySelection(PerfStandardMSOn, 0, 0)
	case idPoll1000:
		g.applySelection(0, Poll1000, 0)
	case idPoll2000:
		g.applySelection(0, Poll2000, 0)
	case idPoll4000:
		g.applySelection(0, Poll4000, 0)
	case idTrajSmooth:
		g.applySelection(0, 0, TrajSmoothSensitive)
	case idTrajStable:
		g.applySelection(0, 0, TrajStableControl)
	}
}

func (g *guiState) applySelection(perf PerfMode, poll PollingRate, traj TrajectoryMode) {
	cfg := g.app.CurrentConfig()
	if g.currentProfile == ProfileHit {
		if perf == 0 {
			perf = cfg.HitMode
		}
		if poll == 0 {
			poll = cfg.HitPoll
		}
		if traj == 0 {
			traj = cfg.HitTraj
		}
	} else {
		if perf == 0 {
			perf = cfg.DefaultMode
		}
		if poll == 0 {
			poll = cfg.DefaultPoll
		}
		if traj == 0 {
			traj = cfg.DefaultTraj
		}
	}

	if err := g.app.UpdateProfile(g.currentProfile, perf, poll, traj); err != nil {
		showMessageBox(g.cfgHwnd, "写入配置失败", err.Error(), mbOK|mbIconError)
	}
	g.syncControls()
}

func (g *guiState) syncControls() {
	cfg := g.app.CurrentConfig()

	setChecked(g.profileButtons[ProfileHit], g.currentProfile == ProfileHit)
	setChecked(g.profileButtons[ProfileDefault], g.currentProfile == ProfileDefault)

	var perf PerfMode
	var poll PollingRate
	var traj TrajectoryMode
	if g.currentProfile == ProfileHit {
		perf = cfg.HitMode
		poll = cfg.HitPoll
		traj = cfg.HitTraj
	} else {
		perf = cfg.DefaultMode
		poll = cfg.DefaultPoll
		traj = cfg.DefaultTraj
	}

	for value, hwnd := range g.perfButtons {
		setChecked(hwnd, value == perf)
	}
	for value, hwnd := range g.pollButtons {
		setChecked(hwnd, value == poll)
	}
	for value, hwnd := range g.trajButtons {
		setChecked(hwnd, value == traj)
	}

	status := fmt.Sprintf("当前编辑: %s | 点击按钮即写入 %s", profileName(g.currentProfile), filepath.Base(cfg.ConfigPath))
	setWindowText(g.statusHW, status)
}

func (g *guiState) toggleConfigWindow() {
	if g.cfgHwnd == 0 {
		return
	}
	visible, _, _ := procIsWindowVisible.Call(g.cfgHwnd)
	if visible != 0 {
		procShowWindow.Call(g.cfgHwnd, swHide)
		return
	}
	g.showConfigWindow()
}

func (g *guiState) showConfigWindow() {
	g.syncControls()
	procShowWindow.Call(g.cfgHwnd, swShow)
	procUpdateWindow.Call(g.cfgHwnd)
	procSetForegroundWin.Call(g.cfgHwnd)
}

func (g *guiState) showTrayMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	procAppendMenuW.Call(menu, mfString, idMenuOpen, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Open Settings"))))
	procAppendMenuW.Call(menu, mfString, idMenuExit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Exit"))))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWin.Call(g.mainHwnd)
	procTrackPopupMenu.Call(menu, tpmLeftAlign|tpmBottomAlign|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, g.mainHwnd, 0)
}

func (g *guiState) close() {
	if g.cancel != nil {
		g.cancel()
	}
	if g.cfgHwnd != 0 {
		procDestroyWindow.Call(g.cfgHwnd)
		g.cfgHwnd = 0
	}
	if g.mainHwnd != 0 {
		procDestroyWindow.Call(g.mainHwnd)
		g.mainHwnd = 0
	}
}

func showSimpleMessageBox(title string, message string) {
	showMessageBox(0, title, message, mbOK|mbIconError)
}

func showMessageBox(hwnd uintptr, title string, message string, flags uintptr) {
	procMessageBoxW.Call(
		hwnd,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		flags,
	)
}

func setChecked(hwnd uintptr, checked bool) {
	state := uintptr(bstUnchecked)
	if checked {
		state = bstChecked
	}
	procSendMessageW.Call(hwnd, bmSetCheck, state, 0)
}

func setWindowText(hwnd uintptr, text string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))))
}

func controlID(wParam uintptr) int {
	return int(uint16(wParam & 0xffff))
}

func copyUTF16(dst []uint16, s string) {
	src := syscall.StringToUTF16(s)
	if len(src) > len(dst) {
		src = src[:len(dst)]
	}
	copy(dst, src)
	if len(src) == len(dst) {
		dst[len(dst)-1] = 0
	}
}

func profileName(profile ProfileKind) string {
	if profile == ProfileHit {
		return "Hit"
	}
	return "Miss(Default)"
}
