package dashboard

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"websearch/pkg/config"
	"websearch/pkg/history"
	"websearch/pkg/i18n"
	"websearch/pkg/telemetry"
)

type settingField struct {
	value   any
	check   func(any) bool
	options []string
	cache   bool
}

var (
	apiPoolOptions   = []string{"anysearch", "baidu", "tavily", "exa", "doubao", "yuanbao", "tinyfish", "keenable", "mcphub"}
	hubEngineOptions = []string{"baidu_web", "bing", "google", "duckduckgo", "so360", "sogou", "mojeek", "wikipedia", "googlenews", "github"}
	scoreEngineNames = []string{"anysearch", "baidu", "baidu_ai", "baidu_api", "baidu_web", "bing", "google", "duckduckgo", "so360", "sogou", "mojeek", "wikipedia", "googlenews", "github", "tavily", "tavily_api", "exa", "doubao", "yuanbao", "tinyfish", "tinyfish_api", "keenable", "keenable_api"}
	namePattern      = regexp.MustCompile("^[a-z][a-z0-9_-]{0,63}$")
	languagePattern  = regexp.MustCompile("^[a-z][a-z0-9]{0,14}(-[a-z0-9]{1,15}){0,3}$")
)

func settingFields(c config.Config) map[string]settingField {
	fields := map[string]settingField{}
	add := func(path string, value any, check func(any) bool, cache bool) {
		fields[path] = settingField{value: canonicalValue(value), check: check, cache: cache}
	}
	enum := func(path, value string, cache bool, options ...string) {
		add(path, value, func(v any) bool {
			s, ok := v.(string)
			return ok && slices.Contains(options, s)
		}, cache)
		field := fields[path]
		field.options = options
		fields[path] = field
	}
	list := func(path string, value []string, allowEmpty bool, options []string) {
		add(path, value, stringsList(allowEmpty, func(s string) bool { return slices.Contains(options, s) }), true)
		field := fields[path]
		field.options = options
		fields[path] = field
	}
	checkBool := func(value any) bool { _, ok := value.(bool); return ok }
	optionalBool := func(value any) bool { return value == nil || checkBool(value) }
	boolField := func(path string, value, cache bool) { add(path, value, checkBool, cache) }
	integer := func(path string, value, min, max int, cache bool) {
		add(path, value, number(float64(min), float64(max), true), cache)
	}
	ratio := func(path string, value float64, cache bool) {
		add(path, value, number(0, 1, false), cache)
	}

	enum("mode", c.GetMode(), true, "hybrid", "engine", "apipool", "mcphub", "baidu", "tavily", "exa", "anysearch", "doubao", "yuanbao")
	network := "china"
	if c.IsInternational() {
		network = "international"
	}
	enum("network", network, true, "china", "international")
	enum("language", i18n.Normalize(c.Language), false, "auto", "zh", "en")
	logLevel := strings.ToLower(c.LogLevel)
	logLevels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled"}
	if !slices.Contains(logLevels, logLevel) {
		logLevel = "info"
	}
	enum("log_level", logLevel, false, logLevels...)
	integer("upstream_timeout_sec", c.UpstreamTimeoutSec, 0, 360, true)
	boolField("cache.enabled", c.CacheEnabled(), false)
	integer("cache.cleanup_interval", int(c.GetCleanupInterval()/time.Minute), 0, 360, false)
	integer("rate_limit.per_sec", c.GetRateLimitPerSec(), 1, 100000, true)
	integer("rate_limit.per_min", c.GetRateLimitPerMin(), 1, 1000000, true)
	add("black_list_host", c.BlackListHost, stringsList(true, validSettingHost), true)
	var enabled any
	if p := c.Proxy.EnabledSetting(); p != nil {
		enabled = *p
	}
	add("proxy.enabled", enabled, optionalBool, true)
	add("proxy.endpoint", visibleEndpoint(c.Proxy.Endpoint, true), endpointValue(true), true)
	boolField("proxy.api_providers", c.Proxy.APIProviders, true)

	enum("apipool.strategy", c.Apipool.GetStrategy(), true, "round-robin", "priority", "weighted")
	list("apipool.engines", c.Apipool.GetEngines(), false, apiPoolOptions)
	add("apipool.weights", c.Apipool.Weights, settingsMap(
		func(name string) bool { return slices.Contains(apiPoolOptions, name) || name == "baidu_web" },
		number(0, 1000000, true)), true)
	hub := c.MCPHub
	hub.NormalizeMCPHub()
	enum("mcp_hub.strategy", hub.Strategy, true, "priority", "round-robin")
	integer("mcp_hub.timeout_sec", hub.TimeoutSec, 1, 3600, true)
	integer("mcp_hub.fetch_timeout_sec", hub.FetchTimeoutSec, 1, 3600, true)
	integer("mcp_hub.cooldown_sec", hub.CooldownSec, 1, 86400, true)
	list("mcp_hub.rotation_engines", hub.GetRotationEngines(), true, hubEngineOptions)
	list("mcp_hub.fallback_engines", hub.GetFallbackEngines(), true, hubEngineOptions)
	enum("mcp_hub.fetch_mode", hub.FetchMode, true, "off", "fallback", "prefer")

	smart := c.SmartSearch
	integer("smartsearch.max_size", smart.MaxSize, 0, 360, true)
	integer("smartsearch.fetch_top_n", smart.FetchTopN, 0, 5, false)
	boolField("smartsearch.enhance", smart.Enhance == nil || *smart.Enhance, true)
	boolField("smartsearch.show_meta", smart.ShowMeta, false)
	enum("smartsearch.off_topic_guard", smart.OffTopicGuardMode(), true, "shadow", "enforce", "off")
	integer("smartsearch.engine_timeout_sec", max(-1, smart.EngineTimeoutSec), -1, 3600, true)
	integer("smartsearch.inline_max_chars", max(-1, smart.InlineMaxChars), -1, 10000000, false)
	ratio("smartsearch.relevance_threshold", smart.RelevanceThreshold, true)
	boolField("smartsearch.mmr.enabled", smart.MMR.Enabled, true)
	ratio("smartsearch.mmr.lambda", smart.MMR.Lambda, true)
	integer("smartsearch.mmr.target_count", smart.MMR.TargetCount, 0, 360, true)
	for _, name := range scoreEngineNames {
		layer := smart.Engines[name]
		prefix := "smartsearch.engines." + name + "."
		add(prefix+"weight", layer.Weight, number(0, 1000, false), true)
		integer(prefix+"max_size", layer.MaxSize, 0, 360, true)
		ratio(prefix+"min_score", layer.MinScore, true)
	}

	fetch := c.CleanFetch
	boolField("cleanfetch.enabled", fetch.Enabled, false)
	boolField("cleanfetch.auto_proxy", fetch.AutoProxyEnabled(), true)
	integer("cleanfetch.timeout_sec", fetch.TimeoutSec, 1, 3600, true)
	integer("cleanfetch.max_fetch_size_mb", fetch.MaxFetchSizeMB, 1, 2048, true)
	integer("cleanfetch.max_inline_lines", fetch.MaxInlineLines, 1, 100000, false)
	integer("cleanfetch.max_inline_chars", fetch.MaxInlineChars, 0, 10000000, false)
	integer("cleanfetch.file_ttl_hours", fetch.FileTTL, 1, 8760, false)
	integer("cleanfetch.max_retries", fetch.MaxRetries, 1, 10, true)
	pdf := c.PDFParser
	boolField("pdf_parser.enabled", pdf.Enabled, false)
	enum("pdf_parser.mineru_model", pdf.GetMinerUModel(), false, "pipeline", "vlm")
	boolField("pdf_parser.mineru_ocr", pdf.MinerUOcr, false)
	boolField("pdf_parser.mineru_formula", pdf.GetMinerUFormula(), false)
	boolField("pdf_parser.mineru_table", pdf.GetMinerUTable(), false)
	add("pdf_parser.mineru_lang", pdf.GetMinerULang(), stringValue(namePattern.MatchString), false)
	boolField("pdf_parser.mineru_remote_pdf", pdf.MinerURemotePDF, false)
	integer("pdf_parser.max_pages", pdf.GetMaxPages(), 1, 10000, false)
	integer("pdf_parser.mineru_page_limit", pdf.GetMinerUPageLimit(), 1, 10000, false)
	add("pdf_parser.mineru_page_batch_size", pdf.GetMinerUPageBatchSize(), func(value any) bool {
		v, ok := asFloat(value)
		return ok && v == 0 || number(10, 200, true)(value)
	}, false)
	integer("pdf_parser.mineru_page_budget", pdf.MinerUPageBudget, 0, 100000, false)
	boolField("academic.enabled", c.Academic.Enabled, true)
	boolField("academic.enhance", c.Academic.Enhance, true)
	add("academic.threshold", c.Academic.Threshold, positiveRatio, true)
	boolField("search_history.enabled", c.SearchHistoryEnabled(), false)
	integer("search_history.retention_days", positiveOr(c.SearchHistory.RetentionDays, history.DefaultRetentionDays), 1, 36500, false)
	add("llm.base_url", visibleEndpoint(c.LLM.BaseURL, false), endpointValue(false), true)
	add("llm.model_id", c.LLM.ModelId, stringValue(func(s string) bool { return safeText(s, 256) }), true)
	integer("llm.timeout_sec", int(c.LLM.GetTimeout()/time.Second), 1, 3600, false)
	integer("llm.summary_progress_interval", int(c.LLM.GetSummaryProgressInterval()/time.Second), 1, 3600, false)
	integer("everything.max_results", config.ClampMaxResults(c.Everything.MaxResults), 1, config.EverythingMaxResultsHardCap, false)
	integer("everything.timeout_sec", positiveOr(c.Everything.TimeoutSec, 5), 1, 3600, false)
	ratio("everything.min_alignment", c.Everything.MinAlignment, false)

	limit := c.Yuanbao.GetMonthlyLimit()
	integer("yuanbao.monthly_limit", max(0, limit), 0, 10000000, true)
	integer("yuanbao.mode", c.Yuanbao.Mode, 0, 2, true)
	enum("yuanbao.industry", strings.ToLower(strings.TrimSpace(c.Yuanbao.Industry)), true, "", "gov", "news", "acad", "finance")
	add("yuanbao.site", c.Yuanbao.Site, stringValue(validYuanbaoSite), true)
	enum("doubao.version", c.Doubao.GetVersion(), true, "global", "custom")
	boolField("doubao.icp_host_only", c.Doubao.ICPHostOnly, true)
	boolField("doubao.need_content", c.Doubao.NeedContent, true)
	integer("doubao.auth_level", c.Doubao.AuthLevel, 0, 1, true)
	boolField("doubao.query_rewrite", c.Doubao.QueryRewrite, true)
	integer("doubao.num_results", positiveOr(c.Doubao.NumResults, 10), 1, 50, true)
	boolField("baidu.enable_ai_search", c.Baidu.EnableAISearch, true)
	add("baidu.model", c.Baidu.Model, stringValue(func(s string) bool { return safeText(s, 256) }), true)
	enum("baidu.search_mode", defaultString(c.Baidu.SearchMode, "auto"), true, "auto", "required", "disabled")
	boolField("tavily.include_raw_content", c.Tavily.IncludeRawContent == nil || *c.Tavily.IncludeRawContent, true)
	boolField("exa.include_text", c.Exa.IncludeText == nil || *c.Exa.IncludeText, true)
	integer("exa.lookback_days", positiveOr(c.Exa.LookbackDays, 90), 1, 36500, true)
	integer("exa.num_results", positiveOr(c.Exa.NumResults, 5), 1, 100, true)
	boolField("keenable.live", c.Keenable.IsLive(), true)
	add("wikipedia.lang", defaultString(strings.ToLower(strings.TrimSpace(c.Wikipedia.Lang)), "zh"), stringValue(languagePattern.MatchString), true)
	enum("google_news.edition", defaultString(c.GoogleNews.Edition, "zh-CN"), true, "zh-CN", "zh-TW", "en-US", "en-GB", "ja-JP", "ko-KR")
	integer("so360.safe_search", c.So360.SafeSearch, 0, 2, true)

	// Source switches share the same route-facing representation as the source catalog.
	for _, name := range apiPoolOptions {
		if name != "mcphub" && name != "keenable" {
			boolField(name+".enabled", c.APISourceEnabled(name), true)
		}
	}
	// Without a key, the omitted switch uses mode and API-pool membership;
	// an explicit true separately permits the public tier in hybrid mode.
	var keenableEnabled any
	if c.Keenable.Enabled != nil || len(c.Keenable.EffectiveSKList()) != 0 {
		keenableEnabled = c.APISourceEnabled("keenable")
	}
	add("keenable.enabled", keenableEnabled, optionalBool, true)
	for name, value := range map[string]bool{
		"baidu.web_enabled": c.Baidu.WebEnabled, "bing.enabled": c.Bing.Enabled,
		"google.enabled": c.Google.Enabled, "duckduckgo.enabled": c.DuckDuckGo.Enabled,
		"so360.enabled": c.So360.Enabled, "sogou.enabled": c.Sogou.Enabled,
		"mojeek.enabled": c.Mojeek.Enabled, "wikipedia.enabled": c.Wikipedia.Enabled,
		"google_news.enabled": c.GoogleNews.Enabled, "github.enabled": c.GitHub.Enabled,
		"academic.disable_arxiv": c.Academic.DisableArxiv, "academic.disable_crossref": c.Academic.DisableCrossref,
		"academic.disable_openalex": c.Academic.DisableOpenAlex, "academic.disable_pubmed": c.Academic.DisablePubMed,
		"academic.disable_europepmc": c.Academic.DisableEuropePMC, "academic.disable_dblp": c.Academic.DisableDBLP,
		"academic.disable_doaj": c.Academic.DisableDOAJ, "academic.disable_semantic_scholar": c.Academic.DisableSemanticScholar,
		"academic.disable_google_scholar": c.Academic.DisableGoogleScholar,
	} {
		boolField(name, value, true)
	}

	dash := c.Dashboard
	enum("dashboard.quotas.reset", dash.Quotas.GetReset(), false, "monthly", "weekly", "daily", "none")
	integer("dashboard.quotas.reset_day", dash.Quotas.GetResetDay(), 1, 28, false)
	add("dashboard.quotas.limits", dash.Quotas.Limits, settingsMap(namePattern.MatchString, number(1, 1000000000, true)), false)
	add("dashboard.suspension.ban_time_on_fail", defaultString(dash.Suspension.BanTimeOnFail, "5s"), stringValue(validSettingDuration), false)
	add("dashboard.suspension.max_ban_time_on_fail", defaultString(dash.Suspension.MaxBanTimeOnFail, "24h"), stringValue(validSettingDuration), false)
	add("dashboard.suspension.suspended_times", dash.Suspension.SuspendedTimes, nil, false)
	add("dashboard.client_projects", dash.ClientProjects, settingsMap(
		func(s string) bool { return s != "" && s == strings.ToLower(s) && safeText(s, 128) },
		stringValue(func(s string) bool { return s != "" && telemetry.NormalizeProject(s) == s })), false)
	integer("dashboard.retention_days", positiveOr(dash.RetentionDays, 30), 1, 36500, false)
	add("dashboard.brand.title", strings.TrimSpace(dash.Brand.Title), stringValue(func(s string) bool { return safeText(s, 128) }), false)
	enum("dashboard.brand.theme", dash.Brand.GetTheme(), false, "green", "blue", "mono")
	add("dashboard.brand.accent", dash.Brand.GetAccent(), stringValue(func(s string) bool {
		return s == "" || (config.BrandConfig{Accent: s}).GetAccent() != ""
	}), false)
	boolField("dashboard.brand.footer", dash.Brand.GetFooter(), false)
	return fields
}

var fieldsContract = settingFields(config.Config{})

func isEditableSetting(path string) bool { return fieldsContract[path].check != nil }

func validateSetting(path string, value any, localizer ...*i18n.Localizer) error {
	loc := i18n.Default()
	if len(localizer) > 0 {
		loc = localizer[0]
	}
	field := fieldsContract[path]
	if field.check == nil {
		return fmt.Errorf("%s", loc.T("dashboard.validate.setting_not_editable", "Path", path))
	}
	if !field.check(value) {
		return fmt.Errorf("%s", loc.T("dashboard.validate.setting_value", "Path", path))
	}
	return nil
}

func validateSettingsCombination(c config.Config, changes map[string]any, loc *i18n.Localizer) error {
	_, versionChanged := changes["doubao.version"]
	_, countChanged := changes["doubao.num_results"]
	if (versionChanged || countChanged) && c.Doubao.GetVersion() == "global" && c.Doubao.NumResults > 20 {
		return fmt.Errorf("%s", loc.T("dashboard.validate.doubao_global_limit"))
	}
	_, banChanged := changes["dashboard.suspension.ban_time_on_fail"]
	_, maxBanChanged := changes["dashboard.suspension.max_ban_time_on_fail"]
	if banChanged || maxBanChanged {
		base, baseErr := parseSuspensionDuration(defaultString(c.Dashboard.Suspension.BanTimeOnFail, "5s"))
		upper, upperErr := parseSuspensionDuration(defaultString(c.Dashboard.Suspension.MaxBanTimeOnFail, "24h"))
		if baseErr != nil || upperErr != nil || base > upper {
			return fmt.Errorf("%s", loc.T("dashboard.validate.suspension_order"))
		}
	}
	return nil
}

func settingsInvalidateSearchCache(changes map[string]any) bool {
	for path := range changes {
		if fieldsContract[path].cache {
			return true
		}
	}
	return false
}

func lockedSettings(c config.Config, loc *i18n.Localizer) map[string]string {
	locked := map[string]string{}
	var envNames []string
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		if value != "" && strings.HasPrefix(key, "APP_") {
			envNames = append(envNames, key)
		}
	}
	for path, field := range fieldsContract {
		if field.check == nil {
			continue
		}
		for _, env := range envNames {
			key := "APP_" + strings.ToUpper(path)
			if env == key || strings.HasPrefix(key, env+".") || strings.HasPrefix(env, key+".") {
				locked[path] = loc.T("dashboard.locked.env")
				break
			}
		}
	}
	for path, env := range map[string]string{"language": "WEBCRAWLER_LANGUAGE", "llm.base_url": "LLM_BASE_URL"} {
		if os.Getenv(env) != "" {
			locked[path] = loc.T("dashboard.locked.env")
		}
	}
	for path, endpoint := range map[string]string{"llm.base_url": c.LLM.BaseURL, "proxy.endpoint": c.Proxy.Endpoint} {
		if endpoint != "" && visibleEndpoint(endpoint, path == "proxy.endpoint") == "" {
			locked[path] = loc.T("dashboard.locked.endpoint")
		}
	}
	return locked
}

func asFloat(value any) (float64, bool) {
	v, ok := value.(float64)
	return v, ok && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func number(min, max float64, whole bool) func(any) bool {
	return func(value any) bool {
		v, ok := asFloat(value)
		return ok && v >= min && v <= max && (!whole || math.Trunc(v) == v)
	}
}

func positiveRatio(value any) bool {
	v, ok := asFloat(value)
	return ok && v > 0 && v <= 1
}

func stringValue(check func(string) bool) func(any) bool {
	return func(value any) bool { s, ok := value.(string); return ok && check(s) }
}

func stringsList(allowEmpty bool, check func(string) bool) func(any) bool {
	return func(value any) bool {
		list, ok := value.([]any)
		if !ok || (!allowEmpty && len(list) == 0) || len(list) > 512 {
			return false
		}
		seen := make(map[string]bool, len(list))
		for _, raw := range list {
			item, ok := raw.(string)
			key := strings.ToLower(strings.TrimSuffix(item, "."))
			if !ok || !check(item) || seen[key] {
				return false
			}
			seen[key] = true
		}
		return true
	}
}

func settingsMap(checkKey func(string) bool, checkValue func(any) bool) func(any) bool {
	return func(value any) bool {
		values, ok := value.(map[string]any)
		if !ok || values == nil || len(values) > 512 {
			return false
		}
		for key, item := range values {
			if !checkKey(key) || !checkValue(item) {
				return false
			}
		}
		return true
	}
}

func safeText(s string, limit int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit &&
		s == strings.TrimSpace(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

func validSettingHost(raw string) bool {
	if raw == "" || !safeText(raw, 254) {
		return false
	}
	if net.ParseIP(raw) != nil {
		return true
	}
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(raw, "."))
	if err != nil || len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range strings.ToLower(label) {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func validYuanbaoSite(raw string) bool {
	if raw == "" {
		return true
	}
	if hosts, ok := strings.CutPrefix(raw, "exclude:"); ok {
		for _, host := range strings.Split(hosts, "|") {
			if !validSettingHost(host) {
				return false
			}
		}
		return len(hosts) <= 1024
	}
	return validSettingHost(raw)
}

func validSettingDuration(s string) bool {
	if !safeText(s, 32) {
		return false
	}
	ms, err := parseSuspensionDuration(s)
	return err == nil && ms > 0
}

func endpointValue(proxy bool) func(any) bool {
	return stringValue(func(s string) bool { return s == "" || visibleEndpoint(s, proxy) != "" })
}

func visibleEndpoint(s string, proxy bool) string {
	if s == "" || !safeText(s, 2048) || strings.ContainsAny(s, "?#") {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || !validSettingHost(u.Hostname()) {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
	case "socks5", "socks5h":
		if !proxy {
			return ""
		}
	default:
		return ""
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return ""
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return ""
	}
	if proxy && u.Path != "" && u.Path != "/" {
		return ""
	}
	// Credentials can also be embedded in a path segment.
	for _, segment := range strings.Split(u.Path, "/") {
		if strings.ContainsAny(segment, "=@") {
			return ""
		}
	}
	return s
}

func defaultString(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func positiveOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func canonicalValue(value any) any {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = canonicalValue(item)
		}
		return out
	case map[string]int:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = canonicalValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = canonicalValue(item)
		}
		return out
	default:
		return value
	}
}

func decodeSettingsDocuments(c config.Config, main, overlay []byte) (config.Config, error) {
	return decodeSettingsAtPaths(main, overlay, config.SettingsFilePaths{Secrets: c.GetDashboardSecretsPath()})
}

func decodeSettingsAtPaths(main, overlay []byte, paths config.SettingsFilePaths) (config.Config, error) {
	candidate, err := config.DecodeSettingsCandidate(main, overlay, paths)
	if err == config.ErrSettingsPathsChanged {
		return config.Config{}, err
	}
	if err != nil {
		// The YAML parser can include arbitrary file contents in its errors.
		return config.Config{}, errSettingsParse
	}
	return *candidate, nil
}

func projectSettingsDocuments(c config.Config, main, overlay []byte) (map[string]any, error) {
	candidate, err := decodeSettingsDocuments(c, main, overlay)
	if err != nil {
		return nil, err
	}
	return settingsView(candidate)["values"].(map[string]any), nil
}
