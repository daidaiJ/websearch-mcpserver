// Package quota reads official usage and accounts for locally admitted keyed searches.
//
// Tavily 与 TinyFish 是能用搜索 Key 查询官方用量的供应商，本包只查询并只返回
// 这两家；其他供应商既不探测也不出现在结果里。TinyFish 的官方 /usage 端点返回
// 分页调用记录（total 随过滤条件返回），本包按「当日（00:00 UTC 起）记录数」
// 折算已用量。额度读取是只读操作：不写遥测、不影响健康判定，也不产生计费调用。
package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/proxy"
	"websearch/pkg/telemetry"
)

const (
	// tavilyUsageEndpoint 是 Tavily 官方文档公开的用量查询端点。
	tavilyUsageEndpoint = "https://api.tavily.com/usage"
	// TinyFish 官方文档公开的用量查询端点（X-API-Key 鉴权）。
	tinyfishSearchUsageEndpoint = "https://api.search.tinyfish.ai/usage"
	tinyfishFetchUsageEndpoint  = "https://api.fetch.tinyfish.ai/usage"
	// TinyFish 官方免费档额度（00:00 UTC 重置）。官方 /usage 端点只给调用计数
	// 不给上限，搜索展示参考按公开速率 500 次/小时 × 24 小时估算。
	tinyfishSearchDailyLimit = 12000
	tinyfishFetchDailyLimit  = 1000
	// cacheTTL 保证控制中心不会高频访问官方端点。
	cacheTTL        = 5 * time.Minute
	refreshInterval = 10 * time.Second
	// requestTimeout 是单次额度查询的整体超时。
	requestTimeout = 12 * time.Second
	// providerTavily / providerTinyfish 是被查询的供应商名。
	providerTavily   = "tavily"
	providerTinyfish = "tinyfish"
)

type Item struct {
	Provider   string         `json:"provider"`
	Configured bool           `json:"configured"`
	Status     string         `json:"status"`
	Unit       string         `json:"unit,omitempty"`
	Used       *float64       `json:"used,omitempty"`
	Limit      *float64       `json:"limit,omitempty"`
	Remaining  *float64       `json:"remaining,omitempty"`
	Plan       string         `json:"plan,omitempty"`
	UpdatedAt  string         `json:"updated_at,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	Error      string         `json:"error,omitempty"`
	Source     string         `json:"source,omitempty"`
	// OfficialQueryFailed distinguishes observed fallback counts from ordinary local quota.
	OfficialQueryFailed bool `json:"official_query_failed,omitempty"`
	// ResetAt 是下一次自动重置时间（RFC3339）；策略为 none 时不返回。
	ResetAt string `json:"reset_at,omitempty"`
	// AutoReset 是生效的重置策略：monthly / weekly / daily / none / official。
	// official 表示该条目由供应商官方端点给出，本地周期不参与计算。
	AutoReset string `json:"auto_reset,omitempty"`
}

// tinyfishUsageResponse 只取官方 /usage 分页列表的 total 字段（配合
// limit=1 + start_after 过滤即可拿到当日调用数，无需翻页聚合）。
type tinyfishUsageResponse struct {
	Total *int `json:"total"`
}

type tavilyUsageResponse struct {
	Key struct {
		Usage        float64 `json:"usage"`
		Limit        float64 `json:"limit"`
		SearchUsage  float64 `json:"search_usage"`
		ExtractUsage float64 `json:"extract_usage"`
	} `json:"key"`
	Account struct {
		CurrentPlan string  `json:"current_plan"`
		PlanUsage   float64 `json:"plan_usage"`
		PlanLimit   float64 `json:"plan_limit"`
		PaygoUsage  float64 `json:"paygo_usage"`
		PaygoLimit  float64 `json:"paygo_limit"`
	} `json:"account"`
}

// Service 读取官方额度端点并缓存结果，同时把遥测中的真实调用派生为本地
// 用量条目（上限来自配置默认值，支持自动周期重置与管理员手动重置/修正）。
type Service struct {
	conf     config.Config
	store    *telemetry.Store
	client   *http.Client
	endpoint string
	now      func() time.Time
	// TinyFish 官方端点可注入（单测覆写）；newService 落默认常量。
	tinyfishSearchURL string
	tinyfishFetchURL  string

	mu          sync.Mutex
	cached      []Item
	cachedAt    time.Time
	routes      map[string]*searchGuard
	lastRefresh time.Time
}

// New 使用 Tavily 官方端点与默认超时构造 Service；store 为 nil 时只返回
// 官方 Tavily 条目，无本地用量。官方端点请求默认直连，不受环境变量代理
// 影响；proxy.api_providers: true 时按引擎层同一套解析走代理。
func New(conf config.Config, store *telemetry.Store) *Service {
	return newService(conf, tavilyUsageEndpoint, newHTTPClient(conf), store)
}

// newHTTPClient 构造官方额度查询客户端：默认直连，api_providers 启用时走代理。
func newHTTPClient(conf config.Config) *http.Client {
	return &http.Client{Timeout: requestTimeout, Transport: proxy.NewUpstreamTransport(conf.Proxy.UpstreamResolver())}
}

// newService 允许测试注入端点与客户端，使测试无需访问真实网络。
func newService(conf config.Config, endpoint string, client *http.Client, store *telemetry.Store) *Service {
	if endpoint == "" {
		endpoint = tavilyUsageEndpoint
	}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	routes := make(map[string]*searchGuard)
	for _, provider := range config.QuotaSearchProviders() {
		enforce := conf.Dashboard.Quotas.Enforces(provider)
		if enforce {
			routes[provider] = &searchGuard{enforce: true, snapshots: make(map[string]searchSnapshot)}
		} else if store != nil && providerKeyConfigured(conf, provider) {
			if present, err := store.HasSearchQuota(provider); err == nil && present {
				routes[provider] = &searchGuard{initialized: true}
			}
		}
	}
	return &Service{conf: conf, store: store, client: client, endpoint: endpoint, now: time.Now,
		tinyfishSearchURL: tinyfishSearchUsageEndpoint, tinyfishFetchURL: tinyfishFetchUsageEndpoint, routes: routes}
}

// Get 返回额度条目：官方 Tavily（有 Key 才查）加上每个 API 供应商的本地
// 用量条目；5 分钟内复用缓存结果，手动重置/修正会立即失效缓存。
func (s *Service) Get(ctx context.Context) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.clock().Sub(s.cachedAt) < cacheTTL {
		return append([]Item(nil), s.cached...)
	}
	items := s.collect(ctx, true)
	if ctx.Err() == nil {
		s.cached = items
		s.cachedAt = s.clock()
	}
	return append([]Item(nil), items...)
}

// Refresh bypasses the regular cache without modifying local quota state.
func (s *Service) Refresh(ctx context.Context) (items []Item, refreshed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.clock().Sub(s.lastRefresh) < refreshInterval {
		return append([]Item{}, s.cached...), false
	}

	items = s.collect(ctx, false)
	s.lastRefresh = s.clock()
	if ctx.Err() != nil {
		return append([]Item{}, s.cached...), false
	}
	s.cached = items
	s.cachedAt = s.lastRefresh
	return append([]Item(nil), items...), true
}

// collect 汇总官方与本地两条来源。没有遥测 store 时保持旧行为：只返回
// 官方条目（含查询失败状态）。有遥测时，官方数字可用则官方优先（本地周期
// 不参与计算），官方失败则回退本地用量。
func (s *Service) collect(ctx context.Context, updateLedger bool) []Item {
	now := s.clock()
	// 两个官方端点并行查询：各自 12s 超时，串行最坏 24s 会拖慢控制中心面板。
	var (
		wg                 sync.WaitGroup
		tavilyItem, tfItem Item
	)
	wg.Add(2)
	go func() { defer wg.Done(); tavilyItem = s.tavily(ctx) }()
	go func() { defer wg.Done(); tfItem = s.tinyfish(ctx) }()
	wg.Wait()
	official := map[string]Item{
		providerTavily:   tavilyItem,
		providerTinyfish: tfItem,
	}
	byName := make(map[string]Item)
	if s.store != nil {
		if names, err := s.store.QuotaProviders(); err == nil {
			for _, rawName := range names {
				name := canonicalQuotaProvider(rawName)
				if _, ok := byName[name]; ok {
					continue
				}
				if item, ok := s.localItem(name, now, updateLedger); ok {
					byName[name] = item
				}
			}
		}
		// 配置里显式给过上限、但遥测还没见过的供应商也要出现（显示 0 用量）。
		for rawName := range s.routes {
			name := canonicalQuotaProvider(rawName)
			if _, ok := byName[name]; !ok {
				if item, ok := s.localItem(name, now, updateLedger); ok {
					byName[name] = item
				}
			}
		}
		for rawName := range s.conf.Dashboard.Quotas.Limits {
			name := canonicalQuotaProvider(rawName)
			if _, ok := byName[name]; !ok {
				if item, ok := s.localItem(name, now, updateLedger); ok {
					byName[name] = item
				}
			}
		}
	}
	for _, provider := range []string{providerTavily, providerTinyfish} {
		item := official[provider]
		local, hasLocal := byName[provider]
		if item.Status == "available" || !hasLocal {
			item.AutoReset = "official"
			if s.SearchEnforced(provider) && local.Used != nil {
				if item.Details == nil {
					item.Details = map[string]any{}
				}
				item.Details["local_search_used"], item.Details["local_search_limit"] = *local.Used, *local.Limit
			}
			byName[provider] = item
		} else if item.Status == "query_failed" {
			local.OfficialQueryFailed = true
			byName[provider] = local
		}
	}
	out := make([]Item, 0, len(byName))
	for _, item := range byName {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// localItem 把一个供应商的真实调用记录折算成本地用量条目；没有配置 Key 也
// 不在 limits 白名单里的免费引擎不生成条目（避免给免费引擎编造上限）。
// 上限口径为「单 Key 积分」：配置了多把 Key 时自动乘以 Key 数，与 apipool
// 的实际消耗容量（Key 数累加）对齐，用户无需手工换算。
func (s *Service) localItem(name string, now time.Time, updateLedger bool) (Item, bool) {
	name = canonicalQuotaProvider(name)
	if name == "" {
		return Item{}, false
	}
	limits := s.conf.Dashboard.Quotas.Limits
	_, explicitLimit := limits[name]
	configured := providerKeyConfigured(s.conf, name)
	if !explicitLimit && !configured {
		return Item{}, false
	}
	limit := float64(s.conf.Dashboard.Quotas.GetLimit(name))
	if n := providerKeyCount(s.conf, name); n > 0 {
		limit *= float64(n)
	}
	item := Item{
		Provider: name, Configured: configured, Status: "no_data", Unit: "calls",
		Source: "local", AutoReset: s.conf.Dashboard.Quotas.GetReset(),
	}
	if s.store == nil {
		return Item{}, false
	}
	if s.SearchEnforced(name) {
		used, err := s.searchUsed(name, now)
		if err != nil {
			item.Error = SearchSkipMessage(i18n.Default(), SkipUnavailable)
			return item, true
		}
		return s.localUsageItem(item, now, used, float64(s.localSearchLimit(name))), true
	}
	periodStart, adjust, err := s.store.QuotaState(name)
	if err != nil {
		item.Error = "读取本地额度状态失败"
		return item, true
	}
	// 自动重置：进入新周期后把周期起点推进到当前窗口、修正量清零。
	// 策略为 none 时不自动推进，只认手动重置。
	if start := s.conf.Dashboard.Quotas.PeriodStart(now); !start.IsZero() {
		if periodStart < start.Unix() {
			if !updateLedger || s.store.SetQuotaState(name, start.Unix(), 0) == nil {
				periodStart, adjust = start.Unix(), 0
			}
		}
	}
	used := adjust
	if periodStart > 0 || s.conf.Dashboard.Quotas.GetReset() != "none" {
		window := periodStart
		if window == 0 {
			window = s.conf.Dashboard.Quotas.PeriodStart(now).Unix()
		}
		if n, err := s.countLocalCalls(name, window); err == nil {
			used = n + adjust
		}
	}
	return s.localUsageItem(item, now, used, limit), true
}

func (s *Service) localUsageItem(item Item, now time.Time, used int64, limit float64) Item {
	if used < 0 {
		used = 0
	}
	remaining := float64(limit) - float64(used)
	if remaining < 0 {
		remaining = 0
	}
	item.Used, item.Limit, item.Remaining = float64Ptr(float64(used)), &limit, &remaining
	item.Status = "observed"
	if used == 0 {
		item.Status = "no_data"
	}
	if reset := s.conf.Dashboard.Quotas.NextReset(now); !reset.IsZero() {
		item.ResetAt = reset.Format(time.RFC3339)
	}
	return item
}

// providerKeyConfigured 报告某 API 供应商是否配置了 Key（免费网页引擎不是
// 额度管理的对象）。
func providerKeyConfigured(conf config.Config, name string) bool {
	return providerKeyCount(conf, name) > 0
}

// providerKeyCount 返回某 API 供应商配置的 Key 数（含主 Key 与 sk_list）。
// 未知供应商返回 0。
func providerKeyCount(conf config.Config, name string) int {
	return len(providerKeys(conf, name))
}

func usableProviderKeys(conf config.Config, name string) []string {
	var keys []string
	seen := make(map[string]bool)
	for _, key := range providerKeys(conf, name) {
		key = strings.TrimSpace(key)
		if key != "" && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	return keys
}

func canonicalQuotaProvider(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "baidu", "baidu_api", "baidu_ai":
		return "baidu"
	case "tavily", "tavily_api":
		return "tavily"
	case "tinyfish", "tinyfish_api":
		return "tinyfish"
	case "keenable", "keenable_api":
		return "keenable"
	case "yuanbao", "tencent_yuanbao":
		return "yuanbao"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func quotaProviderAliases(name string) []string {
	canonical := canonicalQuotaProvider(name)
	switch canonical {
	case "baidu":
		return []string{"baidu", "baidu_api", "baidu_ai"}
	case "tavily":
		return []string{"tavily", "tavily_api"}
	case "tinyfish":
		return []string{"tinyfish", "tinyfish_api"}
	case "keenable":
		return []string{"keenable", "keenable_api"}
	case "yuanbao":
		return []string{"yuanbao", "tencent_yuanbao"}
	default:
		return []string{canonical}
	}
}

func (s *Service) countLocalCalls(provider string, since int64) (int64, error) {
	var total int64
	for _, alias := range quotaProviderAliases(provider) {
		n, err := s.store.CountProviderCalls(alias, since)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func providerKeys(conf config.Config, name string) []string {
	switch canonicalQuotaProvider(name) {
	case "baidu":
		return conf.Baidu.EffectiveSKList()
	case "tavily":
		return conf.Tavily.EffectiveSKList()
	case "exa":
		return conf.Exa.EffectiveSKList()
	case "anysearch":
		return conf.Anysearch.EffectiveSKList()
	case "doubao":
		return conf.Doubao.EffectiveSKList()
	case "yuanbao":
		return conf.Yuanbao.EffectiveSKList()
	case "tinyfish":
		return conf.TinyFish.EffectiveSKList()
	case "keenable":
		return conf.Keenable.EffectiveSKList()
	default:
		return nil
	}
}

// ResetProvider 手动重置某供应商的本地统计周期（管理员操作，需密码鉴权）。
// 窗口起点推到下一秒：occurred_at 按秒落库，取当前秒会把同秒内的既有调用
// 一并计入，违背「从现在重新累计」的语义。
func (s *Service) ResetProvider(provider string) error {
	if s.store == nil {
		return fmt.Errorf("遥测未启用，无法重置本地用量")
	}
	provider = canonicalQuotaProvider(provider)
	if s.TracksSearch(provider) {
		route := s.routes[provider]
		route.mu.Lock()
		err := s.store.SetSearchQuotaUsed(provider, s.clock().UnixNano(), 0)
		if provider == providerTinyfish {
			clear(route.snapshots)
		}
		route.mu.Unlock()
		if err != nil {
			return err
		}
		if s.SearchEnforced(provider) {
			return s.invalidate()
		}
	}
	if err := s.store.SetQuotaState(provider, s.clock().Unix()+1, 0); err != nil {
		return err
	}
	return s.invalidate()
}

// SetProviderUsed 把展示用量修正为指定值（管理员操作，需密码鉴权）。
// 内部换算为修正量：adjust = 目标值 − 当前周期真实观测数，真实记录不动。
func (s *Service) SetProviderUsed(provider string, used int64) error {
	if s.store == nil {
		return fmt.Errorf("遥测未启用，无法修正本地用量")
	}
	provider = canonicalQuotaProvider(provider)
	now := s.clock()
	if s.TracksSearch(provider) {
		route := s.routes[provider]
		route.mu.Lock()
		window, err := s.store.SearchQuotaPeriodStart(provider, s.searchWindow(now))
		if err == nil {
			err = s.store.SetSearchQuotaUsed(provider, window, used)
		}
		route.mu.Unlock()
		if err != nil {
			return err
		}
		if s.SearchEnforced(provider) {
			return s.invalidate()
		}
	}
	periodStart, _, err := s.store.QuotaState(provider)
	if err != nil {
		return err
	}
	if periodStart == 0 {
		if start := s.conf.Dashboard.Quotas.PeriodStart(now); !start.IsZero() {
			periodStart = start.Unix()
		}
	}
	n, err := s.countLocalCalls(provider, periodStart)
	if err != nil {
		return err
	}
	if err := s.store.SetQuotaState(provider, periodStart, used-n); err != nil {
		return err
	}
	return s.invalidate()
}

func (s *Service) invalidate() error {
	s.mu.Lock()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
	return nil
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) tavily(ctx context.Context) Item {
	keys := s.conf.Tavily.EffectiveSKList()
	if len(keys) == 0 {
		return Item{Provider: providerTavily, Status: "not_configured"}
	}
	return s.tavilyForKey(ctx, keys[0])
}

func (s *Service) tavilyForKey(ctx context.Context, key string) Item {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return s.tavilyFailure(err, "")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := s.client.Do(req)
	if err != nil {
		return s.tavilyFailure(err, "")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return s.tavilyFailure(nil, fmt.Sprintf("官方 API 拒绝该 Key（HTTP %d）", res.StatusCode))
	case http.StatusTooManyRequests:
		return s.tavilyFailure(nil, fmt.Sprintf("官方 API 限流（HTTP %d）", res.StatusCode))
	default:
		return s.tavilyFailure(nil, fmt.Sprintf("官方 API 返回 HTTP %d", res.StatusCode))
	}
	var body tavilyUsageResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return s.tavilyFailure(nil, "官方 API 响应无法解析")
	}
	item := tavilyItem(body, s.clock())
	item.Source = s.endpoint
	return item
}

// tavilyFailure 生成统一的失败条目：只给出状态与通用说明，不含 Key 或响应体。
func (s *Service) tavilyFailure(err error, msg string) Item {
	if msg == "" {
		msg = safeError(err)
	}
	return Item{Provider: providerTavily, Configured: true, Status: "query_failed", Error: msg, Source: s.endpoint}
}

func tavilyItem(body tavilyUsageResponse, updatedAt time.Time) Item {
	// The official endpoint returns both a per-key allowance and the account
	// plan allowance. The control center answers the account-level question, so
	// prefer the plan totals when present and fall back to key totals otherwise.
	used, limit := body.Account.PlanUsage, body.Account.PlanLimit
	if limit <= 0 {
		used, limit = body.Key.Usage, body.Key.Limit
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return Item{
		Provider: "tavily", Configured: true, Status: "available", Unit: "credits",
		Used: &used, Limit: &limit, Remaining: &remaining, Plan: body.Account.CurrentPlan,
		UpdatedAt: updatedAt.Format(time.RFC3339), Source: "https://api.tavily.com/usage",
		Details: map[string]any{"key_usage": body.Key.Usage, "key_limit": body.Key.Limit,
			"search_usage": body.Key.SearchUsage, "extract_usage": body.Key.ExtractUsage,
			"account_plan_usage": body.Account.PlanUsage, "account_plan_limit": body.Account.PlanLimit,
			"paygo_usage": body.Account.PaygoUsage, "paygo_limit": body.Account.PaygoLimit},
	}
}

// tinyfish 查询 TinyFish 官方用量端点：搜索为主指标（Used/Limit/Remaining），
// fetch 用量放 Details。两个端点共用同一把 Key（取第一把，与 Tavily 同口径）。
func (s *Service) tinyfish(ctx context.Context) Item {
	keys := s.conf.TinyFish.EffectiveSKList()
	if len(keys) == 0 {
		return Item{Provider: providerTinyfish, Status: "not_configured"}
	}
	dayStart := s.clock().UTC().Truncate(24 * time.Hour)
	item := s.tinyfishSearchItemAt(ctx, keys[0], dayStart)
	if item.Status != "available" {
		return item
	}
	fetchUsed, errMsg := s.tinyfishUsed(ctx, s.tinyfishFetchURL, keys[0], dayStart)
	if errMsg != "" {
		// fetch 计数失败不拖垮主指标：搜索数字照常返回，失败说明进 Details。
		item.Details["fetch_error"] = "fetch 用量查询失败：" + errMsg
		return item
	}
	item.Details["fetch_used"] = fetchUsed
	item.Details["fetch_limit"] = tinyfishFetchDailyLimit
	return item
}

func (s *Service) tinyfishSearchItemAt(ctx context.Context, key string, dayStart time.Time) Item {
	searchUsed, errMsg := s.tinyfishUsed(ctx, s.tinyfishSearchURL, key, dayStart)
	if errMsg != "" {
		return Item{Provider: providerTinyfish, Configured: true, Status: "query_failed", Error: errMsg, Source: s.tinyfishSearchURL}
	}
	used, limit := float64(searchUsed), float64(tinyfishSearchDailyLimit)
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	item := Item{
		Provider: providerTinyfish, Configured: true, Status: "available", Unit: "calls",
		Used: &used, Limit: &limit, Remaining: &remaining,
		UpdatedAt: s.clock().Format(time.RFC3339), Source: s.tinyfishSearchURL,
		ResetAt: dayStart.Add(24 * time.Hour).Format(time.RFC3339),
		Details: map[string]any{
			"search_used": searchUsed, "search_limit": tinyfishSearchDailyLimit,
			"search_limit_source": "hourly_rate_projection",
			"window_start":        dayStart.Format(time.RFC3339),
		},
	}
	return item
}

// tinyfishUsed 用 limit=1 + start_after=当日零点 查官方 /usage 列表，
// 直接取响应里的 total 作为当日调用数（2026-10-24 真机实测：total 随过滤
// 条件返回）。返回值为 (计数, 失败说明)；失败说明为空表示成功。
func (s *Service) tinyfishUsed(ctx context.Context, endpoint, key string, dayStart time.Time) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, safeError(err)
	}
	req.Header.Set("X-API-Key", key)
	q := req.URL.Query()
	q.Set("limit", "1")
	q.Set("start_after", dayStart.Format(time.RFC3339))
	req.URL.RawQuery = q.Encode()
	res, err := s.client.Do(req)
	if err != nil {
		return 0, safeError(err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return 0, fmt.Sprintf("官方 API 拒绝该 Key（HTTP %d）", res.StatusCode)
	case http.StatusTooManyRequests:
		return 0, fmt.Sprintf("官方 API 限流（HTTP %d）", res.StatusCode)
	default:
		return 0, fmt.Sprintf("官方 API 返回 HTTP %d", res.StatusCode)
	}
	var body tinyfishUsageResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return 0, "官方 API 响应无法解析"
	}
	if body.Total == nil || *body.Total < 0 {
		return 0, "官方 API 响应无法解析"
	}
	return *body.Total, ""
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "官方额度接口请求已取消"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "官方额度接口请求超时"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "官方额度接口请求超时"
	}
	return "官方额度接口连接失败"
}

func float64Ptr(v float64) *float64 { return &v }
