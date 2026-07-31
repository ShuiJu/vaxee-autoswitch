//go:build windows

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Win32 COLORREF = 0x00BBGGRR
const (
	clrBg        = 0x00252525
	clrAccent    = 0x003565FF
	clrText      = 0x00F0F0F0
	clrSeparator = 0x00444444
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	_pad1       int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type guiState struct {
	app    *AutoSwitchApp
	cancel context.CancelFunc

	hInstance uintptr
	mainHwnd  uintptr
	cfgHwnd   uintptr

	hdrStatusHW uintptr
	statusLines [5]uintptr

	uiFont     uintptr
	statusFont uintptr
	hbrBg      uintptr

	appIcon   uintptr
	appIconSm uintptr

	currentProfile ProfileKind
	statusNote     string

	profileButtons    map[ProfileKind]uintptr
	perfBaseButtons   map[int]uintptr
	motionSyncButtons map[int]uintptr
	pollButtons       map[PollingRate]uintptr
	trajButtons       map[TrajectoryMode]uintptr
	controls          []uintptr
	separatorYs       []int32

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
	gdi32GUI    = syscall.NewLazyDLL("gdi32.dll")
	dwmapiGUI   = syscall.NewLazyDLL("dwmapi.dll")

	procRegisterClassExW = user32GUI.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32GUI.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32GUI.NewProc("DefWindowProcW")
	procDestroyWindow    = user32GUI.NewProc("DestroyWindow")
	procShowWindow       = user32GUI.NewProc("ShowWindow")
	procUpdateWindow     = user32GUI.NewProc("UpdateWindow")
	procGetMessageW      = user32GUI.NewProc("GetMessageW")
	procTranslateMessage = user32GUI.NewProc("TranslateMessage")
	procDispatchMessageW = user32GUI.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32GUI.NewProc("PostQuitMessage")
	procPostMessageW     = user32GUI.NewProc("PostMessageW")
	procLoadCursorW      = user32GUI.NewProc("LoadCursorW")
	procSendMessageW     = user32GUI.NewProc("SendMessageW")
	procSetWindowTextW   = user32GUI.NewProc("SetWindowTextW")
	procSetForegroundWin = user32GUI.NewProc("SetForegroundWindow")
	procGetCursorPos     = user32GUI.NewProc("GetCursorPos")
	procCreatePopupMenu  = user32GUI.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32GUI.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32GUI.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32GUI.NewProc("DestroyMenu")
	procMessageBoxW      = user32GUI.NewProc("MessageBoxW")
	procGetClientRect    = user32GUI.NewProc("GetClientRect")
	procFillRect         = user32GUI.NewProc("FillRect")
	procBeginPaint       = user32GUI.NewProc("BeginPaint")
	procEndPaint         = user32GUI.NewProc("EndPaint")

	procGetModuleHandleW = kernel32GUI.NewProc("GetModuleHandleW")

	procCreateFontW      = gdi32GUI.NewProc("CreateFontW")
	procDeleteObject     = gdi32GUI.NewProc("DeleteObject")
	procCreateSolidBrush = gdi32GUI.NewProc("CreateSolidBrush")
	procCreatePen        = gdi32GUI.NewProc("CreatePen")
	procSelectObject     = gdi32GUI.NewProc("SelectObject")
	procMoveToEx         = gdi32GUI.NewProc("MoveToEx")
	procLineTo           = gdi32GUI.NewProc("LineTo")
	procSetBkMode        = gdi32GUI.NewProc("SetBkMode")
	procSetTextColor     = gdi32GUI.NewProc("SetTextColor")
	procSetBkColor       = gdi32GUI.NewProc("SetBkColor")

	procShellNotifyIconW      = shell32GUI.NewProc("Shell_NotifyIconW")
	procDwmSetWindowAttribute = dwmapiGUI.NewProc("DwmSetWindowAttribute")
)

var globalGUI *guiState

const (
	classMain = "VaxeeAutoSwitchTrayWindow"
	classCfg  = "VaxeeAutoSwitchConfigWindow"

	wmAppTray = 0x8000 + 1

	wmNull           = 0x0000
	wmCommand        = 0x0111
	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmPaint          = 0x000F
	wmEraseBkgnd     = 0x0014
	wmCtlColorStatic = 0x0138
	wmCtlColorBtn    = 0x0135

	wmLButtonDbl = 0x0203
	wmRButtonUp  = 0x0205

	// 内部自定义消息：后台自动切换流程结束后刷新界面
	wmRefreshUI = 0x8000 + 2

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

	mbOK        = 0x00000000
	mbIconError = 0x00000010

	bmSetCheck   = 0x00F1
	wmSetFont    = 0x0030
	bstChecked   = 1
	bstUnchecked = 0

	nimAdd        = 0x00000000
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

	idcArrow = 32512

	idProfileHit     = 1001
	idProfileDefault = 1002

	idPerfCompetitive = 1101
	idPerfStandard    = 1102
	idMSOff           = 1103
	idMSOn            = 1104

	idPoll1000 = 1201
	idPoll2000 = 1202
	idPoll4000 = 1203

	idTrajSmooth = 1301
	idTrajStable = 1302
	idCaptureFG  = 1401

	idMenuOpen = 9001
	idMenuExit = 9002

	transparent               = 1
	psSolid                   = 0
	dwmwaUseImmersiveDarkMode = 20

	winW = 800
	winH = 820
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
		app:               app,
		cancel:            cancel,
		currentProfile:    ProfileHit,
		profileButtons:    map[ProfileKind]uintptr{},
		perfBaseButtons:   map[int]uintptr{},
		motionSyncButtons: map[int]uintptr{},
		pollButtons:       map[PollingRate]uintptr{},
		trajButtons:       map[TrajectoryMode]uintptr{},
	}
	globalGUI = gui

	// 注册回调：后台每次自动切换流程结束后，向配置窗口投递刷新消息，
	// 由 UI 线程的 wndProc 处理，安全地重新轮询设备状态并更新界面。
	app.SetUINotify(func() {
		if globalGUI != nil && globalGUI.cfgHwnd != 0 {
			procPostMessageW.Call(globalGUI.cfgHwnd, wmRefreshUI, 0, 0)
		}
	})

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

	icon, smallIcon, err := loadEmbeddedAppIcons()
	if err != nil {
		return err
	}
	g.appIcon = icon
	g.appIconSm = smallIcon

	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wndProc := syscall.NewCallback(guiWndProc)

	g.hbrBg, _, _ = procCreateSolidBrush.Call(clrBg)

	if err := registerWindowClass(classMain, instance, icon, smallIcon, cursor, wndProc, g.hbrBg); err != nil {
		g.cleanupResources()
		return err
	}
	if err := registerWindowClass(classCfg, instance, icon, smallIcon, cursor, wndProc, g.hbrBg); err != nil {
		g.cleanupResources()
		return err
	}

	mainHwnd, err := createTopLevelWindow(classMain, appDisplayName, instance, 0, 0)
	if err != nil {
		g.cleanupResources()
		return err
	}
	g.mainHwnd = mainHwnd

	cfgHwnd, err := createTopLevelWindow(classCfg, appDisplayName, instance, winW, winH)
	if err != nil {
		g.cleanupResources()
		return err
	}
	g.cfgHwnd = cfgHwnd

	g.uiFont = createUIFont(24, false)
	g.statusFont = createUIFont(18, false)
	g.buildControls()
	g.applyFonts()
	g.syncControls()

	useDark := uintptr(1)
	procDwmSetWindowAttribute.Call(cfgHwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&useDark)), unsafe.Sizeof(useDark))

	if err := g.addTrayIcon(smallIcon); err != nil {
		g.cleanupResources()
		return err
	}
	return nil
}

func registerWindowClass(name string, instance uintptr, icon uintptr, smallIcon uintptr, cursor uintptr, wndProc uintptr, bgBrush uintptr) error {
	className := syscall.StringToUTF16Ptr(name)
	wc := WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		LpfnWndProc:   wndProc,
		HInstance:     instance,
		HIcon:         icon,
		HCursor:       cursor,
		HbrBackground: bgBrush,
		LpszClassName: className,
		HIconSm:       smallIcon,
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
	const (
		pad    = int32(30)
		secH   = int32(36)
		rowH   = int32(32)
		secGap = int32(10)
		blkGap = int32(16)
		btnH   = int32(40)
	)

	colGap := int32(24)
	twoColW := (winW - pad*2 - colGap) / 2
	threeColGap := int32(18)
	threeColW := (winW - pad*2 - threeColGap*2) / 3

	leftColX := pad
	rightColX := pad + twoColW + colGap
	poll2X := pad + threeColW + threeColGap
	poll3X := poll2X + threeColW + threeColGap

	y := int32(24)
	g.hdrStatusHW = createLabel(g, pad, y, winW-pad*2, 36, "")
	y += 54

	createLabel(g, pad, y, winW-pad*2, secH, "选择要修改的配置文件")
	y += secH + secGap
	g.profileButtons[ProfileHit] = createRadio(g, leftColX, y, twoColW, rowH, idProfileHit, "高性能配置", true)
	g.profileButtons[ProfileDefault] = createRadio(g, rightColX, y, twoColW, rowH, idProfileDefault, "省电用配置（默认配置）", false)
	y += rowH + blkGap
	g.separatorYs = append(g.separatorYs, y-(blkGap/2))

	createLabel(g, pad, y, winW-pad*2, secH, "性能模式")
	y += secH + secGap
	g.perfBaseButtons[0] = createRadio(g, leftColX, y, twoColW, rowH, idPerfCompetitive, "竞技模式", true)
	g.perfBaseButtons[1] = createRadio(g, rightColX, y, twoColW, rowH, idPerfStandard, "标准模式", false)
	y += rowH + blkGap
	g.separatorYs = append(g.separatorYs, y-(blkGap/2))

	createLabel(g, pad, y, winW-pad*2, secH, "Motion Sync")
	y += secH + secGap
	g.motionSyncButtons[0] = createRadio(g, leftColX, y, twoColW, rowH, idMSOff, "关闭", true)
	g.motionSyncButtons[1] = createRadio(g, rightColX, y, twoColW, rowH, idMSOn, "开启", false)
	y += rowH + blkGap
	g.separatorYs = append(g.separatorYs, y-(blkGap/2))

	createLabel(g, pad, y, winW-pad*2, secH, "回报率设置")
	y += secH + secGap
	g.pollButtons[Poll1000] = createRadio(g, pad, y, threeColW, rowH, idPoll1000, "1000 Hz", true)
	g.pollButtons[Poll2000] = createRadio(g, poll2X, y, threeColW, rowH, idPoll2000, "2000 Hz", false)
	g.pollButtons[Poll4000] = createRadio(g, poll3X, y, threeColW, rowH, idPoll4000, "4000 Hz", false)
	y += rowH + blkGap
	g.separatorYs = append(g.separatorYs, y-(blkGap/2))

	createLabel(g, pad, y, winW-pad*2, secH, "追踪轨迹设置")
	y += secH + secGap
	g.trajButtons[TrajSmoothSensitive] = createRadio(g, leftColX, y, twoColW, rowH, idTrajSmooth, "顺滑灵敏", true)
	g.trajButtons[TrajStableControl] = createRadio(g, rightColX, y, twoColW, rowH, idTrajStable, "稳定易控", false)
	y += rowH + blkGap
	g.separatorYs = append(g.separatorYs, y-(blkGap/2))

	createButton(g, pad, y, winW-pad*2, btnH, idCaptureFG, "十秒后登记前台窗口为高性能应用")
	y += btnH + 20

	statusLineH := int32(24)
	statusGap := int32(4)
	for i := range g.statusLines {
		g.statusLines[i] = createLabel(g, pad, y, winW-pad*2, statusLineH, "")
		y += statusLineH + statusGap
	}
}

func createLabel(g *guiState, x, y, w, h int32, text string) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(wsChild|wsVisible),
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		g.cfgHwnd,
		0,
		g.hInstance,
		0,
	)
	g.controls = append(g.controls, hwnd)
	return hwnd
}

func createRadio(g *guiState, x, y, w, h int32, id int, text string, group bool) uintptr {
	style := uintptr(wsChild | wsVisible | wsTabStop | bsAutoradio)
	if group {
		style |= wsGroup
	}
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		style,
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		g.cfgHwnd,
		uintptr(id),
		g.hInstance,
		0,
	)
	g.controls = append(g.controls, hwnd)
	return hwnd
}

func createButton(g *guiState, x, y, w, h int32, id int, text string) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(wsChild|wsVisible|wsTabStop),
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		g.cfgHwnd,
		uintptr(id),
		g.hInstance,
		0,
	)
	g.controls = append(g.controls, hwnd)
	return hwnd
}

func createUIFont(ptSize int32, bold bool) uintptr {
	weight := uintptr(400)
	if bold {
		weight = 700
	}
	name := syscall.StringToUTF16Ptr("Segoe UI Variable Text")
	font, _, _ := procCreateFontW.Call(
		u32ptrFromI32(-ptSize),
		0,
		0,
		0,
		weight,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(name)),
	)
	return font
}

func (g *guiState) applyFonts() {
	if g.uiFont != 0 {
		for _, hwnd := range g.controls {
			procSendMessageW.Call(hwnd, wmSetFont, g.uiFont, 1)
		}
	}
	if g.statusFont != 0 {
		for _, hwnd := range g.statusLines {
			procSendMessageW.Call(hwnd, wmSetFont, g.statusFont, 1)
		}
	}
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
	copyUTF16(g.trayIcon.SzTip[:], appDisplayName)

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
	case wmCommand:
		globalGUI.handleCommand(controlID(wParam))
		return 0
	case wmPaint:
		if hwnd == globalGUI.cfgHwnd {
			globalGUI.paintWindow()
			return 0
		}
	case wmEraseBkgnd:
		if hwnd == globalGUI.cfgHwnd && globalGUI.hbrBg != 0 {
			var rc RECT
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
			procFillRect.Call(wParam, uintptr(unsafe.Pointer(&rc)), globalGUI.hbrBg)
			return 1
		}
	case wmCtlColorStatic, wmCtlColorBtn:
		procSetBkMode.Call(wParam, transparent)
		procSetTextColor.Call(wParam, clrText)
		procSetBkColor.Call(wParam, clrBg)
		return globalGUI.hbrBg
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
		switch trayEvent(lParam) {
		case wmLButtonDbl:
			globalGUI.showConfigWindow()
		case wmRButtonUp:
			globalGUI.showTrayMenu()
		}
		return 0
	case wmRefreshUI:
		// 后台轮询/自动切换流程结束后触发一次界面刷新
		if globalGUI.cfgHwnd != 0 {
			globalGUI.syncControls()
		}
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func (g *guiState) paintWindow() {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(g.cfgHwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(g.cfgHwnd, uintptr(unsafe.Pointer(&ps)))

	accentBrush, _, _ := procCreateSolidBrush.Call(clrAccent)
	rc := RECT{Left: 0, Top: 0, Right: winW, Bottom: 4}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), accentBrush)
	procDeleteObject.Call(accentBrush)

	pen, _, _ := procCreatePen.Call(psSolid, 1, clrSeparator)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	for _, y := range g.separatorYs {
		procMoveToEx.Call(hdc, 30, uintptr(y), 0)
		procLineTo.Call(hdc, uintptr(winW-30), uintptr(y))
	}
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

func (g *guiState) handleCommand(id int) {
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
	case idCaptureFG:
		g.statusNote = "Foreground process append queued for 10 seconds later."
		g.scheduleForegroundAppend()
		g.syncControls()
	case idPerfCompetitive:
		g.applyPerfBase(0)
	case idPerfStandard:
		g.applyPerfBase(1)
	case idMSOff:
		g.applyMotionSync(0)
	case idMSOn:
		g.applyMotionSync(1)
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
		g.statusNote = "Write config failed."
		g.syncControls()
		showMessageBox(g.cfgHwnd, "Write config failed", err.Error(), mbOK|mbIconError)
		return
	}

	g.statusNote = "Settings saved."
	g.syncControls()
}

func decomposePerf(mode PerfMode) (base int, motionSync int) {
	switch mode {
	case PerfCompetitiveMSOff:
		return 0, 0
	case PerfStandardMSOff:
		return 1, 0
	case PerfCompetitiveMSOn:
		return 0, 1
	case PerfStandardMSOn:
		return 1, 1
	default:
		return 0, 0
	}
}

func composePerf(base int, motionSync int) PerfMode {
	if base == 0 {
		if motionSync == 0 {
			return PerfCompetitiveMSOff
		}
		return PerfCompetitiveMSOn
	}
	if motionSync == 0 {
		return PerfStandardMSOff
	}
	return PerfStandardMSOn
}

func (g *guiState) applyPerfBase(base int) {
	cfg := g.app.CurrentConfig()
	current := cfg.DefaultMode
	if g.currentProfile == ProfileHit {
		current = cfg.HitMode
	}
	_, motionSync := decomposePerf(current)
	g.applySelection(composePerf(base, motionSync), 0, 0)
}

func (g *guiState) applyMotionSync(motionSync int) {
	cfg := g.app.CurrentConfig()
	current := cfg.DefaultMode
	if g.currentProfile == ProfileHit {
		current = cfg.HitMode
	}
	base, _ := decomposePerf(current)
	g.applySelection(composePerf(base, motionSync), 0, 0)
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

	base, motionSync := decomposePerf(perf)
	setChecked(g.perfBaseButtons[0], base == 0)
	setChecked(g.perfBaseButtons[1], base == 1)
	setChecked(g.motionSyncButtons[0], motionSync == 0)
	setChecked(g.motionSyncButtons[1], motionSync == 1)

	for value, hwnd := range g.pollButtons {
		setChecked(hwnd, value == poll)
	}
	for value, hwnd := range g.trajButtons {
		setChecked(hwnd, value == traj)
	}

	devCount := g.app.DevCount()
	devErr := g.app.LastDevError()
	switch {
	case devErr != "":
		setWindowText(g.hdrStatusHW, "VAXEE Device: Not Connected")
	case devCount > 0:
		names := VaxeePhysicalDeviceNames(FindAllVaxeeDevices(), nil)
		nameText := ""
		if len(names) > 0 {
			nameText = fmt.Sprintf("(%s)", strings.Join(names, ", "))
		}
		setWindowText(g.hdrStatusHW, fmt.Sprintf("VAXEE Device: %d connected%s", devCount, nameText))
	default:
		setWindowText(g.hdrStatusHW, "VAXEE Device: Scanning...")
	}

	// Device 行：仅显示已连接物理设备数，不再附带 logical HID 信息
	deviceLine := fmt.Sprintf("Device: %d Connected", devCount)
	if devErr != "" {
		deviceLine = fmt.Sprintf("Device: %s", devErr)
	}

	setWindowText(g.statusLines[0], deviceLine)

	// Target 行：显示读取到的设备当前实际模式（最后一次成功写入设备的设置）
	targetLine := buildDeviceCurrentModeLine(g.app)
	setWindowText(g.statusLines[1], targetLine)

	batterySummary, batteryExtrema, _ := BatteryStatusLinesVAXEE()
	setWindowText(g.statusLines[2], batterySummary)
	setWindowText(g.statusLines[3], batteryExtrema)
	setWindowText(g.statusLines[4], "")
}

func (g *guiState) scheduleForegroundAppend() {
	g.app.ScheduleForegroundAppend(10 * time.Second)
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
	procPostMessageW.Call(g.mainHwnd, wmNull, 0, 0)
	procDestroyMenu.Call(menu)
}

func (g *guiState) close() {
	if g.cancel != nil {
		g.cancel()
	}
	g.app.CancelForegroundAppend()
	if g.cfgHwnd != 0 {
		procDestroyWindow.Call(g.cfgHwnd)
		g.cfgHwnd = 0
	}
	if g.mainHwnd != 0 {
		procDestroyWindow.Call(g.mainHwnd)
		g.mainHwnd = 0
	}
	g.cleanupResources()
}

func (g *guiState) cleanupResources() {
	if g.uiFont != 0 {
		procDeleteObject.Call(g.uiFont)
		g.uiFont = 0
	}
	if g.statusFont != 0 {
		procDeleteObject.Call(g.statusFont)
		g.statusFont = 0
	}
	if g.hbrBg != 0 {
		procDeleteObject.Call(g.hbrBg)
		g.hbrBg = 0
	}
	destroyIconHandle(g.appIconSm)
	g.appIconSm = 0
	destroyIconHandle(g.appIcon)
	g.appIcon = 0
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

func trayEvent(lParam uintptr) uint32 {
	return uint32(uint16(lParam & 0xffff))
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
	return "Miss (Default)"
}

// buildDeviceCurrentModeLine 把最后一次成功写入设备的设置格式化为一行，
// 用于 GUI 的 Target 行，反映设备当前的实际模式。
func buildDeviceCurrentModeLine(a *AutoSwitchApp) string {
	if !a.LastAppliedOK() {
		return "设备当前模式: 尚未同步到设备"
	}
	perf := a.LastAppliedPerf()
	poll := a.LastAppliedPoll()
	traj := a.LastAppliedTraj()

	base, motionSync := decomposePerf(perf)

	perfNameCN := "竞技模式"
	if base == 1 {
		perfNameCN = "标准模式"
	}
	msName := "关闭"
	if motionSync == 1 {
		msName = "开启"
	}
	trajNameCN := "顺滑灵敏"
	if traj == TrajStableControl {
		trajNameCN = "稳定易控"
	}

	return fmt.Sprintf("性能模式: %s | Motion Sync: %s | 回报率: %dHz | 追踪轨迹: %s",
		perfNameCN, msName, poll, trajNameCN)
}
