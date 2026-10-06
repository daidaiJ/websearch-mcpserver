package provider

import (
	"websearch/pkg/search/core"
	"fmt"
	"strings"
	"websearch/pkg/client"
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
		return "", fmt.Errorf("百度智能搜索api 调用失败，%w", err)
	}
	return b.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口。
func (b *BaiduAISearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	recency := b.recency
	if lookbackDays > 0 {
		recency = lookbackDaysToRecency(lookbackDays)
	}
	saved := b.recency
	b.recency = recency
	defer func() { b.recency = saved }()
	return b.SearchRaw(query)
}

func (b *BaiduAISearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
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
		SetHeader("X-Appbuilder-Authorization", fmt.Sprintf("Bearer %s", key)).
		SetBody(req).
		SetResult(&resp).
		Post("https://qianfan.baidubce.com/v2/ai_search/chat/completions")
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("百度智能搜索api 调用失败，%w", err)}
	}
	if res.StatusCode() != 200 {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("百度智能搜索api 返回错误状态码: %d", res.StatusCode())}
	}
	if len(resp.References) == 0 && len(resp.Choices) == 0 {
		return nil, fmt.Errorf("百度智能搜索api 内容为空")
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
			Title:   "AI 回答",
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
		return "", fmt.Errorf("没有搜索结果可以合并")
	}

	var buf strings.Builder
	buf.Grow(2048 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))

	// 第一条如果是 AI 回答，单独格式化
	startIdx := 0
	if len(results) > 0 && results[0].Title == "AI 回答" {
		buf.WriteString("\n## 🤖 AI 回答\n\n")
		buf.WriteString(results[0].Content)
		buf.WriteString("\n\n---\n\n## 📚 参考来源\n\n")
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
