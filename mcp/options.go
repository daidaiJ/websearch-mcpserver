package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/fetch/everything"
	"websearch/pkg/fetch/jina"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/llm"
	"websearch/pkg/fetch/webfetch"
)

// ServerOption 服务器组件初始化选项。
type ServerOption func()

// WithSearchEngine 初始化搜索引擎（Bing 引擎 + 按模式选择主引擎）。
func WithSearchEngine(conf config.Config) ServerOption {
	return func() { applySearchEngine(conf) }
}

// WithSummarizer 在 LLM 配置就绪时初始化摘要器。
func WithSummarizer(conf config.Config) ServerOption {
	return func() { applySummarizer(conf) }
}

// WithCache 在缓存配置就绪时初始化缓存。
func WithCache(conf config.Config) ServerOption {
	return func() { applyCache(conf) }
}

// WithJinaReader 在 Jina API Key 配置就绪时初始化 Jina Reader。
func WithJinaReader(conf config.Config) ServerOption {
	return func() { applyJinaReader(conf) }
}

// WithWebFetch 在 CleanFetch 启用时初始化 go-webfetch 引擎；
// 未启用时保留配置，供 fetch_top_n 惰性初始化。
func WithWebFetch(conf config.Config) ServerOption {
	return func() { applyWebFetch(conf) }
}

// WithEverything 保存 Everything HTTP Server 配置；探测延迟到首个 MCP
// 客户端接入时（ensureEverything）——websearch 与 Everything 均为自启动，
// 启动期探测会因时序竞争在 Everything 未就绪时误判不可用，导致
// file_search 工具整个进程周期缺失。
func WithEverything(conf config.Config) ServerOption {
	return func() { applyEverything(conf) }
}

// ── 内部 apply 函数 ──────────────────────────────────────────────────────────

func applySearchEngine(conf config.Config) {
	// 引擎冷却落盘（P1-5）：与缓存数据库同目录的 engine_health.json，
	// 跨进程复用冷却状态；须在引擎构造（收养落盘状态）之前设置
	antirobot.SetHealthStore(filepath.Join(filepath.Dir(conf.GetCacheStoragePath()), "engine_health.json"))
	g, err := search.NewFromConfig(conf)
	if err != nil {
		panic(fmt.Sprintf("搜索引擎初始化失败: %v", err))
	}
	searchGroup = g
	searchapi = g.Primary
	fallbackSearch = g.Fallback
	academicSearcher = g.Academic
	smartSearchConf = conf.SmartSearch
	// smartsearch 超限落盘目录沿用 cleanfetch file_output_dir 约定（search_dump.go 未配置时回退 exe 同目录 fetchdata/）
	searchDumpDir = conf.CleanFetch.FileOutputDir
}

func applySummarizer(conf config.Config) {
	if !conf.LLMEnabled() {
		return
	}
	summarizerInst = llm.NewSummarizer(conf.LLM.BaseURL, conf.LLM.APIKey, conf.LLM.ModelId)
	log.Info("LLM 摘要功能已启用")
}

func applyCache(conf config.Config) {
	if !conf.CacheEnabled() {
		return
	}
	c, err := cache.New(conf.GetCacheStoragePath())
	if err != nil {
		panic(fmt.Sprintf("缓存初始化失败: %v", err))
	}
	cacheInst = c
}

func applyJinaReader(conf config.Config) {
	jinaInst = jina.NewFromConfig(conf.Jina, conf.Proxy)
	if jinaInst != nil {
		log.Info("Jina Reader 已启用")
	}
}

// applyEverything 保存 Everything 配置供惰性探测，不在启动期发请求。
// 未配置 url 且非 Windows 时不保存（file_search 永不启用）。
func applyEverything(conf config.Config) {
	if conf.Everything.URL == "" {
		log.Info("everything.url 未配置且当前平台非 Windows，file_search 不启用")
		return
	}
	if runtime.GOOS != "windows" {
		log.Warnf("当前平台为 %s：Everything 仅支持 Windows，任何 Linux 发行版都不建议启用 file_search，除非运行在 WSL 且 everything.url 指向 Windows 宿主的 Everything HTTP Server（%s）", runtime.GOOS, conf.Everything.URL)
	}
	everythingLazyCfg = &conf.Everything
}

// ensureEverything 惰性探测 Everything HTTP Server：连通且鉴权通过才初始化
// 客户端并返回 true（file_search 工具随之暴露）。由首个 MCP 客户端接入时
// 的工具注册触发；探测失败只记一条 Info，不影响其它工具。
func ensureEverything() bool {
	everythingMu.Lock()
	defer everythingMu.Unlock()
	if everythingInst != nil {
		return true
	}
	if everythingLazyCfg == nil {
		return false
	}
	cfg := *everythingLazyCfg
	client := everything.New(cfg.URL, cfg.Username, cfg.Password,
		time.Duration(cfg.TimeoutSec)*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Probe(ctx); err != nil {
		log.Infof("Everything HTTP Server 探测未通过（%v），file_search 工具不暴露；请确认 Everything 正在运行、已启用 HTTP Server 插件，且 everything.username/password 与服务端一致", err)
		return false
	}
	everythingInst = client
	everythingRoots = cfg.Roots
	everythingMaxResults = cfg.MaxResults
	everythingNoise = everything.NoiseDirSet(cfg.NoiseDirs)
	everythingMinAlign = cfg.MinAlignment
	log.Infof("Everything HTTP Server 探测通过，file_search 已启用（url: %s, 白名单目录: %d 个）", cfg.URL, len(cfg.Roots))
	return true
}

func applyWebFetch(conf config.Config) {
	webfetchLazyCfg = &conf
	if !conf.CleanFetch.Enabled && !conf.PDFParser.Enabled {
		return
	}
	ensureWebFetch()
}

// ensureWebFetch 确保 webfetch 可用：cleanfetch/pdf_parser 已启用时在 Init 阶段
// 就初始化；两者均关闭时由 fetch_top_n 首次使用触发（F1），避免默认部署上
// fetch_top_n 静默退化为不抓取。返回 false 表示初始化失败或无配置，调用方应报错。
func ensureWebFetch() bool {
	webfetchMu.Lock()
	defer webfetchMu.Unlock()
	if webfetchInst != nil {
		return true
	}
	if webfetchLazyCfg == nil {
		return false
	}
	f, err := webfetch.NewFromConfig(webfetchLazyCfg.CleanFetch, webfetchLazyCfg.PDFParser, webfetchLazyCfg.Proxy.GetProxyEndpoint())
	if err != nil {
		log.Errf("WebFetch 初始化失败: %v", err)
		return false
	}
	webfetchInst = f
	cleanFetchMaxSizeMB = webfetchLazyCfg.CleanFetch.MaxFetchSizeMB
	pdfMaxPages = webfetchLazyCfg.PDFParser.GetMaxPages()
	pdfMineruPageLimit = webfetchLazyCfg.PDFParser.GetMinerUPageLimit()
	pdfMineruPageBatch = webfetchLazyCfg.PDFParser.GetMinerUPageBatchSize()
	pdfMineruPageBudget = webfetchLazyCfg.PDFParser.GetMinerUPageBudget()
	log.Info("WebFetch 已按需初始化（fetch_top_n）")
	return true
}
