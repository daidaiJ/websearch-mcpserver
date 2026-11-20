package mode

import (
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search/provider/mcphub"
)

// TestInitBaiduWebEngine_DisabledByDefault 失效引擎默认禁用：
// 百度网页引擎（tn=json 直抓）实测被 CAPTCHA 识别（2026-09-03），
// 未显式配置 baidu.web_enabled=true 时不得参与任何模式的引擎组合。
func TestInitBaiduWebEngine_DisabledByDefault(t *testing.T) {
	if a := InitBaiduWebEngine(config.Config{}); a != nil {
		t.Fatalf("baidu web engine should be disabled by default, got %v", a)
	}
}

// TestBuildMCPHubMode 验证 mcphub 模式能装配内置 preset，并为未知 server 返回 nil。
func TestBuildMCPHubMode(t *testing.T) {
	conf := config.Default()
	conf.MCPHub.Servers = []config.MCPHubServerConfig{{Name: "parallel"}}
	if got := BuildMCPHubMode(*conf, EngineAdapters{}, nil); got == nil {
		t.Fatal("mcphub should build with the parallel preset")
	}
	conf.MCPHub.Servers = []config.MCPHubServerConfig{{Name: "custom"}}
	if got := BuildMCPHubMode(*conf, EngineAdapters{}, nil); got != nil {
		t.Fatal("unknown MCP presets must not be accepted")
	}
}

func TestBuildMCPHubModeSkipsMissingFallbackEngines(t *testing.T) {
	conf := config.Default()
	conf.MCPHub.Servers = []config.MCPHubServerConfig{{Name: "parallel"}}
	got := BuildMCPHubMode(*conf, EngineAdapters{}, nil)
	hub, ok := got.(*mcphub.Hub)
	if !ok {
		t.Fatalf("expected *mcphub.Hub, got %T", got)
	}
	for _, name := range hub.FallbackNames() {
		if name == "so360" {
			t.Fatalf("disabled so360 must not be registered: fallback=%v rotation=%v", hub.FallbackNames(), hub.RotationNames())
		}
	}
	for _, name := range hub.RotationNames() {
		if name == "so360" {
			t.Fatalf("disabled so360 must not be registered: rotation=%v", hub.RotationNames())
		}
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

// TestInitSogouAndMojeekEngine_DisabledByDefault 零 Key 新引擎默认关闭，显式开启才注册。
func TestInitSogouAndMojeekEngine_DisabledByDefault(t *testing.T) {
	if a := InitSogouEngine(config.Config{}); a != nil {
		t.Fatalf("sogou engine should be disabled by default, got %v", a)
	}
	if a := InitSogouEngine(config.Config{Sogou: config.SogouConfig{Enabled: true}}); a == nil {
		t.Fatal("sogou engine should be enabled with sogou.enabled=true")
	}
	if a := InitMojeekEngine(config.Config{}); a != nil {
		t.Fatalf("mojeek engine should be disabled by default, got %v", a)
	}
	if a := InitMojeekEngine(config.Config{Mojeek: config.MojeekConfig{Enabled: true}}); a == nil {
		t.Fatal("mojeek engine should be enabled with mojeek.enabled=true")
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

// TestBuildApipoolModeMCPHubFallback 后备补充源：mcphub_fallback 打开时把托管
// MCP 级联追加在已配置供应商之后；mcp_hub 关闭时不注入。
func TestBuildApipoolModeMCPHubFallback(t *testing.T) {
	conf := config.Default()
	conf.Apipool.Engines = []string{"anysearch"}
	conf.Apipool.MCPHubFallback = true
	conf.MCPHub.Servers = []config.MCPHubServerConfig{{Name: "parallel"}}

	got := BuildApipoolMode(*conf, ProviderPools{}, nil)
	ap, ok := got.(interface{ ProviderNames() []string })
	if !ok {
		t.Fatalf("expected apipool engine, got %T", got)
	}
	names := ap.ProviderNames()
	want := []string{"anysearch", "mcphub"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("providers = %v, want %v", names, want)
	}

	disabled := false
	conf.MCPHub.Enabled = &disabled
	got = BuildApipoolMode(*conf, ProviderPools{}, nil)
	ap, ok = got.(interface{ ProviderNames() []string })
	if !ok {
		t.Fatalf("expected apipool engine, got %T", got)
	}
	for _, name := range ap.ProviderNames() {
		if name == "mcphub" {
			t.Fatalf("disabled mcp_hub must not be injected: %v", ap.ProviderNames())
		}
	}
}

// TestBuildApipoolMCPHubDedupesSharedSources 同一来源（anysearch/exa）已在
// apipool 里启用时，mcphub 不再重复请求，避免同一出口 IP 被两条链路叠加限流。
func TestBuildApipoolMCPHubDedupesSharedSources(t *testing.T) {
	conf := config.Default()
	conf.MCPHub.Servers = []config.MCPHubServerConfig{{Name: "parallel"}, {Name: "anysearch"}, {Name: "exa"}}

	got, err := buildApipoolMCPHub(*conf, map[string]bool{"anysearch": true, "exa": true})
	if err != nil {
		t.Fatalf("build hub: %v", err)
	}
	hub, ok := got.(*mcphub.Hub)
	if !ok {
		t.Fatalf("expected *mcphub.Hub, got %T", got)
	}
	for _, name := range hub.RotationNames() {
		if name == "mcp_hub_anysearch" || name == "mcp_hub_exa" {
			t.Fatalf("shared sources must be deduped, rotation=%v", hub.RotationNames())
		}
	}
	if len(hub.RotationNames()) != 1 || hub.RotationNames()[0] != "mcp_hub_parallel" {
		t.Fatalf("rotation = %v, want only parallel", hub.RotationNames())
	}

	disabled := false
	conf.Apipool.MCPHubDedupe = &disabled
	got, err = buildApipoolMCPHub(*conf, map[string]bool{"anysearch": true, "exa": true})
	if err != nil {
		t.Fatalf("build hub without dedupe: %v", err)
	}
	hub = got.(*mcphub.Hub)
	if len(hub.RotationNames()) != 3 {
		t.Fatalf("dedupe off must keep all servers, rotation=%v", hub.RotationNames())
	}
}
