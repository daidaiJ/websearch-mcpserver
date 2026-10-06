package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	mcpserver "websearch/mcp"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/log"
)

var version = "dev"

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: websearch-mcp-cli [-c <config.yaml>] [command]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  (none)      Start MCP server over stdio")
	fmt.Fprintln(os.Stderr, "  init        Write example config.yaml")
	fmt.Fprintln(os.Stderr, "  version     Show version")
	fmt.Fprintln(os.Stderr, "  help        Show this help")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  -c, --config   config file path; may appear before or after the command")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  websearch-mcp-cli")
	fmt.Fprintln(os.Stderr, "  websearch-mcp-cli init -c /path/to/config.yaml")
}

func runInit(configPath string) error {
	path := configPath
	if path == "" {
		path = "config.yaml"
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config dir: %w", err)
		}
	}
	created, err := config.EnsureExampleFile(path)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if created {
		fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	} else {
		fmt.Fprintf(os.Stderr, "config already exists: %s\n", path)
	}
	return nil
}

func runStdio(conf *config.Config) {
	configDir := config.GetConfigDir()
	log.NewLoggerTo(os.Stderr, configDir, conf.Log)
	log.SetLoggerLevel(conf.LogLevel)

	if err := mcpserver.Init(*conf,
		mcpserver.WithSearchEngine(*conf),
		mcpserver.WithSummarizer(*conf),
		mcpserver.WithCache(*conf),
		mcpserver.WithWebFetch(*conf),
		mcpserver.WithJinaReader(*conf),
	); err != nil {
		fmt.Fprintf(os.Stderr, "failed to init: %v\n", err)
		os.Exit(1)
	}

	var cleanup *cache.CleanupScheduler
	if conf.CacheEnabled() && mcpserver.GetCache() != nil {
		cleanup = cache.NewCleanupScheduler(mcpserver.GetCache(), conf.GetCleanupInterval())
		cleanup.Start()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := mcpserver.RunStdio(ctx, *conf)

	if cleanup != nil {
		cleanup.Stop(context.Background())
	}
	if c := mcpserver.GetCache(); c != nil {
		c.Close()
	}
	if wf := mcpserver.GetWebFetch(); wf != nil {
		wf.Close()
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "stdio server: %v\n", err)
		os.Exit(1)
	}
	_ = log.CloseFile()
}

func main() {
	mcpserver.SetServerVersion(version)
	flag.Usage = printUsage
	var configPath string
	var showHelp bool
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
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "version":
		fmt.Println(version)
		return
	case "help":
		printUsage()
		return
	case "init":
		if err := runInit(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	case "":
		// default: stdio
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}

	conf, err := config.LoadOrDefault(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	runStdio(conf)
}
