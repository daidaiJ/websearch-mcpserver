package mode

import (
	"testing"

	"websearch/pkg/config"
	"websearch/pkg/search/adapter"
	"websearch/pkg/search/core"
	"websearch/pkg/search/provider"
)

func allAPIsDisabled() config.Config {
	disabled := false
	return config.Config{
		Baidu: config.BaiduConfig{Enabled: &disabled}, Tavily: config.TavilyConfig{Enabled: &disabled},
		Exa: config.ExaConfig{Enabled: &disabled}, Anysearch: config.AnysearchConfig{Enabled: &disabled},
		Doubao: config.DoubaoConfig{Enabled: &disabled},
		TinyFish: config.TinyFishConfig{Enabled: &disabled}, Keenable: config.KeenableConfig{Enabled: &disabled},
	}
}

func TestDisabledAPISingleModes(t *testing.T) {
	pool, err := provider.NewKeyPool([]string{"stored-test-key"})
	if err != nil {
		t.Fatal(err)
	}
	conf := allAPIsDisabled()
	builders := map[string]func(*provider.KeyPool, *adapter.BingSearchAdapter) core.SearchInf{
		"baidu": func(p *provider.KeyPool, b *adapter.BingSearchAdapter) core.SearchInf {
			return BuildBaiduMode(conf, p, nil, b)
		},
		"tavily": func(p *provider.KeyPool, b *adapter.BingSearchAdapter) core.SearchInf {
			return BuildTavilyMode(conf, p, b)
		},
		"exa": func(p *provider.KeyPool, b *adapter.BingSearchAdapter) core.SearchInf {
			return BuildExaMode(conf, p, b)
		},
		"anysearch": func(p *provider.KeyPool, b *adapter.BingSearchAdapter) core.SearchInf {
			return BuildAnysearchMode(conf, p, b)
		},
		"doubao": func(p *provider.KeyPool, b *adapter.BingSearchAdapter) core.SearchInf {
			return BuildDoubaoMode(conf, p, b)
		},
	}
	// A supplied fallback must not bypass bing.enabled either.
	bing := InitBingEngine(config.Config{Bing: config.BingConfig{Enabled: true}})
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			for _, p := range []*provider.KeyPool{nil, pool} {
				if got := build(p, bing); got != nil {
					t.Fatal("disabled API/Bing source was installed")
				}
				conf.Bing.Enabled = true
				if got := build(p, bing); got == nil || got.Name() != "bing" {
					t.Fatal("enabled Bing fallback must remain available")
				}
				conf.Bing.Enabled = false
			}
		})
	}
}

func TestMissingKeyAndDisabledBingReturnNil(t *testing.T) {
	for _, got := range []core.SearchInf{
		BuildTavilyMode(config.Config{}, nil, nil),
		BuildExaMode(config.Config{}, nil, nil),
		BuildDoubaoMode(config.Config{}, nil, nil),
	} {
		if got != nil {
			t.Fatal("unavailable mode must return a true nil, not a typed nil adapter")
		}
	}
}

func TestPoolAndHybridSourceControls(t *testing.T) {
	pool, err := provider.NewKeyPool([]string{"stored-test-key"})
	if err != nil {
		t.Fatal(err)
	}
	conf := allAPIsDisabled()
	conf.Apipool.Engines = []string{"anysearch", "baidu", "tavily", "exa", "doubao"}
	if got := BuildHybridMode(conf, pool, pool, pool, pool, pool, pool, pool, EngineAdapters{}, nil); got != nil {
		t.Fatal("hybrid must not install disabled APIs even with stored keys")
	}
	if got := BuildHybridMode(conf, nil, nil, nil, nil, nil, nil, nil, EngineAdapters{}, nil); got != nil {
		t.Fatal("hybrid must not re-enable anonymous AnySearch")
	}
	if got := BuildApipoolMode(conf, pool, pool, pool, pool, pool, pool, pool, nil); got != nil {
		t.Fatal("apipool must not install disabled APIs even with stored keys")
	}
	for _, name := range conf.Apipool.Engines {
		t.Run(name, func(t *testing.T) {
			conf.Apipool.Engines = []string{name}
			if got := BuildApipoolMode(conf, pool, pool, pool, pool, pool, pool, pool, nil); got != nil {
				t.Fatalf("disabled %s was installed", name)
			}
		})
	}
	conf.Anysearch.Enabled = nil
	conf.Apipool.Engines = []string{"anysearch"}
	if BuildAnysearchMode(conf, nil, nil) == nil ||
		BuildApipoolMode(conf, nil, nil, nil, nil, nil, nil, nil, nil) == nil ||
		BuildHybridMode(conf, nil, nil, nil, nil, nil, nil, nil, EngineAdapters{}, nil) == nil {
		t.Fatal("omitted AnySearch switch must preserve anonymous mode")
	}
	conf.Baidu.WebEnabled = true
	web := InitBaiduWebEngine(conf)
	if got := BuildBaiduMode(conf, pool, web, nil); got == nil || got != web {
		t.Fatal("Baidu API switch must not disable its independent web source")
	}
}
