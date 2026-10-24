package dashboard

import (
	"slices"
	"sort"
	"strings"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

type SystemSummary struct {
	Status    string `json:"status"`
	Enabled   int    `json:"enabled"`
	Observed  int    `json:"observed"`
	Degraded  int    `json:"degraded"`
	Down      int    `json:"down"`
	Suspended int    `json:"suspended"`
}

type SourceView struct {
	ID                  string                     `json:"id"`
	Name                string                     `json:"name"`
	Group               string                     `json:"group"`
	Category            string                     `json:"category"`
	Enabled             bool                       `json:"enabled"`
	ControlPath         string                     `json:"control_path"`
	Inverted            bool                       `json:"inverted"`
	Condition           string                     `json:"condition"`
	Configured          bool                       `json:"configured"`
	Active              bool                       `json:"active"`
	Status              string                     `json:"status"`
	ObservationStatus   string                     `json:"observation_status,omitempty"`
	LastSuccessAt       string                     `json:"last_success_at,omitempty"`
	LastFailureAt       string                     `json:"last_failure_at,omitempty"`
	LastSeenAt          string                     `json:"last_seen_at,omitempty"`
	FailureRate         float64                    `json:"failure_rate"`
	SampleSize          int                        `json:"sample_size"`
	ConsecutiveFailures int                        `json:"consecutive_failures"`
	RecentOutcomes      []bool                     `json:"recent_outcomes"`
	Today               telemetry.DailyUsage       `json:"today"`
	LastError           string                     `json:"last_error,omitempty"`
	State               string                     `json:"state,omitempty"`
	Confidence          string                     `json:"confidence,omitempty"`
	Suspended           bool                       `json:"suspended"`
	SuspendedUntil      string                     `json:"suspended_until,omitempty"`
	SuspendReason       string                     `json:"suspend_reason,omitempty"`
	SuspendCountdown    int64                      `json:"suspend_countdown_sec,omitempty"`
	SuspensionScope     string                     `json:"suspension_scope"`
	ActualCooldown      *ActualCooldownView        `json:"actual_cooldown"`
	P95MS               int64                      `json:"p95_ms"`
	ErrorKinds          []telemetry.ErrorKindCount `json:"error_kinds,omitempty"`
	QuotaQueryable      bool                       `json:"quota_queryable"`
	aliases             []string
}

// ActualCooldownView is the routing cooldown registry, not a telemetry warning.
type ActualCooldownView struct {
	Engine           string `json:"engine"`
	Kind             string `json:"kind"`
	Reason           string `json:"reason"`
	Until            string `json:"until"`
	RemainingSeconds int64  `json:"remaining_seconds"`
	CooldownSeconds  int64  `json:"cooldown_seconds"`
	PersistEnabled   bool   `json:"persist_enabled"`
}

type dashboardOverview struct {
	telemetry.Overview
	System          SystemSummary `json:"system"`
	Sources         []SourceView  `json:"sources"`
	AcademicSources []SourceView  `json:"academic_sources"`
	// ConfiguredTools 是公开 MCP 工具的固定清单，不依赖是否已经产生调用。
	ConfiguredTools []MCPToolView `json:"configured_tools"`
	// Brand 是控制台品牌呈现（标题/主题/主色/logo），允许自托管用户自定义。
	Brand map[string]any `json:"brand"`
}

func buildDashboardOverview(conf config.Config, observed telemetry.Overview) dashboardOverview {
	web := joinSources(webSourceCatalog(conf), observed.Providers)
	academic := joinSources(academicSourceCatalog(conf), observed.Providers)
	summary := summarizeSystem(web)
	// Suspension covers both web and academic providers.
	summary.Suspended += countSuspended(academic)
	sortSources(web)
	sortSources(academic)
	return dashboardOverview{
		Overview:        observed,
		System:          summary,
		Sources:         web,
		AcademicSources: academic,
		ConfiguredTools: mcpToolCatalog(conf),
		Brand:           brandView(conf),
	}
}

func joinSources(defs []SourceView, observed []telemetry.Health) []SourceView {
	cooldowns := make(map[string]antirobot.HealthEntry)
	for _, entry := range antirobot.HealthSnapshot() {
		cooldowns[entry.Engine] = entry
	}
	persistEnabled := antirobot.HealthPersistEnabled()
	byName := make(map[string]telemetry.Health, len(observed))
	for _, h := range observed {
		byName[h.Name] = h
	}
	for i := range defs {
		s := &defs[i]
		s.SuspensionScope = "observation"
		s.ActualCooldown = nil
		for _, alias := range append([]string{s.ID}, s.aliases...) {
			if entry, ok := cooldowns[alias]; ok {
				until := time.UnixMilli(int64(entry.Until * 1000))
				remaining := int64(time.Until(until).Seconds())
				if remaining < 1 {
					remaining = 1
				}
				s.ActualCooldown = &ActualCooldownView{Engine: entry.Engine, Kind: entry.Kind,
					Reason: entry.Reason, Until: until.Format(time.RFC3339),
					RemainingSeconds: remaining, CooldownSeconds: int64(entry.Cooldown), PersistEnabled: persistEnabled}
				break
			}
		}
		// Preserve history even when a source is disabled or loses its key.
		var h telemetry.Health
		var ok bool
		var today telemetry.DailyUsage
		for _, alias := range s.aliases {
			candidate, found := byName[alias]
			if !found {
				continue
			}
			// One source can have different runtime names in Hybrid and API-pool
			// modes. Use the newest alias for passive health, while adding today's
			// attempts across aliases so they do not disappear from the source row.
			if !ok || candidate.LastSeenAt > h.LastSeenAt {
				h = candidate
				ok = true
			}
			if today.Day == "" {
				today.Day, today.Kind, today.Provider = candidate.Today.Day, "provider", s.ID
			}
			if candidate.P95MS > h.P95MS {
				h.P95MS = candidate.P95MS
			}
			// Failure composition is additive across aliases: the newest alias
			// may have no failures while an older one did.
			s.ErrorKinds = mergeErrorKindCounts(s.ErrorKinds, candidate.ErrorKinds)
			today.Requests += candidate.Today.Requests
			today.Successes += candidate.Today.Successes
			today.Failures += candidate.Today.Failures
			today.CacheHits += candidate.Today.CacheHits
			today.DurationMS += candidate.Today.DurationMS
			today.ResultCount += candidate.Today.ResultCount
		}
		if !ok {
			s.Status = "unknown"
			s.State = "unknown"
			if !s.Configured {
				s.Status = "not_configured"
			} else if !s.Active {
				s.Status = "inactive"
			}
			continue
		}
		s.Status = h.Status
		s.ObservationStatus = h.Status
		s.LastSuccessAt = h.LastSuccessAt
		s.LastFailureAt = h.LastFailureAt
		s.LastSeenAt = h.LastSeenAt
		s.FailureRate = h.FailureRate
		s.SampleSize = h.SampleSize
		s.ConsecutiveFailures = h.ConsecutiveFailures
		s.RecentOutcomes = h.RecentOutcomes
		s.Today = today
		s.LastError = h.LastError
		s.State = h.State
		if h.SuspendedUntil == "" && s.State == "suspended" {
			// Suspension expired without a newer event: fall back to the
			// rolled-up status instead of showing a stale circuit breaker.
			s.State = h.Status
		}
		if s.State == "" {
			s.State = h.Status
		}
		s.Confidence = h.Confidence
		if h.SuspendedUntil != "" {
			s.Suspended = true
			s.SuspendedUntil = h.SuspendedUntil
			s.SuspendReason = h.SuspendReason
			s.SuspendCountdown = h.SuspendCountdown
		} else {
			s.Suspended = false
		}
		s.P95MS = h.P95MS
		if !s.Configured {
			s.Status = "not_configured"
		} else if !s.Active {
			s.Status = "inactive"
		}
	}
	return defs
}

// mergeErrorKindCounts combines failure compositions from multiple runtime
// aliases of the same source and keeps the newest list ordered by count.
func mergeErrorKindCounts(current, incoming []telemetry.ErrorKindCount) []telemetry.ErrorKindCount {
	if len(incoming) == 0 {
		return current
	}
	counts := make(map[string]int, len(current)+len(incoming))
	for _, item := range current {
		counts[item.Kind] += item.Count
	}
	for _, item := range incoming {
		counts[item.Kind] += item.Count
	}
	merged := make([]telemetry.ErrorKindCount, 0, len(counts))
	for kind, count := range counts {
		merged = append(merged, telemetry.ErrorKindCount{Kind: kind, Count: count})
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Count != merged[j].Count {
			return merged[i].Count > merged[j].Count
		}
		return merged[i].Kind < merged[j].Kind
	})
	return merged
}

// sortSources 把源按展示优先级分层：已启用的最前，已配置未启用的居中，
// 未配置的最后；同层内保持目录原顺序（稳定排序）。
func sortSources(sources []SourceView) {
	sort.SliceStable(sources, func(i, j int) bool {
		return sourceTier(sources[i]) < sourceTier(sources[j])
	})
}

// sourceTier 是源的展示层级：0 = 已启用，1 = 已配置未启用，2 = 未配置。
func sourceTier(s SourceView) int {
	switch {
	case s.Active:
		return 0
	case s.Configured:
		return 1
	default:
		return 2
	}
}

func summarizeSystem(sources []SourceView) SystemSummary {
	out := SystemSummary{Status: "waiting"}
	for _, s := range sources {
		if !s.Active {
			continue
		}
		out.Enabled++
		if s.Suspended || s.State == "suspended" {
			out.Suspended++
		}
		switch s.Status {
		case "down":
			out.Down++
			out.Observed++
		case "degraded":
			out.Degraded++
			out.Observed++
		case "healthy":
			out.Observed++
		}
	}
	switch {
	case out.Down > 0:
		out.Status = "down"
	case out.Degraded > 0:
		out.Status = "degraded"
	case out.Observed == 0:
		out.Status = "waiting"
	default:
		out.Status = "healthy"
	}
	return out
}

// countSuspended counts passive observation warnings; it does not affect routing.
func countSuspended(sources []SourceView) int {
	count := 0
	for _, source := range sources {
		if source.Active && source.Suspended {
			count++
		}
	}
	return count
}

func webSourceCatalog(conf config.Config) []SourceView {
	mode := conf.GetMode()
	proxyAllowed := conf.Proxy.ProxyResolver() != nil
	proxyResolved := conf.Proxy.GetProxyEndpoint() != ""
	apiConfigured := map[string]bool{
		"anysearch": true, // Anonymous mode needs no key.
		"baidu":     len(conf.Baidu.EffectiveSKList()) > 0,
		"tavily":    len(conf.Tavily.EffectiveSKList()) > 0,
		"exa":       len(conf.Exa.EffectiveSKList()) > 0,
		"doubao":    len(conf.Doubao.EffectiveSKList()) > 0,
		"tinyfish":  len(conf.TinyFish.EffectiveSKList()) > 0,
		// keenable 无 Key 可显式 enabled 开启 public 免 Key 档（anysearch 匿名档同口径按已配置呈现）；
		// apipool.engines 显式列出同样视为 opt-in（该模式下 public 档直接参与轮转）。
		"keenable": len(conf.Keenable.EffectiveSKList()) > 0 || conf.Keenable.PublicTierOptedIn() ||
			slices.Contains(conf.Apipool.GetEngines(), "keenable"),
	}
	engineConfigured := map[string]bool{
		"baidu_web": true, "bing": true, "google": true, "duckduckgo": true,
		"so360": true, "wikipedia": true, "googlenews": true, "github": true,
	}
	engineEnabled := map[string]bool{
		"baidu_web": conf.Baidu.WebEnabled, "bing": conf.Bing.Enabled,
		"google": conf.Google.Enabled, "duckduckgo": conf.DuckDuckGo.Enabled,
		"so360": conf.So360.Enabled, "wikipedia": conf.Wikipedia.Enabled,
		"googlenews": conf.GoogleNews.Enabled, "github": conf.GitHub.Enabled,
	}
	engineReady := map[string]bool{}
	for name, enabled := range engineEnabled {
		engineReady[name] = enabled
		if name == "duckduckgo" || name == "wikipedia" || name == "googlenews" {
			engineReady[name] = enabled && proxyAllowed
		}
	}
	active := map[string]bool{}
	activateAPI := func(name string) {
		if apiConfigured[name] && conf.APISourceEnabled(name) {
			active[name] = true
		}
	}
	activateEngines := func() {
		for name, configured := range engineReady {
			if configured {
				active[name] = true
			}
		}
	}
	switch mode {
	case config.ModeHybrid:
		for name := range apiConfigured {
			activateAPI(name)
		}
		activateEngines()
	case config.ModeEngine:
		activateEngines()
	case config.ModeApipool:
		for _, name := range conf.Apipool.GetEngines() {
			activateAPI(name)
		}
		if engineReady["baidu_web"] {
			active["baidu_web"] = true
		}
	case config.ModeBaidu:
		if apiConfigured["baidu"] && conf.APISourceEnabled("baidu") {
			active["baidu"] = true
			if engineReady["baidu_web"] {
				active["baidu_web"] = true
			}
		} else if engineReady["baidu_web"] {
			active["baidu_web"] = true
		} else if engineReady["bing"] {
			active["bing"] = true
		}
	default:
		selected := mode
		if apiConfigured[selected] && conf.APISourceEnabled(selected) {
			active[selected] = true
		} else if engineReady["bing"] {
			active["bing"] = true
		}
	}
	// The MCP handler can retry Bing after a primary failure in every mode.
	active["bing"] = engineReady["bing"]

	baiduAPIAliases := []string{"baidu_ai", "baidu_api"}
	baiduWebAliases := []string{"baidu"}
	if mode == config.ModeApipool {
		baiduAPIAliases = append(baiduAPIAliases, "baidu")
		baiduWebAliases = []string{"baidu_web"}
	}
	out := []SourceView{
		{ID: "anysearch", Name: "AnySearch", Group: "web", Configured: apiConfigured["anysearch"], Active: active["anysearch"], aliases: []string{"anysearch"}},
		{ID: "baidu", Name: "百度千帆", Group: "web", Configured: apiConfigured["baidu"], Active: active["baidu"], aliases: baiduAPIAliases},
		{ID: "tavily", Name: "Tavily", Group: "web", Configured: apiConfigured["tavily"], Active: active["tavily"], QuotaQueryable: true, aliases: []string{"tavily", "tavily_api"}},
		{ID: "exa", Name: "Exa", Group: "web", Configured: apiConfigured["exa"], Active: active["exa"], aliases: []string{"exa"}},
		{ID: "doubao", Name: "豆包搜索", Group: "web", Configured: apiConfigured["doubao"], Active: active["doubao"], aliases: []string{"doubao"}},
		{ID: "tinyfish", Name: "TinyFish", Group: "web", Configured: apiConfigured["tinyfish"], Active: active["tinyfish"], QuotaQueryable: true, aliases: []string{"tinyfish", "tinyfish_api"}},
		{ID: "keenable", Name: "Keenable", Group: "web", Configured: apiConfigured["keenable"], Active: active["keenable"], aliases: []string{"keenable", "keenable_api"}},
		{ID: "baidu_web", Name: "百度网页", Group: "web", Configured: engineConfigured["baidu_web"], Active: active["baidu_web"], aliases: baiduWebAliases},
		{ID: "bing", Name: "Bing", Group: "web", Configured: engineConfigured["bing"], Active: active["bing"], aliases: []string{"bing"}},
		{ID: "google", Name: "Google", Group: "web", Configured: engineConfigured["google"], Active: active["google"], aliases: []string{"google"}},
		{ID: "duckduckgo", Name: "DuckDuckGo", Group: "web", Configured: engineConfigured["duckduckgo"], Active: active["duckduckgo"], aliases: []string{"duckduckgo"}},
		{ID: "so360", Name: "360 搜索", Group: "web", Configured: true, Active: active["so360"], aliases: []string{"so360"}},
		{ID: "wikipedia", Name: "Wikipedia", Group: "web", Configured: true, Active: active["wikipedia"], aliases: []string{"wikipedia"}},
		{ID: "googlenews", Name: "Google News", Group: "web", Configured: true, Active: active["googlenews"], aliases: []string{"googlenews", "google_news"}},
		{ID: "github", Name: "GitHub", Group: "web", Configured: true, Active: active["github"], aliases: []string{"github", "github_api"}},
	}
	for i := range out {
		s := &out[i]
		s.SuspensionScope = "observation"
		if _, engine := engineEnabled[s.ID]; engine {
			s.Category, s.Enabled = "engine", engineEnabled[s.ID]
			s.ControlPath = s.ID + ".enabled"
			s.Condition = "参与 engine/hybrid 模式"
			switch s.ID {
			case "baidu_web":
				s.ControlPath = "baidu.web_enabled"
				s.Condition = "参与 engine/hybrid；baidu/apipool 可作为网页回退"
			case "bing":
				s.Condition = "各模式的失败回退；engine/hybrid 同时参与主检索"
			case "googlenews":
				s.ControlPath = "google_news.enabled"
				fallthrough
			case "duckduckgo", "wikipedia":
				s.Condition = "参与 engine/hybrid；需要代理解析器；未探测连接"
				if !proxyResolved {
					s.Condition += "；代理端点当前未解析"
				}
			case "github":
				s.Condition = "参与 engine/hybrid；REST API 零 Key 匿名档（search 10/min），配 PAT 提额 30/min；直连可达无需代理；cleanfetch 仓库/blob 页复用同批 token"
			}
		} else {
			s.Category, s.Enabled = "api", conf.APISourceEnabled(s.ID)
			s.ControlPath = s.ID + ".enabled"
			s.Condition = "需要已配置 Key；参与对应单源/hybrid 或 apipool.engines"
			if s.ID == "anysearch" {
				s.Condition = "无 Key 可用匿名档；参与 anysearch/hybrid 或 apipool.engines"
			}
			if s.ID == "tinyfish" {
				s.Condition = "需要已配置 Key；参与 hybrid 或 apipool.engines"
			}
			if s.ID == "keenable" {
				s.Condition = "有 Key 走 keyed 档；无 Key 需显式 enabled: true 或加入 apipool.engines（public 免 Key 档按 IP 限流，默认不自动参与 hybrid）"
			}
			if s.ID == "doubao" {
				if conf.Doubao.GetVersion() == "custom" {
					s.Condition += "；Custom；ICP 限制不适用"
				} else if conf.Doubao.ICPHostOnly {
					s.Condition += "；Global；仅国内 ICP 备案站点"
				} else {
					s.Condition += "；Global；不限制 ICP 备案"
				}
			}
		}
	}
	return out
}

func academicSourceCatalog(conf config.Config) []SourceView {
	enabled := conf.Academic.Enabled
	proxyAllowed := conf.Proxy.ProxyResolver() != nil
	proxyResolved := conf.Proxy.GetProxyEndpoint() != ""
	defs := []struct {
		id       string
		name     string
		disabled bool
		intlOnly bool
	}{
		{"arxiv", "arXiv", conf.Academic.DisableArxiv, false},
		{"crossref", "Crossref", conf.Academic.DisableCrossref, false},
		{"openalex", "OpenAlex", conf.Academic.DisableOpenAlex, false},
		{"pubmed", "PubMed", conf.Academic.DisablePubMed, false},
		{"europepmc", "Europe PMC", conf.Academic.DisableEuropePMC, false},
		{"dblp", "DBLP", conf.Academic.DisableDBLP, false},
		{"doaj", "DOAJ", conf.Academic.DisableDOAJ, false},
		{"semantic_scholar", "Semantic Scholar", conf.Academic.DisableSemanticScholar, true},
		{"google_scholar", "Google Scholar", conf.Academic.DisableGoogleScholar, true},
	}
	out := make([]SourceView, 0, len(defs))
	for _, d := range defs {
		active := enabled && !d.disabled && (!d.intlOnly || (conf.IsInternational() && proxyAllowed))
		condition := "需要 academic.enabled；无需 Key"
		if d.intlOnly {
			condition += "；需要 international 网络与代理解析器；未探测连接"
			if !proxyResolved {
				condition += "；代理端点当前未解析"
			}
		}
		out = append(out, SourceView{ID: d.id, Name: d.name, Group: "academic", Category: "academic",
			Configured: true, Enabled: !d.disabled, Active: active, ControlPath: "academic.disable_" + d.id,
			Inverted: true, Condition: condition, SuspensionScope: "observation", aliases: []string{d.id}})
	}
	return out
}

// SourceCatalog returns configuration/routing conditions without health probes.
func SourceCatalog(conf config.Config) []SourceView {
	return append(webSourceCatalog(conf), academicSourceCatalog(conf)...)
}

func activeSourceIDs(sources []SourceView) []string {
	var out []string
	for _, source := range sources {
		if source.Active {
			out = append(out, source.ID)
		}
	}
	slices.Sort(out)
	return out
}

// brandView 返回品牌呈现视图。logo 为 http(s) URL 时原样返回；本地文件路径
// 统一经 /__admin/api/brand/logo 端点提供（与控制台同一访问边界）。
func brandView(conf config.Config) map[string]any {
	logo := ""
	if l := strings.TrimSpace(conf.Dashboard.Brand.Logo); l != "" {
		if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			logo = l
		} else {
			logo = "/__admin/api/brand/logo"
		}
	}
	return map[string]any{
		"title":  conf.Dashboard.Brand.GetTitle(),
		"theme":  conf.Dashboard.Brand.GetTheme(),
		"accent": conf.Dashboard.Brand.GetAccent(),
		"logo":   logo,
		"footer": conf.Dashboard.Brand.GetFooter(),
	}
}
