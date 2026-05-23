package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var configDir string

const (
	ModeBaidu  = "baidu"
	ModeTavily = "tavily"
	ModeHybrid = "hybrid"
	ModeEngine = "engine" // 纯引擎模式，无需 API Key
)

type LogConfig struct {
	MaxSize int `mapstructure:"max_size"` // 单个日志文件最大大小（MB），默认 1
	MaxAge  int `mapstructure:"max_age"`  // 日志保留天数，默认 1
}

type Config struct {
	Port          int              `mapstructure:"port"`
	LogLevel      string           `mapstructure:"log_level"`
	Mode          string           `mapstructure:"mode"`
	BlackListHost []string         `mapstructure:"black_list_host"`
	BaiduSK       string           `mapstructure:"baidu_sk"`
	TavilySk      string           `mapstructure:"tavily_sk"`
	LLM           LLMConfig        `mapstructure:"llm"`
	Jina          JinaConfig       `mapstructure:"jina"`
	Cache         CacheConfig      `mapstructure:"cache"`
	Log           LogConfig        `mapstructure:"log"`
	Bing          BingEngineConfig `mapstructure:"bing"`
}

type LLMConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
	ModelId string `mapstructure:"model_id"`
}

type CacheConfig struct {
	StoragePath     string `mapstructure:"storage_path"`     // SQLite 数据库文件存储路径
	CleanupInterval int    `mapstructure:"cleanup_interval"` // 清理间隔（分钟），默认30分钟，最大360分钟
}

type JinaConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"` // 默认 https://r.jina.ai
}

// BingEngineConfig Bing 引擎配置。
// 默认所有引擎开启，通过 disable_* 子字段单独关闭。
type BingEngineConfig struct {
	Enabled      bool     `mapstructure:"enabled"`        // 总开关（默认 true）
	Academic     bool     `mapstructure:"academic"`       // 学术引擎总开关（默认 true）
	BingFallback bool     `mapstructure:"bing_fallback"`  // 学术搜索时是否用 Bing 兜底（默认 true）
	Network      string   `mapstructure:"network"`        // 网络区域: china / international（默认 china）
	Blocked      []string `mapstructure:"blocked"`        // Bing 屏蔽域名
	PerSec       int      `mapstructure:"per_sec"`        // Bing 每秒限流（默认 1）
	PerMin       int      `mapstructure:"per_min"`        // Bing 每分钟限流（默认 20）

	// 各引擎独立禁用开关（默认 false = 启用）
	DisableArxiv           bool `mapstructure:"disable_arxiv"`
	DisableCrossref        bool `mapstructure:"disable_crossref"`
	DisableOpenAlex        bool `mapstructure:"disable_openalex"`
	DisableSemanticScholar bool `mapstructure:"disable_semantic_scholar"`
}

// IsInternational 返回是否为海外网络环境。
func (c BingEngineConfig) IsInternational() bool {
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

func Load(configPath string) (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	if configPath != "" {
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
	viper.BindEnv("baidu_sk", "BAIDU_SK")
	viper.BindEnv("tavily_sk", "TAVILY_SK")
	viper.BindEnv("llm.base_url", "LLM_BASE_URL")
	viper.BindEnv("llm.api_key", "LLM_API_KEY")
	var conf Config
	if err := viper.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("配置解析失败,%w", err)
	}

	// 日志默认值
	if conf.Log.MaxSize <= 0 {
		conf.Log.MaxSize = 1
	}
	if conf.Log.MaxAge <= 0 {
		conf.Log.MaxAge = 1
	}

	// Bing 引擎默认值：未显式配置 enabled 时默认 true
	if viper.IsSet("bing.enabled") {
		// 用户显式配置了，保留原值
	} else {
		conf.Bing.Enabled = true
	}
	if viper.IsSet("bing.academic") {
		// 用户显式配置了，保留原值
	} else {
		conf.Bing.Academic = true
	}
	// bing_fallback 默认 true：学术搜索时用 Bing 兜底
	if viper.IsSet("bing.bing_fallback") {
		// 用户显式配置了，保留原值
	} else {
		conf.Bing.BingFallback = true
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
