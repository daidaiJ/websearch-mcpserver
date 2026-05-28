package mcpserver

import (
	"fmt"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/jina"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/summarizer"
	"websearch/pkg/webfetch"
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

// WithWebFetch 在 CleanFetch 启用时初始化 go-webfetch 引擎。
func WithWebFetch(conf config.Config) ServerOption {
	return func() { applyWebFetch(conf) }
}

// ── 内部 apply 函数 ──────────────────────────────────────────────────────────

func applySearchEngine(conf config.Config) {
	g, err := search.NewFromConfig(conf)
	if err != nil {
		panic(fmt.Sprintf("搜索引擎初始化失败: %v", err))
	}
	searchapi = g.Primary
	fallbackSearch = g.Fallback
	academicSearcher = g.Academic
}

func applySummarizer(conf config.Config) {
	if !conf.LLMEnabled() {
		return
	}
	summarizerInst = summarizer.NewSummarizer(conf.LLM.BaseURL, conf.LLM.APIKey, conf.LLM.ModelId)
	log.Info("LLM 摘要功能已启用")
}

func applyCache(conf config.Config) {
	if !conf.CacheEnabled() {
		return
	}
	c, err := cache.New(conf.Cache.StoragePath)
	if err != nil {
		panic(fmt.Sprintf("缓存初始化失败: %v", err))
	}
	cacheInst = c
}

func applyJinaReader(conf config.Config) {
	jinaInst = jina.NewFromConfig(conf.Jina, conf.Proxy)
	if jinaInst != nil {
		log.Info("Jina Reader 已启用（通过代理）")
	}
}

func applyWebFetch(conf config.Config) {
	if !conf.CleanFetch.Enabled && !conf.PDFParser.Enabled {
		return
	}
	f, err := webfetch.NewFromConfig(conf.CleanFetch)
	if err != nil {
		log.Errf("WebFetch 初始化失败: %v", err)
		return
	}
	webfetchInst = f
}
