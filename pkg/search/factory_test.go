package search

import (
	"testing"

	"websearch/pkg/config"
)

// TestInitBaiduWebEngine_DisabledByDefault 失效引擎默认禁用：
// 百度网页引擎（tn=json 直抓）实测被 CAPTCHA 识别（2026-09-03），
// 未显式配置 baidu.web_enabled=true 时不得参与任何模式的引擎组合。
func TestInitBaiduWebEngine_DisabledByDefault(t *testing.T) {
	if a := initBaiduWebEngine(config.Config{}); a != nil {
		t.Fatalf("baidu web engine should be disabled by default, got %v", a)
	}
}

func TestInitBaiduWebEngine_ExplicitEnable(t *testing.T) {
	conf := config.Config{Baidu: config.BaiduConfig{WebEnabled: true}}
	if a := initBaiduWebEngine(conf); a == nil {
		t.Fatal("baidu web engine should be enabled with baidu.web_enabled=true")
	}
}

// TestBuildEngineMode_AllDisabledEngines 验证默认配置（baidu/google 禁用、bing/ddg 开关由调用方传入）
// 下 engine 模式对 nil 引擎的容错：全部为 nil 时返回 nil 并告警，不 panic。
func TestBuildEngineMode_AllDisabledEngines(t *testing.T) {
	g := &SearchGroup{conf: config.Config{}}
	if got := buildEngineMode(g, nil, nil, nil); got != nil {
		t.Fatalf("expected nil primary when all engines disabled, got %v", got)
	}
}
