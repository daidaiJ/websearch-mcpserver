package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/daemon"
	"websearch/pkg/log"
	"websearch/server"

	"github.com/spf13/viper"
)

var version = "dev"

func runStart(conf *config.Config) {
	// 控制台启用时确保桌面快捷方式存在（幂等、尽力而为）：
	// 给 lazy 启动一个入口，替代开机自启动的常驻模式。
	ensureDashboardShortcut(conf)

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

	// 监听成功后才写 PID（回调），端口占用时不会留下脏 PID 文件
	if err := srv.Run(*conf, func() {
		if err := daemon.WritePID(os.Getpid(), conf.Port); err != nil {
			log.Errf("failed to write PID file: %v", err)
		}
	}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
		os.Exit(1)
	}
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
	fmt.Println("Usage: websearch-mcpserver [-c <config.yaml>] <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start       Start the server or increase refcount if already running")
	fmt.Println("  open        Lazy start: launch server if needed, then open the WebUI (desktop shortcut target)")
	fmt.Println("  stop        Decrease refcount, shutdown server when refcount reaches zero")
	fmt.Println("  kill        Force kill the server")
	fmt.Println("  status      Show server status")
	fmt.Println("  version     Show version")
	fmt.Println("  install     Install autostart script and create shortcuts (Windows only)")
	fmt.Println("  uninstall   Remove autostart and desktop shortcuts (Windows only)")
	fmt.Println("  help        Show this help")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -c, --config   config file path (default: ./config.yaml, or $WEBSEARCH_CONFIG)")
	fmt.Println("                 may appear before or after the command")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  websearch-mcpserver start")
	fmt.Println("  websearch-mcpserver start -c /path/to/config.yaml")
	fmt.Println("  websearch-mcpserver -c /path/to/config.yaml status")
}

func main() {
	var configPath string
	var showHelp bool
	flag.Usage = printUsage
	flag.StringVar(&configPath, "c", "", "config file path")
	flag.StringVar(&configPath, "config", "", "config file path")
	flag.BoolVar(&showHelp, "h", false, "show help")
	flag.BoolVar(&showHelp, "help", false, "show help")
	flag.Parse()

	// flag 包遇到首个非 flag 参数即停止，子命令后的 -c 需要二次解析；
	// 命令名取自首轮解析结果，二次解析只负责补充 flag
	firstArgs := flag.Args()
	if len(firstArgs) > 1 {
		_ = flag.CommandLine.Parse(firstArgs[1:])
	}

	if showHelp {
		printUsage()
		return
	}

	args := firstArgs
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	// 处理不需要配置文件的命令（help 在配置加载之前，无配置也可查看）
	switch args[0] {
	case "version":
		fmt.Println(version)
		return
	case "help", "-h", "--help":
		printUsage()
		return
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
		// start 且未显式指定配置路径时：首次运行自动生成可编辑的预设 config.yaml
		if args[0] == "start" && configPath == "" && os.Getenv("WEBSEARCH_CONFIG") == "" {
			var notFound viper.ConfigFileNotFoundError
			if errors.As(err, &notFound) {
				if exePath, exeErr := os.Executable(); exeErr == nil {
					presetPath := filepath.Join(filepath.Dir(exePath), "config.yaml")
					if created, werr := config.EnsureExampleFile(presetPath); werr == nil {
						if created {
							fmt.Printf("已生成预设配置: %s（可直接编辑）\n", presetPath)
						}
						conf, err = config.Load(presetPath)
					}
				}
			}
		}
		if err != nil {
			// 对于 stop/kill/status，尝试在无配置时也能执行基本操作
			if args[0] == "kill" || args[0] == "stop" || args[0] == "status" {
				conf = &config.Config{Port: 8338} // 使用默认端口尝试
			} else {
				fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// 默认启用控制台：从未显式配置过控制中心时生成 dashboard.yaml
	// （随机本机口令）。不需要时改 enabled: false 或删除该文件即回到零开销。
	// 只对 start/open 生效；stop/kill/status 等运维命令不产生文件。
	if args[0] == "start" || args[0] == "open" {
		if !config.DashboardExplicitlyConfigured() {
			if created, path, gerr := config.EnsureDashboardFile(config.GetConfigDir()); gerr == nil && created {
				fmt.Printf("已生成控制中心配置: %s（默认启用，本机管理员口令在文件内；不需要可删除或改 enabled: false）\n", path)
				if reloaded, lerr := config.Load(configPath); lerr == nil {
					conf = reloaded
				}
			}
		}
	}

	configDir := config.GetConfigDir()
	daemon.SetBaseDir(configDir)
	log.NewLogger(configDir, conf.Log)
	log.SetLoggerLevel(conf.LogLevel)

	switch args[0] {
	case "start":
		runStart(conf)
	case "open":
		runOpen(conf)
	case "stop":
		runStop(conf)
	case "kill":
		runKill(conf)
	case "status":
		runStatus(conf)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}
