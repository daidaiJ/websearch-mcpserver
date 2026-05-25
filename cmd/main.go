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
	info, err := daemon.ReadPID()
	if err == nil && info != nil && daemon.IsRunning(info.PID) {
		// 服务已在运行，增加引用计数
		resp, err := daemon.PostRefCount(info.Port, 1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "server is running (PID %d), but failed to increase refcount: %v\n", info.PID, err)
			os.Exit(1)
		}
		fmt.Printf("server is already running (PID %d), refcount increased to %d\n", info.PID, resp.RefCount)
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
	srv.Run(*conf)
}

func runStop(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err != nil || info == nil {
		fmt.Println("server is not running (no PID file)")
		os.Exit(0)
	}

	if !daemon.IsRunning(info.PID) {
		fmt.Printf("server is not running (stale PID file for PID %d)\n", info.PID)
		_ = daemon.RemovePID()
		os.Exit(0)
	}

	resp, err := daemon.PostRefCount(info.Port, -1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to decrease refcount: %v\n", err)
		os.Exit(1)
	}

	if resp.RefCount > 0 {
		fmt.Printf("refcount decreased to %d, server continues running (PID %d)\n", resp.RefCount, info.PID)
		return
	}

	fmt.Printf("refcount reached zero, waiting for server (PID %d) to exit...\n", info.PID)
	if daemon.WaitForExit(info.PID, 10*time.Second) {
		fmt.Println("server exited gracefully")
		_ = daemon.RemovePID()
	} else {
		fmt.Println("timeout waiting for graceful exit, use 'kill' to force stop")
		os.Exit(1)
	}
}

func runKill(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err != nil || info == nil {
		fmt.Println("server is not running (no PID file)")
		os.Exit(0)
	}

	if !daemon.IsRunning(info.PID) {
		fmt.Printf("server is not running (stale PID file for PID %d)\n", info.PID)
		_ = daemon.RemovePID()
		os.Exit(0)
	}

	// 先尝试请求服务端自行退出
	_ = daemon.PostShutdown(info.Port)
	if daemon.WaitForExit(info.PID, 3*time.Second) {
		fmt.Println("server exited gracefully")
		_ = daemon.RemovePID()
		return
	}

	// 强制杀死
	if err := daemon.KillProcess(info.PID); err != nil {
		fmt.Fprintf(os.Stderr, "failed to kill process %d: %v\n", info.PID, err)
		os.Exit(1)
	}
	fmt.Printf("server (PID %d) killed\n", info.PID)
	_ = daemon.RemovePID()
}

func runStatus(conf *config.Config) {
	info, err := daemon.ReadPID()
	if err != nil || info == nil {
		fmt.Println("server status: stopped")
		return
	}

	if !daemon.IsRunning(info.PID) {
		fmt.Printf("server status: stopped (stale PID file for PID %d)\n", info.PID)
		return
	}

	resp, err := daemon.GetStatus(info.Port)
	if err != nil {
		fmt.Printf("server status: running (PID %d, port %d), but failed to query refcount: %v\n", info.PID, info.Port, err)
		return
	}

	fmt.Printf("server status: running (PID %d, port %d, refcount %d)\n", info.PID, info.Port, resp.RefCount)
}

func printUsage() {
	fmt.Println("Usage: websearch-mcpserver <start|stop|kill|status>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start   Start the server or increase refcount if already running")
	fmt.Println("  stop    Decrease refcount, shutdown server when refcount reaches zero")
	fmt.Println("  kill    Force kill the server")
	fmt.Println("  status  Show server status")
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
