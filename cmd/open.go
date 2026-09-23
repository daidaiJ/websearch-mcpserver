// open_windows.go / open_other.go 共用的 open 子命令核心逻辑：
// 「lazy 启动」入口 —— 服务未运行则先拉起，再打开控制台页面。
// 桌面快捷方式的目标就是本子命令（websearch-mcpserver open）。

package main

import (
	"fmt"
	"os"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/daemon"
)

const openWaitTimeout = 15 * time.Second

// runOpen 先确保服务在运行（不在则后台拉起并等待就绪），然后打开 WebUI。
func runOpen(conf *config.Config) {
	url := fmt.Sprintf("http://127.0.0.1:%d/dashboard/", conf.Port)

	running := false
	if _, err := daemon.GetHealth(conf.Port); err == nil {
		running = true
	}

	if !running {
		if err := spawnServerDetached(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
			os.Exit(1)
		}
		if !waitHealthy(conf.Port, openWaitTimeout) {
			fmt.Fprintf(os.Stderr, "server did not become healthy within %s\n", openWaitTimeout)
			os.Exit(1)
		}
		fmt.Println("server started")
	} else {
		fmt.Println("server already running")
	}

	if !conf.Dashboard.Enabled {
		fmt.Println("控制台未启用：在 dashboard.yaml 或 config.yaml 设置 dashboard.enabled: true 后重启再试")
		return
	}

	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "failed to open browser: %v\n", err)
		fmt.Println("请手动访问:", url)
		return
	}
	fmt.Println("opened", url)
}

func waitHealthy(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := daemon.GetHealth(port); err == nil {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
