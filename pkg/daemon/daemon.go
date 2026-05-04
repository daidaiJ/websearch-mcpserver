package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// PIDInfo 存储在 PID 文件中的信息
type PIDInfo struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
}

// PIDFileName 返回 PID 文件路径
func PIDFileName() string {
	// 优先使用当前工作目录，便于项目内管理
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, ".websearch.pid")
	}
	return filepath.Join(os.TempDir(), "websearch-mcpserver.pid")
}

// ReadPID 读取 PID 文件
func ReadPID() (*PIDInfo, error) {
	data, err := os.ReadFile(PIDFileName())
	if err != nil {
		return nil, err
	}
	var info PIDInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// WritePID 写入 PID 文件
func WritePID(pid, port int) error {
	info := PIDInfo{PID: pid, Port: port}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(PIDFileName(), data, 0644)
}

// RemovePID 删除 PID 文件
func RemovePID() error {
	return os.Remove(PIDFileName())
}

// IsRunning 检测进程是否存活（Windows 兼容）
func IsRunning(pid int) bool {
	if runtime.GOOS == "windows" {
		handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
		if err != nil {
			return false
		}
		defer syscall.CloseHandle(handle)

		var exitCode uint32
		err = syscall.GetExitCodeProcess(handle, &exitCode)
		if err != nil {
			return false
		}
		return exitCode == 259 // STILL_ACTIVE
	}

	// Unix-like: 发送信号 0 检测
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(os.Signal(nil))
	return err == nil
}

// AdminURL 构建 admin API URL
func AdminURL(port int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/__admin%s", port, path)
}

// RefCountResponse admin API 返回结构
type RefCountResponse struct {
	RefCount int    `json:"ref_count"`
	Message  string `json:"message,omitempty"`
}

// PostRefCount 向服务端发送引用计数变更请求
func PostRefCount(port, delta int) (*RefCountResponse, error) {
	url := AdminURL(port, "/refcount")
	body := fmt.Sprintf(`{"delta":%d}`, delta)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RefCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetStatus 获取服务端状态
func GetStatus(port int) (*RefCountResponse, error) {
	url := AdminURL(port, "/status")
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RefCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PostShutdown 请求服务端强制关闭
func PostShutdown(port int) error {
	url := AdminURL(port, "/shutdown")
	_, err := http.Post(url, "application/json", nil)
	return err
}

// WaitForExit 等待进程退出（轮询）
func WaitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// KillProcess 强制结束进程
func KillProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
