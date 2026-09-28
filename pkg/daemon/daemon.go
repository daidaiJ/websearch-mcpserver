package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var baseDir string

// adminClient 用于访问本机 admin API 的共享 client，带超时防止对端挂起时永久阻塞。
var adminClient = &http.Client{Timeout: 5 * time.Second}

func SetBaseDir(dir string) {
	baseDir = dir
}

// PIDInfo 存储在 PID 文件中的信息
type PIDInfo struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
}

// exeDir 返回可执行文件所在目录（不受 cwd 影响）。
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// PIDFileName 返回 PID 文件路径。
// 优先级：baseDir > 可执行文件目录 > cwd > temp。
func PIDFileName() string {
	if baseDir != "" {
		return filepath.Join(baseDir, ".websearch.pid")
	}
	if dir := exeDir(); dir != "" {
		return filepath.Join(dir, ".websearch.pid")
	}
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
	resp, err := adminClient.Post(url, "application/json", strings.NewReader(body))
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
	resp, err := adminClient.Get(url)
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

// GetHealth 通过 health 端点检测服务是否存活。
func GetHealth(port int) (*RefCountResponse, error) {
	url := AdminURL(port, "/health")
	resp, err := adminClient.Get(url)
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
	resp, err := adminClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
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
