package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"websearch/pkg/client"
	"websearch/pkg/i18n"
	"websearch/pkg/search/core"
)

// BaiduAISearchImpl 百度智能搜索（chat/completions），返回 LLM 生成的回答 + 参考来源。
type BaiduAISearchImpl struct {
	name             string
	keys             *KeyPool
	blacklist        []string
	recency          string
	model            string
	searchSource     string
	enableReasoning  bool
	enableDeepSearch bool
	searchMode       string
}

// 百度智能搜索请求/响应结构体

type baiduAIReqMsg struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type baiduAIReq struct {
	Messages            []baiduAIReqMsg         `json:"messages"`
	Model               string                  `json:"model,omitempty"`
	SearchSource        string                  `json:"search_source,omitempty"`
	ResourceTypeFilter  []baiduSearchTypeFliter `json:"resource_type_filter,omitempty"`
	SearchRecencyFilter string                  `json:"search_recency_filter,omitempty"`
	BlockWebsites       []string                `json:"block_websites,omitempty"`
	Stream              bool                    `json:"stream"`
	EnableReasoning     bool                    `json:"enable_reasoning,omitempty"`
	EnableDeepSearch    bool                    `json:"enable_deep_search,omitempty"`
	SearchMode          string                  `json:"search_mode,omitempty"`
	Temperature         float64                 `json:"temperature,omitempty"`
	TopP                float64                 `json:"top_p,omitempty"`
}

type baiduAIChoiceMsg struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type baiduAIChoice struct {
	FinishReason string           `json:"finish_reason"`
	Index        int              `json:"index"`
	Message      baiduAIChoiceMsg `json:"message"`
}

type baiduAIReference struct {
	Content string `json:"content"`
	Title   string `json:"title"`
	Url     string `json:"url"`
	Date    string `json:"date"`
}

type baiduAIResponse struct {
	Choices    []baiduAIChoice    `json:"choices"`
	References []baiduAIReference `json:"references"`
	RequestID  string             `json:"request_id"`
}

// NewBaiduAISearch 创建百度智能搜索实例。
// model 为空时走免费百度搜索（不产生 LLM 费用），传入模型名时启用 LLM 智能搜索生成。
func NewBaiduAISearch(keys *KeyPool, blacklist []string, model, searchSource string, enableReasoning, enableDeepSearch bool, searchMode string) *BaiduAISearchImpl {
	if searchSource == "" {
		searchSource = "baidu_search_v2"
	}
	if searchMode == "" {
		searchMode = "auto"
	}
	return &BaiduAISearchImpl{
		name:             "baidu_ai",
		keys:             keys,
		blacklist:        blacklist,
		recency:          "semiyear",
		model:            model,
		searchSource:     searchSource,
		enableReasoning:  enableReasoning,
		enableDeepSearch: enableDeepSearch,
		searchMode:       searchMode,
	}
}

func (b *BaiduAISearchImpl) Name() string { return b.name }

func (b *BaiduAISearchImpl) Search(query string) (string, error) {
	results, err := b.SearchRaw(query)
	if err != nil {
		return "", fmt.Errorf("%s, %w", i18n.T("provider.baidu_ai.call_failed"), err)
	}
	return b.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口。
func (b *BaiduAISearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	return b.SearchRawWithTimeRangeContext(context.Background(), query, lookbackDays)
}

// SearchRawContext 实现 core.ContextSearcher：ctx 下传 HTTP 请求。
func (b *BaiduAISearchImpl) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	return b.search(ctx, query)
}

// SearchRawWithTimeRangeContext 实现 core.ContextTimeRanger。
func (b *BaiduAISearchImpl) SearchRawWithTimeRangeContext(ctx context.Context, query string, lookbackDays int) ([]core.SearchResult, error) {
	request := *b
	request.recency = ""
	if lookbackDays > 0 {
		request.recency = lookbackDaysToRecency(lookbackDays)
	}
	return request.search(ctx, query)
}

func (b *BaiduAISearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	return b.search(context.Background(), query)
}

func (b *BaiduAISearchImpl) search(ctx context.Context, query string) ([]core.SearchResult, error) {
	req := baiduAIReq{
		Messages:            []baiduAIReqMsg{{Content: query, Role: "user"}},
		Model:               b.model,
		SearchSource:        b.searchSource,
		ResourceTypeFilter:  []baiduSearchTypeFliter{{Type: "web", TopK: 5}},
		SearchRecencyFilter: b.recency,
		BlockWebsites:       b.blacklist,
		Stream:              false,
		EnableReasoning:     b.enableReasoning,
		EnableDeepSearch:    b.enableDeepSearch,
		SearchMode:          b.searchMode,
	}

	var resp baiduAIResponse
	key := b.keys.Next()
	res, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("X-Appbuilder-Authorization", fmt.Sprintf("Bearer %s", key)).
		SetBody(req).
		SetResult(&resp).
		Post("https://qianfan.baidubce.com/v2/ai_search/chat/completions")
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("%s, %w", i18n.T("provider.baidu_ai.call_failed"), err)}
	}
	if res.StatusCode() != 200 {
		return nil, &KeyError{Key: key, Err: errors.New(i18n.T("provider.baidu_ai.bad_status", "Status", res.StatusCode()))}
	}
	if len(resp.References) == 0 && len(resp.Choices) == 0 {
		return nil, errors.New(i18n.T("provider.baidu_ai.empty_content"))
	}

	// 将 AI 回答的 content 存入第一条结果的 Content 字段，便于 MergeContent 使用
	aiAnswer := ""
	if len(resp.Choices) > 0 {
		aiAnswer = resp.Choices[0].Message.Content
	}

	ret := make([]core.SearchResult, 0, len(resp.References)+1)
	// AI 回答作为第一条结果（如果有）
	if aiAnswer != "" {
		ret = append(ret, core.SearchResult{
			Title:   i18n.T("provider.baidu_ai.answer_title"),
			Url:     "",
			Content: aiAnswer,
			Engine:  b.name,
		})
	}
	for _, ref := range resp.References {
		ret = append(ret, core.SearchResult{
			Title:       ref.Title,
			Url:         ref.Url,
			Content:     ref.Content,
			PublishDate: ref.Date,
			DateSource:  core.DateSourceStructured,
			Engine:      b.name,
		})
	}
	return ret, nil
}

func (b *BaiduAISearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", errors.New(i18n.T("provider.merge.no_results"))
	}

	var buf strings.Builder
	buf.Grow(2048 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))

	// 第一条如果是 AI 回答，单独格式化
	startIdx := 0
	if len(results) > 0 && results[0].Title == i18n.T("provider.baidu_ai.answer_title") {
		buf.WriteString(i18n.T("provider.baidu_ai.answer_section"))
		buf.WriteString(results[0].Content)
		buf.WriteString(i18n.T("provider.baidu_ai.sources_section"))
		startIdx = 1
	}

	for i := startIdx; i < len(results); i++ {
		val := results[i]
		idx := i - startIdx + 1
		if core.ShowMeta {
			buf.WriteString(core.FormatMDScore(idx, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), core.FormatDateSource(val.PublishDate, val.DateSource), val.Content))
		} else {
			buf.WriteString(core.FormatMD(idx, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}
