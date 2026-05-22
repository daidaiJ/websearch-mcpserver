//go:build !windows

package daemon

import "os"

// IsRunning 检测进程是否存活。
func IsRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(os.Signal(nil)) == nil
}
