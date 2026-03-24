package config

import (
	"fmt"

	"github.com/spf13/viper"
)

const (
	Baidu = iota
	Tavily
	Hybird
)

type Config struct {
	Port          int      `mapstructure:"port"`
	LogLevel      string   `mapstructure:"log_level"`
	Mode          string   `mapstructure:"mode"`
	BlackListHost []string `mapstructure:"black_list_host"`
	BaiduSK       string   `mapstructure:"baidu_sk"`
	TavilySk      string   `mapstructure:"tavily_sk"`
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
	var conf Config
	if err := viper.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("配置解析失败,%w", err)
	}
	return &conf, nil
}
