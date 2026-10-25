package mcpserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/log"
)

var cacheNamespace string

// Only the hash is persisted; keys and proxy credentials never leave memory.
func searchCacheNamespace(conf config.Config) (string, error) {
	sources := config.Config{
		Mode: conf.GetMode(), Network: conf.Network, BlackListHost: conf.BlackListHost,
		Baidu: conf.Baidu, Tavily: conf.Tavily, Exa: conf.Exa, Anysearch: conf.Anysearch, Doubao: conf.Doubao,
		Bing: conf.Bing, So360: conf.So360, Wikipedia: conf.Wikipedia, GoogleNews: conf.GoogleNews,
		DuckDuckGo: conf.DuckDuckGo, Google: conf.Google, Academic: conf.Academic,
		Proxy: conf.Proxy, SmartSearch: conf.SmartSearch, Apipool: conf.Apipool,
		LLM: conf.LLM, Jina: conf.Jina, CleanFetch: conf.CleanFetch, PDFParser: conf.PDFParser,
	}
	data, err := json.Marshal(struct {
		Sources      config.Config
		ProxyAllowed bool
	}{sources, conf.Proxy.ProxyResolver() != nil})
	if err != nil {
		return "", fmt.Errorf("序列化搜索缓存配置失败: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func requestSearchCache() *cache.Cache {
	if cacheInst == nil {
		return nil
	}
	view, err := cacheInst.ForRequest(cacheNamespace)
	if err != nil {
		log.ErrorT("log.mcp.cache_generation_read_error_skipping_cache", "err", err)
		return nil
	}
	return view
}
