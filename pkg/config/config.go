package config

import (
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
	ModeBaidu  = "baidu"
	ModeTavily = "tavily"
	ModeHybrid = "hybrid"
	ModeEngine = "engine" // 纯引擎模式，无需 API Key
)

// ── 顶层配置 ──

type Config struct {
	Port          int              `mapstructure:"port"`
	LogLevel      string           `mapstructure:"log_level"`
	Mode          string           `mapstructure:"mode"`
	Network       string           `mapstructure:"network"`        // 全局网络区域: china / international
	BlackListHost []string         `mapstructure:"black_list_host"` // 全局屏蔽站点
	RateLimit     RateLimitConfig  `mapstructure:"rate_limit"`      // 全局限流配置
	Baidu         BaiduConfig      `mapstructure:"baidu"`
	Tavily        TavilyConfig     `mapstructure:"tavily"`
	LLM           LLMConfig        `mapstructure:"llm"`
	Jina          JinaConfig       `mapstructure:"jina"`
	Cache         CacheConfig      `mapstructure:"cache"`
	Log           LogConfig        `mapstructure:"log"`
	Bing          BingConfig       `mapstructure:"bing"`
	Academic      AcademicConfig   `mapstructure:"academic"`
	CleanFetch    CleanFetchConfig   `mapstructure:"cleanfetch"`
	PDFParser     PDFParserConfig    `mapstructure:"pdf_parser"`
	Proxy         ProxyConfig        `mapstructure:"proxy"`
	SmartSearch   SmartSearchConfig  `mapstructure:"smartsearch"`
}

// ── 各搜索引擎配置 ──

type BaiduConfig struct {
	APIKey string `mapstructure:"api_key"` // 百度千帆 AI Search API Key
}

type TavilyConfig struct {
	APIKey string `mapstructure:"api_key"` // Tavily Search API Key
}

type BingConfig struct {
	Enabled bool     `mapstructure:"enabled"` // 总开关（默认 true）
	Blocked []string `mapstructure:"blocked"` // Bing 屏蔽域名
}

// RateLimitConfig 全局搜索引擎限流配置（对所有引擎统一生效）。
type RateLimitConfig struct {
	PerSec int `mapstructure:"per_sec"` // 每秒请求数上限（默认 3）
	PerMin int `mapstructure:"per_min"` // 每分钟请求数上限（默认 60）
}

// ── 学术引擎配置 ──

type AcademicConfig struct {
	Enabled      bool `mapstructure:"enabled"`       // 学术引擎总开关（默认 true）
	BingFallback bool `mapstructure:"bing_fallback"` // 学术搜索时用 Bing 兜底（默认 true）

	// 各引擎独立禁用（默认 false = 启用）
	DisableArxiv           bool `mapstructure:"disable_arxiv"`
	DisableCrossref        bool `mapstructure:"disable_crossref"`
	DisableOpenAlex        bool `mapstructure:"disable_openalex"`
	DisableSemanticScholar bool `mapstructure:"disable_semantic_scholar"`
	DisablePubMed          bool `mapstructure:"disable_pubmed"`
	DisableGoogleScholar   bool `mapstructure:"disable_google_scholar"`
}

// ── CleanFetch 配置 ──

type CleanFetchConfig struct {
	Enabled        bool   `mapstructure:"enabled"`          // 总开关（默认 false，旧配置不启用）
	FileOutputDir  string `mapstructure:"file_output_dir"`  // 大文本文件输出目录（默认 os.TempDir()/webfetch/）
	FileTTL        int    `mapstructure:"file_ttl_hours"`   // 文件保留时长（小时），默认 24
	MaxInlineLines int    `mapstructure:"max_inline_lines"` // 内联返回最大行数（默认 100）
	MaxInlineChars int    `mapstructure:"max_inline_chars"` // 内联返回最大字符数（默认 0 = 不限）
	TimeoutSec     int    `mapstructure:"timeout_sec"`      // 单次请求超时（秒），默认 30
}

// ── PDF 解析配置 ──

type PDFParserConfig struct {
	Enabled bool `mapstructure:"enabled"` // 总开关（默认 false）
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
	Enabled         *bool  `mapstructure:"enabled"`           // 缓存总开关（默认 nil = 按 storage_path 判断；显式 false 强制禁用；显式 true 强制启用）
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
	MaxSize int                          `mapstructure:"max_size"`  // 全局最大结果数（按 score 排序后截断），0 = 不限
	ShowMeta bool                        `mapstructure:"show_meta"` // 输出中是否显示引擎来源和 score（默认 true）
	Engines  map[string]SmartSearchEngine `mapstructure:"engines"`   // 按引擎名配置
}

// SmartSearchEngine 单引擎的 smartsearch 配置。
type SmartSearchEngine struct {
	MinScore float64 `mapstructure:"min_score"` // 最低相关性分数阈值，0 = 不过滤；引擎不支持 score 时忽略
	MaxSize  int     `mapstructure:"max_size"`  // 单引擎最大结果数，0 = 使用默认值 4
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
	case ModeTavily:
		return ModeTavily
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
	viper.BindEnv("llm.base_url", "LLM_BASE_URL")
	viper.BindEnv("llm.api_key", "LLM_API_KEY")
	var conf Config
	if err := viper.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("配置解析失败,%w", err)
	}

	// ── 默认值 ──

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
	// 学术引擎默认开启
	if !viper.IsSet("academic.enabled") {
		conf.Academic.Enabled = true
	}
	if !viper.IsSet("academic.bing_fallback") {
		conf.Academic.BingFallback = true
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

	// 代理：标记用户显式禁用（enabled: false），跳过自动检测
	if viper.IsSet("proxy.enabled") && !viper.GetBool("proxy.enabled") {
		conf.Proxy.autoDisabled = true
	}

	// SmartSearch 默认值
	if !viper.IsSet("smartsearch.show_meta") {
		conf.SmartSearch.ShowMeta = true // 默认显示引擎来源和 score
	}

	return &conf, nil
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
