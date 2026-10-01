package mcpserver

import (
	"fmt"
	"websearch/pkg/cache"
	"websearch/pkg/config"
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

// ── 内部 apply 函数 ──────────────────────────────────────────────────────────

func applySearchEngine(conf config.Config) {
	g, err := search.NewFromConfig(conf)
	if err != nil {
		panic(fmt.Sprintf("搜索引擎初始化失败: %v", err))
	}
	searchGroup = g
	searchapi = g.Primary
	fallbackSearch = g.Fallback
	academicSearcher = g.Academic
	smartSearchConf = conf.SmartSearch
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
