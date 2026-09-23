// Package quota reads only documented, official provider usage endpoints.
// It never estimates quota from local request counts and never scrapes a web UI.
//
// Tavily 是唯一能用搜索 Key 查询官方用量的供应商，因此本包只查询并只返回
// Tavily；其他供应商既不探测也不出现在结果里。额度读取是只读操作：不写遥测、
// 不影响健康判定，也不产生计费调用。
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
	"websearch/pkg/proxy"
	"websearch/pkg/telemetry"
)

const (
	// tavilyUsageEndpoint 是 Tavily 官方文档公开的用量查询端点。
	tavilyUsageEndpoint = "https://api.tavily.com/usage"
	// cacheTTL 保证控制中心不会高频访问官方端点。
	cacheTTL = 5 * time.Minute
	// requestTimeout 是单次额度查询的整体超时。
	requestTimeout = 12 * time.Second
	// providerTavily 是唯一被查询的供应商名。
	providerTavily = "tavily"
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
	// ResetAt 是下一次自动重置时间（RFC3339）；策略为 none 时不返回。
	ResetAt string `json:"reset_at,omitempty"`
	// AutoReset 是生效的重置策略：monthly / weekly / daily / none / official。
	// official 表示该条目由供应商官方端点给出，本地周期不参与计算。
	AutoReset string `json:"auto_reset,omitempty"`
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

	mu       sync.Mutex
	cached   []Item
	cachedAt time.Time
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
	return &Service{conf: conf, store: store, client: client, endpoint: endpoint, now: time.Now}
}

// Get 返回额度条目：官方 Tavily（有 Key 才查）加上每个 API 供应商的本地
// 用量条目；5 分钟内复用缓存结果，手动重置/修正会立即失效缓存。
func (s *Service) Get(ctx context.Context) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.clock().Sub(s.cachedAt) < cacheTTL {
		return append([]Item(nil), s.cached...)
	}
	items := s.collect(ctx)
	s.cached = items
	s.cachedAt = s.clock()
	return append([]Item(nil), items...)
}

// collect 汇总官方与本地两条来源。没有遥测 store 时保持旧行为：只返回
// 官方 Tavily 条目（含查询失败状态）。有遥测时，Tavily 官方数字可用则官方
// 优先（本地周期不参与计算），官方失败则回退本地用量。
func (s *Service) collect(ctx context.Context) []Item {
	now := s.clock()
	official := s.tavily(ctx)
	byName := make(map[string]Item)
	if s.store != nil {
		if names, err := s.store.QuotaProviders(); err == nil {
			for _, name := range names {
				if item, ok := s.localItem(name, now); ok {
					byName[name] = item
				}
			}
		}
		// 配置里显式给过上限、但遥测还没见过的供应商也要出现（显示 0 用量）。
		for name := range s.conf.Dashboard.Quotas.Limits {
			if _, ok := byName[name]; !ok {
				if item, ok := s.localItem(name, now); ok {
					byName[name] = item
				}
			}
		}
	}
	if official.Status == "available" || byName[providerTavily].Status == "" {
		official.AutoReset = "official"
		byName[providerTavily] = official
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
func (s *Service) localItem(name string, now time.Time) (Item, bool) {
	if name == "" {
		return Item{}, false
	}
	limits := s.conf.Dashboard.Quotas.Limits
	_, explicitLimit := limits[name]
	if !explicitLimit && !providerKeyConfigured(s.conf, name) {
		return Item{}, false
	}
	limit := float64(s.conf.Dashboard.Quotas.GetLimit(name))
	if n := providerKeyCount(s.conf, name); n > 0 {
		limit *= float64(n)
	}
	item := Item{
		Provider: name, Status: "no_data", Unit: "calls",
		Source: "local", AutoReset: s.conf.Dashboard.Quotas.GetReset(),
	}
	if s.store == nil {
		return Item{}, false
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
			if err := s.store.SetQuotaState(name, start.Unix(), 0); err == nil {
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
		if n, err := s.store.CountProviderCalls(name, window); err == nil {
			used = n + adjust
		}
	}
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
	return item, true
}

// providerKeyConfigured 报告某 API 供应商是否配置了 Key（免费网页引擎不是
// 额度管理的对象）。
func providerKeyConfigured(conf config.Config, name string) bool {
	return providerKeyCount(conf, name) > 0
}

// providerKeyCount 返回某 API 供应商配置的 Key 数（含主 Key 与 sk_list）。
// 未知供应商返回 0。
func providerKeyCount(conf config.Config, name string) int {
	switch strings.ToLower(name) {
	case "baidu", "baidu_api":
		return len(conf.Baidu.EffectiveSKList())
	case "tavily", "tavily_api":
		return len(conf.Tavily.EffectiveSKList())
	case "exa":
		return len(conf.Exa.EffectiveSKList())
	case "anysearch":
		return len(conf.Anysearch.EffectiveSKList())
	case "doubao":
		return len(conf.Doubao.EffectiveSKList())
	default:
		return 0
	}
}

// ResetProvider 手动重置某供应商的本地统计周期（管理员操作，需密码鉴权）。
// 窗口起点推到下一秒：occurred_at 按秒落库，取当前秒会把同秒内的既有调用
// 一并计入，违背「从现在重新累计」的语义。
func (s *Service) ResetProvider(provider string) error {
	if s.store == nil {
		return fmt.Errorf("遥测未启用，无法重置本地用量")
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
	now := s.clock()
	periodStart, _, err := s.store.QuotaState(provider)
	if err != nil {
		return err
	}
	if periodStart == 0 {
		if start := s.conf.Dashboard.Quotas.PeriodStart(now); !start.IsZero() {
			periodStart = start.Unix()
		}
	}
	n, err := s.store.CountProviderCalls(provider, periodStart)
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
	s.cached = nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return s.tavilyFailure(err, "")
	}
	req.Header.Set("Authorization", "Bearer "+keys[0])
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
