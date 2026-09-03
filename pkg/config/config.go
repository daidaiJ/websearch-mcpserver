package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websearch/pkg/proxy"

	"github.com/spf13/viper"
)

var configDir string

const (
	ModeBaidu   = "baidu"   // 百度千帆搜索（enable_ai_search 控制端点，失败自动回退网页搜索）
	ModeApipool = "apipool" // API Key 池轮转：百度 + Tavily + Exa 并发去重
	ModeTavily  = "tavily"
	ModeExa     = "exa"
	ModeHybrid  = "hybrid"
	ModeEngine  = "engine" // 纯引擎模式，无需 API Key
)

// ── 顶层配置 ──

type Config struct {
	Port               int               `mapstructure:"port"`
	Host               string            `mapstructure:"host"`                 // 监听地址，默认 127.0.0.1；"0.0.0.0" 才对所有网卡开放
	AuthToken          string            `mapstructure:"auth_token"`           // 业务端点 Bearer token，空 = 不鉴权；环境变量 WEBSEARCH_TOKEN
	MCPStateless       bool              `mapstructure:"mcp_stateless"`        // MCP 无状态 HTTP 模式：每个 POST 独立处理，无需 initialize 握手与 Mcp-Session-Id 会话（对齐 MCP 2026-07-28 stateless-first 方向），便于水平扩展；代价是 GET SSE 长连与 sampling/elicitation 等服务端主动交互不可用（本项目未使用，见 mcp/server.go RegisterRouter）
	UpstreamTimeoutSec int               `mapstructure:"upstream_timeout_sec"` // API 上游超时（秒），默认 30；显式 0 = 不设超时（有挂起风险）
	LogLevel           string            `mapstructure:"log_level"`
	Mode               string            `mapstructure:"mode"`
	Network            string            `mapstructure:"network"`         // 全局网络区域: china / international
	BlackListHost      []string          `mapstructure:"black_list_host"` // 全局屏蔽站点
	RateLimit          RateLimitConfig   `mapstructure:"rate_limit"`      // 全局限流配置
	Baidu              BaiduConfig       `mapstructure:"baidu"`
	Tavily             TavilyConfig      `mapstructure:"tavily"`
	Exa                ExaConfig         `mapstructure:"exa"`
	LLM                LLMConfig         `mapstructure:"llm"`
	Jina               JinaConfig        `mapstructure:"jina"`
	Cache              CacheConfig       `mapstructure:"cache"`
	Log                LogConfig         `mapstructure:"log"`
	Bing               BingConfig        `mapstructure:"bing"`
	DuckDuckGo         DuckDuckGoConfig  `mapstructure:"duckduckgo"`
	Google             GoogleConfig      `mapstructure:"google"`
	Academic           AcademicConfig    `mapstructure:"academic"`
	CleanFetch         CleanFetchConfig  `mapstructure:"cleanfetch"`
	PDFParser          PDFParserConfig   `mapstructure:"pdf_parser"`
	Proxy              ProxyConfig       `mapstructure:"proxy"`
	SmartSearch        SmartSearchConfig `mapstructure:"smartsearch"`
	Apipool            ApipoolConfig     `mapstructure:"apipool"`
}

// ── 各搜索引擎配置 ──

type BaiduConfig struct {
	// 反爬现状（2026-09-03 实测，勿照抄上游）：
	// SearXNG baidu.py 用 www.baidu.com/s?tn=json JSON 接口绕开 HTML 反爬，配合
	// 302→wappass 验证码探测与 antiFlag 检查，图片分类另做 image.baidu.com cookie
	// 预热（缓存 1h）。但该接口并非免检通道：本地直连实测（curl 裸客户端，无 TLS
	// 指纹伪装）tn=json 3/3 被 302 到 wappass 图形验证码，预热 cookie 后依然被拦；
	// 本项目 pkg/baidu 的 HTML 引擎测试（TestBaiduSearch）在同一 IP 下同样被
	// CAPTCHA。识别主因疑似 IP 信誉 + 客户端 TLS 指纹，与入口选择（HTML/JSON）
	// 关系不大——SearXNG 公共实例可用是因为出口 IP 干净，不代表裸客户端可复现。

	// WebEnabled 百度网页搜索引擎（tn=json 直抓）开关，默认 false：
	// 实测被百度 CAPTCHA 识别（见上方反爬现状），失效引擎默认禁用，
	// 出口 IP 干净的部署环境可显式置 true 启用。
	WebEnabled bool `mapstructure:"web_enabled"`

	APIKey string   `mapstructure:"api_key"` // 百度千帆 AI Search API Key（单 key 时自动作为 sk_list）
	SKList []string `mapstructure:"sk_list"` // 多 Key 轮询列表（优先级高于 api_key）
	// 搜索模式配置
	EnableAISearch   bool   `mapstructure:"enable_ai_search"`   // true=智能搜索 chat/completions（默认），false=网页搜索 web_search；不传 model 不产生 LLM 费用
	Model            string `mapstructure:"model"`              // 智能搜索模型名，不传时走免费百度搜索（不产生 LLM 费用），传入模型名启用 LLM 智能搜索
	SearchSource     string `mapstructure:"search_source"`      // 搜索引擎版本，默认 baidu_search_v2
	EnableReasoning  bool   `mapstructure:"enable_reasoning"`   // 深度思考（默认 false）
	EnableDeepSearch bool   `mapstructure:"enable_deep_search"` // 深搜索（默认 false）
	SearchMode       string `mapstructure:"search_mode"`        // 搜索模式: auto/required/disabled（默认 auto）
}

// EffectiveSKList 返回合并后的 Key 列表：sk_list 非空时直接返回，否则用 api_key 构造单元素列表。
func (c BaiduConfig) EffectiveSKList() []string {
	if len(c.SKList) > 0 {
		return c.SKList
	}
	if c.APIKey != "" {
		return []string{c.APIKey}
	}
	return nil
}

type TavilyConfig struct {
	APIKey string   `mapstructure:"api_key"` // Tavily Search API Key（单 key 时自动作为 sk_list）
	SKList []string `mapstructure:"sk_list"` // 多 Key 轮询列表（优先级高于 api_key）
}

// EffectiveSKList 返回合并后的 Key 列表。
func (c TavilyConfig) EffectiveSKList() []string {
	if len(c.SKList) > 0 {
		return c.SKList
	}
	if c.APIKey != "" {
		return []string{c.APIKey}
	}
	return nil
}

type ExaConfig struct {
	APIKey       string   `mapstructure:"api_key"`       // Exa Search API Key（单 key 时自动作为 sk_list）
	SKList       []string `mapstructure:"sk_list"`       // 多 Key 轮询列表（优先级高于 api_key）
	NumResults   int      `mapstructure:"num_results"`   // 单次搜索结果数量（默认 5）
	LookbackDays int      `mapstructure:"lookback_days"` // 搜索时间范围（天），默认 90
}

// EffectiveSKList 返回合并后的 Key 列表。
func (c ExaConfig) EffectiveSKList() []string {
	if len(c.SKList) > 0 {
		return c.SKList
	}
	if c.APIKey != "" {
		return []string{c.APIKey}
	}
	return nil
}

type BingConfig struct {
	Enabled bool     `mapstructure:"enabled"` // 总开关（默认 true）
	Blocked []string `mapstructure:"blocked"` // Bing 屏蔽域名
}

type DuckDuckGoConfig struct {
	Enabled bool     `mapstructure:"enabled"` // 总开关（默认 true，需代理）
	Blocked []string `mapstructure:"blocked"` // DuckDuckGo 屏蔽域名
}

// GoogleConfig Google 网页搜索配置。
//
// 反爬现状（2026-08 调研，勿再尝试伪装修复）：
// Google 自 2025-01-15 起灰度上线 JS 挑战（SearchGuard），2025 上半年灰度期间仍可
// 间歇直抓（当年 4-5 月尚可用），2025 下半年起全量硬化。挑战是"凭据缺失"模型：
// HTTP 200 返回 ~91KB 空壳页面、零结果、静默失败（无 4xx/5xx 错误码），没有真实
// 浏览器 JS 执行环境就没有结果。UA 伪装（含 Nokia 功能机 UA + gbv=1 旧端点）与
// TLS 指纹伪装已全部失效（参见 SearXNG #5651/#6570）。剩余可行路径只有无头浏览器
// + 住宅/移动代理、SERP API、或聚合其他引擎（本项目现有方案），因此保持默认关闭。
//
// SearXNG wml 方案实测（2026-09-03，勿照抄上游）：
// SearXNG PR #6546（2026-08-22）改用 Nokia Symbian UA 请求 /wml/search 遗留版式，
// 作者宣称 12h/36k 请求 100% 成功。本地实测（HK 代理出口，4 连发）：首请求即 429
// 被送入 /sorry/，其余返回 200 但为 JS 挑战空壳（"Please click here..." + 混淆 JS，
// 无任何 WML/XML 内容），即 Google 并未对 Nokia UA 免除挑战。说明该方案强依赖部署
// 环境的 IP 信誉（SearXNG 公共实例多为干净住宅/机构出口），并非普适绕过；且 wml 是
// Google 遗留端点，GSA UA（2026-07-03 失效）的先例表明随时可能被清理。维持默认关闭。
type GoogleConfig struct {
	Enabled bool     `mapstructure:"enabled"` // 总开关（默认 false，JS 挑战拦截无法伪装绕过，见上）
	Blocked []string `mapstructure:"blocked"` // Google 屏蔽域名
}

// RateLimitConfig 全局搜索引擎限流配置（对所有引擎统一生效）。
type RateLimitConfig struct {
	PerSec int `mapstructure:"per_sec"` // 每秒请求数上限（默认 3）
	PerMin int `mapstructure:"per_min"` // 每分钟请求数上限（默认 60）
}

// ── 学术引擎配置 ──

type AcademicConfig struct {
	Enabled      bool    `mapstructure:"enabled"`       // 学术引擎总开关（默认 true）
	BingFallback bool    `mapstructure:"bing_fallback"` // 学术搜索时用 Bing 兜底（默认 true）
	Enhance      bool    `mapstructure:"enhance"`       // 学术搜索评分增强（RRF 融合 + 引用数/期刊权威/PDF/新鲜度信号），默认 true
	Threshold    float64 `mapstructure:"threshold"`     // 学术结果阀值（比通用搜索更宽松），默认 0.02

	// Semantic Scholar 可选 API key（环境变量 SEMANTIC_SCHOLAR_API_KEY 可覆盖）。
	// 匿名配额限流严格，带 key 连续 429 时引擎自动降级为匿名模式。
	SemanticScholarAPIKey string `mapstructure:"semantic_scholar_api_key"`

	// 各引擎独立禁用（默认 false = 启用）
	DisableArxiv           bool `mapstructure:"disable_arxiv"`
	DisableCrossref        bool `mapstructure:"disable_crossref"`
	DisableOpenAlex        bool `mapstructure:"disable_openalex"`
	DisableSemanticScholar bool `mapstructure:"disable_semantic_scholar"`
	DisablePubMed          bool `mapstructure:"disable_pubmed"`
	DisableGoogleScholar   bool `mapstructure:"disable_google_scholar"`
	DisableEuropePMC       bool `mapstructure:"disable_europepmc"`
	DisableDBLP            bool `mapstructure:"disable_dblp"`
	DisableDOAJ            bool `mapstructure:"disable_doaj"`
}

// ── CleanFetch 配置 ──

type CleanFetchConfig struct {
	Enabled        bool   `mapstructure:"enabled"`           // 总开关（默认 false，旧配置不启用）
	FileOutputDir  string `mapstructure:"file_output_dir"`   // 大文本文件输出目录（默认 os.TempDir()/webfetch/）
	FileTTL        int    `mapstructure:"file_ttl_hours"`    // 文件保留时长（小时），默认 24
	MaxInlineLines int    `mapstructure:"max_inline_lines"`  // 内联返回最大行数（默认 100）
	MaxInlineChars int    `mapstructure:"max_inline_chars"`  // 内联返回最大字符数（默认 0 = 不限）
	TimeoutSec     int    `mapstructure:"timeout_sec"`       // 单次请求超时（秒），默认 30
	MaxFetchSizeMB int    `mapstructure:"max_fetch_size_mb"` // 最大抓取文件大小（MB），HEAD 预检用，默认 10
	UseSystemProxy bool   `mapstructure:"use_system_proxy"`  // 自动使用系统代理（默认 false）
	MaxRetries     int    `mapstructure:"max_retries"`       // 最大重试次数（默认 3）
}

// ── PDF 解析配置 ──

type PDFParserConfig struct {
	Enabled         bool   `mapstructure:"enabled"`           // 总开关（默认 false）
	MinerUToken     string `mapstructure:"mineru_token"`      // MinerU API Token（精准解析 API 需要）
	MinerUModel     string `mapstructure:"mineru_model"`      // 模型版本: pipeline(默认) / vlm
	MinerUOcr       bool   `mapstructure:"mineru_ocr"`        // OCR 识别（默认 false）
	MinerUFormula   *bool  `mapstructure:"mineru_formula"`    // 公式识别（nil=默认 true）
	MinerUTable     *bool  `mapstructure:"mineru_table"`      // 表格识别（nil=默认 true）
	MinerULang      string `mapstructure:"mineru_lang"`       // 文档语言（默认 ch）
	MinerURemotePDF bool   `mapstructure:"mineru_remote_pdf"` // 远程 PDF URL 走 MinerU 精准 API（默认 true；false 则远程一律不走 MinerU，只保留本地 PDF OCR 回退）
}

// MinerUEnabled 返回是否需要初始化 MinerU 客户端。
// 有 Token（远程精准 API）或开启 OCR（扫描件回退）时启用。
func (c PDFParserConfig) MinerUEnabled() bool {
	return c.MinerUToken != "" || c.MinerUOcr
}

// MinerUOCREnabled 返回是否启用 MinerU OCR 回退（本地 PDF 库读不到文本时使用）。
func (c PDFParserConfig) MinerUOCREnabled() bool {
	return c.MinerUOcr
}

// GetMinerUModel 返回模型版本（默认 pipeline）。
func (c PDFParserConfig) GetMinerUModel() string {
	if c.MinerUModel != "" {
		return c.MinerUModel
	}
	return "pipeline"
}

// GetMinerULang 返回文档语言（默认 ch）。
func (c PDFParserConfig) GetMinerULang() string {
	if c.MinerULang != "" {
		return c.MinerULang
	}
	return "ch"
}

// GetMinerUFormula 返回公式识别开关（默认 true）。
func (c PDFParserConfig) GetMinerUFormula() bool {
	if c.MinerUFormula != nil {
		return *c.MinerUFormula
	}
	return true
}

// GetMinerUTable 返回表格识别开关（默认 true）。
func (c PDFParserConfig) GetMinerUTable() bool {
	if c.MinerUTable != nil {
		return *c.MinerUTable
	}
	return true
}

// ── 代理配置 ──
// 默认自动检测系统代理（读取 Windows 注册表 / 环境变量）。
// Clash、V2rayN 等代理软件开启系统代理后无需手动配置即可生效。
// 显式设置 enabled: false 可关闭代理；显式设置 enabled: true 使用 endpoint。

type ProxyConfig struct {
	Enabled      bool   `mapstructure:"enabled"`  // 显式启用代理（默认 false，未设置时自动检测）
	Endpoint     string `mapstructure:"endpoint"` // 代理地址（默认 http://127.0.0.1:7897")
	autoDisabled bool   // Load() 中设置：用户显式 enabled: false 时为 true，跳过自动检测
}

// GetProxyEndpoint 返回代理端点地址。
// 显式 enabled: true 时返回配置的 endpoint；
// 显式 enabled: false 时返回空字符串（禁用代理）；
// 未显式设置时自动检测系统代理，检测到则返回代理地址，否则返回空字符串。
func (c ProxyConfig) GetProxyEndpoint() string {
	// 显式禁用
	if c.autoDisabled {
		return ""
	}
	// 显式启用，使用配置的 endpoint
	if c.Enabled {
		if c.Endpoint != "" {
			return c.Endpoint
		}
		return "http://127.0.0.1:7897"
	}
	// 未显式设置 → 自动检测系统代理
	if ep := proxy.DetectSystemProxy(); ep != "" {
		return ep
	}
	return ""
}

// ProxyResolver 返回动态代理解析函数。
// 每次请求时实时获取当前代理端点，支持运行时代理开关切换。
// 显式禁用时返回 nil；显式启用时返回固定端点；未设置时返回自动检测函数。
func (c ProxyConfig) ProxyResolver() proxy.ProxyResolver {
	// 显式禁用
	if c.autoDisabled {
		return nil
	}
	// 显式启用，返回固定端点
	if c.Enabled {
		ep := c.Endpoint
		if ep == "" {
			ep = "http://127.0.0.1:7897"
		}
		return func() string { return ep }
	}
	// 未显式设置 → 返回自动检测函数（每次请求实时解析）
	return func() string { return proxy.DetectSystemProxy() }
}

// NeedsProxy 返回是否需要初始化代理相关引擎。
// 显式 enabled: false 时不需要；其他情况始终初始化（由 resolver 在请求时决定是否走代理）。
func (c ProxyConfig) NeedsProxy() bool {
	return !c.autoDisabled
}

// ── 其他子配置 ──

type LLMConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
	ModelId string `mapstructure:"model_id"`
}

type CacheConfig struct {
	Enabled         *bool  `mapstructure:"enabled"`          // 缓存总开关（默认 nil = 按 storage_path 判断；显式 false 强制禁用；显式 true 强制启用）
	StoragePath     string `mapstructure:"storage_path"`     // SQLite 数据库文件存储路径
	CleanupInterval int    `mapstructure:"cleanup_interval"` // 清理间隔（分钟），默认30分钟，最大360分钟
}

type JinaConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"` // 默认 https://r.jina.ai
}

type LogConfig struct {
	MaxSize int `mapstructure:"max_size"` // 单个日志文件最大大小（MB），默认 1
	MaxAge  int `mapstructure:"max_age"`  // 日志保留天数，默认 1
}

// SmartSearchConfig smartsearch 工具高级配置。
type SmartSearchConfig struct {
	MaxSize            int                          `mapstructure:"max_size"`            // 全局最大结果数（按 score 排序后截断），0 = 不限
	ShowMeta           bool                         `mapstructure:"show_meta"`           // 输出中是否显示引擎来源和 score（默认 true）
	Enhance            *bool                        `mapstructure:"enhance"`             // 是否启用 Wigolo 本地评分增强（RRF+词汇对齐+域名品质+多层 Boost），默认 true
	RelevanceThreshold float64                      `mapstructure:"relevance_threshold"` // 增强评分后的相关性阀值，低于此值丢弃（Top-1/每引擎保底），默认 0.05
	MMR                MMRConfig                    `mapstructure:"mmr"`                 // MMR 多样性重排配置
	Engines            map[string]SmartSearchEngine `mapstructure:"engines"`             // 按引擎名配置
}

// MMRConfig MMR（Maximal Marginal Relevance）多样性重排配置。
// 在评分流水线阀值过滤之后、maxSize 截断之前执行，
// 用于打散同一话题的多条高相似结果。
type MMRConfig struct {
	Enabled     bool    `mapstructure:"enabled"`      // MMR 重排开关（默认 true）
	Lambda      float64 `mapstructure:"lambda"`       // 相关性-多样性权衡系数 [0,1]，越高越偏相关性，默认 0.7
	TargetCount int     `mapstructure:"target_count"` // MMR 后的目标条数，0 = 不额外截断（由 max_size 统一截断）
}

// SmartSearchEngine 单引擎的 smartsearch 配置。
type SmartSearchEngine struct {
	MinScore float64 `mapstructure:"min_score"` // 最低相关性分数阀值，0 = 不过滤；引擎不支持 score 时忽略
	MaxSize  int     `mapstructure:"max_size"`  // 单引擎最大结果数，0 = 使用默认值 4
	Weight   float64 `mapstructure:"weight"`    // 引擎权重，影响 RRF 融合分（0 = 默认 1.0）
}

// ApipoolConfig apipool 模式配置。
type ApipoolConfig struct {
	Strategy string   `mapstructure:"strategy"` // "round-robin"(默认) / "priority"
	Engines  []string `mapstructure:"engines"`  // 供应商优先级顺序（默认: baidu, tavily, exa）
}

// GetApipoolStrategy 返回 apipool 策略，默认 round-robin。
func (c ApipoolConfig) GetStrategy() string {
	switch strings.ToLower(c.Strategy) {
	case "priority":
		return "priority"
	default:
		return "round-robin"
	}
}

// GetEngines 返回供应商顺序，默认 baidu → tavily → exa。
func (c ApipoolConfig) GetEngines() []string {
	if len(c.Engines) > 0 {
		return c.Engines
	}
	return []string{"baidu", "tavily", "exa"}
}

// ── Config 方法 ──

// IsInternational 返回是否为海外网络环境。
func (c Config) IsInternational() bool {
	switch strings.ToLower(c.Network) {
	case "international", "intl":
		return true
	default:
		return false
	}
}

func (c Config) LLMEnabled() bool {
	return c.LLM.BaseURL != "" && c.LLM.APIKey != "" && c.LLM.ModelId != ""
}

func (c Config) CacheEnabled() bool {
	// 显式设置 enabled 字段时以该字段为准
	if c.Cache.Enabled != nil {
		return *c.Cache.Enabled
	}
	// 未显式设置时按 storage_path 判断（向后兼容）
	return c.Cache.StoragePath != ""
}

func (c Config) GetCleanupInterval() time.Duration {
	minutes := c.Cache.CleanupInterval
	if minutes <= 0 {
		minutes = 30
	}
	if minutes > 360 {
		minutes = 360
	}
	return time.Duration(minutes) * time.Minute
}

func (c Config) GetMode() string {
	switch strings.ToLower(c.Mode) {
	case ModeApipool:
		return ModeApipool
	case ModeTavily:
		return ModeTavily
	case ModeExa:
		return ModeExa
	case ModeHybrid, "hybird":
		return ModeHybrid
	case ModeEngine:
		return ModeEngine
	case ModeBaidu, "":
		return ModeBaidu
	default:
		return ModeBaidu
	}
}

// NeedsAPIKey 当前模式是否需要 API Key。
func (c Config) NeedsAPIKey() bool {
	switch c.GetMode() {
	case ModeEngine:
		return false
	default:
		return true
	}
}

// GetRateLimitPerSec 返回每秒限流上限（默认 3）。
func (c Config) GetRateLimitPerSec() int {
	if c.RateLimit.PerSec > 0 {
		return c.RateLimit.PerSec
	}
	return 3
}

// GetRateLimitPerMin 返回每分钟限流上限（默认 60）。
func (c Config) GetRateLimitPerMin() int {
	if c.RateLimit.PerMin > 0 {
		return c.RateLimit.PerMin
	}
	return 60
}

// ── 配置加载 ──

func Load(configPath string) (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// 优先使用环境变量指定的配置文件
	envConfigPath := os.Getenv("WEBSEARCH_CONFIG")
	if envConfigPath != "" {
		viper.SetConfigFile(envConfigPath)
	} else if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		viper.AddConfigPath(".")
		if exePath, err := os.Executable(); err == nil {
			if exeDir := filepath.Dir(exePath); exeDir != "" {
				viper.AddConfigPath(exeDir)
			}
		}
	}

	err := viper.ReadInConfig()
	if err != nil {
		return nil, fmt.Errorf("read config file failed: %w", err)
	}

	if cfgFile := viper.ConfigFileUsed(); cfgFile != "" {
		configDir = filepath.Dir(cfgFile)
	}

	viper.SetEnvPrefix("APP")
	viper.AutomaticEnv()
	viper.BindEnv("baidu.api_key", "BAIDU_SK")
	viper.BindEnv("tavily.api_key", "TAVILY_SK")
	viper.BindEnv("exa.api_key", "EXA_API_KEY")
	viper.BindEnv("llm.base_url", "LLM_BASE_URL")
	viper.BindEnv("llm.api_key", "LLM_API_KEY")
	viper.BindEnv("pdf_parser.mineru_token", "MINERU_TOKEN")
	var conf Config
	if err := viper.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("配置解析失败,%w", err)
	}

	// ── 默认值 ──

	// 服务端口默认 8338：未配置时避免绑定到随机端口 :0，
	// 否则 daemon/CLI 与 agent 通过 http://127.0.0.1:{port}/__admin 访问端点会失败。
	if conf.Port <= 0 {
		conf.Port = 8338
	}

	// 监听地址默认只绑回环，避免局域网任意主机访问业务端点
	if conf.Host == "" {
		conf.Host = "127.0.0.1"
	}

	// API 上游超时默认 30s；显式 0 = 不设超时（有挂起风险，文档已注明）
	if !viper.IsSet("upstream_timeout_sec") {
		conf.UpstreamTimeoutSec = 30
	} else if conf.UpstreamTimeoutSec < 0 {
		conf.UpstreamTimeoutSec = 30 // 负值视为无效，回退默认
	}

	if conf.Log.MaxSize <= 0 {
		conf.Log.MaxSize = 1
	}
	if conf.Log.MaxAge <= 0 {
		conf.Log.MaxAge = 1
	}

	// Bing 默认开启
	if !viper.IsSet("bing.enabled") {
		conf.Bing.Enabled = true
	}
	// DuckDuckGo 默认开启（需代理才能访问）
	if !viper.IsSet("duckduckgo.enabled") {
		conf.DuckDuckGo.Enabled = true
	}
	// 学术引擎默认开启
	if !viper.IsSet("academic.enabled") {
		conf.Academic.Enabled = true
	}
	if !viper.IsSet("academic.bing_fallback") {
		conf.Academic.BingFallback = true
	}
	// 学术搜索评分增强默认开启，阀值比通用搜索更宽松
	if !viper.IsSet("academic.enhance") {
		conf.Academic.Enhance = true
	}
	if conf.Academic.Threshold <= 0 {
		conf.Academic.Threshold = 0.02
	}
	// 网络区域默认 china
	if conf.Network == "" {
		conf.Network = "china"
	}

	// CleanFetch 默认值（Enabled 默认 false，旧配置不启用）
	if conf.CleanFetch.FileTTL <= 0 {
		conf.CleanFetch.FileTTL = 24
	}
	if conf.CleanFetch.MaxInlineLines <= 0 {
		conf.CleanFetch.MaxInlineLines = 100
	}
	if conf.CleanFetch.TimeoutSec <= 0 {
		conf.CleanFetch.TimeoutSec = 30
	}
	if conf.CleanFetch.MaxFetchSizeMB <= 0 {
		conf.CleanFetch.MaxFetchSizeMB = 10
	}

	if conf.CleanFetch.MaxRetries <= 0 {
		conf.CleanFetch.MaxRetries = 3
	}

	// MinerU 远程 PDF 精准解析默认开启（false 则远程 URL 一律不走 MinerU）
	if !viper.IsSet("pdf_parser.mineru_remote_pdf") {
		conf.PDFParser.MinerURemotePDF = true
	}

	// 代理：标记用户显式禁用（enabled: false），跳过自动检测
	if viper.IsSet("proxy.enabled") && !viper.GetBool("proxy.enabled") {
		conf.Proxy.autoDisabled = true
	}

	// 百度 enable_ai_search 默认 true（不传 model 时走免费搜索，不产生 LLM 费用）
	if !viper.IsSet("baidu.enable_ai_search") {
		conf.Baidu.EnableAISearch = true
	}

	// SmartSearch 默认值
	if !viper.IsSet("smartsearch.show_meta") {
		conf.SmartSearch.ShowMeta = true // 默认显示引擎来源和 score
	}
	if conf.SmartSearch.Enhance == nil {
		enhance := true // 默认启用 Wigolo 本地评分增强
		conf.SmartSearch.Enhance = &enhance
	}
	if conf.SmartSearch.RelevanceThreshold <= 0 {
		conf.SmartSearch.RelevanceThreshold = 0.05 // 默认相关性阀值
	}
	// MMR 多样性重排默认开启，λ=0.7
	if !viper.IsSet("smartsearch.mmr.enabled") {
		conf.SmartSearch.MMR.Enabled = true
	}
	if !viper.IsSet("smartsearch.mmr.lambda") {
		conf.SmartSearch.MMR.Lambda = 0.7
	}

	// 环境变量回填：精简 yaml 缺字段时（如未写 tavily.api_key），
	// viper 的 BindEnv 不会为不存在的 key 生效，这里显式覆盖。
	applyKnownEnv(&conf)

	return &conf, nil
}

// Default 返回零配置可用的内存默认值（mode=engine，Bing/学术默认开启）。
func Default() *Config {
	enhance := true
	conf := &Config{
		Port:               8338,
		Host:               "127.0.0.1",
		UpstreamTimeoutSec: 30,
		Mode:               ModeEngine,
		Network:            "china",
		Log:                LogConfig{MaxSize: 1, MaxAge: 1},
		Baidu:              BaiduConfig{EnableAISearch: true},
		Bing:               BingConfig{Enabled: true},
		DuckDuckGo:         DuckDuckGoConfig{Enabled: true},
		Academic: AcademicConfig{
			Enabled:      true,
			BingFallback: true,
			Enhance:      true,
			Threshold:    0.02,
		},
		CleanFetch: CleanFetchConfig{
			FileTTL:        24,
			MaxInlineLines: 100,
			TimeoutSec:     30,
			MaxFetchSizeMB: 10,
			MaxRetries:     3,
		},
		PDFParser: PDFParserConfig{
			MinerURemotePDF: true,
		},
		SmartSearch: SmartSearchConfig{
			ShowMeta:           true,
			Enhance:            &enhance,
			RelevanceThreshold: 0.05,
			MMR: MMRConfig{
				Enabled: true,
				Lambda:  0.7,
			},
		},
	}
	applyKnownEnv(conf)
	return conf
}

// LoadOrDefault 加载配置；未指定路径且找不到文件时返回 Default()。
// 若 -c / WEBSEARCH_CONFIG 指向的文件不存在或无法解析，返回错误。
func LoadOrDefault(configPath string) (*Config, error) {
	if explicitConfigPath(configPath) != "" {
		return Load(configPath)
	}
	conf, err := Load("")
	if err == nil {
		return conf, nil
	}
	var notFound viper.ConfigFileNotFoundError
	if errors.As(err, &notFound) {
		return Default(), nil
	}
	return nil, err
}

func explicitConfigPath(configPath string) string {
	if env := os.Getenv("WEBSEARCH_CONFIG"); env != "" {
		return env
	}
	return configPath
}

func applyKnownEnv(conf *Config) {
	if v := os.Getenv("BAIDU_SK"); v != "" {
		conf.Baidu.APIKey = v
	}
	if v := os.Getenv("TAVILY_SK"); v != "" {
		conf.Tavily.APIKey = v
	}
	if v := os.Getenv("EXA_API_KEY"); v != "" {
		conf.Exa.APIKey = v
	}
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		conf.LLM.BaseURL = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		conf.LLM.APIKey = v
	}
	if v := os.Getenv("MINERU_TOKEN"); v != "" {
		conf.PDFParser.MinerUToken = v
	}
	if v := os.Getenv("SEMANTIC_SCHOLAR_API_KEY"); v != "" {
		conf.Academic.SemanticScholarAPIKey = v
	}
	if v := os.Getenv("WEBSEARCH_TOKEN"); v != "" {
		conf.AuthToken = v
	}
}

// EnsureExampleFile 确保目标路径存在可编辑的预设配置文件。
// 文件不存在时写入 ExampleConfig；已存在时不覆盖（幂等）。
func EnsureExampleFile(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.WriteFile(path, ExampleConfig, 0644); err != nil {
		return false, err
	}
	return true, nil
}

func GetConfigDir() string {
	if configDir != "" {
		return configDir
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return os.TempDir()
}
