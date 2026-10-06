package mode

import (
	"testing"

	"websearch/pkg/config"
)

// TestInitBaiduWebEngine_DisabledByDefault 失效引擎默认禁用：
// 百度网页引擎（tn=json 直抓）实测被 CAPTCHA 识别（2026-09-03），
// 未显式配置 baidu.web_enabled=true 时不得参与任何模式的引擎组合。
func TestInitBaiduWebEngine_DisabledByDefault(t *testing.T) {
	if a := InitBaiduWebEngine(config.Config{}); a != nil {
		t.Fatalf("baidu web engine should be disabled by default, got %v", a)
	}
}

func TestInitBaiduWebEngine_ExplicitEnable(t *testing.T) {
	conf := config.Config{Baidu: config.BaiduConfig{WebEnabled: true}}
	if a := InitBaiduWebEngine(conf); a == nil {
		t.Fatal("baidu web engine should be enabled with baidu.web_enabled=true")
	}
}

// TestBuildEngineMode_AllDisabledEngines 验证默认配置（baidu/so360/google 禁用、bing/ddg 开关由调用方传入）
// 下 engine 模式对 nil 引擎的容错：全部为 nil 时返回 nil 并告警，不 panic。
// TestBuildEngineMode_AllDisabledEngines 验证默认配置（各引擎 nil）下 engine 模式
// 的容错：全部为 nil 时返回 nil 并告警，不 panic。
func TestBuildEngineMode_AllDisabledEngines(t *testing.T) {
	if got := BuildEngineMode(config.Config{}, nil, EngineAdapters{}); got != nil {
		t.Fatalf("expected nil primary when all engines disabled, got %v", got)
	}
}

// TestInitSo360Engine_DisabledByDefault 零 Key 新引擎默认关闭，显式开启才注册。
func TestInitSo360Engine_DisabledByDefault(t *testing.T) {
	if a := InitSo360Engine(config.Config{}); a != nil {
		t.Fatalf("so360 engine should be disabled by default, got %v", a)
	}
	conf := config.Config{So360: config.So360Config{Enabled: true}}
	if a := InitSo360Engine(conf); a == nil {
		t.Fatal("so360 engine should be enabled with so360.enabled=true")
	}
}

// TestInitWikipediaEngine_DisabledByDefault wikipedia 零 Key 新引擎默认关闭。
// 需代理跳过逻辑与 DDG 一致（ProxyResolver 返回 nil 时不注册），依赖环境代理
// 状态，不做真网断言。
func TestInitWikipediaEngine_DisabledByDefault(t *testing.T) {
	if a := InitWikipediaEngine(config.Config{}); a != nil {
		t.Fatalf("wikipedia engine should be disabled by default, got %v", a)
	}
}
