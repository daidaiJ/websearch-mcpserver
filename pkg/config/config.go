package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websearch/pkg/proxy"

	"github.com/spf13/viper"
)

var configDir string
var configFile string

const (
	ModeBaidu     = "baidu"   // 百度千帆搜索（enable_ai_search 控制端点，失败自动回退网页搜索）
	ModeApipool   = "apipool" // API Key 池轮转：Anysearch + 百度 + Tavily + Exa，失败自动切换
	ModeTavily    = "tavily"
	ModeExa       = "exa"
	ModeAnysearch = "anysearch"
	ModeDoubao    = "doubao" // 火山引擎豆包联网搜索（Global / Custom，由 doubao.version 选择）
	ModeHybrid    = "hybrid"
	ModeEngine    = "engine" // 纯引擎模式，无需 API Key
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
	Anysearch          AnysearchConfig   `mapstructure:"anysearch"`
	Doubao             DoubaoConfig      `mapstructure:"doubao"`
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
	Dashboard          DashboardConfig   `mapstructure:"dashboard"`
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
	SKList []string `mapstructure:"sk_list"` // 多 Key 轮转列表（优先级高于 api_key）
	// IncludeRawContent 是否请求 API 返回页面原文（raw_content），默认 true；
	// 显式 false 退回摘要片段，减小响应体与配额消耗
	IncludeRawContent *bool `mapstructure:"include_raw_content"`
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
	SKList       []string `mapstructure:"sk_list"`       // 多 Key 轮转列表（优先级高于 api_key）
	NumResults   int      `mapstructure:"num_results"`   // 单次搜索结果数量（默认 5）
	LookbackDays int      `mapstructure:"lookback_days"` // 搜索时间范围（天），默认 90
	// IncludeText 是否请求 API 返回页面正文（contents.text），默认 true
	IncludeText *bool `mapstructure:"include_text"`
	// TextMaxCharacters 正文最大字符数（默认 3000，<=0 时取默认）
	TextMaxCharacters int `mapstructure:"text_max_characters"`
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

type AnysearchConfig struct {
	APIKey     string   `mapstructure:"api_key"`     // AnySearch API Key（单 key 时自动作为 sk_list；https://www.anysearch.com/docs）
	SKList     []string `mapstructure:"sk_list"`     // 多 Key 轮询列表（优先级高于 api_key）
	NumResults int      `mapstructure:"num_results"` // 单次搜索结果数量（默认 10）
}

// EffectiveSKList 返回合并后的 Key 列表。
func (c AnysearchConfig) EffectiveSKList() []string {
	if len(c.SKList) > 0 {
		return c.SKList
	}
	if c.APIKey != "" {
		return []string{c.APIKey}
	}
	return nil
}

// DoubaoConfig 火山引擎豆包联网搜索（Global / Custom）。
// API Key 来自「联网搜索 API」控制台，与 Ark 豆包大模型 Key 不通用。
// 免费档每月默认 500 积分；显式加入 apipool.engines 时 weighted 默认权重 500。
type DoubaoConfig struct {
	APIKey              string   `mapstructure:"api_key"`                 // 搜索 API Key；环境变量 DOUBAO_SEARCH_API_KEY
	SKList              []string `mapstructure:"sk_list"`                 // 多 Key 轮询列表（优先级高于 api_key）
	Version             string   `mapstructure:"version"`                 // global（默认）/ custom
	NumResults          int      `mapstructure:"num_results"`             // 请求条数：Global 最大 20，Custom 最大 50
	TimeRange           string   `mapstructure:"time_range"`              // Custom 默认时间范围；MCP 请求级 time_range 优先
	AuthLevel           int      `mapstructure:"auth_level"`              // Custom: 0=默认，1=仅非常权威来源
	QueryRewrite        bool     `mapstructure:"query_rewrite"`           // Custom: 是否启用查询改写
	NeedContent         bool     `mapstructure:"need_content"`            // Custom: 是否请求网页正文
	MaxSnippetLength    int      `mapstructure:"max_snippet_length"`      // Global: 单片段最大 tokens，默认 500，最大 3000
	MaxImageCountPerDoc int      `mapstructure:"max_image_count_per_doc"` // Global: 单结果图片数，默认 0
	ICPHostOnly         bool     `mapstructure:"icp_host_only"`           // Global: 仅搜索国内 ICP 备案网站
}

// EffectiveSKList 返回合并后的 Key 列表。
func (c DoubaoConfig) EffectiveSKList() []string {
	if len(c.SKList) > 0 {
		return c.SKList
	}
	if c.APIKey != "" {
		return []string{c.APIKey}
	}
	return nil
}

// GetVersion 返回 global 或 custom，其他值回落 global。
func (c DoubaoConfig) GetVersion() string {
	if strings.ToLower(strings.TrimSpace(c.Version)) == "custom" {
		return "custom"
	}
	return "global"
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

	// UnpaywallEmail 用于补全缺失的 OA PDF（环境变量 UNPAYWALL_EMAIL）。空则跳过 Unpaywall。
	UnpaywallEmail string `mapstructure:"unpaywall_email"`
}

// ── CleanFetch 配置 ──

type CleanFetchConfig struct {
	Enabled        bool   `mapstructure:"enabled"`           // 总开关（默认 false，旧配置不启用）
	FileOutputDir  string `mapstructure:"file_output_dir"`   // 大文本文件输出目录（默认 exe 同目录 fetchdata/）
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
	MaxPages        int    `mapstructure:"max_pages"`         // 省略 pages 时最多解析的页数（默认 20，与 MinerU 轻量档对齐）
	MinerUToken     string `mapstructure:"mineru_token"`      // MinerU API Token（精准解析 API 需要）
	MinerUModel     string `mapstructure:"mineru_model"`      // 模型版本: pipeline(默认) / vlm
	MinerUOcr       bool   `mapstructure:"mineru_ocr"`        // OCR 识别（默认 false）
	MinerUFormula   *bool  `mapstructure:"mineru_formula"`    // 公式识别（nil=默认 true）
	MinerUTable     *bool  `mapstructure:"mineru_table"`      // 表格识别（nil=默认 true）
	MinerULang      string `mapstructure:"mineru_lang"`       // 文档语言（默认 ch）
	MinerURemotePDF bool   `mapstructure:"mineru_remote_pdf"` // 远程 PDF URL 走 MinerU 精准 API（默认 true；false 则远程一律不走 MinerU，只保留本地 PDF OCR 回退）
}

// GetMaxPages 返回省略 pages 时一次最多解析的页数，默认 20。
func (c PDFParserConfig) GetMaxPages() int {
	if c.MaxPages > 0 {
		return c.MaxPages
	}
	return 20
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
	// APIProviders 控制 API 供应商上游请求（baidu/tavily/exa/anysearch/doubao/LLM 等）
	// 是否走代理。默认 false = 强制直连：显式置空 transport 的 Proxy，不吃
	// HTTP(S)_PROXY 等环境变量，避免本机代理环境静默劫持供应商请求。
	// true 时按 enabled/endpoint/自动检测的同一套解析走代理。
	APIProviders bool `mapstructure:"api_providers"`
	autoDisabled bool // Load() 中设置：用户显式 enabled: false 时为 true，跳过自动检测
}

// UpstreamResolver 返回 API 供应商上游请求使用的代理解析器。
// api_providers 未启用时返回 nil（调用方据此强制直连）；启用时与引擎层
// 共用同一套 enabled/endpoint/自动检测解析。
func (c ProxyConfig) UpstreamResolver() proxy.ProxyResolver {
	if !c.APIProviders {
		return nil
	}
	return c.ProxyResolver()
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
	Enabled         *bool  `mapstructure:"enabled"`          // 缓存总开关（默认 nil = 关闭；显式 true 启用并按 storage_path 或默认路径建库）
	StoragePath     string `mapstructure:"storage_path"`     // SQLite 数据库文件存储路径（空 = exe 同目录 cache/websearch-cache.db）
	CleanupInterval int    `mapstructure:"cleanup_interval"` // 清理间隔（分钟），默认30分钟，最大360分钟
}

// DashboardConfig controls the local-only observability dashboard. It does not
// enable active provider probes; all health observations come from real calls.
type DashboardConfig struct {
	Enabled       bool             `mapstructure:"enabled"`
	StoragePath   string           `mapstructure:"storage_path"`
	RetentionDays int              `mapstructure:"retention_days"`
	SecretsPath   string           `mapstructure:"secrets_path"`
	// ConfigPath 指定控制中心独立配置文件（dashboard.yaml）。空 = 主配置
	// 同目录的 dashboard.yaml；也可用环境变量 WEBSEARCH_DASHBOARD_CONFIG 覆盖。
	// 独立文件按字段覆盖主配置的 dashboard: 块，删除该文件即完整回退。
	ConfigPath    string           `mapstructure:"config_path"`
	// AllowedNetworks 显式放行可「查看」控制台的来源网段（CIDR，如 192.168.1.0/24）。
	// 空列表 = 仅本机 loopback 可访问；放行仅限只读，写操作永远仅限 loopback。
	AllowedNetworks []string `mapstructure:"allowed_networks"`
	// Shortcut 控制快捷方式落位行为（Windows）：desktop（默认，桌面）/
	// start（开始菜单，可在开始屏幕搜索并手动固定）/ both / off（不创建）。
	// 仅 start/open 安装路径生效；uninstall 始终清理全部已知位置。
	Shortcut string `mapstructure:"shortcut"`
	// AdminPassword 是写操作（设置/密钥/重启/清缓存/额度管理）的口令，必须在
	// config.yaml 显式配置。出于安全考虑它不在控制台设置页的白名单里，也不能
	// 写入 secrets 覆盖文件——即 WebUI 永远无法读取或修改它。二选一：明文或
	// SHA-256 十六进制（admin_password_sha256 = sha256(明文) 的小写十六进制）。
	// AdminUsername 是可选的管理员用户名：仅当显式配置时，写请求才必须携带
	// 匹配的 X-Admin-User 头（常量时间比较）；未配置 = 不做任何用户名检查
	// （与口令-only 行为一致）。和口令一样只能配在 dashboard.yaml，
	// WebUI 无法读取或修改。
	AdminUsername       string           `mapstructure:"admin_username"`
	AdminPassword       string           `mapstructure:"admin_password"`
	AdminPasswordSHA256 string           `mapstructure:"admin_password_sha256"`
	Suspension          SuspensionConfig `mapstructure:"suspension"`
	Quotas              QuotasConfig     `mapstructure:"quotas"`
	// Brand 允许自托管用户自定义控制台的标题与 logo（更开放：打自己的牌子）。
	// 空值 = 内置默认品牌；旧配置文件没有该块时完全兼容。
	Brand BrandConfig `mapstructure:"brand"`
}

// BrandConfig 控制控制台的品牌呈现。
type BrandConfig struct {
	// Title 覆盖侧栏与浏览器标签页标题，默认 "WebSearch 控制中心"。
	Title string `mapstructure:"title"`
	// Logo 指向 logo 图片：http(s) URL 原样使用；本地文件路径经
	// /__admin/api/brand/logo 端点提供（仅限控制台同一访问边界）。
	// 空值 = 内置默认 logo。支持绝对路径或相对 config.yaml 所在目录。
	Logo string `mapstructure:"logo"`
	// Theme 内置主题预设：green（默认，绿色办公）/ blue（蓝白科技）/
	// mono（黑白灰度立体）。只影响 WebUI 与桌面快捷方式图标配色。
	Theme string `mapstructure:"theme"`
	// Accent 自定义主色（#RGB / #RRGGBB），设置后覆盖主题预设的主色。
	Accent string `mapstructure:"accent"`
	// Icon 自定义快捷方式图标（.ico 文件路径）。空 = 按主题内置图标；
	// 每次启动轻量检查，文件变化时重建桌面快捷方式。
	Icon string `mapstructure:"icon"`
	// Footer 控制页面底部的项目介绍（含 GitHub 项目链接）。nil/true =
	// 显示（默认）；false = 隐藏。零值兼容：旧配置无该字段时保持显示。
	Footer *bool `mapstructure:"footer"`
}

// GetFooter 报告是否显示页面底部项目介绍，默认启用（nil = true）。
func (b BrandConfig) GetFooter() bool {
	return b.Footer == nil || *b.Footer
}

// GetTitle 返回控制台标题，空值回退内置默认。
func (b BrandConfig) GetTitle() string {
	if t := strings.TrimSpace(b.Title); t != "" {
		return t
	}
	return "WebSearch 控制中心"
}

// GetTheme 返回主题预设名，非法值回退 green。
func (b BrandConfig) GetTheme() string {
	switch strings.ToLower(strings.TrimSpace(b.Theme)) {
	case "blue", "mono":
		return strings.ToLower(strings.TrimSpace(b.Theme))
	default:
		return "green"
	}
}

// GetAccent 返回合法的自定义主色（规范化为 #rrggbb）；未设置或非法时返回空。
func (b BrandConfig) GetAccent() string {
	s := strings.TrimSpace(b.Accent)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "#") {
		return ""
	}
	hex := strings.ToLower(s[1:])
	switch len(hex) {
	case 3: // #RGB → #rrggbb
		for _, c := range hex {
			if !isHexDigit(byte(c)) {
				return ""
			}
		}
		return "#" + string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	case 6:
		for i := 0; i < 6; i++ {
			if !isHexDigit(hex[i]) {
				return ""
			}
		}
		return "#" + hex
	default:
		return ""
	}
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}

// QuotasConfig 控制 API 供应商的本地用量统计与上限。
// 用量本身来自遥测的真实调用记录（成功次数），不主动探测供应商。
type QuotasConfig struct {
	// Reset 自动重置周期：monthly（默认）/ weekly / daily / none（仅手动重置）。
	Reset string `mapstructure:"reset"`
	// ResetDay monthly 周期的每月重置日（1-28，默认 1）。
	ResetDay int `mapstructure:"reset_day"`
	// Limits 每供应商调用次数上限；未配置的供应商使用 DefaultQuotaLimit。
	Limits map[string]int `mapstructure:"limits"`
}

// DefaultQuotaLimit 是未显式配置 limits 的供应商的默认上限（次/周期）。
const DefaultQuotaLimit = 1000

// GetReset 返回重置周期，空值回退 monthly；非法值也回退 monthly。
func (q QuotasConfig) GetReset() string {
	switch strings.ToLower(strings.TrimSpace(q.Reset)) {
	case "weekly", "daily", "none":
		return strings.ToLower(strings.TrimSpace(q.Reset))
	default:
		return "monthly"
	}
}

// GetResetDay 返回 monthly 周期的重置日，范围钳制到 1-28（默认 1）。
func (q QuotasConfig) GetResetDay() int {
	if q.ResetDay >= 1 && q.ResetDay <= 28 {
		return q.ResetDay
	}
	return 1
}

// GetLimit 返回供应商的上限；未配置时用 DefaultQuotaLimit。
func (q QuotasConfig) GetLimit(provider string) int {
	if v, ok := q.Limits[provider]; ok && v > 0 {
		return v
	}
	return DefaultQuotaLimit
}

// PeriodStart 返回 now 所在统计周期的起点（本地时区）；"none" 返回零值，
// 表示不做自动重置，只认手动重置。周期边界由调用方用于推进本地额度状态。
func (q QuotasConfig) PeriodStart(now time.Time) time.Time {
	y, m, d := now.Date()
	switch q.GetReset() {
	case "daily":
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case "weekly":
		offset := (int(now.Weekday()) + 6) % 7 // 周一为一周起点
		return time.Date(y, m, d-offset, 0, 0, 0, 0, now.Location())
	case "none":
		return time.Time{}
	default: // monthly
		day := q.GetResetDay()
		if d >= day {
			return time.Date(y, m, day, 0, 0, 0, 0, now.Location())
		}
		return time.Date(y, m, day, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
	}
}

// NextReset 返回下一次自动重置时间；"none" 返回零值。
func (q QuotasConfig) NextReset(now time.Time) time.Time {
	if q.GetReset() == "none" {
		return time.Time{}
	}
	start := q.PeriodStart(now)
	switch q.GetReset() {
	case "daily":
		return start.AddDate(0, 0, 1)
	case "weekly":
		return start.AddDate(0, 0, 7)
	default: // monthly
		return start.AddDate(0, 1, 0)
	}
}

// AdminPasswordConfigured 报告管理员口令是否已显式配置。
// 未配置时所有写端点直接禁用（返回 403），这是安全的默认。
func (d DashboardConfig) AdminPasswordConfigured() bool {
	return d.AdminPassword != "" || d.AdminPasswordSHA256 != ""
}

// GetShortcut 归一化快捷方式落位配置：desktop（默认）/ start / both / off。
// 非法值回退 desktop（向后兼容：旧配置无该字段 = 桌面快捷方式）。
func (d DashboardConfig) GetShortcut() string {
	switch strings.ToLower(strings.TrimSpace(d.Shortcut)) {
	case "start", "startmenu", "start-menu":
		return "start"
	case "both", "all":
		return "both"
	case "off", "none", "false", "disabled":
		return "off"
	default:
		return "desktop"
	}
}

// VerifyAdminPassword 以常量时间比较校验管理员口令，支持明文与 SHA-256 两种配置。
func (d DashboardConfig) VerifyAdminPassword(input string) bool {
	if input == "" {
		return false
	}
	if d.AdminPasswordSHA256 != "" {
		sum := sha256.Sum256([]byte(input))
		want := strings.ToLower(strings.TrimSpace(d.AdminPasswordSHA256))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(want)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(input), []byte(d.AdminPassword)) == 1
}

// SuspensionConfig mirrors SearXNG's ban_time_on_fail / max_ban_time_on_fail /
// suspended_times knobs. Values are duration strings such as "5s", "10m",
// "1h", "24h" (or plain numbers, treated as seconds). The control center only
// reports suspension; it never skips a call.
type SuspensionConfig struct {
	BanTimeOnFail    string            `mapstructure:"ban_time_on_fail"`
	MaxBanTimeOnFail string            `mapstructure:"max_ban_time_on_fail"`
	SuspendedTimes   map[string]string `mapstructure:"suspended_times"`
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
	FetchTopN          int                          `mapstructure:"fetch_top_n"`         // 服务端默认抓取正文条数（agent 未传 fetch_top_n 参数时生效），默认 0 = 与旧版一致不抓取；1-5 = 一次搜索即含正文
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
	Strategy string         `mapstructure:"strategy"` // "round-robin"(默认) / "priority" / "weighted"
	Engines  []string       `mapstructure:"engines"`  // 供应商优先级顺序（默认: anysearch, baidu, tavily, exa）
	Weights  map[string]int `mapstructure:"weights"`  // weighted 策略的供应商权重（单 Key 权重，实际权重按可用 Key 数累加）
}

// GetApipoolStrategy 返回 apipool 策略，默认 round-robin。
func (c ApipoolConfig) GetStrategy() string {
	switch strings.ToLower(c.Strategy) {
	case "priority":
		return "priority"
	case "weighted":
		return "weighted"
	default:
		return "round-robin"
	}
}

// GetEngines 返回供应商顺序，默认 anysearch → baidu → tavily → exa。
func (c ApipoolConfig) GetEngines() []string {
	if len(c.Engines) > 0 {
		return c.Engines
	}
	return []string{"anysearch", "baidu", "tavily", "exa"}
}

// GetWeights 返回 weighted 策略的供应商权重（供应商名 → 单 Key 权重），
// 内置默认值可被配置覆盖；显式配置 0 表示该供应商不参与加权起始选择。
func (c ApipoolConfig) GetWeights() map[string]int {
	w := map[string]int{
		"anysearch": 30000,
		"baidu":     1500,
		"tavily":    1200,
		"exa":       1200,
		"doubao":    500, // 火山免费档每月默认 500 积分
	}
	maps.Copy(w, c.Weights)
	return w
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
	// 未显式设置时默认关闭（v3.5.0 起）：SQLite 缓存对轻量部署收益有限，
	// 需要缓存时在配置中显式 enabled: true（storage_path 未配置时用默认路径）
	return false
}

// GetCacheStoragePath 返回缓存 SQLite 数据库路径。
// 未配置时默认 exe 同目录的 cache 子目录（websearch-cache.db）。
func (c Config) GetCacheStoragePath() string {
	if c.Cache.StoragePath != "" {
		return c.Cache.StoragePath
	}
	return filepath.Join(ExeBaseDir(), "cache", "websearch-cache.db")
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
	case ModeAnysearch:
		return ModeAnysearch
	case ModeDoubao:
		return ModeDoubao
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
		configFile = cfgFile
	}

	viper.SetEnvPrefix("APP")
	viper.AutomaticEnv()
	viper.BindEnv("baidu.api_key", "BAIDU_SK")
	viper.BindEnv("tavily.api_key", "TAVILY_SK")
	viper.BindEnv("exa.api_key", "EXA_API_KEY")
	viper.BindEnv("anysearch.api_key", "ANYSEARCH_API_KEY")
	viper.BindEnv("doubao.api_key", "DOUBAO_SEARCH_API_KEY")
	viper.BindEnv("llm.base_url", "LLM_BASE_URL")
	viper.BindEnv("llm.api_key", "LLM_API_KEY")
	viper.BindEnv("pdf_parser.mineru_token", "MINERU_TOKEN")
	viper.BindEnv("academic.unpaywall_email", "UNPAYWALL_EMAIL")
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

	// 豆包 Custom 版请求网页正文默认开启（API 快速路径：一次搜索即含正文）；
	// 显式写 need_content: false 才退回摘要
	if !viper.IsSet("doubao.need_content") {
		conf.Doubao.NeedContent = true
	}

	// SmartSearch 默认值
	if !viper.IsSet("smartsearch.show_meta") {
		conf.SmartSearch.ShowMeta = true // 默认显示引擎来源和 score
	}
	// fetch_top_n 默认 0：默认部署与旧版行为一致（不抓取正文）；
	// 用户可设 1-5 让 agent 未传参时也默认获取正文，yaml 显式 0 即纯摘要模式
	if !viper.IsSet("smartsearch.fetch_top_n") {
		conf.SmartSearch.FetchTopN = 0
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

	// 用户显式声明过控制中心（主配置 dashboard: 块）则不再套用
	// 「默认生成启用配置」的兜底，尊重用户选择（含显式关闭）。
	if viper.IsSet("dashboard.enabled") {
		dashboardExplicit = true
	}

	// 环境变量回填：精简 yaml 缺字段时（如未写 tavily.api_key），
	// viper 的 BindEnv 不会为不存在的 key 生效，这里显式覆盖。
	applyKnownEnv(&conf)
	applyDashboardOverlay(&conf)
	applyDashboardSecrets(&conf)

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
			FetchTopN:          0,
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
	if v := os.Getenv("ANYSEARCH_API_KEY"); v != "" {
		conf.Anysearch.APIKey = v
	}
	for _, envName := range []string{"DOUBAO_SEARCH_API_KEY", "ASK_ECHO_SEARCH_INFINITY_API_KEY"} {
		if v := os.Getenv(envName); v != "" {
			conf.Doubao.APIKey = v
			break
		}
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
	if v := os.Getenv("UNPAYWALL_EMAIL"); v != "" {
		conf.Academic.UnpaywallEmail = v
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

// GetConfigFile returns the exact file Viper loaded. The dashboard uses this
// only for validated, backed-up settings changes.
func GetConfigFile() string { return configFile }

func (c Config) GetDashboardStoragePath() string {
	if c.Dashboard.StoragePath != "" {
		return c.Dashboard.StoragePath
	}
	return filepath.Join(filepath.Dir(c.GetCacheStoragePath()), "dashboard.db")
}

func (c Config) GetDashboardSecretsPath() string {
	if c.Dashboard.SecretsPath != "" {
		return c.Dashboard.SecretsPath
	}
	return filepath.Join(filepath.Dir(c.GetDashboardStoragePath()), "dashboard-secrets.json")
}

// ParseAllowedNetworks 解析 allowed_networks 中的 CIDR 网段；裸 IP 视为单地址
// 网段。任一条目非法即返回错误，调用方必须 fail-closed（回退为仅 loopback）。
func (d DashboardConfig) ParseAllowedNetworks() ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(d.AllowedNetworks))
	for _, raw := range d.AllowedNetworks {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			_, ipnet, err := net.ParseCIDR(s)
			if err != nil {
				return nil, fmt.Errorf("条目 %q 不是合法 CIDR: %w", raw, err)
			}
			out = append(out, ipnet)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("条目 %q 不是合法 IP 或 CIDR", raw)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}

// applyDashboardSecrets loads an optional private overlay from the persistent
// data volume. Values are intentionally kept out of config.yaml and logs.
var dashboardOverlayFile string

// GetDashboardOverlayFile 返回本次加载实际使用的控制中心独立配置文件路径；
// 未使用时返回空串。仅供启动日志提示，不参与业务逻辑。
func GetDashboardOverlayFile() string { return dashboardOverlayFile }

// DashboardOverlayPath 返回控制中心独立配置文件（dashboard.yaml）的路径：
// 环境变量 WEBSEARCH_DASHBOARD_CONFIG > dashboard.config_path（相对主配置
// 目录解析）> 主配置同目录的 dashboard.yaml。
func DashboardOverlayPath(conf *Config) string {
	if v := strings.TrimSpace(os.Getenv("WEBSEARCH_DASHBOARD_CONFIG")); v != "" {
		return v
	}
	base := configDir
	if base == "" {
		base = GetConfigDir()
	}
	if p := strings.TrimSpace(conf.Dashboard.ConfigPath); p != "" {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	return filepath.Join(base, "dashboard.yaml")
}

// applyDashboardOverlay 读取控制中心独立配置文件（dashboard.yaml），按字段
// 覆盖主配置的 dashboard: 块（整键覆盖，不做深合并）。
//
// 设计动机（迁移与回退）：
//   - 升级：主 config.yaml 零改动，控制中心专属配置（管理员口令、访问网段、
//     额度、品牌）全部住在这个文件，且完全在控制台设置页写路径之外——WebUI
//     无法读取或修改它们，这是结构性保证而非白名单约定；
//   - 回退：删除该文件并重启即回到基线；旧版本二进制不读取该文件，不受影响。
//
// 文件不存在时无操作；存在但解析失败时打警告并忽略，不拖垮主服务。
func applyDashboardOverlay(conf *Config) {
	path := DashboardOverlayPath(conf)
	if _, err := os.Stat(path); err != nil {
		return
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: 控制中心配置 %s 无法解析，已忽略: %v\n", path, err)
		return
	}
	var dash DashboardConfig
	if err := v.UnmarshalKey("dashboard", &dash); err != nil {
		fmt.Fprintf(os.Stderr, "warning: 控制中心配置 %s 结构异常，已忽略: %v\n", path, err)
		return
	}
	// Enabled 的零值与「未设置」无法从结构体区分，用 IsSet 判定显式配置。
	if v.IsSet("dashboard.enabled") {
		conf.Dashboard.Enabled = dash.Enabled
	}
	if dash.StoragePath != "" {
		conf.Dashboard.StoragePath = dash.StoragePath
	}
	if dash.RetentionDays > 0 {
		conf.Dashboard.RetentionDays = dash.RetentionDays
	}
	if dash.SecretsPath != "" {
		conf.Dashboard.SecretsPath = dash.SecretsPath
	}
	if dash.ConfigPath != "" {
		conf.Dashboard.ConfigPath = dash.ConfigPath
	}
	if len(dash.AllowedNetworks) > 0 {
		conf.Dashboard.AllowedNetworks = dash.AllowedNetworks
	}
	if dash.Shortcut != "" {
		conf.Dashboard.Shortcut = dash.Shortcut
	}
	if dash.AdminUsername != "" {
		conf.Dashboard.AdminUsername = dash.AdminUsername
	}
	if dash.AdminPassword != "" {
		conf.Dashboard.AdminPassword = dash.AdminPassword
	}
	if dash.AdminPasswordSHA256 != "" {
		conf.Dashboard.AdminPasswordSHA256 = dash.AdminPasswordSHA256
	}
	if dash.Suspension.BanTimeOnFail != "" {
		conf.Dashboard.Suspension.BanTimeOnFail = dash.Suspension.BanTimeOnFail
	}
	if dash.Suspension.MaxBanTimeOnFail != "" {
		conf.Dashboard.Suspension.MaxBanTimeOnFail = dash.Suspension.MaxBanTimeOnFail
	}
	if len(dash.Suspension.SuspendedTimes) > 0 {
		conf.Dashboard.Suspension.SuspendedTimes = dash.Suspension.SuspendedTimes
	}
	if dash.Quotas.Reset != "" {
		conf.Dashboard.Quotas.Reset = dash.Quotas.Reset
	}
	if dash.Quotas.ResetDay > 0 {
		conf.Dashboard.Quotas.ResetDay = dash.Quotas.ResetDay
	}
	if len(dash.Quotas.Limits) > 0 {
		conf.Dashboard.Quotas.Limits = dash.Quotas.Limits
	}
	if dash.Brand.Title != "" {
		conf.Dashboard.Brand.Title = dash.Brand.Title
	}
	if dash.Brand.Logo != "" {
		conf.Dashboard.Brand.Logo = dash.Brand.Logo
	}
	if dash.Brand.Theme != "" {
		conf.Dashboard.Brand.Theme = dash.Brand.Theme
	}
	if dash.Brand.Accent != "" {
		conf.Dashboard.Brand.Accent = dash.Brand.Accent
	}
	if dash.Brand.Icon != "" {
		conf.Dashboard.Brand.Icon = dash.Brand.Icon
	}
	// Footer 用指针区分「未设置」（保持默认显示）与显式 false
	if dash.Brand.Footer != nil {
		conf.Dashboard.Brand.Footer = dash.Brand.Footer
	}
	dashboardOverlayFile = path
	dashboardExplicit = true
}

func applyDashboardSecrets(conf *Config) {
	path := conf.GetDashboardSecretsPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var values map[string]string
	if json.Unmarshal(b, &values) != nil {
		return
	}
	if v := values["BAIDU_SK"]; v != "" {
		conf.Baidu.APIKey = v
		conf.Baidu.SKList = nil
	}
	if v := values["TAVILY_SK"]; v != "" {
		conf.Tavily.APIKey = v
		conf.Tavily.SKList = nil
	}
	if v := values["EXA_API_KEY"]; v != "" {
		conf.Exa.APIKey = v
		conf.Exa.SKList = nil
	}
	if v := values["ANYSEARCH_API_KEY"]; v != "" {
		conf.Anysearch.APIKey = v
		conf.Anysearch.SKList = nil
	}
	if v := values["DOUBAO_SEARCH_API_KEY"]; v != "" {
		conf.Doubao.APIKey = v
		conf.Doubao.SKList = nil
	}
	if v := values["JINA_API_KEY"]; v != "" {
		conf.Jina.APIKey = v
	}
	if v := values["MINERU_TOKEN"]; v != "" {
		conf.PDFParser.MinerUToken = v
	}
}

// ExeBaseDir 返回可执行文件所在目录，获取失败时回退配置目录。
// 用作 cache / fetchdata 等派生数据目录的基准（exe 同目录）。
func ExeBaseDir() string {
	if exePath, err := os.Executable(); err == nil {
		if exeDir := filepath.Dir(exePath); exeDir != "" {
			return exeDir
		}
	}
	return GetConfigDir()
}
