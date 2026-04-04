package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	ModeBaidu  = "baidu"
	ModeTavily = "tavily"
	ModeHybrid = "hybrid"
)

type Config struct {
	Port          int         `mapstructure:"port"`
	LogLevel      string      `mapstructure:"log_level"`
	Mode          string      `mapstructure:"mode"`
	BlackListHost []string    `mapstructure:"black_list_host"`
	BaiduSK       string      `mapstructure:"baidu_sk"`
	TavilySk      string      `mapstructure:"tavily_sk"`
	LLM           LLMConfig   `mapstructure:"llm"`
	Cache         CacheConfig `mapstructure:"cache"`
}

type LLMConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
}

type CacheConfig struct {
	StoragePath     string `mapstructure:"storage_path"`     // SQLite 数据库文件存储路径
	CleanupInterval int    `mapstructure:"cleanup_interval"` // 清理间隔（分钟），默认30分钟，最大360分钟
}

func (c Config) LLMEnabled() bool {
	return c.LLM.BaseURL != "" && c.LLM.APIKey != ""
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
	case ModeBaidu, "":
		return ModeBaidu
	default:
		return ModeBaidu
	}
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		return nil, fmt.Errorf("read config file failed: %w", err)
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
	return &conf, nil
}
