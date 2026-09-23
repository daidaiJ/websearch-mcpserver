//go:build !windows

package main

import (
	"fmt"

	"websearch/pkg/config"
)

func runInstall() {
	fmt.Println("Auto-start installation is not supported on this platform.")
	fmt.Println("Please configure your system's service manager (e.g., systemd) manually.")
}

func runUninstall() {
	fmt.Println("Auto-start uninstallation is not supported on this platform.")
}

// ensureDashboardShortcut 非 Windows 平台暂无快捷方式实现（no-op）。
func ensureDashboardShortcut(conf *config.Config) {}
