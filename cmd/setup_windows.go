//go:build windows

package main

import (
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

	procCoInitializeEx  = ole32.MustFindProc("CoInitializeEx")
	procCoCreateInstance = ole32.MustFindProc("CoCreateInstance")
	procCoUninitialize  = ole32.MustFindProc("CoUninitialize")
)

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
	QueryInterface        uintptr
	AddRef                uintptr
	Release               uintptr
	GetPath               uintptr
	GetIDList             uintptr
	SetIDList             uintptr
	GetDescription        uintptr
	SetDescription        uintptr
	GetWorkingDirectory   uintptr
	SetWorkingDirectory   uintptr
	GetArguments          uintptr
	SetArguments          uintptr
	GetHotkey             uintptr
	SetHotkey             uintptr
	GetShowCmd            uintptr
	SetShowCmd            uintptr
	GetIconLocation       uintptr
	SetIconLocation       uintptr
	SetRelativePath       uintptr
	Resolve               uintptr
	SetPath               uintptr
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

func createShortcut(shortcutPath, targetPath, arguments, workingDir string, windowStyle int) error {
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
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, config.ExampleConfig, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write config.yaml: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("created config.yaml")
	} else {
		fmt.Println("config.yaml already exists")
	}

	// 2. 生成 vbs
	vbsPath := filepath.Join(exeDir, "autostart.vbs")
	vbsData := fmt.Sprintf(vbsContent, configPath, exePath)
	if err := os.WriteFile(vbsPath, []byte(vbsData), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write vbs script: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("created autostart.vbs")

	// 3. 创建快捷方式
	startupDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	shortcutPath := filepath.Join(startupDir, "WebSearchMCP.lnk")

	// 创建指向 wscript.exe 的快捷方式，并设置 SW_HIDE (0) 隐藏窗口
	err = createShortcut(
		shortcutPath,
		"wscript.exe",
		fmt.Sprintf(`"%s"`, vbsPath),
		exeDir,
		0, // SW_HIDE
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shortcut: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("created shortcut in startup folder")
	fmt.Println("installation complete!")
}

func runUninstall() {
	startupDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	shortcutPath := filepath.Join(startupDir, "WebSearchMCP.lnk")

	if _, err := os.Stat(shortcutPath); os.IsNotExist(err) {
		fmt.Println("shortcut not found in startup folder")
		return
	}

	if err := os.Remove(shortcutPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to remove shortcut: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("removed shortcut from startup folder")
	fmt.Println("uninstallation complete!")
}
