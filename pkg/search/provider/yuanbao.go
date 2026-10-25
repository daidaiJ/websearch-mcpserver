package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"websearch/pkg/client"
	"websearch/pkg/log"
	"websearch/pkg/search/core"
)

const (
	yuanbaoAPIEndpoint         = "https://api.wsa.cloud.tencent.com/SearchPro"
	yuanbaoDefaultMonthlyLimit = 2000
)

// YuanbaoOptions 腾讯元宝搜索（API KEY 方式）适配器选项。
// 上游每月有免费调用额度，超出后按量后付费；MonthlyLimit 在本端先行拦截，
// 达到上限后不再发起请求（由 apipool/hybrid 回退到其他来源）。
type YuanbaoOptions struct {
	NumResults     int      // 0 = 不传 Cnt（上游默认 10）；其余收敛到 10/20/30/40/50
	Mode           int      // 0 公开网页 / 1 优质权威垂直信源 / 2 混合；0 不传（兼容极速版）
	Site           string   // 指定站点或 exclude:a|b 过滤；空 = 不传
	Industry       string   // gov / news / acad / finance；空 = 不传
	Freshness      string   // 默认时效；MCP 请求级 time_range 优先
	ExcludeDomains []string // 本地黑名单过滤
	MonthlyLimit   int      // 0 = 默认 2000；负数 = 不限制（显式承担后付费风险）
	StatePath      string   // 月度计数落盘路径；空 = 仅进程内计数
}

// YuanbaoSearchImpl 实现 core.SearchInf，调用腾讯云元宝搜索 API KEY 方式。
// 只实现 130615 文档的 Bearer sk-... 接入；腾讯云 API 3.0 签名方式不在此适配器内。
type YuanbaoSearchImpl struct {
	name           string
	keys           *KeyPool
	numResults     int
	mode           int
	site           string
	industry       string
	freshness      string
	excludeDomains []string
	endpoint       string
	budget         *yuanbaoBudget
}

type yuanbaoSearchRequest struct {
	Query     string `json:"Query"`
	Mode      int    `json:"Mode,omitempty"`
	Site      string `json:"Site,omitempty"`
	Cnt       int    `json:"Cnt,omitempty"`
	Industry  string `json:"Industry,omitempty"`
	Freshness string `json:"Freshness,omitempty"`
}

type yuanbaoAPIError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type yuanbaoSearchResponse struct {
	Response struct {
		Pages     []string         `json:"Pages"`
		Query     string           `json:"Query"`
		Version   string           `json:"Version"`
		Msg       string           `json:"Msg"`
		RequestID string           `json:"RequestId"`
		Error     *yuanbaoAPIError `json:"Error"`
	} `json:"Response"`
	Error *yuanbaoAPIError `json:"Error"`
}

type yuanbaoPage struct {
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	Date           string  `json:"date"`
	Passage        string  `json:"passage"`
	Content        string  `json:"content"`
	Site           string  `json:"site"`
	Score          float64 `json:"score"`
	AuthorityLevel float64 `json:"authority_level"`
	Favicon        string  `json:"favicon"`
}

// NewYuanbaoSearch 创建元宝搜索实例；keys 必须非空。
func NewYuanbaoSearch(keys *KeyPool, opts YuanbaoOptions) *YuanbaoSearchImpl {
	limit := opts.MonthlyLimit
	switch {
	case limit == 0:
		limit = yuanbaoDefaultMonthlyLimit
	case limit < 0:
		limit = -1
	}
	return &YuanbaoSearchImpl{
		name:           "yuanbao",
		keys:           keys,
		numResults:     normalizeYuanbaoCount(opts.NumResults),
		mode:           normalizeYuanbaoMode(opts.Mode),
		site:           strings.TrimSpace(opts.Site),
		industry:       strings.ToLower(strings.TrimSpace(opts.Industry)),
		freshness:      strings.TrimSpace(opts.Freshness),
		excludeDomains: opts.ExcludeDomains,
		endpoint:       yuanbaoAPIEndpoint,
		budget:         newYuanbaoBudget(limit, opts.StatePath),
	}
}

func (y *YuanbaoSearchImpl) Name() string { return y.name }

func (y *YuanbaoSearchImpl) Search(query string) (string, error) {
	results, err := y.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return y.MergeContent(query, results)
}

func (y *YuanbaoSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	return y.search(context.Background(), query, y.freshness)
}

// SearchRawContext 实现 core.ContextSearcher：ctx 下传 HTTP 请求。
func (y *YuanbaoSearchImpl) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	return y.search(ctx, query, y.freshness)
}

// SearchRawWithTimeRange 实现 core.SearchTimeRanger。
func (y *YuanbaoSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	return y.SearchRawWithTimeRangeContext(context.Background(), query, lookbackDays)
}

// SearchRawWithTimeRangeContext 实现 core.ContextTimeRanger；请求级时间范围覆盖配置默认值。
func (y *YuanbaoSearchImpl) SearchRawWithTimeRangeContext(ctx context.Context, query string, lookbackDays int) ([]core.SearchResult, error) {
	return y.search(ctx, query, lookbackDaysToYuanbaoFreshness(lookbackDays))
}

func (y *YuanbaoSearchImpl) search(ctx context.Context, query, freshness string) ([]core.SearchResult, error) {
	if y.keys == nil {
		return nil, fmt.Errorf("yuanbao 搜索未配置 API Key")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("yuanbao 搜索 query 不能为空")
	}
	if ok, used := y.budget.Allow(time.Now()); !ok {
		return nil, fmt.Errorf("yuanbao 本月已调用 %d 次，达到本地月度上限 %d；为避免超出免费额度产生后付费，已停止调用", used, y.budget.limit)
	}

	req := yuanbaoSearchRequest{
		Query:     query,
		Mode:      y.mode,
		Site:      y.site,
		Cnt:       y.numResults,
		Industry:  y.industry,
		Freshness: freshness,
	}

	key := y.keys.Next()
	var resp yuanbaoSearchResponse
	res, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", key)).
		SetHeader("Content-Type", "application/json; charset=utf-8").
		SetBody(req).
		SetResult(&resp).
		Post(y.endpoint)
	if err != nil {
		return nil, &KeyError{Key: key, Err: fmt.Errorf("yuanbao 搜索 API 调用失败: %w", err)}
	}

	status := res.StatusCode()
	apiErr := resp.Response.Error
	if resp.Error != nil {
		apiErr = resp.Error
	}
	if apiErr != nil {
		return nil, yuanbaoAPIErr(key, status, apiErr.Code, apiErr.Message)
	}
	if status != 200 {
		return nil, yuanbaoStatusErr(key, status)
	}
	if len(resp.Response.Pages) == 0 {
		if msg := strings.TrimSpace(resp.Response.Msg); msg != "" {
			return nil, fmt.Errorf("yuanbao 搜索 API 无结果: %s", msg)
		}
		return nil, fmt.Errorf("yuanbao 搜索 API 结果为空")
	}

	results := make([]core.SearchResult, 0, len(resp.Response.Pages))
	for _, raw := range resp.Response.Pages {
		var page yuanbaoPage
		if err := json.Unmarshal([]byte(raw), &page); err != nil {
			log.WarnT("log.search.provider.yuanbao_search_api_returned_unparseable_result", "err", err)
			continue
		}
		rawURL := strings.TrimSpace(page.URL)
		if rawURL == "" || isBlockedHost(rawURL, y.excludeDomains) {
			continue
		}
		results = append(results, core.SearchResult{
			Title:       firstNonEmpty(page.Title, page.Site),
			Url:         rawURL,
			Content:     firstNonEmpty(page.Passage, page.Content),
			PublishDate: strings.TrimSpace(page.Date),
			DateSource:  core.DateSourceStructured,
			Score:       page.Score,
			Engine:      y.name,
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("yuanbao 搜索 API 结果为空")
	}
	return results, nil
}

func (y *YuanbaoSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
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

// normalizeYuanbaoCount 把配置条数收敛到上游枚举 10/20/30/40/50；0 表示不传。
func normalizeYuanbaoCount(n int) int {
	if n <= 0 {
		return 0
	}
	if n > 50 {
		return 50
	}
	return ((n + 9) / 10) * 10
}

func normalizeYuanbaoMode(mode int) int {
	if mode < 0 || mode > 2 {
		return 0
	}
	return mode
}

// lookbackDaysToYuanbaoFreshness 把 MCP 的月数（按 30 天/月换算）映射为上游
// dN/mN/yN 时效参数；0 表示不限制。
func lookbackDaysToYuanbaoFreshness(days int) string {
	if days <= 0 {
		return ""
	}
	if days <= 30 {
		if days <= 1 {
			return "d1"
		}
		return "d" + strconv.Itoa(days)
	}
	months := (days + 29) / 30
	if months <= 12 {
		return "m" + strconv.Itoa(months)
	}
	years := (days + 364) / 365
	if years > 5 {
		years = 5
	}
	return "y" + strconv.Itoa(years)
}

func yuanbaoAPIErr(key string, status int, code, message string) error {
	err := fmt.Errorf("yuanbao 搜索 API 返回错误: code=%s message=%s", code, message)
	if isYuanbaoKeyError(status, code) {
		return &KeyError{Key: key, Err: err}
	}
	return err
}

func yuanbaoStatusErr(key string, status int) error {
	err := fmt.Errorf("yuanbao 搜索 API 返回错误状态码: %d", status)
	if isYuanbaoKeyError(status, "") {
		return &KeyError{Key: key, Err: err}
	}
	return err
}

func isYuanbaoKeyError(status int, code string) bool {
	switch status {
	case 401, 403, 429:
		return true
	}
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "unauthorizedoperation", "requestlimitexceeded", "authfailure":
		return true
	}
	for _, marker := range []string{"unauthorized", "invalidapikey", "invalid_api_key", "ratelimit", "rate_limit", "throttl", "quota"} {
		if strings.Contains(code, marker) {
			return true
		}
	}
	return false
}

// ── 月度调用预算 ──

// yuanbaoBudgetState 是落盘的月度调用计数。
type yuanbaoBudgetState struct {
	Month string `json:"month"`
	Used  int    `json:"used"`
}

// yuanbaoBudget 在进程内累计本月调用次数，并按 statePath 落盘，
// 使重启后仍能守住免费额度。limit < 0 表示不限制。
type yuanbaoBudget struct {
	mu        sync.Mutex
	limit     int
	statePath string
	month     string
	used      int
	persistOK bool
}

func newYuanbaoBudget(limit int, statePath string) *yuanbaoBudget {
	b := &yuanbaoBudget{limit: limit, statePath: strings.TrimSpace(statePath), persistOK: true}
	b.load(time.Now())
	return b
}

// Allow 在发起上游请求前占用一次调用额度。返回 false 时 used 为当前已用次数。
func (b *yuanbaoBudget) Allow(now time.Time) (bool, int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	month := now.Format("2006-01")
	if b.month != month {
		b.month = month
		b.used = 0
	}
	if b.limit >= 0 && b.used >= b.limit {
		return false, b.used
	}
	b.used++
	b.persistLocked()
	return true, b.used
}

func (b *yuanbaoBudget) load(now time.Time) {
	b.month = now.Format("2006-01")
	if b.statePath == "" {
		return
	}
	data, err := os.ReadFile(b.statePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.WarnT("log.search.provider.yuanbao_failed_to_read_the_monthly", "err", err)
		}
		return
	}
	var state yuanbaoBudgetState
	if err := json.Unmarshal(data, &state); err != nil {
		log.WarnT("log.search.provider.yuanbao_monthly_counter_file_unparseable_starting", "err", err)
		return
	}
	if state.Month == b.month && state.Used > 0 {
		b.used = state.Used
	}
}

func (b *yuanbaoBudget) persistLocked() {
	if b.statePath == "" || !b.persistOK {
		return
	}
	data, err := json.Marshal(yuanbaoBudgetState{Month: b.month, Used: b.used})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.statePath), 0o755); err != nil {
		b.persistOK = false
		log.WarnT("log.search.provider.yuanbao_failed_to_create_the_monthly", "err", err)
		return
	}
	if err := os.WriteFile(b.statePath, data, 0o600); err != nil {
		b.persistOK = false
		log.WarnT("log.search.provider.yuanbao_failed_to_write_the_monthly", "err", err)
	}
}
