package provider

import (
	"websearch/pkg/search/core"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"websearch/pkg/client"
)

const (
	doubaoGlobalSearchAPIEndpoint = "https://open.feedcoopapi.com/search_api/global_search"
	doubaoCustomSearchAPIEndpoint = "https://open.feedcoopapi.com/search_api/web_search"
	doubaoTrafficTag              = "websearch-mcpserver"
	doubaoVersionGlobal           = "global"
	doubaoVersionCustom           = "custom"
)

// DoubaoOptions 豆包联网搜索适配器选项。
// Global 与 Custom 共用同一搜索 API Key（不是 Ark 大模型 Key）。
type DoubaoOptions struct {
	NumResults          int
	ExcludeDomains      []string
	Version             string // global（默认）/ custom
	TimeRange           string // Custom 默认时间范围；请求级 core.SearchTimeRanger 优先
	AuthLevel           int
	QueryRewrite        bool
	NeedContent         bool
	MaxSnippetLength    int
	MaxImageCountPerDoc int
	ICPHostOnly         bool
}

// DoubaoSearchImpl 实现 core.SearchInf，调用火山引擎豆包联网搜索 Global 或 Custom。
// 一个实例只打一个上游；两版并发交给 hybrid.go。
type DoubaoSearchImpl struct {
	name                string
	keys                *KeyPool
	numResults          int
	excludeDomains      []string
	version             string
	timeRange           string
	authLevel           int
	queryRewrite        bool
	needContent         bool
	maxSnippetLength    int
	maxImageCountPerDoc int
	icpHostOnly         bool
	globalEndpoint      string
	customEndpoint      string
}

type doubaoCustomSearchRequest struct {
	Query        string                    `json:"Query"`
	SearchType   string                    `json:"SearchType"`
	Count        int                       `json:"Count"`
	Filter       *doubaoCustomSearchFilter `json:"Filter,omitempty"`
	TimeRange    string                    `json:"TimeRange,omitempty"`
	QueryControl *doubaoQueryControl       `json:"QueryControl,omitempty"`
}

type doubaoCustomSearchFilter struct {
	AuthInfoLevel int  `json:"AuthInfoLevel,omitempty"`
	NeedContent   bool `json:"NeedContent"`
	NeedUrl       bool `json:"NeedUrl"`
}

type doubaoQueryControl struct {
	QueryRewrite bool `json:"QueryRewrite"`
}

type doubaoGlobalSearchRequest struct {
	Query               string              `json:"Query"`
	SearchType          string              `json:"SearchType"`
	DocCount            int                 `json:"DocCount,omitempty"`
	MaxSnippetLength    int                 `json:"MaxSnippetLength,omitempty"`
	MaxImageCountPerDoc int                 `json:"MaxImageCountPerDoc"`
	Filter              *doubaoGlobalFilter `json:"Filter,omitempty"`
}

type doubaoGlobalFilter struct {
	ICPHostOnly bool `json:"IcpHostOnly,omitempty"`
}

type doubaoAPIError struct {
	CodeN   int    `json:"CodeN"`
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type doubaoCustomSearchResponse struct {
	ResponseMetadata struct {
		Error *doubaoAPIError `json:"Error"`
	} `json:"ResponseMetadata"`
	Error  *doubaoAPIError `json:"Error"`
	Result struct {
		WebResults []doubaoCustomSearchResult `json:"WebResults"`
	} `json:"Result"`
	WebResults []doubaoCustomSearchResult `json:"WebResults"`
}

type doubaoCustomSearchResult struct {
	ID          string  `json:"Id"`
	Title       string  `json:"Title"`
	Snippet     string  `json:"Snippet"`
	URL         string  `json:"Url"`
	DisplayURL  string  `json:"DisplayUrl"`
	Summary     string  `json:"Summary"`
	Content     string  `json:"Content"`
	PublishTime string  `json:"PublishTime"`
	PublishDate string  `json:"PublishDate"`
	Time        string  `json:"Time"`
	RankScore   float64 `json:"RankScore"`
	Score       float64 `json:"Score"`
}

type doubaoGlobalSearchResponse struct {
	ResponseMetadata struct {
		Error *doubaoAPIError `json:"Error"`
	} `json:"ResponseMetadata"`
	Result *struct {
		Documents []doubaoGlobalDocument `json:"Documents"`
		ErrorCode int                    `json:"ErrorCode"`
		ErrorMsg  string                 `json:"ErrorMsg"`
	} `json:"Result"`
}

type doubaoGlobalDocument struct {
	URL          string                `json:"Url"`
	Title        string                `json:"Title"`
	Snippet      []doubaoGlobalSnippet `json:"Snippet"`
	DocumentInfo doubaoGlobalDocInfo   `json:"DocumentInfo"`
}

type doubaoGlobalSnippet struct {
	Type  string                 `json:"Type"`
	Text  string                 `json:"Text"`
	Image doubaoGlobalSnippetImg `json:"Image"`
}

type doubaoGlobalSnippetImg struct {
	Alt string `json:"Alt"`
}

type doubaoGlobalDocInfo struct {
	PublishTime string `json:"PublishTime"`
}

// NewDoubaoSearch 创建豆包联网搜索实例。version 只接受 global / custom，其余回落 global。
func NewDoubaoSearch(keys *KeyPool, opts DoubaoOptions) *DoubaoSearchImpl {
	if opts.NumResults <= 0 {
		opts.NumResults = 10
	}
	if opts.NumResults > 50 {
		opts.NumResults = 50
	}
	if opts.AuthLevel < 0 || opts.AuthLevel > 1 {
		opts.AuthLevel = 0
	}
	return &DoubaoSearchImpl{
		name:                "doubao",
		keys:                keys,
		numResults:          opts.NumResults,
		excludeDomains:      opts.ExcludeDomains,
		version:             normalizeDoubaoVersion(opts.Version),
		timeRange:           strings.TrimSpace(opts.TimeRange),
		authLevel:           opts.AuthLevel,
		queryRewrite:        opts.QueryRewrite,
		needContent:         opts.NeedContent,
		maxSnippetLength:    opts.MaxSnippetLength,
		maxImageCountPerDoc: opts.MaxImageCountPerDoc,
		icpHostOnly:         opts.ICPHostOnly,
		globalEndpoint:      doubaoGlobalSearchAPIEndpoint,
		customEndpoint:      doubaoCustomSearchAPIEndpoint,
	}
}

func (d *DoubaoSearchImpl) Name() string { return d.name }

func (d *DoubaoSearchImpl) Search(query string) (string, error) {
	results, err := d.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return d.MergeContent(query, results)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger。Custom 把天数映射为官方枚举；Global 无时间过滤接口，忽略。
func (d *DoubaoSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	if d.version != doubaoVersionCustom {
		return d.SearchRaw(query)
	}
	saved := d.timeRange
	if mapped := lookbackDaysToDoubaoRange(lookbackDays); mapped != "" {
		d.timeRange = mapped
	}
	defer func() { d.timeRange = saved }()
	return d.SearchRaw(query)
}

func (d *DoubaoSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	if d.keys == nil {
		return nil, fmt.Errorf("doubao 搜索未配置 API Key")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("doubao 搜索 query 不能为空")
	}
	if utf8.RuneCountInString(query) > 100 {
		return nil, fmt.Errorf("doubao 搜索 query 超过 100 个字符")
	}
	if d.version == doubaoVersionCustom {
		return d.searchCustom(query)
	}
	return d.searchGlobal(query)
}

func (d *DoubaoSearchImpl) searchGlobal(query string) ([]core.SearchResult, error) {
	count := d.numResults
	if count > 20 {
		count = 20
	}
	maxSnippetLength := d.maxSnippetLength
	if maxSnippetLength <= 0 {
		maxSnippetLength = 500
	}
	if maxSnippetLength > 3000 {
		maxSnippetLength = 3000
	}
	maxImages := d.maxImageCountPerDoc
	if maxImages < 0 {
		maxImages = 0
	}
	if maxImages > 10 {
		maxImages = 10
	}

	req := doubaoGlobalSearchRequest{
		Query:               query,
		SearchType:          "web",
		DocCount:            count,
		MaxSnippetLength:    maxSnippetLength,
		MaxImageCountPerDoc: maxImages,
	}
	if d.icpHostOnly {
		req.Filter = &doubaoGlobalFilter{ICPHostOnly: true}
	}

	key := d.keys.Next()
	var resp doubaoGlobalSearchResponse
	res, err := client.DefaultClient.R().
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", key)).
		SetHeader("Content-Type", "application/json").
		SetHeader("X-Traffic-Tag", doubaoTrafficTag).
		SetBody(req).
		SetResult(&resp).
		Post(d.globalEndpoint)
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao 搜索 API 调用失败: %w", err)}
	}

	status := res.StatusCode()
	if apiErr := resp.ResponseMetadata.Error; apiErr != nil {
		return nil, doubaoAPIErr(key, status, apiErr.Code, apiErr.Message)
	}
	if status != 200 {
		return nil, doubaoStatusErr(key, status)
	}
	if resp.Result == nil {
		return nil, fmt.Errorf("doubao 搜索 API 返回空结果体")
	}
	if resp.Result.ErrorCode != 0 {
		return nil, doubaoAPIErr(key, status, strconv.Itoa(resp.Result.ErrorCode), resp.Result.ErrorMsg)
	}
	if len(resp.Result.Documents) == 0 {
		return nil, fmt.Errorf("doubao 搜索 API 结果为空")
	}

	results := make([]core.SearchResult, 0, len(resp.Result.Documents))
	for _, doc := range resp.Result.Documents {
		rawURL := strings.TrimSpace(doc.URL)
		if isBlockedHost(rawURL, d.excludeDomains) {
			continue
		}
		results = append(results, core.SearchResult{
			Title:       strings.TrimSpace(doc.Title),
			Url:         rawURL,
			Content:     doubaoGlobalContent(doc),
			PublishDate: strings.TrimSpace(doc.DocumentInfo.PublishTime),
			DateSource:  core.DateSourceStructured,
			Engine:      d.name,
		})
	}
	return results, nil
}

func (d *DoubaoSearchImpl) searchCustom(query string) ([]core.SearchResult, error) {
	req := doubaoCustomSearchRequest{
		Query:      query,
		SearchType: "web",
		Count:      d.numResults,
		Filter: &doubaoCustomSearchFilter{
			AuthInfoLevel: d.authLevel,
			NeedContent:   d.needContent,
			NeedUrl:       true,
		},
		TimeRange: d.timeRange,
	}
	if d.queryRewrite {
		req.QueryControl = &doubaoQueryControl{QueryRewrite: true}
	}

	key := d.keys.Next()
	var resp doubaoCustomSearchResponse
	res, err := client.DefaultClient.R().
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", key)).
		SetHeader("Content-Type", "application/json").
		SetHeader("X-Traffic-Tag", doubaoTrafficTag).
		SetBody(req).
		SetResult(&resp).
		Post(d.customEndpoint)
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao 搜索 API 调用失败: %w", err)}
	}

	status := res.StatusCode()
	apiErr := resp.Error
	if resp.ResponseMetadata.Error != nil {
		apiErr = resp.ResponseMetadata.Error
	}
	if apiErr != nil {
		return nil, doubaoAPIErr(key, status, apiErr.Code, apiErr.Message)
	}
	if status != 200 {
		return nil, doubaoStatusErr(key, status)
	}

	rawResults := resp.Result.WebResults
	if len(rawResults) == 0 {
		rawResults = resp.WebResults
	}
	if len(rawResults) == 0 {
		return nil, fmt.Errorf("doubao 搜索 API 结果为空")
	}

	results := make([]core.SearchResult, 0, len(rawResults))
	for _, item := range rawResults {
		rawURL := strings.TrimSpace(item.URL)
		if rawURL == "" {
			rawURL = strings.TrimSpace(item.DisplayURL)
		}
		if isBlockedHost(rawURL, d.excludeDomains) {
			continue
		}
		score := item.RankScore
		if score == 0 {
			score = item.Score
		}
		results = append(results, core.SearchResult{
			Title:       strings.TrimSpace(item.Title),
			Url:         rawURL,
			Content:     firstNonEmpty(item.Content, item.Summary, item.Snippet),
			PublishDate: firstNonEmpty(item.PublishTime, item.PublishDate, item.Time),
			DateSource:  core.DateSourceStructured,
			Score:       score,
			Engine:      d.name,
		})
	}
	return results, nil
}

func (d *DoubaoSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if core.ShowMeta {
			buf.WriteString(core.FormatMDScore(i+1, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), core.FormatDateSource(val.PublishDate, val.DateSource), val.Content))
		} else {
			buf.WriteString(core.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}

func normalizeDoubaoVersion(version string) string {
	if strings.ToLower(strings.TrimSpace(version)) == doubaoVersionCustom {
		return doubaoVersionCustom
	}
	return doubaoVersionGlobal
}

func lookbackDaysToDoubaoRange(days int) string {
	switch {
	case days <= 0:
		return ""
	case days <= 1:
		return "OneDay"
	case days <= 7:
		return "OneWeek"
	case days <= 30:
		return "OneMonth"
	default:
		return "OneYear"
	}
}

func doubaoGlobalContent(doc doubaoGlobalDocument) string {
	var parts []string
	for _, snippet := range doc.Snippet {
		if snippet.Type == "text" {
			if text := strings.TrimSpace(snippet.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		for _, snippet := range doc.Snippet {
			if alt := strings.TrimSpace(snippet.Image.Alt); alt != "" {
				parts = append(parts, alt)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func doubaoAPIErr(key string, status int, code, message string) error {
	err := fmt.Errorf("doubao 搜索 API 返回错误: code=%s message=%s", code, message)
	if isDoubaoKeyError(status, code) {
		return &KeyError{Key: key, Err: err}
	}
	return err
}

func doubaoStatusErr(key string, status int) error {
	err := fmt.Errorf("doubao 搜索 API 返回错误状态码: %d", status)
	if isDoubaoKeyError(status, "") {
		return &KeyError{Key: key, Err: err}
	}
	return err
}

func isDoubaoKeyError(status int, code string) bool {
	switch status {
	case 401, 403, 429:
		return true
	}
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "10403", "10412", "700429", "700901":
		return true
	}
	for _, marker := range []string{"auth", "accessdenied", "signature", "quota", "throttl", "ratelimit", "rate_limit"} {
		if strings.Contains(code, marker) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
