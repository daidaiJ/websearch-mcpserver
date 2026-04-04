package search

import (
	"fmt"
	"websearch/pkg/client"
	md "websearch/pkg/xml"
)

type BaiduSearchImpl struct {
	hostUlr    string
	authHeader string
	sk         string
	blacklist  []string
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
	Message    []baiduSearchMsg        `json:"messages"`
	TypeFliter []baiduSearchTypeFliter `json:"resource_type_filter"`
	BlackSites []string                `json:"block_websites"`
	Recency    string                  `json:"search_recency_filter"`
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

func NewBaiduSeach(sk string, blacklist []string) *BaiduSearchImpl {
	return &BaiduSearchImpl{
		hostUlr:    "https://qianfan.baidubce.com/v2/ai_search/web_search",
		authHeader: "X-Appbuilder-Authorization",
		sk:         sk,
		blacklist:  blacklist,
	}
}

func (b *BaiduSearchImpl) Search(query string) (string, error) {
	results, err := b.SearchRaw(query)
	if err != nil {
		return "", fmt.Errorf("百度搜索api 调用失败，%w", err)
	}
	return b.MergeContent(query, results)

}

//	curl --location 'https://qianfan.baidubce.com/v2/ai_search/web_search' \
//
// --header 'X-Appbuilder-Authorization: Bearer <AppBuilder API Key>' \
// --header 'Content-Type: application/json' \
//
//	--data '{
//	  "messages": [
//	    {
//	      "content": "百度千帆平台",
//	      "role": "user"
//	    }
//	  ],
//	  "search_source": "baidu_search_v2",
//	  "resource_type_filter": [{"type": "web","top_k": 10}]
//	}'

func (b *BaiduSearchImpl) SearchRaw(query string) ([]SearchResult, error) {
	req := baiduSearchReq{Message: []baiduSearchMsg{{Content: query, Role: "user"}}, TypeFliter: []baiduSearchTypeFliter{{Type: "web", TopK: 5}}, BlackSites: b.blacklist,
		Recency: "semiyear"}
	rep := baidSearchReponse{}
	res, err := client.DefaultClient.R().SetHeader(b.authHeader, fmt.Sprintf("Bearer %s", b.sk)).SetBody(req).SetResult(&rep).Post(b.hostUlr)
	if err != nil {
		if rep.Message != "" {
			return nil, fmt.Errorf("百度搜索api 调用失败，%s", rep.Message)
		}
		return nil, fmt.Errorf("百度搜索api 调用失败，%w", err)
	}
	if len(rep.References) == 0 {
		return nil, fmt.Errorf("百度搜索api 内容为空 %+v", res)
	}
	ret := make([]SearchResult, 0, len(rep.References))
	for _, val := range rep.References {
		ret = append(ret, SearchResult{Title: val.Title, Url: val.Url, Content: val.Content, PublishDate: val.Date})
	}
	return ret, nil
}
func (b *BaiduSearchImpl) MergeContent(query string, results []SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}

	ret := md.MDSearchHeader(query, len(results))
	for i, val := range results {
		ret = fmt.Sprintf("%s%s", ret, md.FormatMD(i+1, val.Title, val.Url, val.Content))
	}
	return ret, nil
}
