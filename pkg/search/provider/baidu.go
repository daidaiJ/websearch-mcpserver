package provider

import (
	"websearch/pkg/search/core"
	"fmt"
	"websearch/pkg/client"
)

type BaiduSearchImpl struct {
	name       string
	hostUlr    string
	authHeader string
	keys       *KeyPool
	blacklist  []string
	recency    string // 搜索时间范围: "day", "week", "month", "semiyear", "year"
}
type baiduSearchMsg struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type baiduSearchTypeFliter struct {
	Type string `json:"type"`
	TopK int    `json:"top_k"`
}

type baiduSearchReq struct {
	Message      []baiduSearchMsg        `json:"messages"`
	SearchSource string                  `json:"search_source"` // 必填，固定值 baidu_search_v2
	TypeFliter   []baiduSearchTypeFliter `json:"resource_type_filter"`
	BlackSites   []string                `json:"block_websites"`
	Recency      string                  `json:"search_recency_filter"`
}

type referenceCtx struct {
	Content string `json:"content"`
	Title   string `json:"title"`
	Url     string `json:"url"`
	Date    string `json:"date"`
}

type baidSearchReponse struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	References []referenceCtx `json:"references"`
}

// NewBaiduSeach 创建百度搜索实例，支持 KeyPool 轮询。
func NewBaiduSeach(keys *KeyPool, blacklist []string) *BaiduSearchImpl {
	return &BaiduSearchImpl{
		name:       "baidu_api",
		hostUlr:    "https://qianfan.baidubce.com/v2/ai_search/web_search",
		authHeader: "X-Appbuilder-Authorization",
		keys:       keys,
		blacklist:  blacklist,
		recency:    "semiyear",
	}
}

func (b *BaiduSearchImpl) Name() string { return b.name }

func (b *BaiduSearchImpl) Search(query string) (string, error) {
	results, err := b.SearchRaw(query)
	if err != nil {
		return "", fmt.Errorf("百度搜索api 调用失败，%w", err)
	}
	return b.MergeContent(query, results)

}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger 接口，支持动态时间范围。
func (b *BaiduSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	recency := b.recency
	if lookbackDays > 0 {
		recency = lookbackDaysToRecency(lookbackDays)
	}
	saved := b.recency
	b.recency = recency
	defer func() { b.recency = saved }()
	return b.SearchRaw(query)
}

// lookbackDaysToRecency 将天数转换为百度 API 的 recency 值。
func lookbackDaysToRecency(days int) string {
	switch {
	case days <= 0:
		return "semiyear" // 默认
	case days <= 1:
		return "day"
	case days <= 7:
		return "week"
	case days <= 30:
		return "month"
	case days <= 180:
		return "semiyear"
	default:
		return "year"
	}
}

func (b *BaiduSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	req := baiduSearchReq{
		Message:      []baiduSearchMsg{{Content: query, Role: "user"}},
		SearchSource: "baidu_search_v2",
		TypeFliter:   []baiduSearchTypeFliter{{Type: "web", TopK: 5}},
		BlackSites:   b.blacklist,
		Recency:      b.recency,
	}
	rep := baidSearchReponse{}
	key := b.keys.Next()
	res, err := client.DefaultClient.R().SetHeader(b.authHeader, fmt.Sprintf("Bearer %s", key)).SetBody(req).SetResult(&rep).Post(b.hostUlr)
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("百度搜索api 调用失败，%w", err)}
	}
	// 非 200 通常意味着鉴权/配额/参数错误，显式暴露状态码与服务端消息，避免误报为“内容为空”
	if res.StatusCode() != 200 {
		if rep.Message != "" {
			return nil, &KeyError{Key: key, Err: fmt.Errorf("百度搜索api 返回错误状态码 %d: %s (code=%s)", res.StatusCode(), rep.Message, rep.Code)}
		}
		return nil, &KeyError{Key: key, Err: fmt.Errorf("百度搜索api 返回错误状态码 %d: %s", res.StatusCode(), res.String())}
	}
	if len(rep.References) == 0 {
		return nil, fmt.Errorf("百度搜索api 内容为空")
	}
	ret := make([]core.SearchResult, 0, len(rep.References))
	for _, val := range rep.References {
		ret = append(ret, core.SearchResult{Title: val.Title, Url: val.Url, Content: val.Content, PublishDate: val.Date, DateSource: core.DateSourceStructured, Engine: b.name})
	}
	return ret, nil
}
func (b *BaiduSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}

	buf := core.MDSearchHeader(query, len(results))
	for i, val := range results {
		if core.ShowMeta {
			buf += core.FormatMDScore(i+1, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), core.FormatDateSource(val.PublishDate, val.DateSource), val.Content)
		} else {
			buf += core.FormatMD(i+1, val.Title, val.Url, val.Content)
		}
	}
	return buf, nil
}
