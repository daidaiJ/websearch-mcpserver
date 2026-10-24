package dashboard

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"
	"websearch/pkg/search/mode"
	"websearch/pkg/search/provider"
	"websearch/pkg/telemetry"
)

// sourceByID 返回目录中的指定来源，缺失时直接失败。
func sourceByID(t *testing.T, sources []SourceView, id string) SourceView {
	t.Helper()
	for _, s := range sources {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("来源 %q 不在目录中", id)
	return SourceView{}
}

func idsWhere(sources []SourceView, keep func(SourceView) bool) []string {
	var out []string
	for _, s := range sources {
		if keep(s) {
			out = append(out, s.ID)
		}
	}
	slices.Sort(out)
	return out
}

// fullWebConfig 给出所有 Web 来源都有配置的基线配置。
func fullWebConfig() config.Config {
	return config.Config{
		Anysearch:  config.AnysearchConfig{APIKey: "anysearch-key"},
		Baidu:      config.BaiduConfig{APIKey: "baidu-key", WebEnabled: true},
		Tavily:     config.TavilyConfig{APIKey: "tavily-key"},
		Exa:        config.ExaConfig{APIKey: "exa-key"},
		Doubao:     config.DoubaoConfig{APIKey: "doubao-key"},
		Yuanbao:    config.YuanbaoConfig{APIKey: "yuanbao-key"},
		Bing:       config.BingConfig{Enabled: true},
		Google:     config.GoogleConfig{Enabled: true},
		DuckDuckGo: config.DuckDuckGoConfig{Enabled: true},
	}
}

func TestOverviewApipoolProviderNamesMatchRouting(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name       string
		engine     string
		key        string
		enabled    *bool
		wantActive bool
	}{
		{name: "mixed case and spaces", engine: " TaViLy ", key: "test-key", wantActive: true},
		{name: "canonical name", engine: "tavily", key: "test-key", wantActive: true},
		{name: "disabled with key", engine: " TaViLy ", key: "test-key", enabled: &off},
		{name: "missing key", engine: " TaViLy "},
		{name: "unknown provider", engine: " unknown ", key: "test-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := config.Config{
				Mode:    config.ModeApipool,
				Apipool: config.ApipoolConfig{Engines: []string{tc.engine}},
				Tavily:  config.TavilyConfig{APIKey: tc.key, Enabled: tc.enabled},
			}
			var pool *provider.KeyPool
			if tc.key != "" {
				var err error
				pool, err = provider.NewKeyPool([]string{tc.key})
				if err != nil {
					t.Fatal(err)
				}
			}
			route := mode.BuildApipoolMode(conf, nil, nil, pool, nil, nil, nil, nil)
			if (route != nil) != tc.wantActive {
				t.Fatalf("actual apipool route exists = %v, want %v", route != nil, tc.wantActive)
			}
			handler, _ := newTestHandler(t)
			mux := http.NewServeMux()
			New(conf, handler.store, nil, nil).Register(mux, func(fn http.HandlerFunc) http.HandlerFunc { return fn })
			var out dashboardOverview
			if err := json.Unmarshal(getJSON(t, mux, "/__admin/api/overview"), &out); err != nil {
				t.Fatal(err)
			}
			if source := sourceByID(t, out.Sources, "tavily"); source.Active != (route != nil) ||
				source.Configured != (tc.key != "") || source.Enabled != (tc.enabled == nil) {
				t.Fatalf("overview Tavily differs from routing or configuration: %+v; route exists = %v", source, route != nil)
			}
		})
	}
}

func TestWebSourceCatalogConfiguredAndActivePerMode(t *testing.T) {
	cases := []struct {
		name             string
		configure        func(*config.Config)
		wantActive       []string
		wantUnconfigured []string
	}{
		{
			name:      "hybrid 全部有 Key 的来源与引擎",
			configure: func(c *config.Config) { c.Mode = config.ModeHybrid },
			wantActive: []string{
				"anysearch", "baidu", "baidu_web", "bing", "doubao",
				"duckduckgo", "exa", "google", "tavily", "yuanbao",
			},
		},
		{
			name:       "engine 只用网页引擎",
			configure:  func(c *config.Config) { c.Mode = config.ModeEngine },
			wantActive: []string{"baidu_web", "bing", "duckduckgo", "google"},
		},
		{
			name:       "baidu 千帆加网页回退",
			configure:  func(c *config.Config) { c.Mode = config.ModeBaidu },
			wantActive: []string{"baidu", "baidu_web", "bing"},
		},
		{
			name: "baidu 无 Key 时只剩网页引擎",
			configure: func(c *config.Config) {
				c.Mode = config.ModeBaidu
				c.Baidu = config.BaiduConfig{WebEnabled: true}
			},
			wantActive:       []string{"baidu_web", "bing"},
			wantUnconfigured: []string{"baidu"},
		},
		{
			name: "baidu 无 Key 无网页时回退 Bing",
			configure: func(c *config.Config) {
				c.Mode = config.ModeBaidu
				c.Baidu = config.BaiduConfig{}
			},
			wantActive:       []string{"bing"},
			wantUnconfigured: []string{"baidu"},
		},
		{
			name: "apipool 只激活池内供应商加网页兜底",
			configure: func(c *config.Config) {
				c.Mode = config.ModeApipool
				c.Apipool = config.ApipoolConfig{Engines: []string{"tavily", "doubao"}}
			},
			wantActive: []string{"baidu_web", "bing", "doubao", "tavily"},
		},
		{
			name: "tavily 单引擎模式",
			configure: func(c *config.Config) {
				c.Mode = config.ModeTavily
			},
			wantActive: []string{"bing", "tavily"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf := fullWebConfig()
			tc.configure(&conf)
			sources := webSourceCatalog(conf)

			if got := idsWhere(sources, func(s SourceView) bool { return s.Active }); !slices.Equal(got, tc.wantActive) {
				t.Fatalf("active = %v, want %v", got, tc.wantActive)
			}

			// 未配置只表示缺 Key：其它有 Key 的来源即使当前模式不参与，也只应显示为未启用。
			if got := idsWhere(sources, func(s SourceView) bool { return !s.Configured }); !slices.Equal(got, tc.wantUnconfigured) {
				t.Fatalf("configured=false 的来源 = %v, want %v", got, tc.wantUnconfigured)
			}
			for _, s := range sources {
				if s.Status != "" {
					t.Errorf("%s 在目录阶段不应带状态 %q", s.ID, s.Status)
				}
			}

			// Tavily 是唯一支持官方额度查询的来源。
			for _, s := range sources {
				if s.QuotaQueryable && s.ID != "tavily" {
					t.Errorf("%s 不应标记 quota_queryable", s.ID)
				}
			}
			if !sourceByID(t, sources, "tavily").QuotaQueryable {
				t.Error("tavily 应标记 quota_queryable")
			}
		})
	}
}

// 百度 API 与百度网页在 telemetry 里使用同一个 "baidu" 名字，但含义随模式变化：
// apipool 下 "baidu" 是千帆 API，其余模式是百度网页引擎；别名错配会把历史挂在错误的来源上。
func TestWebSourceCatalogBaiduAliasesFollowMode(t *testing.T) {
	apipool := webSourceCatalog(config.Config{
		Mode:  config.ModeApipool,
		Baidu: config.BaiduConfig{APIKey: "k", WebEnabled: true},
	})
	api := sourceByID(t, apipool, "baidu")
	if !slices.Contains(api.aliases, "baidu") {
		t.Errorf("apipool 的百度 API 别名 = %v, 应包含 baidu", api.aliases)
	}
	if slices.Contains(api.aliases, "baidu_web") {
		t.Errorf("apipool 的百度 API 别名 = %v, 不应包含 baidu_web", api.aliases)
	}
	if web := sourceByID(t, apipool, "baidu_web"); !slices.Equal(web.aliases, []string{"baidu_web"}) {
		t.Errorf("apipool 的百度网页别名 = %v, want [baidu_web]", web.aliases)
	}

	for _, mode := range []string{config.ModeHybrid, config.ModeEngine, config.ModeBaidu} {
		sources := webSourceCatalog(config.Config{
			Mode:  mode,
			Baidu: config.BaiduConfig{APIKey: "k", WebEnabled: true},
		})
		api := sourceByID(t, sources, "baidu")
		if !slices.Equal(api.aliases, []string{"baidu_ai", "baidu_api"}) {
			t.Errorf("%s 模式的百度 API 别名 = %v, want [baidu_ai baidu_api]", mode, api.aliases)
		}
		if web := sourceByID(t, sources, "baidu_web"); !slices.Equal(web.aliases, []string{"baidu"}) {
			t.Errorf("%s 模式的百度网页别名 = %v, want [baidu]", mode, web.aliases)
		}
	}
}

func TestJoinSourcesStatusRules(t *testing.T) {
	defs := []SourceView{
		{ID: "down_one", Configured: true, Active: true, aliases: []string{"down_one"}},
		{ID: "aliased", Configured: true, Active: true, aliases: []string{"tavily_api", "tavily"}},
		{ID: "no_history", Configured: true, Active: true, aliases: []string{"no_history"}},
		{ID: "inactive", Configured: true, Active: false, aliases: []string{"inactive"}},
		{ID: "unconfigured", Configured: false, Active: false, aliases: []string{"unconfigured"}},
	}
	observed := []telemetry.Health{
		{
			Name: "down_one", Status: "down", FailureRate: 0.5, SampleSize: 4,
			ConsecutiveFailures: 3, RecentOutcomes: []bool{false, true}, LastError: "boom",
		},
		{Name: "tavily_api", Status: "healthy", SampleSize: 1},
		{Name: "inactive", Status: "down", SampleSize: 5},
	}

	out := joinSources(defs, observed)

	down := sourceByID(t, out, "down_one")
	if down.Status != "down" || down.FailureRate != 0.5 || down.SampleSize != 4 ||
		down.ConsecutiveFailures != 3 || down.LastError != "boom" ||
		!slices.Equal(down.RecentOutcomes, []bool{false, true}) {
		t.Errorf("down_one 未原样带出监控数据: %+v", down)
	}

	if got := sourceByID(t, out, "aliased"); got.Status != "healthy" {
		t.Errorf("aliased 未按别名匹配历史: status = %q", got.Status)
	}
	if got := sourceByID(t, out, "no_history"); got.Status != "unknown" || got.SampleSize != 0 {
		t.Errorf("已启用但无历史应为 unknown: %+v", got)
	}
	if got := sourceByID(t, out, "inactive"); got.Status != "inactive" || got.SampleSize != 5 || got.ObservationStatus != "down" {
		t.Errorf("未启用来源保留历史但不参与路由: %+v", got)
	}
	if got := sourceByID(t, out, "unconfigured"); got.Status != "not_configured" {
		t.Errorf("未配置的来源状态 = %q, want not_configured", got.Status)
	}
}

func TestSummarizeSystemRollup(t *testing.T) {
	cases := []struct {
		name    string
		sources []SourceView
		want    SystemSummary
	}{
		{
			name: "未配置与未启用都不计入异常",
			sources: []SourceView{
				{Status: "not_configured"},
				{Configured: true, Active: false, Status: "inactive"},
				{Configured: true, Active: false, Status: "down"},
			},
			want: SystemSummary{Status: "waiting"},
		},
		{
			name:    "已启用但无调用记录为 waiting",
			sources: []SourceView{{Configured: true, Active: true, Status: "unknown"}},
			want:    SystemSummary{Status: "waiting", Enabled: 1},
		},
		{
			name:    "全部正常",
			sources: []SourceView{{Active: true, Status: "healthy"}, {Active: true, Status: "healthy"}},
			want:    SystemSummary{Status: "healthy", Enabled: 2, Observed: 2},
		},
		{
			name: "不稳定优先于未知",
			sources: []SourceView{
				{Active: true, Status: "degraded"},
				{Active: true, Status: "unknown"},
			},
			want: SystemSummary{Status: "degraded", Enabled: 2, Observed: 1, Degraded: 1},
		},
		{
			name: "异常优先于不稳定",
			sources: []SourceView{
				{Active: true, Status: "healthy"},
				{Active: true, Status: "degraded"},
				{Active: true, Status: "down"},
			},
			want: SystemSummary{Status: "down", Enabled: 3, Observed: 3, Degraded: 1, Down: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarizeSystem(tc.sources); got != tc.want {
				t.Fatalf("summarizeSystem = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBuildDashboardOverviewSortsSourcesByState(t *testing.T) {
	conf := fullWebConfig()
	conf.Mode = config.ModeEngine

	out := buildDashboardOverview(conf, telemetry.Overview{})

	var got []string
	for _, s := range out.Sources {
		got = append(got, s.ID)
	}
	// 已启用在前（保持目录顺序），已配置未启用居中，未配置最后。
	want := []string{"baidu_web", "bing", "google", "duckduckgo", "anysearch", "baidu", "tavily", "exa", "doubao", "yuanbao", "so360", "wikipedia", "googlenews"}
	if !slices.Equal(got, want) {
		t.Fatalf("sources 顺序 = %v, want %v", got, want)
	}

	// 学术表同样分层：国内网络下国际学术源（configured 未 active）排在最后。
	conf.Academic = config.AcademicConfig{Enabled: true}
	conf.Network = "china"
	acadOut := buildDashboardOverview(conf, telemetry.Overview{})
	var acad []string
	for _, s := range acadOut.AcademicSources {
		acad = append(acad, s.ID)
	}
	wantAcad := []string{"arxiv", "crossref", "openalex", "pubmed", "europepmc", "dblp", "doaj", "semantic_scholar", "google_scholar"}
	if !slices.Equal(acad, wantAcad) {
		t.Fatalf("academic_sources 顺序 = %v, want %v", acad, wantAcad)
	}
}

func TestBuildDashboardOverviewKeepsAcademicSourcesSeparate(t *testing.T) {
	conf := config.Config{
		Mode:     config.ModeEngine,
		Bing:     config.BingConfig{Enabled: true},
		Network:  "china",
		Academic: config.AcademicConfig{Enabled: true},
	}
	observed := telemetry.Overview{Providers: []telemetry.Health{
		{Name: "bing", Status: "healthy", SampleSize: 2},
		{Name: "arxiv", Status: "down", SampleSize: 3, ConsecutiveFailures: 3},
	}}

	out := buildDashboardOverview(conf, observed)

	if got := idsWhere(out.Sources, func(s SourceView) bool { return s.Active }); !slices.Equal(got, []string{"bing"}) {
		t.Errorf("主表 active = %v, want [bing]", got)
	}
	for _, s := range out.Sources {
		if s.Group != "web" {
			t.Errorf("主表出现非 Web 来源: %s", s.ID)
		}
	}
	academic := idsWhere(out.AcademicSources, func(s SourceView) bool { return s.Active })
	if !slices.Contains(academic, "arxiv") || slices.Contains(academic, "semantic_scholar") ||
		slices.Contains(academic, "google_scholar") {
		t.Errorf("国内网络下学术来源 active = %v, 应含 arxiv 且不含国际来源", academic)
	}

	// 学术来源的异常不进入 Web 汇总。
	if want := (SystemSummary{Status: "healthy", Enabled: 1, Observed: 1}); out.System != want {
		t.Errorf("system = %+v, want %+v", out.System, want)
	}

	// JSON 契约：主表 + 独立学术表，同时保留 telemetry 概览字段。
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal overview: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	for _, key := range []string{"system", "sources", "academic_sources", "providers", "trend"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("overview JSON 缺少字段 %q", key)
		}
	}
}

func TestSourceCatalogSeparatesSwitchConfigurationAndRouting(t *testing.T) {
	off := false
	conf := fullWebConfig()
	conf.Mode = config.ModeHybrid
	conf.Tavily.Enabled = &off
	conf.Anysearch.APIKey = ""
	conf.So360.Enabled, conf.Wikipedia.Enabled, conf.GoogleNews.Enabled = true, true, true
	conf.Proxy = config.ProxyConfig{Enabled: true, Endpoint: "http://127.0.0.1:1"}
	conf.Academic = config.AcademicConfig{Enabled: true, DisableArxiv: true}
	catalog := SourceCatalog(conf)
	if len(catalog) != 22 {
		t.Fatalf("catalog must include 13 web and 9 academic sources: %d", len(catalog))
	}
	if s := sourceByID(t, catalog, "tavily"); s.Enabled || !s.Configured || s.Active || s.Category != "api" || s.ControlPath != "tavily.enabled" {
		t.Fatalf("stored key must not override disabled source: %+v", s)
	}
	if s := sourceByID(t, catalog, "anysearch"); !s.Configured || !s.Enabled || !s.Active || !strings.Contains(s.Condition, "匿名") {
		t.Fatalf("anonymous AnySearch must remain available without a key: %+v", s)
	}
	for _, id := range []string{"so360", "wikipedia", "googlenews"} {
		if s := sourceByID(t, catalog, id); !s.Enabled || !s.Active || s.Category != "engine" || s.Group != "web" {
			t.Fatalf("new engine missing from route: %+v", s)
		}
	}
	if s := sourceByID(t, catalog, "arxiv"); s.Enabled || s.Active || !s.Configured || !s.Inverted || s.ControlPath != "academic.disable_arxiv" {
		t.Fatalf("academic inverse switch must be separate from configuration: %+v", s)
	}
	conf.Mode = config.ModeEngine
	if s := sourceByID(t, SourceCatalog(conf), "anysearch"); s.Active || !s.Enabled || !s.Configured {
		t.Fatalf("mode exclusion must not erase configuration: %+v", s)
	}
}

func TestJoinSourcesSeparatesActualCooldownFromObservation(t *testing.T) {
	antirobot.SetHealthStore("")
	t.Cleanup(func() { antirobot.SetHealthStore("") })
	antirobot.EnterCooldown("duckduckgo", "rate_limit", "HTTP 429", time.Now().Add(time.Minute), time.Minute)
	defs := []SourceView{{ID: "duckduckgo", Configured: true, Active: false, aliases: []string{"duckduckgo"}}}
	observed := []telemetry.Health{{Name: "duckduckgo", Status: "down", State: "suspended", SampleSize: 4,
		SuspendedUntil: time.Now().Add(2 * time.Minute).Format(time.RFC3339), SuspendReason: "passive warning"}}
	s := joinSources(defs, observed)[0]
	if s.Active || s.Status != "inactive" || s.ObservationStatus != "down" || s.SampleSize != 4 || s.SuspensionScope != "observation" {
		t.Fatalf("disabled history/observation warning incorrectly changes routing: %+v", s)
	}
	if s.ActualCooldown == nil || s.ActualCooldown.Engine != "duckduckgo" || s.ActualCooldown.Reason != "HTTP 429" || s.ActualCooldown.RemainingSeconds < 1 || s.ActualCooldown.PersistEnabled {
		t.Fatalf("actual cooldown must come from antirobot: %+v", s.ActualCooldown)
	}
	antirobot.ClearCooldown("duckduckgo")
	if after := joinSources(defs, observed)[0]; after.ActualCooldown != nil || !after.Suspended {
		t.Fatalf("telemetry warning must not become an actual cooldown: %+v", after)
	}
}

func TestDoubaoSourceConditionSeparatesVersionAndSiteScope(t *testing.T) {
	conf := fullWebConfig()
	for _, tc := range []struct {
		version string
		icp     bool
		want    string
	}{
		{"global", false, "不限制 ICP 备案"},
		{"global", true, "仅国内 ICP 备案站点"},
		{"custom", true, "ICP 限制不适用"},
	} {
		conf.Doubao.Version, conf.Doubao.ICPHostOnly = tc.version, tc.icp
		if s := sourceByID(t, SourceCatalog(conf), "doubao"); !strings.Contains(s.Condition, tc.want) {
			t.Fatalf("version %s filter %v: %s", tc.version, tc.icp, s.Condition)
		}
	}
}
