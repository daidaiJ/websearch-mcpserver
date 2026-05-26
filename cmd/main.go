package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/daemon"
	"websearch/pkg/log"
	"websearch/server"
)

func runStart(conf *config.Config) {
	// 尝试通过 health 端点检测服务是否已运行
	_, err := daemon.GetHealth(conf.Port)
	if err == nil {
		// 服务已在运行，增加引用计数
		refResp, err := daemon.PostRefCount(conf.Port, 1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "server is running, but failed to increase refcount: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("server is already running, refcount increased to %d\n", refResp.RefCount)
		return
	}

	// 清理可能残留的 PID 文件
	_ = daemon.RemovePID()

	// 启动新服务，初始引用计数为 1
	srv := server.New()
	srv.SetRefCount(1)
	if err := daemon.WritePID(os.Getpid(), conf.Port); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write PID file: %v\n", err)
		os.Exit(1)
	}
	
	// 启动服务
	srv.Run(*conf)
}

func runStop(conf *config.Config) {
	// 直接通过 HTTP 检测服务是否运行
	_, err := daemon.GetHealth(conf.Port)
	if err != nil {
		fmt.Println("server is not running")
		_ = daemon.RemovePID()
		os.Exit(0)
	}

	refResp, err := daemon.PostRefCount(conf.Port, -1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to decrease refcount: %v\n", err)
		os.Exit(1)
	}

	if refResp.RefCount > 0 {
		fmt.Printf("refcount decreased to %d, server continues running\n", refResp.RefCount)
		return
	}

	fmt.Println("refcount reached zero, waiting for server to exit...")
	// 轮询 health 端点直到服务停止
	if waitForHealthDown(conf.Port, 10*time.Second) {
		fmt.Println("server exited gracefully")
		_ = daemon.RemovePID()
	} else {
		fmt.Println("timeout waiting for graceful exit, use 'kill' to force stop")
		os.Exit(1)
	}
}

func runKill(conf *config.Config) {
	// 先尝试 HTTP 关闭
	_, err := daemon.GetHealth(conf.Port)
	if err != nil {
		fmt.Println("server is not running")
		_ = daemon.RemovePID()
		os.Exit(0)
	}

	_ = daemon.PostShutdown(conf.Port)
	if waitForHealthDown(conf.Port, 3*time.Second) {
		fmt.Println("server exited gracefully")
		_ = daemon.RemovePID()
		return
	}

	// HTTP 关闭失败，尝试 PID 文件强杀
	info, pidErr := daemon.ReadPID()
	if pidErr == nil && info != nil && daemon.IsRunning(info.PID) {
		if err := daemon.KillProcess(info.PID); err != nil {
			fmt.Fprintf(os.Stderr, "failed to kill process %d: %v\n", info.PID, err)
			os.Exit(1)
		}
		fmt.Printf("server (PID %d) killed\n", info.PID)
		_ = daemon.RemovePID()
		return
	}

	fmt.Println("server did not respond to shutdown request")
	os.Exit(1)
}

func runStatus(conf *config.Config) {
	resp, err := daemon.GetHealth(conf.Port)
	if err != nil {
		fmt.Println("server status: stopped")
		_ = daemon.RemovePID()
		return
	}
	fmt.Printf("server status: running (port %d, refcount %d)\n", conf.Port, resp.RefCount)
}

// waitForHealthDown 轮询 health 端点直到服务停止。
func waitForHealthDown(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := daemon.GetHealth(port); err != nil {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func printUsage() {
	fmt.Println("Usage: websearch-mcpserver <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start       Start the server or increase refcount if already running")
	fmt.Println("  stop        Decrease refcount, shutdown server when refcount reaches zero")
	fmt.Println("  kill        Force kill the server")
	fmt.Println("  status      Show server status")
	fmt.Println("  install     Install autostart script and create shortcut in startup folder (Windows only)")
	fmt.Println("  uninstall   Remove shortcut from startup folder (Windows only)")
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "c", "", "config file path")
	flag.StringVar(&configPath, "config", "", "config file path")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	// 处理不需要配置文件的命令
	switch args[0] {
	case "install":
		runInstall()
		return
	case "uninstall":
		runUninstall()
		return
	}

	// 对于其他命令，加载配置
	conf, err := config.Load(configPath)
	if err != nil {
		// 对于 stop/kill/status，尝试在无配置时也能执行基本操作
		if args[0] == "kill" || args[0] == "stop" || args[0] == "status" {
			conf = &config.Config{Port: 8338} // 使用默认端口尝试
		} else {
			fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
			os.Exit(1)
		}
	}

	configDir := config.GetConfigDir()
	daemon.SetBaseDir(configDir)
	log.NewLogger(configDir, conf.Log)
	log.SetLoggerLevel(conf.LogLevel)

	switch args[0] {
	case "start":
		runStart(conf)
	case "stop":
		runStop(conf)
	case "kill":
		runKill(conf)
	case "status":
		runStatus(conf)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}
