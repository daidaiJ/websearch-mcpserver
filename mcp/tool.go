package mcpserver

import (
	"context"
	"fmt"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/summarizer"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchParams struct {
	Query  string `json:"query" jsonschema:"description,搜索关键词，例如 'Go并发编程' 或 '2024年新能源汽车销量"`
	Intent string `json:"intent" jsonschema:"description,搜索意图，描述你希望通过搜索解决什么问题或获取什么信息。例如 '了解goroutine调度原理''对比React和Vue的生态差异' '查找某API的用法示例'。提供意图后可获得更精准的结构化摘要"`
}

var searchapi search.SearchInf
var summarizerInst *summarizer.Summarizer
var cacheInst *cache.Cache

func Init(conf config.Config) error {
	switch conf.GetMode() {
	case config.ModeTavily:
		if conf.TavilySk == "" {
			log.Error("mode=tavily 但未配置 tavily_sk，回退到 baidu")
			searchapi = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
		} else {
			searchapi = search.NewTavilySearch(conf.TavilySk)
		}
	case config.ModeHybrid:
		var engines []search.SearchInf
		if conf.BaiduSK != "" {
			engines = append(engines, search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost))
		}
		if conf.TavilySk != "" {
			engines = append(engines, search.NewTavilySearch(conf.TavilySk))
		}
		if len(engines) == 0 {
			log.Error("mode=hybrid 但未配置任何搜索引擎 sk")
			panic("hybrid 模式需要至少配置 baidu_sk 或 tavily_sk")
		}
		searchapi = search.NewHybridSearch(engines...)
	default:
		searchapi = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
	}
	log.Infof("搜索模式: %s", conf.GetMode())

	if conf.LLMEnabled() {
		summarizerInst = summarizer.NewSummarizer(conf.LLM.BaseURL, conf.LLM.APIKey)
		log.Info("LLM 摘要功能已启用")
	}

	if conf.CacheEnabled() {
		c, err := cache.New(conf.Cache.StoragePath)
		if err != nil {
			return fmt.Errorf("缓存初始化失败: %w", err)
		}
		cacheInst = c
	}
	return nil
}

func GetCache() *cache.Cache {
	return cacheInst
}

func WebSearch(ctx context.Context, req *mcp.CallToolRequest, params *SearchParams) (*mcp.CallToolResult, any, error) {
	if searchapi == nil {
		return nil, nil, fmt.Errorf("api 初始化未完成")
	}

	// ---- 缓存查询 ----
	if cacheInst != nil {
		rec, hitType, err := cacheInst.Lookup(params.Query, params.Intent)
		if err != nil {
			log.Errf("缓存查询异常，跳过缓存: %v", err)
		} else if rec != nil {
			switch hitType {
			case "exact_intent":
				// query + intent 精确命中
				if rec.Summary != "" {
					log.Infof("缓存命中(exact_intent+summary): query=%s", params.Query)
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: rec.Summary}}}, nil, nil
				}
				return nil, nil, fmt.Errorf("缓存命中(exact_intent)，但摘要为空")

			case "query_only":
				// 仅 query 命中 → 返回原始网页内容
				results, parseErr := rec.GetRawResults()
				if parseErr == nil {
					log.Infof("缓存命中(query_only): query=%s", params.Query)
					ret, mergeErr := formatRawResults(params.Query, results)
					if mergeErr != nil {
						return nil, nil, mergeErr
					}
					// 如果 LLM 可用且有 intent，后台异步生成摘要并补充到缓存
					if params.Intent != "" && summarizerInst != nil && rec.Summary == "" {
						go func() {
							output, sumErr := summarizerInst.Summarize(params.Query, params.Intent, results)
							if sumErr == nil {
								_ = cacheInst.UpdateSummary(params.Query, params.Intent, output)
								log.Infof("后台异步摘要完成: query=%s, intent=%s", params.Query, params.Intent)
							}
						}()
					}
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
				}
			}
		}
	}

	// ---- 缓存未命中，执行实际搜索 ----
	results, err := searchapi.SearchRaw(params.Query)
	if err != nil {
		return nil, nil, err
	}

	// 有 intent 且 LLM 可用 → 生成摘要
	if params.Intent != "" && summarizerInst != nil {
		output, sumErr := summarizerInst.Summarize(params.Query, params.Intent, results)
		if sumErr == nil {
			if cacheInst != nil {
				_ = cacheInst.Store(params.Query, params.Intent, results, output)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
		}
		log.Errf("LLM 摘要失败，回退到原始结果: %v", sumErr)
	}

	// 无 intent 或 LLM 失败 → 返回原始结果
	ret, err := formatRawResults(params.Query, results)
	if err != nil {
		return nil, nil, err
	}
	if cacheInst != nil {
		_ = cacheInst.Store(params.Query, params.Intent, results, "")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
}

func formatRawResults(query string, results []search.SearchResult) (string, error) {
	return searchapi.MergeContent(query, results)
}
