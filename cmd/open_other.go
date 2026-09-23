//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// spawnServerDetached 非 Windows 平台的分离启动：以独立会话运行，
// open 进程退出后服务继续驻留。
func spawnServerDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "start")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
