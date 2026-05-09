package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"websearch/pkg/bing"
	"websearch/pkg/cache"
	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/search"
	"websearch/pkg/summarizer"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchParams struct {
	Query    string `json:"query" jsonschema:"description,搜索关键词，例如 'Go并发编程' 或 '2024年新能源汽车销量'"`
	Intent   string `json:"intent" jsonschema:"description,搜索意图，描述你希望通过搜索解决什么问题或获取什么信息。例如 '了解goroutine调度原理' '对比React和Vue的生态差异' '查找某API的用法示例'。提供意图后可获得更精准的结构化摘要"`
	Academic bool   `json:"academic,omitempty" jsonschema:"description,是否启用学术搜索引擎进行学术论文检索。当需要查找论文、学术研究、文献综述、技术论文等学术相关内容时设为 true"`
}

var (
	searchapi       search.SearchInf
	fallbackSearch  *search.BingSearchAdapter // 引擎兜底
	summarizerInst  *summarizer.Summarizer
	cacheInst       *cache.Cache
)

// academicSearcher 学术搜索接口（通过类型断言获取）
var academicSearcher search.AcademicSearcher

func Init(conf config.Config) error {
	// ── 初始化 Bing 引擎（兜底 + 引擎模式） ──
	initBingEngine(conf)

	// ── 初始化主搜索引擎 ──
	switch conf.GetMode() {
	case config.ModeEngine:
		// 纯引擎模式，不需要 API Key
		if fallbackSearch == nil {
			return fmt.Errorf("engine 模式需要 bing 引擎，请检查 bing 配置")
		}
		searchapi = fallbackSearch
		log.Info("搜索模式: engine（纯引擎模式，无需 API Key）")

	case config.ModeTavily:
		if conf.TavilySk == "" {
			log.Error("mode=tavily 但未配置 tavily_sk，回退到 engine 模式")
			if fallbackSearch != nil {
				searchapi = fallbackSearch
			} else {
				return fmt.Errorf("无可用搜索引擎")
			}
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
			log.Error("mode=hybrid 但未配置任何 API Key，回退到 engine 模式")
			if fallbackSearch != nil {
				searchapi = fallbackSearch
			} else {
				return fmt.Errorf("无可用搜索引擎")
			}
		} else {
			searchapi = search.NewHybridSearch(engines...)
		}

	default: // baidu
		if conf.BaiduSK == "" {
			log.Error("mode=baidu 但未配置 baidu_sk，回退到 engine 模式")
			if fallbackSearch != nil {
				searchapi = fallbackSearch
			} else {
				return fmt.Errorf("无可用搜索引擎")
			}
		} else {
			searchapi = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
		}
	}

	log.Infof("搜索模式: %s", conf.GetMode())

	// 检查是否支持学术搜索（优先从主搜索引擎获取，否则从 Bing 引擎获取）
	if as, ok := searchapi.(search.AcademicSearcher); ok {
		academicSearcher = as
		log.Info("学术搜索引擎已就绪（主引擎）")
	} else if fallbackSearch != nil {
		academicSearcher = fallbackSearch
		log.Info("学术搜索引擎已就绪（Bing 引擎）")
	}

	if conf.LLMEnabled() {
		summarizerInst = summarizer.NewSummarizer(conf.LLM.BaseURL, conf.LLM.APIKey, conf.LLM.ModelId)
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

// initBingEngine 根据配置初始化 Bing 引擎适配器。
// 默认所有引擎开启，通过 disable_* 字段关闭特定引擎。
func initBingEngine(conf config.Config) {
	bc := conf.Bing
	if !bc.Enabled {
		log.Info("Bing 引擎已禁用（bing.enabled=false）")
		return
	}

	// ── 构建通用搜索 Options（仅 Bing，不含学术引擎） ──
	regularOpts := bing.DefaultOptions()
	regularOpts.Bing.Blocked = bc.Blocked
	if bc.PerSec > 0 {
		regularOpts.Bing.PerSec = bc.PerSec
	}
	if bc.PerMin > 0 {
		regularOpts.Bing.PerMin = bc.PerMin
	}

	// ── 构建学术搜索 Options（Bing + 学术引擎） ──
	academicOpts := regularOpts
	if bc.Academic {
		academicOpts.Academic = true
		// 默认全部开启，仅当 disable_* 为 true 时关闭
		academicOpts.Arxiv.Enabled = !bc.DisableArxiv
		academicOpts.Crossref.Enabled = !bc.DisableCrossref
		academicOpts.OpenAlex.Enabled = !bc.DisableOpenAlex
		academicOpts.SemanticScholar.Enabled = !bc.DisableSemanticScholar

		// 网络区域设置：国内网络自动禁用海外引擎
		if bc.IsInternational() {
			academicOpts.Network = bing.RegionInternational
		} else {
			academicOpts.Network = bing.RegionChina
			// 国内网络下，即使用户未显式禁用，也跳过海外引擎
			if academicOpts.Arxiv.Enabled {
				log.Info("国内网络环境，arXiv 引擎将由引擎调度器自动跳过")
			}
			if academicOpts.SemanticScholar.Enabled {
				log.Info("国内网络环境，Semantic Scholar 引擎将由引擎调度器自动跳过")
			}
		}
	}

	fallbackSearch = search.NewBingSearchAdapter(regularOpts, academicOpts)

	engines := fallbackSearch.Engines()
	log.Infof("Bing 引擎已启用，通用引擎: %v", engines)
	if bc.Academic {
		acadEngines := fallbackSearch.AcademicEngines()
		log.Infof("学术引擎: %v", acadEngines)
	}
}

func GetCache() *cache.Cache {
	return cacheInst
}

// isAcademicQuery 判断查询是否为学术意图。
func isAcademicQuery(query, intent string) bool {
	keywords := []string{
		"论文", "paper", "学术", "academic", "研究", "research",
		"文献", "journal", "arxiv", "doi", "引用", "citation",
		"发表", "publish", "期刊", "会议", "conference",
		"preprint", "预印本", "综述", "survey", "review",
		"算法", "algorithm", "模型", "model",
	}
	combined := strings.ToLower(query + " " + intent)
	for _, kw := range keywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return false
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
				if rec.Summary != "" {
					log.Infof("缓存命中(exact_intent+summary): query=%s", params.Query)
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: rec.Summary}}}, nil, nil
				}
				return nil, nil, fmt.Errorf("缓存命中(exact_intent)，但摘要为空")

			case "query_only":
				results, parseErr := rec.GetRawResults()
				if parseErr == nil {
					log.Infof("缓存命中(query_only): query=%s", params.Query)
					ret, mergeErr := formatRawResults(params.Query, results)
					if mergeErr != nil {
						return nil, nil, mergeErr
					}
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

	// ---- 确定是否使用学术搜索 ----
	useAcademic := params.Academic || isAcademicQuery(params.Query, params.Intent)
	var results []search.SearchResult
	var err error

	if useAcademic && academicSearcher != nil {
		// 学术搜索
		log.Infof("使用学术搜索引擎: query=%s, explicit=%v", params.Query, params.Academic)
		results, err = academicSearcher.SearchAcademicRaw(params.Query)
		if err != nil {
			log.Errf("学术搜索失败，回退到通用搜索: %v", err)
			results = nil
		}
	}

	// 通用搜索（学术搜索无结果时也走这里）
	if results == nil {
		results, err = searchapi.SearchRaw(params.Query)
		if err != nil {
			// ── 主搜索引擎失败，尝试 Bing 引擎兜底 ──
			if fallbackSearch != nil && searchapi != fallbackSearch {
				log.Errf("主搜索引擎失败(%v)，回退到 Bing 引擎", err)
				results, err = fallbackSearch.SearchRaw(params.Query)
			}
			if err != nil {
				return nil, nil, err
			}
		}
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
