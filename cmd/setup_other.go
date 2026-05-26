//go:build !windows

package main

import (
	"fmt"
)

func runInstall() {
	fmt.Println("Auto-start installation is not supported on this platform.")
	fmt.Println("Please configure your system's service manager (e.g., systemd) manually.")
}

func runUninstall() {
	fmt.Println("Auto-start uninstallation is not supported on this platform.")
}
