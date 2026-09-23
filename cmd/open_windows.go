//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// spawnServerDetached 以分离进程拉起 `websearch-mcpserver start`：
// 隐藏窗口、脱离当前控制台，open 进程退出后服务继续驻留。
func spawnServerDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "start")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
	return cmd.Start()
}

// openBrowser 用系统默认浏览器打开 URL。
func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
