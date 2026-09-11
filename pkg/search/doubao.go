package search

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"websearch/pkg/client"
	"websearch/pkg/log"
	md "websearch/pkg/xml"
)

const (
	doubaoGlobalSearchAPIEndpoint = "https://open.feedcoopapi.com/search_api/global_search"
	doubaoCustomSearchAPIEndpoint = "https://open.feedcoopapi.com/search_api/web_search"
	doubaoTrafficTag              = "websearch-mcpserver"
)

const (
	doubaoVersionGlobal = "global"
	doubaoVersionCustom = "custom"
	doubaoVersionBoth   = "both"
)

// DoubaoOptions configures the Volcengine Doubao Search adapter.
// Global and Custom share one API key and the same monthly free quota.
type DoubaoOptions struct {
	NumResults          int
	ExcludeDomains      []string
	Version             string
	TimeRange           string
	AuthLevel           int
	QueryRewrite        bool
	NeedContent         bool
	MaxSnippetLength    int
	MaxImageCountPerDoc int
	ICPHostOnly         bool
}

// DoubaoSearchImpl implements SearchInf using the Volcengine Doubao Search
// Global and/or Custom API. This is the dedicated search API, not the Ark chat
// completion endpoint.
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
	SortID      int     `json:"SortId"`
	Title       string  `json:"Title"`
	Snippet     string  `json:"Snippet"`
	SiteName    string  `json:"SiteName"`
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
		TotalDocCount int                    `json:"TotalDocCount"`
		Documents     []doubaoGlobalDocument `json:"Documents"`
		ErrorCode     int                    `json:"ErrorCode"`
		ErrorMsg      string                 `json:"ErrorMsg"`
	} `json:"Result"`
}

type doubaoGlobalDocument struct {
	Rank         int                   `json:"Rank"`
	URL          string                `json:"Url"`
	Title        string                `json:"Title"`
	Snippet      []doubaoGlobalSnippet `json:"Snippet"`
	DocumentInfo doubaoGlobalDocInfo   `json:"DocumentInfo"`
	HostInfo     doubaoGlobalHostInfo  `json:"HostInfo"`
}

type doubaoGlobalSnippet struct {
	Type  string                 `json:"Type"`
	Text  string                 `json:"Text"`
	Image doubaoGlobalSnippetImg `json:"Image"`
}

type doubaoGlobalSnippetImg struct {
	Width    int    `json:"Width"`
	Height   int    `json:"Height"`
	ImageURL string `json:"ImageUrl"`
	Alt      string `json:"Alt"`
}

type doubaoGlobalDocInfo struct {
	ContentCharCount  int    `json:"ContentCharCount"`
	ContentTokenCount int    `json:"ContentTokenCount"`
	Filetype          string `json:"Filetype"`
	PublishTime       string `json:"PublishTime"`
}

type doubaoGlobalHostInfo struct {
	Hostname       string `json:"Hostname"`
	IconURL        string `json:"IconUrl"`
	AuthorityLevel string `json:"AuthorityLevel"`
}

type doubaoSearchOutcome struct {
	version string
	results []SearchResult
	err     error
}

// NewDoubaoSearch creates a Doubao Search engine. Version accepts global,
// custom, or both; an empty/unknown value defaults to global.
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

func (d *DoubaoSearchImpl) SearchRaw(query string) ([]SearchResult, error) {
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

	switch d.version {
	case doubaoVersionCustom:
		return d.searchCustom(query)
	case doubaoVersionBoth:
		return d.searchBoth(query)
	default:
		return d.searchGlobal(query)
	}
}

func (d *DoubaoSearchImpl) searchGlobal(query string) ([]SearchResult, error) {
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
		return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao global 搜索 API 调用失败: %w", err)}
	}

	status := res.StatusCode()
	if apiErr := resp.ResponseMetadata.Error; apiErr != nil {
		if isDoubaoKeyError(status, apiErr.Code) {
			return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao global 搜索 API 鉴权/额度错误: code=%s message=%s", apiErr.Code, apiErr.Message)}
		}
		return nil, fmt.Errorf("doubao global 搜索 API 返回错误: code=%s message=%s", apiErr.Code, apiErr.Message)
	}
	if status != 200 {
		if isDoubaoKeyError(status, "") {
			return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao global 搜索 API 返回错误状态码: %d", status)}
		}
		return nil, fmt.Errorf("doubao global 搜索 API 返回错误状态码: %d", status)
	}
	if resp.Result == nil {
		return nil, fmt.Errorf("doubao global 搜索 API 返回空结果体")
	}
	if resp.Result.ErrorCode != 0 {
		code := strconv.Itoa(resp.Result.ErrorCode)
		if isDoubaoKeyError(status, code) {
			return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao global 搜索 API 鉴权/额度错误: code=%d message=%s", resp.Result.ErrorCode, resp.Result.ErrorMsg)}
		}
		return nil, fmt.Errorf("doubao global 搜索 API 返回错误: code=%d message=%s", resp.Result.ErrorCode, resp.Result.ErrorMsg)
	}
	if len(resp.Result.Documents) == 0 {
		return nil, fmt.Errorf("doubao global 搜索 API 结果为空")
	}

	results := make([]SearchResult, 0, len(resp.Result.Documents))
	for _, doc := range resp.Result.Documents {
		rawURL := strings.TrimSpace(doc.URL)
		if isBlockedHost(rawURL, d.excludeDomains) {
			continue
		}
		results = append(results, SearchResult{
			Title:       strings.TrimSpace(doc.Title),
			Url:         rawURL,
			Content:     doubaoGlobalContent(doc),
			PublishDate: strings.TrimSpace(doc.DocumentInfo.PublishTime),
			Engine:      "doubao_global",
		})
	}
	return results, nil
}

func (d *DoubaoSearchImpl) searchCustom(query string) ([]SearchResult, error) {
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
		return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao custom 搜索 API 调用失败: %w", err)}
	}

	status := res.StatusCode()
	apiErr := resp.Error
	if resp.ResponseMetadata.Error != nil {
		apiErr = resp.ResponseMetadata.Error
	}
	if apiErr != nil {
		if isDoubaoKeyError(status, apiErr.Code) {
			return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao custom 搜索 API 鉴权/额度错误: code=%s message=%s", apiErr.Code, apiErr.Message)}
		}
		return nil, fmt.Errorf("doubao custom 搜索 API 返回错误: code=%s message=%s", apiErr.Code, apiErr.Message)
	}
	if status != 200 {
		if isDoubaoKeyError(status, "") {
			return nil, &KeyError{Key: key, Err: fmt.Errorf("doubao custom 搜索 API 返回错误状态码: %d", status)}
		}
		return nil, fmt.Errorf("doubao custom 搜索 API 返回错误状态码: %d", status)
	}

	rawResults := resp.Result.WebResults
	if len(rawResults) == 0 {
		rawResults = resp.WebResults
	}
	if len(rawResults) == 0 {
		return nil, fmt.Errorf("doubao custom 搜索 API 结果为空")
	}

	results := make([]SearchResult, 0, len(rawResults))
	for _, item := range rawResults {
		rawURL := strings.TrimSpace(item.URL)
		if rawURL == "" {
			rawURL = strings.TrimSpace(item.DisplayURL)
		}
		if isBlockedHost(rawURL, d.excludeDomains) {
			continue
		}
		content := firstNonEmpty(item.Content, item.Summary, item.Snippet)
		score := item.RankScore
		if score == 0 {
			score = item.Score
		}
		results = append(results, SearchResult{
			Title:       strings.TrimSpace(item.Title),
			Url:         rawURL,
			Content:     content,
			PublishDate: firstNonEmpty(item.PublishTime, item.PublishDate, item.Time),
			Score:       score,
			Engine:      "doubao_custom",
		})
	}
	return results, nil
}

func (d *DoubaoSearchImpl) searchBoth(query string) ([]SearchResult, error) {
	outcomes := make(chan doubaoSearchOutcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		results, err := d.searchGlobal(query)
		outcomes <- doubaoSearchOutcome{version: doubaoVersionGlobal, results: results, err: err}
	}()
	go func() {
		defer wg.Done()
		results, err := d.searchCustom(query)
		outcomes <- doubaoSearchOutcome{version: doubaoVersionCustom, results: results, err: err}
	}()
	wg.Wait()
	close(outcomes)

	var globalResults, customResults []SearchResult
	var globalErr, customErr error
	for outcome := range outcomes {
		switch outcome.version {
		case doubaoVersionGlobal:
			globalResults, globalErr = outcome.results, outcome.err
		case doubaoVersionCustom:
			customResults, customErr = outcome.results, outcome.err
		}
	}

	if globalErr != nil && customErr != nil {
		return nil, fmt.Errorf("doubao 两版均失败: global: %v; custom: %s", globalErr, customErr)
	}
	if globalErr != nil {
		log.Warnf("doubao global 失败，保留 custom 结果: %v", globalErr)
	}
	if customErr != nil {
		log.Warnf("doubao custom 失败，保留 global 结果: %v", customErr)
	}
	results := mergeDoubaoResults(globalResults, customResults)
	if len(results) == 0 {
		return nil, fmt.Errorf("doubao 两版均无有效结果")
	}
	return results, nil
}

func (d *DoubaoSearchImpl) MergeContent(query string, results []SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(md.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if ShowMeta {
			buf.WriteString(md.FormatMDScore(i+1, val.Title, val.Url, val.Engine, formatScore(val.Score), val.Content))
		} else {
			buf.WriteString(md.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}

func normalizeDoubaoVersion(version string) string {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case doubaoVersionCustom:
		return doubaoVersionCustom
	case doubaoVersionBoth:
		return doubaoVersionBoth
	default:
		return doubaoVersionGlobal
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

func mergeDoubaoResults(groups ...[]SearchResult) []SearchResult {
	var merged []SearchResult
	seen := make(map[string]int)
	for _, group := range groups {
		for _, result := range group {
			key := strings.ToLower(strings.TrimSpace(result.Url))
			if key != "" {
				if idx, ok := seen[key]; ok {
					if len(result.Content) > len(merged[idx].Content) {
						merged[idx] = result
					}
					continue
				}
				seen[key] = len(merged)
			}
			merged = append(merged, result)
		}
	}
	return merged
}

func isDoubaoKeyError(status int, code string) bool {
	switch status {
	case 401, 403, 429:
		return true
	}
	code = strings.TrimSpace(code)
	switch code {
	case "10403", "10412", "700429", "700901":
		return true
	}
	code = strings.ToLower(code)
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
