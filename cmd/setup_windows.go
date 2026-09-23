//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

	shell32                 = syscall.NewLazyDLL("shell32.dll")
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

func createShortcut(shortcutPath, targetPath, arguments, workingDir string, windowStyle int, iconPath string) error {
	// 初始化 COM
	hr, _, _ := procCoInitializeEx.Call(0, 0)
	if hr != 0 {
		return fmt.Errorf("CoInitializeEx failed: %d", hr)
	}
	defer procCoUninitialize.Call()

	// 解析 CLSID_ShellLink
	clsid, err := parseGUID("{00021401-0000-0000-C000-000000000046}")
	if err != nil {
		return fmt.Errorf("invalid CLSID: %v", err)
	}

	// 解析 IID_IShellLinkW
	iid, err := parseGUID("{000214F9-0000-0000-C000-000000000046}")
	if err != nil {
		return fmt.Errorf("invalid IID: %v", err)
	}

	// 创建 ShellLink 对象
	var shellLink *IShellLinkW
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&shellLink)),
	)
	if hr != 0 {
		return fmt.Errorf("CoCreateInstance failed: %d", hr)
	}
	defer syscall.SyscallN(shellLink.LpVtbl.Release, uintptr(unsafe.Pointer(shellLink)))

	// 设置目标路径
	targetPathPtr, _ := syscall.UTF16PtrFromString(targetPath)
	syscall.SyscallN(shellLink.LpVtbl.SetPath, uintptr(unsafe.Pointer(shellLink)), uintptr(unsafe.Pointer(targetPathPtr)))

	// 设置参数
	argumentsPtr, _ := syscall.UTF16PtrFromString(arguments)
	syscall.SyscallN(shellLink.LpVtbl.SetArguments, uintptr(unsafe.Pointer(shellLink)), uintptr(unsafe.Pointer(argumentsPtr)))

	// 设置工作目录
	workingDirPtr, _ := syscall.UTF16PtrFromString(workingDir)
	syscall.SyscallN(shellLink.LpVtbl.SetWorkingDirectory, uintptr(unsafe.Pointer(shellLink)), uintptr(unsafe.Pointer(workingDirPtr)))

	// 设置窗口样式 (SW_HIDE = 0)
	syscall.SyscallN(shellLink.LpVtbl.SetShowCmd, uintptr(unsafe.Pointer(shellLink)), uintptr(windowStyle))

	// 设置图标（空串则沿用目标文件默认图标）
	if iconPath != "" {
		iconPtr, _ := syscall.UTF16PtrFromString(iconPath)
		syscall.SyscallN(shellLink.LpVtbl.SetIconLocation,
			uintptr(unsafe.Pointer(shellLink)), uintptr(unsafe.Pointer(iconPtr)), 0)
	}

	// 获取 IPersistFile 接口
	persistFileIID, err := parseGUID("{0000010b-0000-0000-C000-000000000046}")
	if err != nil {
		return fmt.Errorf("invalid PersistFile IID: %v", err)
	}

	var persistFile *IPersistFile
	hr, _, _ = syscall.SyscallN(shellLink.LpVtbl.QueryInterface,
		uintptr(unsafe.Pointer(shellLink)),
		uintptr(unsafe.Pointer(&persistFileIID)),
		uintptr(unsafe.Pointer(&persistFile)),
	)
	if hr != 0 {
		return fmt.Errorf("QueryInterface IPersistFile failed: %d", hr)
	}
	defer syscall.SyscallN(persistFile.LpVtbl.Release, uintptr(unsafe.Pointer(persistFile)))

	// 保存快捷方式
	shortcutPathPtr, _ := syscall.UTF16PtrFromString(shortcutPath)
	syscall.SyscallN(persistFile.LpVtbl.Save,
		uintptr(unsafe.Pointer(persistFile)),
		uintptr(unsafe.Pointer(shortcutPathPtr)),
		1, // TRUE
	)

	return nil
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

// shortcutTargets 按 dashboard.shortcut 配置返回应存在的快捷方式路径。
func shortcutTargets(mode string) []string {
	switch mode {
	case "start":
		return []string{startMenuShortcutPath()}
	case "both":
		return []string{desktopShortcutPath(), startMenuShortcutPath()}
	default: // desktop
		return []string{desktopShortcutPath()}
	}
}

// themeICO 返回主题对应的内嵌图标字节；每次启动的图标检查只做字符串
// 比较，主题变化时才落盘新 ico 并重建 .lnk（轻量级行为）。
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

// ensureShortcuts 创建/维护「双击即启动并打开控制台」的快捷方式（目标 =
// websearch-mcpserver open，图标跟随品牌主题），落位由 dashboard.shortcut
// 决定（桌面 / 开始菜单 / 两者）。marker 记录主题、exe 路径与落位模式，
// 均未变化且目标文件在位时每次启动只做一次文件读取（轻量级检查）；任一
// 变化（换主题、exe 挪位置、改落位）才清理旧位置并重建。
func ensureShortcuts(theme, customIcon, mode string) (created bool, err error) {
	targets := shortcutTargets(mode)
	exePath, err := os.Executable()
	if err != nil {
		return false, err
	}
	exePath, _ = filepath.Abs(exePath)
	exeDir := filepath.Dir(exePath)
	markerPath := filepath.Join(exeDir, "websearch-shortcut.theme")
	iconKey := theme
	if customIcon != "" {
		iconKey = "custom"
	}
	want := iconKey + "\n" + exePath + "\n" + mode + "\n"

	upToDate := true
	for _, p := range targets {
		if _, statErr := os.Stat(p); statErr != nil {
			upToDate = false
			break
		}
	}
	if upToDate {
		if b, rerr := os.ReadFile(markerPath); rerr == nil && string(b) == want {
			return false, nil
		}
	}

	// 落位变化时清理另一处的旧快捷方式（desktop ↔ start 互换不留死链）
	for _, stale := range []string{desktopShortcutPath(), startMenuShortcutPath()} {
		if _, statErr := os.Stat(stale); statErr == nil {
			_ = os.Remove(stale)
		}
	}

	// 每主题/自定义一个 ico 文件：换图标即换 IconLocation 路径，绕开 Windows 图标缓存
	var iconData []byte
	if customIcon != "" {
		b, rerr := os.ReadFile(customIcon)
		if rerr != nil {
			return false, fmt.Errorf("read brand.icon: %w", rerr)
		}
		iconData = b
	} else {
		iconData = themeICO(theme)
	}
	iconPath := filepath.Join(exeDir, "websearch-"+iconKey+".ico")
	if existing, err := os.ReadFile(iconPath); err != nil || !bytes.Equal(existing, iconData) {
		if err := os.WriteFile(iconPath, iconData, 0o644); err != nil {
			return false, err
		}
	}

	for _, p := range targets {
		if err := createShortcut(
			p,
			exePath,
			"open",
			exeDir,
			0, // SW_HIDE：open 进程秒退，窗口无需可见
			iconPath,
		); err != nil {
			return false, err
		}
	}
	if err := os.WriteFile(markerPath, []byte(want), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// removeShortcuts 删除全部已知位置的快捷方式（uninstall 时调用）。
func removeShortcuts() error {
	var firstErr error
	for _, p := range []string{desktopShortcutPath(), startMenuShortcutPath()} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		} else if err := os.Remove(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ensureDashboardShortcut 在控制台启用时按 dashboard.shortcut 配置创建/维护
// 快捷方式（幂等、尽力而为），由 start 启动路径调用：给 lazy 启动一个入口，
// 替代开机自启动的常驻。shortcut: off 时不创建任何快捷方式。
func ensureDashboardShortcut(conf *config.Config) {
	if !conf.Dashboard.Enabled {
		return
	}
	mode := conf.Dashboard.GetShortcut()
	if mode == "off" {
		return
	}
	created, err := ensureShortcuts(conf.Dashboard.Brand.GetTheme(), conf.Dashboard.Brand.Icon, mode)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "shortcut (%s): %v\n", mode, err)
	case created:
		fmt.Println("shortcut created/refreshed (WebSearchMCP, mode: " + mode + ")")
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
		0, // SW_HIDE
		"", // 沿用目标默认图标
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shortcut: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("created shortcut in startup folder")

	// 4. 快捷方式（lazy 启动入口：双击 = start + 打开控制台）。落位与主题
	// 按 dashboard.yaml 实际配置（读不到配置时回退桌面 + 默认主题）。
	mode, theme := "desktop", "green"
	if conf, err := config.Load(configPath); err == nil {
		mode, theme = conf.Dashboard.GetShortcut(), conf.Dashboard.Brand.GetTheme()
	}
	if mode == "off" {
		fmt.Println("dashboard.shortcut=off: skip shortcuts")
	} else {
		created, serr := ensureShortcuts(theme, "", mode)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "shortcut (%s): %v\n", mode, serr)
		} else if created {
			fmt.Println("created shortcut (WebSearchMCP, mode: " + mode + ")")
		} else {
			fmt.Println("shortcut already exists")
		}
	}

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
