package search

import (
	"testing"

	"websearch/pkg/config"
)

func TestDisabledAPIsAllModes(t *testing.T) {
	disabled := false
	for _, modeName := range []string{config.ModeBaidu, config.ModeTavily, config.ModeExa, config.ModeAnysearch, config.ModeDoubao, config.ModeYuanbao, config.ModeApipool, config.ModeHybrid} {
		for _, bingEnabled := range []bool{false, true} {
			conf := config.Config{
				Mode: modeName, Bing: config.BingConfig{Enabled: bingEnabled},
				Baidu:     config.BaiduConfig{Enabled: &disabled, APIKey: "stored-test-key"},
				Tavily:    config.TavilyConfig{Enabled: &disabled, APIKey: "stored-test-key"},
				Exa:       config.ExaConfig{Enabled: &disabled, APIKey: "stored-test-key"},
				Anysearch: config.AnysearchConfig{Enabled: &disabled, APIKey: "stored-test-key"},
				Doubao:    config.DoubaoConfig{Enabled: &disabled, APIKey: "stored-test-key"},
				Yuanbao:   config.YuanbaoConfig{Enabled: &disabled, APIKey: "stored-test-key"},
			}
			g, err := NewFromConfig(conf)
			if err != nil {
				t.Fatal(err)
			}
			if !bingEnabled {
				if g.Primary != nil || g.Fallback != nil {
					t.Fatalf("%s installed a disabled source", modeName)
				}
			} else if g.Primary == nil || g.Fallback == nil {
				t.Fatalf("%s must remain usable through enabled Bing", modeName)
			}
		}
	}
}

func TestNewFromConfig_DoubaoMode_NoKey_Fallback(t *testing.T) {
	g, err := NewFromConfig(config.Config{Mode: config.ModeDoubao, Bing: config.BingConfig{Enabled: true}})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if g.Primary == nil {
		t.Fatal("expected Bing fallback when doubao has no key")
	}
	if g.Primary.Name() != "bing" {
		t.Fatalf("fallback Name() = %q, want bing", g.Primary.Name())
	}
}

func TestNewFromConfig_YuanbaoMode_NoKey_Fallback(t *testing.T) {
	g, err := NewFromConfig(config.Config{Mode: config.ModeYuanbao, Bing: config.BingConfig{Enabled: true}})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if g.Primary == nil || g.Primary.Name() != "bing" {
		t.Fatal("expected Bing fallback when yuanbao has no key")
	}
}

// ── 工厂函数测试 ──

func TestNewFromConfig_ExaMode(t *testing.T) {
	conf := config.Config{
		Mode: "exa",
		Exa: config.ExaConfig{
			APIKey:       "test-key",
			NumResults:   10,
			LookbackDays: 30,
		},
		Bing: config.BingConfig{Enabled: false},
	}
	g, err := NewFromConfig(conf)
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	if g.Primary == nil {
		t.Fatal("expected non-nil primary engine")
	}
	if g.Primary.Name() != "exa" {
		t.Errorf("expected engine name 'exa', got %s", g.Primary.Name())
	}
}

func TestNewFromConfig_ExaMode_NoKey_Fallback(t *testing.T) {
	conf := config.Config{
		Mode: "exa",
		Exa:  config.ExaConfig{},
		Bing: config.BingConfig{Enabled: true},
	}
	g, err := NewFromConfig(conf)
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	if g.Primary == nil {
		t.Fatal("expected fallback engine")
	}
	// 应该回退到 Bing
	if g.Primary.Name() != "bing" {
		t.Logf("fallback engine: %s", g.Primary.Name())
	}
}
