// Package mcphub 零 Key 托管 MCP 级联 hub。
//
// 按配置顺序轮转 Parallel / AnySearch / Exa / Firecrawl 等托管 MCP server：
// 单次请求命中一个后端，失败进入冷却并级联到下一个。可把本地网页引擎
// （baidu_web / so360 / …）加入同一轮转池分摊匿名限流；全部轮转后端失败后
// 再走 fallback_engines，最后由 smartsearch 统一回退 Bing。
// cleanfetch 可通过 mcp_hub.fetch_mode 复用同一批 MCP server。
package mcphub

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/proxy"
	"websearch/pkg/search/core"
	"websearch/pkg/search/provider"
	"websearch/pkg/telemetry"
)

// preset 内置 server 的端点、工具与鉴权约定。
type preset struct {
	url           string
	searchTool    string
	fetchTool     string
	authHeader    string
	authPrefix    string
	requiresProxy bool
	searchArgs    func(query string, max int) map[string]any
	fetchArgs     func(rawURL string) map[string]any
}

var presets = map[string]preset{
	"parallel": {
		url:        "https://search.parallel.ai/mcp",
		searchTool: "web_search",
		fetchTool:  "web_fetch",
		authHeader: "Authorization",
		authPrefix: "Bearer ",
		searchArgs: func(query string, _ int) map[string]any {
			return map[string]any{"objective": query, "search_queries": []string{query}}
		},
		fetchArgs: func(rawURL string) map[string]any {
			return map[string]any{"urls": []string{rawURL}, "full_content": true}
		},
	},
	"exa": {
		url:           "https://mcp.exa.ai/mcp",
		searchTool:    "web_search_exa",
		fetchTool:     "web_fetch_exa",
		authHeader:    "x-api-key",
		requiresProxy: true,
		searchArgs: func(query string, max int) map[string]any {
			return map[string]any{"query": query, "numResults": max}
		},
		fetchArgs: func(rawURL string) map[string]any {
			return map[string]any{"urls": []string{rawURL}}
		},
	},
	"anysearch": {
		url:        "https://api.anysearch.com/mcp",
		searchTool: "search",
		fetchTool:  "extract",
		authHeader: "Authorization",
		authPrefix: "Bearer ",
		searchArgs: func(query string, max int) map[string]any {
			if max <= 0 || max > 10 {
				max = 10
			}
			return map[string]any{"query": query, "max_results": max}
		},
		fetchArgs: func(rawURL string) map[string]any {
			return map[string]any{"url": rawURL}
		},
	},
	"firecrawl": {
		url:           "https://mcp.firecrawl.dev/v2/mcp",
		searchTool:    "firecrawl_search",
		fetchTool:     "firecrawl_scrape",
		authHeader:    "Authorization",
		authPrefix:    "Bearer ",
		requiresProxy: true,
		searchArgs: func(query string, max int) map[string]any {
			return map[string]any{"query": query, "limit": max}
		},
		fetchArgs: func(rawURL string) map[string]any {
			return map[string]any{"url": rawURL, "formats": []string{"markdown"}}
		},
	},
}

// backend 轮转池中的搜索/抓取后端。engineBackend 只支持搜索。
type backend interface {
	Name() string
	Search(ctx context.Context, query string, max int) ([]core.SearchResult, error)
	Fetch(ctx context.Context, rawURL string, maxChars int) (string, error)
	SupportsFetch() bool
	Available() bool
}

type server struct {
	name       string
	url        string
	searchTool string
	fetchTool  string
	authHeader string
	authPrefix string
	keys       *provider.KeyPool

	caller       toolCaller
	timeout      time.Duration
	fetchTimeout time.Duration

	requiresProxy bool
	proxyResolver proxy.ProxyResolver
}

func (s *server) Name() string        { return "mcp_hub_" + s.name }
func (s *server) SupportsFetch() bool { return s.fetchTool != "" }

// Available 在 requires_proxy=true 时按当前代理解析结果动态判断；
// 代理稍后才可用时，下一次请求即可重新启用，无需重启。
func (s *server) Available() bool {
	if !s.requiresProxy {
		return true
	}
	return s.proxyResolver != nil && s.proxyResolver() != ""
}

func (s *server) Search(ctx context.Context, query string, max int) ([]core.SearchResult, error) {
	key := ""
	if s.keys != nil {
		key = s.keys.Next()
	}
	res, err := s.caller.Call(ctx, s, s.searchTool, s.searchArgs(query, max), s.timeout, key)
	if err != nil {
		if key != "" && s.keys != nil && isAuthError(err) {
			s.keys.MarkInvalid(key)
		}
		return nil, err
	}
	results, err := parseSearch(s.name, res, max)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.name, err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("%s: 空结果", s.name)
	}
	return results, nil
}

func (s *server) Fetch(ctx context.Context, rawURL string, maxChars int) (string, error) {
	key := ""
	if s.keys != nil {
		key = s.keys.Next()
	}
	res, err := s.caller.Call(ctx, s, s.fetchTool, s.fetchArgs(rawURL), s.fetchTimeout, key)
	if err != nil {
		if key != "" && s.keys != nil && isAuthError(err) {
			s.keys.MarkInvalid(key)
		}
		return "", err
	}
	text, err := parseFetch(s.name, res)
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.name, err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s: 抓取结果为空", s.name)
	}
	if maxChars > 0 {
		runes := []rune(text)
		if len(runes) > maxChars {
			text = string(runes[:maxChars]) + fmt.Sprintf("\n\n...（已截断，原长度 %d 字符；可调大 mcp_hub.max_fetch_chars）", len(runes))
		}
	}
	return text, nil
}

func (s *server) searchArgs(query string, max int) map[string]any {
	return presets[s.name].searchArgs(query, max)
}

func (s *server) fetchArgs(rawURL string) map[string]any {
	return presets[s.name].fetchArgs(rawURL)
}

// engineBackend 把本地网页引擎接入同一轮转池，用于分摊 MCP 匿名限流。
type engineBackend struct {
	name   string
	engine core.SearchInf
}

func (e *engineBackend) Name() string        { return e.name }
func (e *engineBackend) SupportsFetch() bool { return false }
func (e *engineBackend) Available() bool     { return true }

func (e *engineBackend) Search(ctx context.Context, query string, max int) ([]core.SearchResult, error) {
	var results []core.SearchResult
	var err error
	if cs, ok := e.engine.(core.ContextSearcher); ok {
		results, err = cs.SearchRawContext(ctx, query)
	} else {
		results, err = e.engine.SearchRaw(query)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.name, err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("%s: 空结果", e.name)
	}
	if max > 0 && len(results) > max {
		results = results[:max]
	}
	for i := range results {
		if results[i].Engine == "" {
			results[i].Engine = e.name
		}
	}
	return results, nil
}

func (e *engineBackend) Fetch(context.Context, string, int) (string, error) {
	return "", fmt.Errorf("%s: 不支持网页抓取", e.name)
}

type backendState struct {
	backend        backend
	searchCooldown atomic.Int64
	fetchCooldown  atomic.Int64
}

func (s *backendState) cooled(fetch bool, now time.Time) bool {
	until := s.searchCooldown.Load()
	if fetch {
		until = s.fetchCooldown.Load()
	}
	return until > now.UnixNano()
}

func (s *backendState) cooldownUntil(fetch bool) time.Time {
	if fetch {
		return time.Unix(0, s.fetchCooldown.Load())
	}
	return time.Unix(0, s.searchCooldown.Load())
}

func (s *backendState) markCooldown(fetch bool, d time.Duration) {
	until := time.Now().Add(d).UnixNano()
	if fetch {
		s.fetchCooldown.Store(until)
	} else {
		s.searchCooldown.Store(until)
	}
}

func (s *backendState) clearCooldown(fetch bool) {
	if fetch {
		s.fetchCooldown.Store(0)
	} else {
		s.searchCooldown.Store(0)
	}
}

// Hub 实现 core.SearchInf；Fetch 供 cleanfetch 增强路径使用。
type Hub struct {
	rotation []*backendState
	fallback []*backendState
	strategy string

	timeout      time.Duration
	fetchTimeout time.Duration
	cooldown     time.Duration
	maxResults   int
	maxFetch     int

	searchIdx atomic.Uint64
	fetchIdx  atomic.Uint64
}

// New 创建级联 hub。engines 为可加入轮转/降级的本地网页引擎；
// resolver 为 nil 时直连（不会读取环境变量代理）。fetch-only 场景可传 nil engines。
func New(cfg config.MCPHubConfig, resolver proxy.ProxyResolver, engines map[string]core.SearchInf) (*Hub, error) {
	return newHub(cfg, resolver, nil, engines)
}

func newHub(cfg config.MCPHubConfig, resolver proxy.ProxyResolver, caller toolCaller, engines map[string]core.SearchInf) (*Hub, error) {
	cfg.NormalizeMCPHub()
	if !cfg.IsEnabled() {
		return nil, fmt.Errorf("mcp_hub 已禁用")
	}
	directBase := proxy.NewUpstreamTransport(nil)
	proxyBase := proxy.NewUpstreamTransport(resolver)
	h := &Hub{
		strategy:     cfg.GetStrategy(),
		timeout:      cfg.GetTimeout(),
		fetchTimeout: cfg.GetFetchTimeout(),
		cooldown:     cfg.GetCooldown(),
		maxResults:   cfg.MaxResults,
		maxFetch:     cfg.MaxFetchChars,
	}
	seen := map[string]bool{}
	for _, sc := range cfg.GetServers() {
		if !sc.IsEnabled() {
			continue
		}
		s, err := buildServer(sc)
		if err != nil {
			return nil, err
		}
		if seen[s.name] {
			log.Infof("mcp_hub: server %q 重复配置，已跳过", s.name)
			continue
		}
		if caller != nil {
			s.caller = caller
		} else if s.requiresProxy {
			s.caller = &mcpCaller{base: proxyBase}
		} else {
			s.caller = &mcpCaller{base: directBase}
		}
		s.proxyResolver = resolver
		s.timeout = h.timeout
		s.fetchTimeout = h.fetchTimeout
		if s.requiresProxy && !s.Available() {
			log.Infof("mcp_hub: %s 标记为需代理，当前未检测到可用代理，运行时自动跳过", s.name)
		}
		h.rotation = append(h.rotation, &backendState{backend: s})
		seen[s.name] = true
	}
	if engines != nil {
		h.rotation = append(h.rotation, buildEngineStates(cfg.GetRotationEngines(), engines, seen)...)
		h.fallback = buildEngineStates(cfg.GetFallbackEngines(), engines, seen)
	}
	if len(h.rotation) == 0 && len(h.fallback) == 0 {
		return nil, fmt.Errorf("mcp_hub 无可用后端")
	}
	return h, nil
}

func buildEngineStates(names []string, engines map[string]core.SearchInf, seen map[string]bool) []*backendState {
	out := make([]*backendState, 0, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || seen[name] {
			continue
		}
		eng, ok := engines[name]
		if !ok || eng == nil {
			log.Infof("mcp_hub: engine %q 不可用（未启用或缺少代理条件），跳过", name)
			continue
		}
		seen[name] = true
		out = append(out, &backendState{backend: &engineBackend{name: name, engine: eng}})
	}
	return out
}

func buildServer(sc config.MCPHubServerConfig) (*server, error) {
	p, ok := presets[sc.Name]
	if !ok {
		return nil, fmt.Errorf("mcp_hub: 未知 server %q（仅支持 parallel/anysearch/exa/firecrawl；新增上游请在 mcphub 包内添加 preset 与解析器）", sc.Name)
	}
	s := &server{
		name:          sc.Name,
		url:           p.url,
		searchTool:    p.searchTool,
		fetchTool:     p.fetchTool,
		authHeader:    p.authHeader,
		authPrefix:    p.authPrefix,
		requiresProxy: p.requiresProxy,
	}
	if sc.RequiresProxy != nil {
		s.requiresProxy = *sc.RequiresProxy
	}
	if strings.TrimSpace(s.url) == "" {
		return nil, fmt.Errorf("mcp_hub: server %q 缺少 url", sc.Name)
	}
	u, err := url.Parse(s.url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("mcp_hub: server %q 的 url 无效: %q", sc.Name, s.url)
	}
	if s.searchTool == "" {
		return nil, fmt.Errorf("mcp_hub: server %q 缺少 tool", sc.Name)
	}
	if keys := sc.EffectiveKeys(); len(keys) > 0 {
		pool, err := provider.NewKeyPool(keys)
		if err != nil {
			return nil, fmt.Errorf("mcp_hub: server %q 的 Key 配置无效: %w", sc.Name, err)
		}
		s.keys = pool
	}
	return s, nil
}

func (h *Hub) Name() string { return "mcphub" }

// RotationNames 返回主轮转池顺序，供启动日志使用。
func (h *Hub) RotationNames() []string {
	out := make([]string, 0, len(h.rotation))
	for _, st := range h.rotation {
		out = append(out, st.backend.Name())
	}
	return out
}

// FallbackNames 返回轮转池全部失败后的降级顺序。
func (h *Hub) FallbackNames() []string {
	out := make([]string, 0, len(h.fallback))
	for _, st := range h.fallback {
		out = append(out, st.backend.Name())
	}
	return out
}

func (h *Hub) Search(query string) (string, error) {
	results, err := h.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return h.MergeContent(query, results)
}

func (h *Hub) SearchRaw(query string) ([]core.SearchResult, error) {
	return h.SearchRawContext(context.Background(), query)
}

// SearchRawContext 实现 core.ContextSearcher：ctx 贯穿轮转、降级与冷却。
func (h *Hub) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := h.startIndex(false)
	var errs []string
	if results, ok := h.trySearch(ctx, h.rotation, start, query, &errs); ok {
		return results, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(h.fallback) > 0 {
		if results, ok := h.trySearch(ctx, h.fallback, start, query, &errs); ok {
			return results, nil
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(errs) == 0 {
		errs = append(errs, "无可用后端")
	}
	return nil, fmt.Errorf("mcp_hub: 所有搜索后端均失败: %s", strings.Join(errs, "; "))
}

func (h *Hub) trySearch(ctx context.Context, states []*backendState, start int, query string, errs *[]string) ([]core.SearchResult, bool) {
	candidates, err := h.candidates(states, false, start)
	if err != nil {
		*errs = append(*errs, err.Error())
		return nil, false
	}
	for _, st := range candidates {
		if ctx.Err() != nil {
			return nil, false
		}
		started := time.Now()
		results, err := st.backend.Search(ctx, query, h.maxResults)
		if err == nil && len(results) > 0 {
			st.clearCooldown(false)
			telemetry.Record(telemetry.Event{Kind: "provider", Provider: st.backend.Name(),
				Query: query, Success: true, Duration: time.Since(started), ResultCount: len(results)})
			return results, true
		}
		if err == nil {
			err = errors.New("空结果")
		}
		if ctx.Err() != nil {
			return nil, false
		}
		h.handleFailure(st, false, err)
		telemetry.Record(telemetry.Event{Kind: "provider", Provider: st.backend.Name(),
			Query: query, Success: false, Duration: time.Since(started), Error: err})
		*errs = append(*errs, fmt.Sprintf("%s: %v", st.backend.Name(), err))
	}
	return nil, false
}

// Fetch 通过托管 MCP 抓取网页正文；本地引擎不参与抓取。
func (h *Hub) Fetch(ctx context.Context, rawURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := h.startIndex(true)
	candidates, err := h.candidates(h.rotation, true, start)
	if err != nil {
		return "", err
	}
	var errs []string
	for _, st := range candidates {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		started := time.Now()
		text, err := st.backend.Fetch(ctx, rawURL, h.maxFetch)
		if err == nil && strings.TrimSpace(text) != "" {
			st.clearCooldown(true)
			telemetry.Record(telemetry.Event{Kind: "provider", Provider: st.backend.Name(),
				Query: rawURL, Success: true, Duration: time.Since(started), ResultCount: 1})
			return text, nil
		}
		if err == nil {
			err = errors.New("空结果")
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		h.handleFailure(st, true, err)
		telemetry.Record(telemetry.Event{Kind: "provider", Provider: st.backend.Name(),
			Query: rawURL, Success: false, Duration: time.Since(started), Error: err})
		errs = append(errs, fmt.Sprintf("%s: %v", st.backend.Name(), err))
	}
	return "", fmt.Errorf("mcp_hub: 所有托管 MCP 抓取均失败: %s", strings.Join(errs, "; "))
}

func (h *Hub) startIndex(fetch bool) int {
	if h.strategy != "round-robin" {
		return 0
	}
	if fetch {
		return int(h.fetchIdx.Add(1) - 1)
	}
	return int(h.searchIdx.Add(1) - 1)
}

func (h *Hub) handleFailure(st *backendState, fetch bool, err error) {
	if err == nil {
		err = errors.New("空结果")
	}
	if shouldCooldown(err) {
		st.markCooldown(fetch, h.cooldown)
		log.Infof("mcp_hub: %s %s失败，冷却 %s: %v", st.backend.Name(), capabilityWord(fetch), h.cooldown, err)
	}
}

func capabilityWord(fetch bool) string {
	if fetch {
		return "抓取"
	}
	return "搜索"
}

// candidates 返回按策略排序且未冷却的后端；全部冷却时直接报错，让上层降级。
func (h *Hub) candidates(states []*backendState, fetch bool, start int) ([]*backendState, error) {
	n := len(states)
	if n == 0 {
		return nil, fmt.Errorf("mcp_hub: 无可用于%s的后端", capabilityWord(fetch))
	}
	now := time.Now()
	out := make([]*backendState, 0, n)
	var earliest time.Time
	for i := 0; i < n; i++ {
		st := states[(start+i)%n]
		if fetch && !st.backend.SupportsFetch() {
			continue
		}
		if !st.backend.Available() {
			continue
		}
		if st.cooled(fetch, now) {
			until := st.cooldownUntil(fetch)
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			continue
		}
		out = append(out, st)
	}
	if len(out) == 0 {
		if earliest.IsZero() {
			return nil, fmt.Errorf("mcp_hub: 无可用于%s的后端", capabilityWord(fetch))
		}
		return nil, fmt.Errorf("mcp_hub: 所有后端处于冷却中（最早 %s 恢复）", earliest.Format("15:04:05"))
	}
	return out, nil
}

func (h *Hub) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}
	var b strings.Builder
	b.Grow(1024 * len(results))
	b.WriteString(core.MDSearchHeader(query, len(results)))
	for i, r := range results {
		if core.ShowMeta {
			b.WriteString(core.FormatMDScore(i+1, r.Title, r.Url, r.Engine, core.FormatScore(r.Score), core.FormatDateSource(r.PublishDate, r.DateSource), r.Content))
		} else {
			b.WriteString(core.FormatMD(i+1, r.Title, r.Url, r.Content))
		}
	}
	return b.String(), nil
}
