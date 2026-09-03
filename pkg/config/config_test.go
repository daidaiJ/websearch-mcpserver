package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func boolPtr(b bool) *bool { return &b }

func TestDefault(t *testing.T) {
	conf := Default()
	if conf.GetMode() != ModeEngine {
		t.Errorf("Default mode = %q, want %s", conf.GetMode(), ModeEngine)
	}
	if !conf.Bing.Enabled {
		t.Error("Default Bing should be enabled")
	}
	if !conf.Academic.Enabled {
		t.Error("Default Academic should be enabled")
	}
	if conf.Port != 8338 {
		t.Errorf("Default Port = %d, want 8338", conf.Port)
	}
	if conf.CleanFetch.Enabled {
		t.Error("Default CleanFetch should be disabled")
	}
	if conf.PDFParser.Enabled {
		t.Error("Default PDFParser should be disabled")
	}
	if conf.Network != "china" {
		t.Errorf("Default Network = %q", conf.Network)
	}
	if conf.SmartSearch.Enhance == nil || !*conf.SmartSearch.Enhance {
		t.Error("Default smartsearch.enhance should be true")
	}
	// 失效引擎（被反爬识别）默认禁用
	if conf.Google.Enabled {
		t.Error("Default Google should be disabled (anti-bot)")
	}
	if conf.Baidu.WebEnabled {
		t.Error("Default Baidu web engine should be disabled (anti-bot)")
	}
}

func TestDefault_AppliesEnv(t *testing.T) {
	t.Setenv("BAIDU_SK", "sk-test")
	t.Setenv("TAVILY_SK", "tv-test")
	t.Setenv("EXA_API_KEY", "exa-test")
	t.Setenv("LLM_BASE_URL", "http://llm.local")
	t.Setenv("LLM_API_KEY", "llm-key")
	t.Setenv("MINERU_TOKEN", "mu-test")

	conf := Default()
	if conf.Baidu.APIKey != "sk-test" {
		t.Errorf("Baidu.APIKey = %q", conf.Baidu.APIKey)
	}
	if conf.Tavily.APIKey != "tv-test" {
		t.Errorf("Tavily.APIKey = %q", conf.Tavily.APIKey)
	}
	if conf.Exa.APIKey != "exa-test" {
		t.Errorf("Exa.APIKey = %q", conf.Exa.APIKey)
	}
	if conf.LLM.BaseURL != "http://llm.local" || conf.LLM.APIKey != "llm-key" {
		t.Errorf("LLM = %+v", conf.LLM)
	}
	if conf.PDFParser.MinerUToken != "mu-test" {
		t.Errorf("MinerUToken = %q", conf.PDFParser.MinerUToken)
	}
	if conf.LLMEnabled() {
		t.Error("LLMEnabled should be false without model_id")
	}
}

func TestDefault_MCPStateless(t *testing.T) {
	if Default().MCPStateless {
		t.Error("Default MCPStateless should be false (session mode, backward compatible)")
	}
}

func TestLoadOrDefault_MCPStateless(t *testing.T) {
	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("mode: engine\nmcp_stateless: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	conf, err := LoadOrDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if !conf.MCPStateless {
		t.Error("MCPStateless = false, want true (parsed from yaml)")
	}
}

func TestLoadOrDefault_MissingExplicitFile(t *testing.T) {	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	_, err := LoadOrDefault(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for missing explicit config file")
	}
}

func TestLoadOrDefault_ReadsFile(t *testing.T) {
	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("mode: engine\nport: 9001\n"), 0644); err != nil {
		t.Fatal(err)
	}
	conf, err := LoadOrDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Port != 9001 {
		t.Errorf("Port = %d, want 9001", conf.Port)
	}
	if conf.GetMode() != ModeEngine {
		t.Errorf("mode = %q", conf.GetMode())
	}
}

func TestLoadOrDefault_WEBSEARCH_CONFIG(t *testing.T) {
	viper.Reset()
	path := filepath.Join(t.TempDir(), "from-env.yaml")
	if err := os.WriteFile(path, []byte("mode: engine\nport: 9002\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEBSEARCH_CONFIG", path)
	conf, err := LoadOrDefault(filepath.Join(t.TempDir(), "ignored.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if conf.Port != 9002 {
		t.Errorf("Port = %d, want 9002 (WEBSEARCH_CONFIG should win)", conf.Port)
	}
}

func TestLoadOrDefault_InvalidYAML(t *testing.T) {
	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(":\n  - broken"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadOrDefault(path)
	if err == nil {
		t.Fatal("expected parse error, not Default()")
	}
}

func TestLoadOrDefault_FallsBackToDefault(t *testing.T) {
	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Chdir(t.TempDir())
	conf, err := LoadOrDefault("")
	if err != nil {
		t.Fatal(err)
	}
	if conf.GetMode() != ModeEngine {
		t.Errorf("fallback mode = %q", conf.GetMode())
	}
}

func TestExplicitConfigPath(t *testing.T) {
	t.Setenv("WEBSEARCH_CONFIG", "")
	if got := explicitConfigPath("a.yaml"); got != "a.yaml" {
		t.Errorf("got %q", got)
	}
	t.Setenv("WEBSEARCH_CONFIG", "/env/config.yaml")
	if got := explicitConfigPath("a.yaml"); got != "/env/config.yaml" {
		t.Errorf("env should win, got %q", got)
	}
}

func TestCacheEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled *bool
		path    string
		want    bool
	}{
		{
			name:    "nil enabled, empty path -> disabled",
			enabled: nil,
			path:    "",
			want:    false,
		},
		{
			name:    "nil enabled, non-empty path -> enabled (backward compat)",
			enabled: nil,
			path:    "/tmp/cache.db",
			want:    true,
		},
		{
			name:    "explicit true, non-empty path -> enabled",
			enabled: boolPtr(true),
			path:    "/tmp/cache.db",
			want:    true,
		},
		{
			name:    "explicit true, empty path -> enabled",
			enabled: boolPtr(true),
			path:    "",
			want:    true,
		},
		{
			name:    "explicit false, non-empty path -> disabled",
			enabled: boolPtr(false),
			path:    "/tmp/cache.db",
			want:    false,
		},
		{
			name:    "explicit false, empty path -> disabled",
			enabled: boolPtr(false),
			path:    "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := Config{
				Cache: CacheConfig{
					Enabled:     tt.enabled,
					StoragePath: tt.path,
				},
			}
			got := conf.CacheEnabled()
			if got != tt.want {
				t.Errorf("CacheEnabled() = %v, want %v (enabled=%v, path=%q)",
					got, tt.want, tt.enabled, tt.path)
			}
		})
	}
}

func TestMinerUEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PDFParserConfig
		want    bool
		wantOCR bool
	}{
		{"disabled", PDFParserConfig{}, false, false},
		{"enabled only", PDFParserConfig{Enabled: true}, false, false},
		{"token only", PDFParserConfig{MinerUToken: "tok"}, true, false},
		{"ocr only", PDFParserConfig{MinerUOcr: true}, true, true},
		{"token and ocr", PDFParserConfig{MinerUToken: "tok", MinerUOcr: true}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.MinerUEnabled(); got != tt.want {
				t.Errorf("MinerUEnabled() = %v, want %v", got, tt.want)
			}
			if got := tt.cfg.MinerUOCREnabled(); got != tt.wantOCR {
				t.Errorf("MinerUOCREnabled() = %v, want %v", got, tt.wantOCR)
			}
		})
	}
}

func TestExampleConfigIsYAML(t *testing.T) {
	if len(ExampleConfig) == 0 {
		t.Fatal("ExampleConfig is empty")
	}
	if !strings.Contains(string(ExampleConfig), "mode:") {
		t.Error("ExampleConfig should contain mode")
	}
}

func TestDefault_HostAndAuthToken(t *testing.T) {
	conf := Default()
	if conf.Host != "127.0.0.1" {
		t.Errorf("Default Host = %q, want 127.0.0.1", conf.Host)
	}
	if conf.AuthToken != "" {
		t.Errorf("Default AuthToken = %q, want empty", conf.AuthToken)
	}
}

func TestLoad_AppliesKnownEnv(t *testing.T) {
	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Setenv("TAVILY_SK", "tv-env")
	t.Setenv("BAIDU_SK", "bd-env")
	t.Setenv("MINERU_TOKEN", "mu-env")
	t.Setenv("WEBSEARCH_TOKEN", "ws-token")

	// 精简 yaml：缺 tavily.api_key 等字段，env 必须回填
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8338\nmode: engine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	conf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Tavily.APIKey != "tv-env" {
		t.Errorf("Tavily.APIKey = %q, want tv-env (env backfill)", conf.Tavily.APIKey)
	}
	if conf.Baidu.APIKey != "bd-env" {
		t.Errorf("Baidu.APIKey = %q, want bd-env", conf.Baidu.APIKey)
	}
	if conf.PDFParser.MinerUToken != "mu-env" {
		t.Errorf("MinerUToken = %q, want mu-env", conf.PDFParser.MinerUToken)
	}
	if conf.AuthToken != "ws-token" {
		t.Errorf("AuthToken = %q, want ws-token", conf.AuthToken)
	}
	if conf.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1 default", conf.Host)
	}
}

func TestLoad_YAMLValueOverriddenByEnv(t *testing.T) {
	viper.Reset()
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Setenv("TAVILY_SK", "tv-env")

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("tavily:\n  api_key: tv-yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	conf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Tavily.APIKey != "tv-env" {
		t.Errorf("Tavily.APIKey = %q, want tv-env (env wins over yaml)", conf.Tavily.APIKey)
	}
}

func TestEnsureExampleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	created, err := EnsureExampleFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("first call should create the file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mode:") {
		t.Error("written file should contain mode:")
	}

	// 已存在 → 不覆盖
	modified := []byte("# user edited\nmode: engine\n")
	if err := os.WriteFile(path, modified, 0644); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureExampleFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second call should not create/overwrite")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(modified) {
		t.Error("existing file content must not be overwritten")
	}
}
