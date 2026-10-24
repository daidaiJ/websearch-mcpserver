//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"websearch/pkg/config"
)

const vbsContent = `Set WshShell = CreateObject("WScript.Shell")
WshShell.Environment("Process").Item("WEBSEARCH_CONFIG") = "%s"
WshShell.Run """%s"" start", 0, False
`

var (
	ole32 = syscall.MustLoadDLL("ole32.dll")

	procCoInitializeEx   = ole32.MustFindProc("CoInitializeEx")
	procCoCreateInstance = ole32.MustFindProc("CoCreateInstance")
	procCoUninitialize   = ole32.MustFindProc("CoUninitialize")
	procCoTaskMemFree    = ole32.MustFindProc("CoTaskMemFree")

	shell32                  = syscall.NewLazyDLL("shell32.dll")
	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
)

// desktopDir 返回当前用户的桌面目录（经 SHGetKnownFolderPath 解析，
// 能正确处理 OneDrive 重定向；失败时回退 %USERPROFILE%\Desktop）。
func desktopDir() string {
	guid, err := parseGUID("{B4BFCC3A-DB2C-424C-B029-7FE99A87C641}") // FOLDERID_DESKTOP
	if err == nil {
		var p *uint16
		hr, _, _ := procSHGetKnownFolderPath.Call(
			uintptr(unsafe.Pointer(&guid)), 0, 0, uintptr(unsafe.Pointer(&p)))
		if hr == 0 && p != nil {
			dir := syscall.UTF16ToString(unsafe.Slice(p, wcslen(p)))
			procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
			if dir != "" {
				return dir
			}
		}
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
}

func wcslen(p *uint16) int {
	n := 0
	for unsafe.Slice(p, n+1)[n] != 0 {
		n++
	}
	return n
}

// parseGUID 手动解析 GUID 字符串到 syscall.GUID，避免依赖 oleaut32.dll
func parseGUID(s string) (syscall.GUID, error) {
	// {00021401-0000-0000-C000-000000000046}
	if len(s) != 38 || s[0] != '{' || s[37] != '}' {
		return syscall.GUID{}, fmt.Errorf("invalid GUID format: %s", s)
	}

	d1, err := strconv.ParseUint(s[1:9], 16, 32)
	if err != nil {
		return syscall.GUID{}, err
	}
	d2, err := strconv.ParseUint(s[10:14], 16, 16)
	if err != nil {
		return syscall.GUID{}, err
	}
	d3, err := strconv.ParseUint(s[15:19], 16, 16)
	if err != nil {
		return syscall.GUID{}, err
	}

	var d4 [8]byte
	for i := 0; i < 8; i++ {
		start := 20 + i*2
		if i >= 2 {
			start = 21 + i*2 // 跳过连字符
		}
		b, err := strconv.ParseUint(s[start:start+2], 16, 8)
		if err != nil {
			return syscall.GUID{}, err
		}
		d4[i] = byte(b)
	}

	return syscall.GUID{
		Data1: uint32(d1),
		Data2: uint16(d2),
		Data3: uint16(d3),
		Data4: d4,
	}, nil
}

type IShellLinkWVtbl struct {
	QueryInterface      uintptr
	AddRef              uintptr
	Release             uintptr
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type IShellLinkW struct {
	LpVtbl *IShellLinkWVtbl
}

type IPersistFileVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetClassID     uintptr
	IsDirty        uintptr
	Load           uintptr
	Save           uintptr
	SaveCompleted  uintptr
	GetCurFile     uintptr
}

type IPersistFile struct {
	LpVtbl *IPersistFileVtbl
}

const (
	coinitMultithreaded = 0
	coinitSFalse        = 1
	rpcEChangedMode     = 0x80010106
	slgpRawPath         = 4 // SLGP_RAWPATH：返回 .lnk 中保存的原始目标路径
)

// coInitialize 初始化当前线程的 COM，返回成对的清理函数。同线程已初始化
// （S_FALSE）时仍须成对调用 CoUninitialize；套间模式冲突时沿用已有套间。
func coInitialize() (func(), error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	switch uint32(hr) {
	case 0, coinitSFalse:
		return func() { _, _, _ = procCoUninitialize.Call() }, nil
	case rpcEChangedMode:
		return func() {}, nil
	default:
		return nil, fmt.Errorf("CoInitializeEx failed: HRESULT %#x", hr)
	}
}

func newShellLink() (*IShellLinkW, *IPersistFile, error) {
	clsid, err := parseGUID("{00021401-0000-0000-C000-000000000046}")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CLSID: %w", err)
	}
	iid, err := parseGUID("{000214F9-0000-0000-C000-000000000046}")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid IID: %w", err)
	}

	var link *IShellLinkW
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&link)),
	)
	if hr != 0 {
		return nil, nil, fmt.Errorf("CoCreateInstance failed: HRESULT %#x", hr)
	}

	persistIID, err := parseGUID("{0000010b-0000-0000-C000-000000000046}")
	if err != nil {
		_, _, _ = syscall.SyscallN(link.LpVtbl.Release, uintptr(unsafe.Pointer(link)))
		return nil, nil, fmt.Errorf("invalid PersistFile IID: %w", err)
	}
	var persist *IPersistFile
	hr, _, _ = syscall.SyscallN(link.LpVtbl.QueryInterface,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&persistIID)),
		uintptr(unsafe.Pointer(&persist)),
	)
	if hr != 0 {
		_, _, _ = syscall.SyscallN(link.LpVtbl.Release, uintptr(unsafe.Pointer(link)))
		return nil, nil, fmt.Errorf("QueryInterface IPersistFile failed: HRESULT %#x", hr)
	}
	return link, persist, nil
}

func setShellLinkString(fn uintptr, link *IShellLinkW, value string) error {
	p, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	if hr, _, _ := syscall.SyscallN(fn, uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(p))); hr != 0 {
		return fmt.Errorf("HRESULT %#x", hr)
	}
	return nil
}

func setShellLinkIcon(link *IShellLinkW, iconPath string) error {
	p, err := syscall.UTF16PtrFromString(iconPath)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(link.LpVtbl.SetIconLocation,
		uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(p)), 0)
	if hr != 0 {
		return fmt.Errorf("HRESULT %#x", hr)
	}
	return nil
}

func createShortcut(shortcutPath, targetPath, arguments, workingDir string, windowStyle int, iconPath string) error {
	uninit, err := coInitialize()
	if err != nil {
		return err
	}
	defer uninit()

	link, persist, err := newShellLink()
	if err != nil {
		return err
	}
	defer func() { _, _, _ = syscall.SyscallN(link.LpVtbl.Release, uintptr(unsafe.Pointer(link))) }()
	defer func() { _, _, _ = syscall.SyscallN(persist.LpVtbl.Release, uintptr(unsafe.Pointer(persist))) }()

	if err := setShellLinkString(link.LpVtbl.SetPath, link, targetPath); err != nil {
		return fmt.Errorf("SetPath: %w", err)
	}
	if err := setShellLinkString(link.LpVtbl.SetArguments, link, arguments); err != nil {
		return fmt.Errorf("SetArguments: %w", err)
	}
	if err := setShellLinkString(link.LpVtbl.SetWorkingDirectory, link, workingDir); err != nil {
		return fmt.Errorf("SetWorkingDirectory: %w", err)
	}
	if hr, _, _ := syscall.SyscallN(link.LpVtbl.SetShowCmd,
		uintptr(unsafe.Pointer(link)), uintptr(windowStyle)); hr != 0 {
		return fmt.Errorf("SetShowCmd: HRESULT %#x", hr)
	}
	if iconPath != "" {
		if err := setShellLinkIcon(link, iconPath); err != nil {
			return fmt.Errorf("SetIconLocation: %w", err)
		}
	}

	shortcutPathPtr, err := syscall.UTF16PtrFromString(shortcutPath)
	if err != nil {
		return err
	}
	if hr, _, _ := syscall.SyscallN(persist.LpVtbl.Save,
		uintptr(unsafe.Pointer(persist)),
		uintptr(unsafe.Pointer(shortcutPathPtr)),
		1); hr != 0 { // TRUE
		return fmt.Errorf("save: HRESULT %#x", hr)
	}
	return nil
}

func readShellLinkString(fn uintptr, link *IShellLinkW) (string, error) {
	buf := make([]uint16, 32768)
	hr, _, _ := syscall.SyscallN(fn,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if hr != 0 {
		return "", fmt.Errorf("HRESULT %#x", hr)
	}
	return syscall.UTF16ToString(buf), nil
}

func readShortcut(shortcutPath string) (shortcutSpec, error) {
	uninit, err := coInitialize()
	if err != nil {
		return shortcutSpec{}, err
	}
	defer uninit()

	link, persist, err := newShellLink()
	if err != nil {
		return shortcutSpec{}, err
	}
	defer func() { _, _, _ = syscall.SyscallN(link.LpVtbl.Release, uintptr(unsafe.Pointer(link))) }()
	defer func() { _, _, _ = syscall.SyscallN(persist.LpVtbl.Release, uintptr(unsafe.Pointer(persist))) }()

	pathPtr, err := syscall.UTF16PtrFromString(shortcutPath)
	if err != nil {
		return shortcutSpec{}, err
	}
	if hr, _, _ := syscall.SyscallN(persist.LpVtbl.Load,
		uintptr(unsafe.Pointer(persist)),
		uintptr(unsafe.Pointer(pathPtr)),
		0); hr != 0 { // STGM_READ
		return shortcutSpec{}, fmt.Errorf("load: HRESULT %#x", hr)
	}

	var spec shortcutSpec
	targetBuf := make([]uint16, 32768)
	if hr, _, _ := syscall.SyscallN(link.LpVtbl.GetPath,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&targetBuf[0])),
		uintptr(len(targetBuf)),
		0, // WIN32_FIND_DATAW：不需要
		slgpRawPath); hr != 0 {
		return shortcutSpec{}, fmt.Errorf("GetPath: HRESULT %#x", hr)
	}
	spec.Target = syscall.UTF16ToString(targetBuf)
	if spec.Arguments, err = readShellLinkString(link.LpVtbl.GetArguments, link); err != nil {
		return shortcutSpec{}, fmt.Errorf("GetArguments: %w", err)
	}
	if spec.WorkingDir, err = readShellLinkString(link.LpVtbl.GetWorkingDirectory, link); err != nil {
		return shortcutSpec{}, fmt.Errorf("GetWorkingDirectory: %w", err)
	}
	iconBuf := make([]uint16, 32768)
	if hr, _, _ := syscall.SyscallN(link.LpVtbl.GetIconLocation,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&iconBuf[0])),
		uintptr(len(iconBuf)),
		uintptr(unsafe.Pointer(&spec.IconIndex))); hr != 0 {
		return shortcutSpec{}, fmt.Errorf("GetIconLocation: HRESULT %#x", hr)
	}
	spec.IconPath = syscall.UTF16ToString(iconBuf)
	return spec, nil
}

// desktopShortcutPath 返回桌面快捷方式的固定路径；固定文件名天然去重。
func desktopShortcutPath() string {
	return filepath.Join(desktopDir(), "WebSearchMCP.lnk")
}

// startMenuShortcutPath 返回开始菜单（Programs）快捷方式的固定路径：
// 开始屏幕可直接搜索到该程序，用户也可手动「固定到开始屏幕」——
// 程序化直接 pin 开始屏幕在 Win10/11 上不可靠，以开始菜单条目为准。
func startMenuShortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"),
		"Microsoft", "Windows", "Start Menu", "Programs", "WebSearchMCP.lnk")
}

type shortcutLocation struct {
	Label string
	Path  string
}

func desktopLocation() shortcutLocation {
	return shortcutLocation{Label: "desktop", Path: desktopShortcutPath()}
}

func startMenuLocation() shortcutLocation {
	return shortcutLocation{Label: "start menu", Path: startMenuShortcutPath()}
}

func allShortcutLocations() []shortcutLocation {
	return []shortcutLocation{desktopLocation(), startMenuLocation()}
}

// shortcutLocations 按 dashboard.shortcut 配置返回应存在的快捷方式。
func shortcutLocations(mode string) []shortcutLocation {
	switch mode {
	case "start":
		return []shortcutLocation{startMenuLocation()}
	case "both":
		return []shortcutLocation{desktopLocation(), startMenuLocation()}
	default: // desktop
		return []shortcutLocation{desktopLocation()}
	}
}

// themeICO 返回主题对应的内嵌图标字节。
func themeICO(theme string) []byte {
	switch theme {
	case "blue":
		return appBlueICO
	case "mono":
		return appMonoICO
	default:
		return appGreenICO
	}
}

type shortcutSpec struct {
	Target     string
	Arguments  string
	WorkingDir string
	IconPath   string
	IconIndex  int32
}

func sameShortcutPath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	// 8.3 短名、junction/符号链接等写法差异按文件身份比较。
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
}

// matches 逐项比较 .lnk 的实际内容与期望值；路径大小写不敏感。
func (s shortcutSpec) matches(other shortcutSpec) bool {
	return sameShortcutPath(s.Target, other.Target) &&
		strings.TrimSpace(s.Arguments) == strings.TrimSpace(other.Arguments) &&
		sameShortcutPath(s.WorkingDir, other.WorkingDir) &&
		sameShortcutPath(s.IconPath, other.IconPath) &&
		s.IconIndex == other.IconIndex
}

func shortcutIconKey(theme, customIcon string) string {
	if customIcon != "" {
		return "custom"
	}
	return theme
}

// shortcutIconPath 把图标内容哈希写进文件名：内容变化即换 IconLocation
// 路径，绕开 Windows 图标缓存（同路径覆盖图标可能不刷新）。
func shortcutIconPath(exeDir, iconKey string, data []byte) string {
	sum := sha256.Sum256(data)
	name := fmt.Sprintf("websearch-%s-%s.ico", iconKey, hex.EncodeToString(sum[:4]))
	return filepath.Join(exeDir, name)
}

func shortcutIconData(theme, customIcon string) ([]byte, error) {
	if customIcon == "" {
		return themeICO(theme), nil
	}
	b, err := os.ReadFile(customIcon)
	if err != nil {
		return nil, fmt.Errorf("read brand.icon: %w", err)
	}
	return b, nil
}

// desiredShortcutMode 返回期望的落位；控制台关闭或 shortcut: off 时为 off。
func desiredShortcutMode(conf *config.Config) string {
	if conf == nil || !conf.Dashboard.Enabled {
		return "off"
	}
	return conf.Dashboard.GetShortcut()
}

type shortcutReport struct {
	Created   []shortcutLocation
	Updated   []shortcutLocation
	Removed   []shortcutLocation
	Unchanged []shortcutLocation
}

func (r shortcutReport) print() {
	for _, group := range []struct {
		action    string
		locations []shortcutLocation
	}{
		{"created", r.Created},
		{"updated", r.Updated},
		{"removed", r.Removed},
		{"unchanged", r.Unchanged},
	} {
		for _, loc := range group.locations {
			fmt.Printf("shortcut %s (%s)\n", group.action, loc.Label)
		}
	}
}

func removeShortcutLocations(locations []shortcutLocation, report *shortcutReport) error {
	for _, loc := range locations {
		err := os.Remove(loc.Path)
		switch {
		case err == nil:
			report.Removed = append(report.Removed, loc)
		case os.IsNotExist(err):
		default:
			return fmt.Errorf("remove %s shortcut: %w", loc.Label, err)
		}
	}
	return nil
}

// syncShortcuts 以配置和当前 exe 为期望状态，逐个校正已知位置的 .lnk：
// 缺失则创建，目标/参数/工作目录/图标任一不符（含手工改坏、升级换图标、
// exe 挪位置）则重写，落位不再需要的位置删除。幂等，重复执行只报 unchanged。
func syncShortcuts(conf *config.Config, exePath string) (shortcutReport, error) {
	var report shortcutReport
	mode := desiredShortcutMode(conf)
	exePath, _ = filepath.Abs(exePath)
	exeDir := filepath.Dir(exePath)
	// 旧版用 marker 跳过重建，内容校验后不再需要。
	defer func() { _ = os.Remove(shortcutMarkerPath(exeDir)) }()

	if mode == "off" {
		return report, removeShortcutLocations(allShortcutLocations(), &report)
	}

	wanted := shortcutLocations(mode)
	needed := make(map[string]bool, len(wanted))
	for _, loc := range wanted {
		needed[loc.Path] = true
	}
	var stale []shortcutLocation
	for _, loc := range allShortcutLocations() {
		if !needed[loc.Path] {
			stale = append(stale, loc)
		}
	}
	if err := removeShortcutLocations(stale, &report); err != nil {
		return report, err
	}

	iconData, err := shortcutIconData(conf.Dashboard.Brand.GetTheme(), conf.Dashboard.Brand.Icon)
	if err != nil {
		return report, err
	}
	iconPath := shortcutIconPath(exeDir, shortcutIconKey(
		conf.Dashboard.Brand.GetTheme(), conf.Dashboard.Brand.Icon), iconData)
	if existing, rerr := os.ReadFile(iconPath); rerr != nil || !bytes.Equal(existing, iconData) {
		if werr := os.WriteFile(iconPath, iconData, 0o644); werr != nil {
			return report, werr
		}
	}

	want := shortcutSpec{
		Target:     exePath,
		Arguments:  "open",
		WorkingDir: exeDir,
		IconPath:   iconPath,
	}
	for _, loc := range wanted {
		_, statErr := os.Stat(loc.Path)
		existed := statErr == nil
		if existed {
			if actual, rerr := readShortcut(loc.Path); rerr == nil && actual.matches(want) {
				report.Unchanged = append(report.Unchanged, loc)
				continue
			}
		}
		if err := createShortcut(loc.Path, want.Target, want.Arguments, want.WorkingDir, 0, want.IconPath); err != nil {
			return report, fmt.Errorf("write %s shortcut: %w", loc.Label, err)
		}
		if existed {
			report.Updated = append(report.Updated, loc)
		} else {
			report.Created = append(report.Created, loc)
		}
	}
	return report, nil
}

func shortcutMarkerPath(exeDir string) string {
	return filepath.Join(exeDir, "websearch-shortcut.theme")
}

// removeShortcuts 删除全部已知位置的快捷方式（uninstall 时调用）。
func removeShortcuts() error {
	var report shortcutReport
	return removeShortcutLocations(allShortcutLocations(), &report)
}

// syncDashboardShortcut 按配置同步快捷方式（start/open/install 共用）。
// 快捷方式维护是尽力而为：失败只打日志，不影响服务启动。
func syncDashboardShortcut(conf *config.Config) {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "shortcut: %v\n", err)
		return
	}
	report, err := syncShortcuts(conf, exePath)
	report.print()
	if err != nil {
		fmt.Fprintf(os.Stderr, "shortcut: %v\n", err)
	}
}

func runInstall() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get executable path: %v\n", err)
		os.Exit(1)
	}
	exePath, _ = filepath.Abs(exePath)
	exeDir := filepath.Dir(exePath)

	// 1. 检查并生成 config.yaml
	configPath := filepath.Join(exeDir, "config.yaml")
	created, err := config.EnsureExampleFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to write config.yaml: %v\n", err)
		os.Exit(1)
	}
	if created {
		fmt.Println("created config.yaml")
	} else {
		fmt.Println("config.yaml already exists")
	}

	// 控制中心配置（默认启用；口令与开关都在 dashboard.yaml 内）
	if created, path, err := config.EnsureDashboardFile(exeDir); err == nil && created {
		fmt.Println("created dashboard.yaml (control center enabled):", path)
	} else {
		fmt.Println("dashboard.yaml already exists")
	}

	// 2. 生成 vbs
	vbsPath := filepath.Join(exeDir, "autostart.vbs")
	vbsData := fmt.Sprintf(vbsContent, configPath, exePath)
	if err := os.WriteFile(vbsPath, []byte(vbsData), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write vbs script: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("created autostart.vbs")

	// 3. 创建启动文件夹快捷方式（开机自启动，可选场景）
	startupDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	shortcutPath := filepath.Join(startupDir, "WebSearchMCP.lnk")

	// 创建指向 wscript.exe 的快捷方式，并设置 SW_HIDE (0) 隐藏窗口
	err = createShortcut(
		shortcutPath,
		"wscript.exe",
		fmt.Sprintf(`"%s"`, vbsPath),
		exeDir,
		0,  // SW_HIDE
		"", // 沿用目标默认图标
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shortcut: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("created shortcut in startup folder")

	// 4. 快捷方式（lazy 启动入口：双击 = start + 打开控制台）。与 start/open
	// 共用同一同步逻辑：落位、主题、自定义图标按 dashboard.yaml 实际配置
	// （配置读不到时回退默认启用 + 桌面 + 内置主题）。
	conf, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config for shortcuts: %v\n", err)
		conf = &config.Config{Dashboard: config.DashboardConfig{Enabled: true}}
	}
	syncDashboardShortcut(conf)

	fmt.Println("installation complete!")
}

func runUninstall() {
	startupDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	shortcutPath := filepath.Join(startupDir, "WebSearchMCP.lnk")

	if _, err := os.Stat(shortcutPath); os.IsNotExist(err) {
		fmt.Println("shortcut not found in startup folder")
	} else if err := os.Remove(shortcutPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to remove shortcut: %v\n", err)
		os.Exit(1)
	} else {
		fmt.Println("removed shortcut from startup folder")
	}

	if err := removeShortcuts(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to remove shortcuts: %v\n", err)
		os.Exit(1)
	} else {
		fmt.Println("shortcuts removed (if existed)")
	}
	fmt.Println("uninstallation complete!")
}
