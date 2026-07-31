//go:build windows

package main

import (
	"fmt"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type HIDD_ATTRIBUTES struct {
	Size      uint32
	VendorID  uint16
	ProductID uint16
	Version   uint16
}

type SP_DEVICE_INTERFACE_DATA struct {
	CbSize             uint32
	InterfaceClassGuid GUID
	Flags              uint32
	Reserved           uintptr
}

type SP_DEVINFO_DATA struct {
	CbSize    uint32
	ClassGuid GUID
	DevInst   uint32
	Reserved  uintptr
}

type DEVPROPKEY struct {
	Fmtid GUID
	Pid   uint32
}

// HIDP_CAPS 结构：包含 FeatureReportByteLength（包含 ReportID 字节）[2](https://learn.microsoft.com/zh-tw/windows-hardware/drivers/ddi/hidpi/ns-hidpi-_hidp_caps)
type HIDP_CAPS struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

// syscall 没有 ERROR_NO_MORE_ITEMS 常量，Windows 该错误码是 259
const ERROR_NO_MORE_ITEMS syscall.Errno = 259

// HidP_GetCaps 成功状态：HIDP_STATUS_SUCCESS（常用 0x00110000）[4](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidpi/nf-hidpi-hidp_getcaps)[5](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidpi/nf-hidpi-hidp_getspecificvaluecaps)
const HIDP_STATUS_SUCCESS uint32 = 0x00110000

var (
	setupapiHID = syscall.NewLazyDLL("setupapi.dll")
	hidDLLHID   = syscall.NewLazyDLL("hid.dll")
	k32HID      = syscall.NewLazyDLL("kernel32.dll")

	reVaxeeLogicalCollection = regexp.MustCompile(`&col\d+`)
	reVaxeeInterfaceNumber   = regexp.MustCompile(`&mi_\d+`)

	procSetupDiGetClassDevsW_HID             = setupapiHID.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces_HID      = setupapiHID.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW_HID = setupapiHID.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList_HID     = setupapiHID.NewProc("SetupDiDestroyDeviceInfoList")
	procSetupDiGetDevicePropertyW_HID        = setupapiHID.NewProc("SetupDiGetDevicePropertyW")

	procHidDGetHidGuid_HID            = hidDLLHID.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes_HID         = hidDLLHID.NewProc("HidD_GetAttributes")
	procHidDGetManufacturerString_HID = hidDLLHID.NewProc("HidD_GetManufacturerString")
	procHidDGetProductString_HID      = hidDLLHID.NewProc("HidD_GetProductString")

	procHidDSetFeature_HID        = hidDLLHID.NewProc("HidD_SetFeature") // [1](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_setfeature)
	procHidDGetFeature_HID        = hidDLLHID.NewProc("HidD_GetFeature") // [3](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_getfeature)
	procHidDGetPreparsedData_HID  = hidDLLHID.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData_HID = hidDLLHID.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps_HID           = hidDLLHID.NewProc("HidP_GetCaps") // [4](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidpi/nf-hidpi-hidp_getcaps)

	procCreateFileW_HID  = k32HID.NewProc("CreateFileW")
	procCloseHandle_HID  = k32HID.NewProc("CloseHandle")
	procGetLastError_HID = k32HID.NewProc("GetLastError")
)

var devPropKeyDeviceContainerID = DEVPROPKEY{
	Fmtid: GUID{
		Data1: 0x8c7ed206,
		Data2: 0x3f8a,
		Data3: 0x4827,
		Data4: [8]byte{0xb3, 0xab, 0xae, 0x9e, 0x1f, 0xae, 0xfc, 0x6c},
	},
	Pid: 2,
}

const (
	DIGCF_PRESENT         = 0x00000002
	DIGCF_DEVICEINTERFACE = 0x00000010

	GENERIC_READ  = 0x80000000
	GENERIC_WRITE = 0x40000000

	FILE_SHARE_READ  = 0x00000001
	FILE_SHARE_WRITE = 0x00000002

	OPEN_EXISTING = 3

	vaxeeCommandInterval = 100 * time.Millisecond
)

// Unicode DetailData：cbSize x86=6 x64=8；但 DevicePath 偏移固定为 4（DWORD cbSize 之后）[6](https://blog.csdn.net/ShmilyCode/article/details/73105035)[7](https://www.cnblogs.com/ollie-lin/p/10188001.html)[8](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/VAXEE%E6%8A%93%E5%8C%85%E7%AD%9B%E9%80%89%E7%BB%93%E6%9E%9C.txt)
func detailCbSizeW() uint32 {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return 8
	}
	return 6
}

const detailDevicePathOffset = 4

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

// 生成指定长度的 feature report（保证 buffer 长度符合 caps.FeatureReportByteLength）[1](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_setfeature)[2](https://learn.microsoft.com/zh-tw/windows-hardware/drivers/ddi/hidpi/ns-hidpi-_hidp_caps)
func buildReportSized(total int, cmd byte, val byte) []byte {
	if total < 6 {
		total = 6
	}
	buf := make([]byte, total)
	buf[0] = 0x0e // ReportID 14（你的抓包就是 0x0e）[9](https://blog.csdn.net/frederick_master/article/details/78845161)
	buf[1] = 0xa5
	buf[2] = cmd
	buf[3] = 0x02
	buf[4] = 0x01
	buf[5] = val
	return buf
}

func lastErrno() syscall.Errno {
	r1, _, _ := procGetLastError_HID.Call()
	return syscall.Errno(r1)
}

func sendFeatureReport(path string, report []byte) error {
	if len(report) == 0 {
		return fmt.Errorf("empty report")
	}
	h, err := openHIDPath(path)
	if err != nil {
		return err
	}
	defer closeHandle(h)

	r1, _, _ := procHidDSetFeature_HID.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&report[0])),
		uintptr(len(report)),
	)
	if r1 == 0 {
		return fmt.Errorf("HidD_SetFeature failed: %v", lastErrno()) // e.g. ERROR_INVALID_FUNCTION => "Incorrect function."
	}
	return nil
}

func getFeature(path string, reportID byte, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("invalid length")
	}
	h, err := openHIDPath(path)
	if err != nil {
		return nil, err
	}
	defer closeHandle(h)

	buf := make([]byte, length)
	buf[0] = reportID // HidD_GetFeature 需要第一个字节写 report ID [3](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_getfeature)
	r1, _, _ := procHidDGetFeature_HID.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("HidD_GetFeature failed: %v", lastErrno())
	}
	return buf, nil
}

func openHIDPath(path string) (syscall.Handle, error) {
	p16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	// RW
	h, _, _ := procCreateFileW_HID.Call(
		uintptr(unsafe.Pointer(p16)),
		uintptr(GENERIC_READ|GENERIC_WRITE),
		uintptr(FILE_SHARE_READ|FILE_SHARE_WRITE),
		0,
		uintptr(OPEN_EXISTING),
		0,
		0,
	)
	if h != 0 && h != uintptr(syscall.InvalidHandle) {
		return syscall.Handle(h), nil
	}

	// Write only（有些设备只允许写）
	h2, _, _ := procCreateFileW_HID.Call(
		uintptr(unsafe.Pointer(p16)),
		uintptr(GENERIC_WRITE),
		uintptr(FILE_SHARE_READ|FILE_SHARE_WRITE),
		0,
		uintptr(OPEN_EXISTING),
		0,
		0,
	)
	if h2 != 0 && h2 != uintptr(syscall.InvalidHandle) {
		return syscall.Handle(h2), nil
	}

	return 0, fmt.Errorf("CreateFileW failed: %s (%v)", path, lastErrno())
}

func openHIDPathForQuery(path string) (syscall.Handle, error) {
	p16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, _, _ := procCreateFileW_HID.Call(
		uintptr(unsafe.Pointer(p16)),
		0,
		uintptr(FILE_SHARE_READ|FILE_SHARE_WRITE),
		0,
		uintptr(OPEN_EXISTING),
		0,
		0,
	)
	if h != 0 && h != uintptr(syscall.InvalidHandle) {
		return syscall.Handle(h), nil
	}
	return 0, fmt.Errorf("CreateFileW(query) failed: %s (%v)", path, lastErrno())
}

func closeHandle(h syscall.Handle) {
	procCloseHandle_HID.Call(uintptr(h))
}

func hidGuid() GUID {
	var g GUID
	procHidDGetHidGuid_HID.Call(uintptr(unsafe.Pointer(&g)))
	return g
}

func utf16FromPtr(p *uint16) string {
	if p == nil {
		return ""
	}
	var arr []uint16
	for i := 0; ; i++ {
		u := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + uintptr(i*2)))
		if u == 0 {
			break
		}
		arr = append(arr, u)
	}
	return syscall.UTF16ToString(arr)
}

func hidGetString(h syscall.Handle, proc *syscall.LazyProc) string {
	buf := make([]uint16, 256)
	r1, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2))
	if r1 == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// 读取 HIDP_CAPS（拿 FeatureReportByteLength / UsagePage / Usage）[4](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidpi/nf-hidpi-hidp_getcaps)[2](https://learn.microsoft.com/zh-tw/windows-hardware/drivers/ddi/hidpi/ns-hidpi-_hidp_caps)
func queryCaps(h syscall.Handle) (HIDP_CAPS, error) {
	var pp uintptr
	r1, _, _ := procHidDGetPreparsedData_HID.Call(uintptr(h), uintptr(unsafe.Pointer(&pp)))
	if r1 == 0 || pp == 0 {
		return HIDP_CAPS{}, fmt.Errorf("HidD_GetPreparsedData failed: %v", lastErrno())
	}
	defer procHidDFreePreparsedData_HID.Call(pp)

	var caps HIDP_CAPS
	st, _, _ := procHidPGetCaps_HID.Call(pp, uintptr(unsafe.Pointer(&caps)))
	if uint32(st) != HIDP_STATUS_SUCCESS {
		return HIDP_CAPS{}, fmt.Errorf("HidP_GetCaps failed: 0x%08x", uint32(st))
	}
	return caps, nil
}

func guidToString(g GUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1],
		g.Data4[2], g.Data4[3], g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7],
	)
}

func queryContainerID(hDevInfo uintptr, devInfoData *SP_DEVINFO_DATA) string {
	if hDevInfo == 0 || devInfoData == nil {
		return ""
	}
	var propType uint32
	var required uint32
	var containerGUID GUID
	r1, _, _ := procSetupDiGetDevicePropertyW_HID.Call(
		hDevInfo,
		uintptr(unsafe.Pointer(devInfoData)),
		uintptr(unsafe.Pointer(&devPropKeyDeviceContainerID)),
		uintptr(unsafe.Pointer(&propType)),
		uintptr(unsafe.Pointer(&containerGUID)),
		unsafe.Sizeof(containerGUID),
		uintptr(unsafe.Pointer(&required)),
		0,
	)
	if r1 == 0 {
		return ""
	}
	return guidToString(containerGUID)
}

func queryDeviceInfo(hDevInfo uintptr, devInfoData *SP_DEVINFO_DATA, path string) (VaxeeDeviceInfo, bool) {
	h, err := openHIDPathForQuery(path)
	if err != nil {
		return VaxeeDeviceInfo{}, false
	}
	defer closeHandle(h)

	var attr HIDD_ATTRIBUTES
	attr.Size = uint32(unsafe.Sizeof(attr))
	r1, _, _ := procHidDGetAttributes_HID.Call(uintptr(h), uintptr(unsafe.Pointer(&attr)))
	if r1 == 0 {
		return VaxeeDeviceInfo{}, false
	}

	manu := hidGetString(h, procHidDGetManufacturerString_HID)
	prod := hidGetString(h, procHidDGetProductString_HID)

	caps, capErr := queryCaps(h)
	// caps 失败不影响枚举展示，但会影响后续“选择控制通道”
	if capErr != nil {
		return VaxeeDeviceInfo{
			Path: path, VID: attr.VendorID, PID: attr.ProductID,
			Manufacturer: manu, Product: prod, ContainerID: queryContainerID(hDevInfo, devInfoData),
		}, true
	}

	return VaxeeDeviceInfo{
		Path: path, VID: attr.VendorID, PID: attr.ProductID,
		Manufacturer: manu, Product: prod, ContainerID: queryContainerID(hDevInfo, devInfoData),
		UsagePage: caps.UsagePage, Usage: caps.Usage,
		FeatureLen: caps.FeatureReportByteLength,
	}, true
}

func vaxeeDeviceKey(dev VaxeeDeviceInfo) string {
	key := strings.ToLower(strings.TrimSpace(dev.ContainerID))
	if key != "" {
		return key
	}
	key = vaxeePhysicalKey(dev.Path)
	if key != "" {
		return key
	}
	return fmt.Sprintf("%04x:%04x:%s:%s", dev.VID, dev.PID, strings.ToLower(dev.Manufacturer), strings.ToLower(dev.Product))
}

func orderedVaxeeInterfaces(devs []VaxeeDeviceInfo) []VaxeeDeviceInfo {
	order := make([]VaxeeDeviceInfo, 0, len(devs))
	for _, d := range devs {
		if strings.HasSuffix(strings.ToLower(d.Path), `\kbd`) {
			continue
		}
		order = append(order, d)
	}
	for _, d := range devs {
		if strings.HasSuffix(strings.ToLower(d.Path), `\kbd`) {
			order = append(order, d)
		}
	}
	return order
}

func EnumerateVaxeeDevices() ([]VaxeeDeviceInfo, error) {
	g := hidGuid()

	hDevInfo, _, _ := procSetupDiGetClassDevsW_HID.Call(
		uintptr(unsafe.Pointer(&g)), 0, 0,
		uintptr(DIGCF_PRESENT|DIGCF_DEVICEINTERFACE),
	)
	if hDevInfo == 0 || hDevInfo == uintptr(syscall.InvalidHandle) {
		return nil, fmt.Errorf("SetupDiGetClassDevsW failed: %v", lastErrno())
	}
	defer procSetupDiDestroyDeviceInfoList_HID.Call(hDevInfo)

	var out []VaxeeDeviceInfo
	for idx := 0; ; idx++ {
		var ifData SP_DEVICE_INTERFACE_DATA
		ifData.CbSize = uint32(unsafe.Sizeof(ifData))
		var devInfoData SP_DEVINFO_DATA
		devInfoData.CbSize = uint32(unsafe.Sizeof(devInfoData))

		r1, _, eEnum := procSetupDiEnumDeviceInterfaces_HID.Call(
			hDevInfo, 0,
			uintptr(unsafe.Pointer(&g)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifData)),
		)
		if r1 == 0 {
			if errno, ok := eEnum.(syscall.Errno); ok && errno == ERROR_NO_MORE_ITEMS {
				break
			}
			break
		}

		var required uint32
		procSetupDiGetDeviceInterfaceDetailW_HID.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&ifData)),
			0, 0,
			uintptr(unsafe.Pointer(&required)),
			0,
		)
		if required == 0 {
			continue
		}

		buf := make([]byte, required)
		*(*uint32)(unsafe.Pointer(&buf[0])) = detailCbSizeW()

		r2, _, _ := procSetupDiGetDeviceInterfaceDetailW_HID.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&ifData)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(required),
			uintptr(unsafe.Pointer(&required)),
			uintptr(unsafe.Pointer(&devInfoData)),
		)
		if r2 == 0 {
			continue
		}

		pathPtr := (*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + detailDevicePathOffset))
		path := utf16FromPtr(pathPtr)
		if path == "" {
			continue
		}

		info, ok := queryDeviceInfo(hDevInfo, &devInfoData, path)
		if !ok {
			continue
		}
		m := strings.ToLower(info.Manufacturer)
		p := strings.ToLower(info.Product)
		if strings.Contains(m, "vaxee") || strings.Contains(p, "vaxee") {
			out = append(out, info)
		}
	}
	return out, nil
}

// 选择“真正能收发 ReportID=0x0e Feature Report”的顶级集合
// 用 HidD_GetFeature 探测最安全：失败就换下一个。[3](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_getfeature)[2](https://learn.microsoft.com/zh-tw/windows-hardware/drivers/ddi/hidpi/ns-hidpi-_hidp_caps)
func SelectVaxeeControlPath() (VaxeeDeviceInfo, error) {
	ds, err := EnumerateVaxeeDevices()
	if err != nil {
		return VaxeeDeviceInfo{}, err
	}
	if len(ds) == 0 {
		return VaxeeDeviceInfo{}, fmt.Errorf("no VAXEE HID device found")
	}

	// 逐个探测
	for _, d := range orderedVaxeeInterfaces(ds) {
		flen := int(d.FeatureLen)
		// 如果 caps 取不到，就先用 64 试探（你的抓包 wLength=64）[9](https://blog.csdn.net/frederick_master/article/details/78845161)
		if flen <= 0 {
			flen = 64
		}

		_, e := getFeature(d.Path, 0x0e, flen)
		if e == nil {
			// 找到了可用控制通道
			return d, nil
		}
	}

	return VaxeeDeviceInfo{}, fmt.Errorf("no VAXEE top-level collection accepts Feature ReportID=0x0e")
}

func SelectAllVaxeeControlPaths() ([]VaxeeDeviceInfo, error) {
	ds, err := EnumerateVaxeeDevices()
	if err != nil {
		return nil, err
	}
	return SelectAllVaxeeControlPathsFrom(ds)
}

func SelectAllVaxeeControlPathsFrom(ds []VaxeeDeviceInfo) ([]VaxeeDeviceInfo, error) {
	if len(ds) == 0 {
		return nil, fmt.Errorf("no VAXEE HID device found")
	}

	physicalCount := CountUniqueVaxeeDevices(ds)
	seen := make(map[string]struct{}, physicalCount)
	out := make([]VaxeeDeviceInfo, 0, physicalCount)

	for _, d := range orderedVaxeeInterfaces(ds) {
		key := vaxeeDeviceKey(d)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(d.Path))
		}
		if _, ok := seen[key]; ok {
			continue
		}

		flen := int(d.FeatureLen)
		if flen <= 0 {
			flen = 64
		}
		if _, err := getFeature(d.Path, 0x0e, flen); err != nil {
			continue
		}

		seen[key] = struct{}{}
		out = append(out, d)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no VAXEE top-level collection accepts Feature ReportID=0x0e")
	}
	if physicalCount > len(out) {
		return out, fmt.Errorf("found %d VAXEE device(s), but only %d expose the control report", physicalCount, len(out))
	}
	return out, nil
}

func FindOneVaxeeDevice() (VaxeeDeviceInfo, error) {
	return SelectVaxeeControlPath()
}

func FindAllVaxeeDevices() []VaxeeDeviceInfo {
	devs, err := EnumerateVaxeeDevices()
	if err != nil {
		return nil
	}
	return devs
}

func vaxeePhysicalKey(path string) string {
	key := strings.ToLower(strings.TrimSpace(path))
	key = strings.TrimSuffix(key, `\kbd`)
	key = reVaxeeInterfaceNumber.ReplaceAllString(key, "")
	key = reVaxeeLogicalCollection.ReplaceAllString(key, "")
	return key
}

func CountUniqueVaxeeDevices(devs []VaxeeDeviceInfo) int {
	if len(devs) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(devs))
	for _, dev := range devs {
		seen[vaxeeDeviceKey(dev)] = struct{}{}
	}
	return len(seen)
}

func VaxeeDeviceSetSignature(devs []VaxeeDeviceInfo) string {
	if len(devs) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(devs))
	keys := make([]string, 0, len(devs))
	for _, dev := range devs {
		key := vaxeeDeviceKey(dev)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return strings.Join(keys, "|")
}

func VaxeePhysicalDeviceNames(devs []VaxeeDeviceInfo, aliases map[string]string) []string {
	if len(devs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(devs))
	firstByKey := make(map[string]VaxeeDeviceInfo, len(devs))
	keys := make([]string, 0, len(devs))

	for _, dev := range devs {
		key := vaxeeDeviceKey(dev)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		firstByKey[key] = dev
		keys = append(keys, key)
	}

	names := make([]string, 0, len(keys))
	for _, key := range keys {
		dev := firstByKey[key]
		if alias := vaxeeDeviceAlias(dev, key, aliases); alias != "" {
			names = append(names, alias)
			continue
		}

		// 回退：从 HID Product / Manufacturer 字符串解析出真实设备名
		// （例如 "VAXEE E1" -> "E1"，"VAXEE NP-01S V2" -> "NP01S V2"）
		// vaxeeBatteryDeviceName 在拿不到有效字符串时会返回 "D{n}" 占位，
		// 此处仍把占位当作未知设备处理。
		if name := vaxeeBatteryDeviceName(dev, 0); !strings.HasPrefix(name, "D") {
			names = append(names, name)
			continue
		}

		names = append(names, fmt.Sprintf("未知设备[%s]", shortVaxeeKey(key)))
	}
	return names
}

func vaxeeDeviceAlias(dev VaxeeDeviceInfo, key string, aliases map[string]string) string {
	candidates := []string{
		key,
		strings.ToLower(strings.TrimSpace(dev.ContainerID)),
		vaxeePhysicalKey(dev.Path),
		shortVaxeeKey(key),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		candidate = strings.ToLower(candidate)
		if alias := strings.TrimSpace(aliases[candidate]); alias != "" {
			return alias
		}
		if alias := strings.TrimSpace(builtinVaxeeDeviceAliases[candidate]); alias != "" {
			return alias
		}
	}
	return ""
}

var builtinVaxeeDeviceAliases = map[string]string{
	"05e6f9e6-3cc8-11f1-a655-d8bbc1c6fbcf": "E1",
	"5594f398-51e6-11f1-a675-bb83b5341d34": "NP-01S V2",
}

func shortVaxeeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "unknown"
	}
	if len(key) > 8 {
		return key[:8]
	}
	return key
}

func resolveVaxeeControlDevice(path string) (VaxeeDeviceInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return FindOneVaxeeDevice()
	}

	dev := VaxeeDeviceInfo{Path: path, FeatureLen: 64}
	if devs, err := EnumerateVaxeeDevices(); err == nil {
		for _, d := range devs {
			if strings.EqualFold(d.Path, path) {
				dev = d
				break
			}
		}
	}
	return dev, nil
}

// 应用设置：按 caps.FeatureLen 发送，避免长度不匹配[1](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_setfeature)[2](https://learn.microsoft.com/zh-tw/windows-hardware/drivers/ddi/hidpi/ns-hidpi-_hidp_caps)
func ApplyVaxeeSetting(path string, perf PerfMode, poll PollingRate) error {
	dev, err := resolveVaxeeControlDevice(path)
	if err != nil {
		return err
	}
	return applyVaxeeSettingToDevice(dev, perf, poll)
}

func applyVaxeeSettingToDevice(dev VaxeeDeviceInfo, perf PerfMode, poll PollingRate) error {
	flen := int(dev.FeatureLen)
	if flen <= 0 {
		flen = 64
	}

	// 1) 性能模式 cmd=0x08
	if err := sendFeatureReport(dev.Path, buildReportSized(flen, 0x08, byte(perf))); err != nil {
		return fmt.Errorf("perf feature report failed: %w", err)
	}
	time.Sleep(vaxeeCommandInterval)

	// 2) 回报率 cmd=0x07
	yy, err := pollingToYY(poll)
	if err != nil {
		return err
	}
	if err := sendFeatureReport(dev.Path, buildReportSized(flen, 0x07, yy)); err != nil {
		return fmt.Errorf("poll feature report failed: %w", err)
	}
	time.Sleep(vaxeeCommandInterval)
	return nil
}

func ApplyVaxeeProfileToAll(devs []VaxeeDeviceInfo, perf PerfMode, poll PollingRate, traj TrajectoryMode) ([]VaxeeDeviceInfo, error) {
	if len(devs) == 0 {
		var err error
		devs, err = EnumerateVaxeeDevices()
		if err != nil {
			return nil, err
		}
	}

	controls, selectErr := SelectAllVaxeeControlPathsFrom(devs)
	if len(controls) == 0 {
		return nil, selectErr
	}

	var errs []string
	if selectErr != nil {
		errs = append(errs, selectErr.Error())
	}
	for i, dev := range controls {
		if err := applyVaxeeSettingToDevice(dev, perf, poll); err != nil {
			errs = append(errs, fmt.Sprintf("device %d setting: %v", i+1, err))
			continue
		}
		if err := applyVaxeeTrajectoryToDevice(dev, traj); err != nil {
			errs = append(errs, fmt.Sprintf("device %d trajectory: %v", i+1, err))
		}
	}

	if len(errs) > 0 {
		return controls, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return controls, nil
}

// EnumerateAllHidDevices 枚举所有 HID 顶级集合（能读到 attributes/字符串的接口）
// 用于：启动时找不到 VAXEE 时打印一次全量设备信息（便于定位识别规则）。
func EnumerateAllHidDevices() ([]VaxeeDeviceInfo, error) {
	g := hidGuid()

	hDevInfo, _, _ := procSetupDiGetClassDevsW_HID.Call(
		uintptr(unsafe.Pointer(&g)), 0, 0,
		uintptr(DIGCF_PRESENT|DIGCF_DEVICEINTERFACE),
	)
	if hDevInfo == 0 || hDevInfo == uintptr(syscall.InvalidHandle) {
		return nil, fmt.Errorf("SetupDiGetClassDevsW failed: %v", lastErrno())
	}
	defer procSetupDiDestroyDeviceInfoList_HID.Call(hDevInfo)

	var out []VaxeeDeviceInfo
	for idx := 0; ; idx++ {
		var ifData SP_DEVICE_INTERFACE_DATA
		ifData.CbSize = uint32(unsafe.Sizeof(ifData))
		var devInfoData SP_DEVINFO_DATA
		devInfoData.CbSize = uint32(unsafe.Sizeof(devInfoData))

		r1, _, eEnum := procSetupDiEnumDeviceInterfaces_HID.Call(
			hDevInfo, 0,
			uintptr(unsafe.Pointer(&g)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifData)),
		)
		if r1 == 0 {
			if errno, ok := eEnum.(syscall.Errno); ok && errno == ERROR_NO_MORE_ITEMS {
				break
			}
			break
		}

		// query required size
		var required uint32
		procSetupDiGetDeviceInterfaceDetailW_HID.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&ifData)),
			0, 0,
			uintptr(unsafe.Pointer(&required)),
			0,
		)
		if required == 0 {
			continue
		}

		buf := make([]byte, required)
		*(*uint32)(unsafe.Pointer(&buf[0])) = detailCbSizeW()

		r2, _, _ := procSetupDiGetDeviceInterfaceDetailW_HID.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&ifData)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(required),
			uintptr(unsafe.Pointer(&required)),
			uintptr(unsafe.Pointer(&devInfoData)),
		)
		if r2 == 0 {
			continue
		}

		pathPtr := (*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + detailDevicePathOffset))
		path := utf16FromPtr(pathPtr)
		if path == "" {
			continue
		}

		info, ok := queryDeviceInfo(hDevInfo, &devInfoData, path)
		if !ok {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// ==================== 追踪轨迹（Tracking Trajectory） ====================
// 抓包确认：
// 顺滑灵敏 -> SET_REPORT Data Fragment: 0e a5 13 02 01 00 ...（后续全 0）
// 稳定易控 -> SET_REPORT Data Fragment: 0e a5 13 01 01 00 ...（后续全 0）[1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)
//
// 注意：与你现有 buildReportSized 不同，追踪轨迹的“模式位”在 byte3（0x02/0x01），而不是 byte5(val)。[1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)[1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)

func buildTrajectoryReportSized(total int, mode TrajectoryMode) []byte {
	if total < 6 {
		total = 6
	}
	buf := make([]byte, total)
	buf[0] = 0x0e
	buf[1] = 0xa5
	buf[2] = 0x13       // cmd/page
	buf[3] = byte(mode) // 0x02=顺滑灵敏, 0x01=稳定易控
	buf[4] = 0x01
	buf[5] = 0x00
	return buf
}

func ApplyVaxeeTrajectory(path string, mode TrajectoryMode) error {
	dev, err := resolveVaxeeControlDevice(path)
	if err != nil {
		return err
	}
	return applyVaxeeTrajectoryToDevice(dev, mode)
}

func applyVaxeeTrajectoryToDevice(dev VaxeeDeviceInfo, mode TrajectoryMode) error {
	flen := int(dev.FeatureLen)
	if flen <= 0 {
		flen = 64
	}

	if err := sendFeatureReport(dev.Path, buildTrajectoryReportSized(flen, mode)); err != nil {
		return fmt.Errorf("trajectory feature report failed: %w", err)
	}
	time.Sleep(vaxeeCommandInterval)
	return nil
}
