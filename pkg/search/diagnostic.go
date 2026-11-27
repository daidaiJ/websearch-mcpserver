package search

import (
	"context"
	"errors"
	"strings"

	"websearch/pkg/config"
	"websearch/pkg/quota"
	"websearch/pkg/search/adapter"
	"websearch/pkg/search/mode"
	"websearch/pkg/search/provider"
	"websearch/pkg/search/provider/mcphub"
)

var ErrDiagnosticSource = errors.New("diagnostic search source unavailable")

func DiagnosticSearchNeedsConfirmation(conf config.Config, id string) bool {
	return len(diagnosticProviderKeys(conf, id)) > 0 || strings.HasPrefix(id, "mcp_hub_")
}

func IsAcademicDiagnosticSource(id string) bool {
	switch id {
	case "arxiv", "crossref", "openalex", "pubmed", "europepmc", "dblp", "doaj", "semantic_scholar", "google_scholar":
		return true
	default:
		return false
	}
}

// DiagnosticSearch makes one contextual search on the selected adapter.
func DiagnosticSearch(ctx context.Context, conf config.Config, budget *quota.Service, id, query string) ([]SearchResult, error) {
	if name, ok := strings.CutPrefix(id, "mcp_hub_"); ok {
		hub, err := mcphub.New(conf.MCPHub, conf.Proxy.ProxyResolver(), nil)
		if err != nil {
			return nil, err
		}
		rows, err := hub.DiagnosticSearch(ctx, name, query)
		for _, server := range conf.MCPHub.GetServers() {
			if server.Name == name && server.IsEnabled() {
				hideDiagnosticKeys(rows, server.EffectiveKeys())
				break
			}
		}
		return rows, err
	}
	keys := diagnosticProviderKeys(conf, id)
	if IsAcademicDiagnosticSource(id) {
		engine, ok := mode.InitAcademicEngine(conf).(*adapter.AcademicAdapter)
		if !ok {
			return nil, ErrDiagnosticSource
		}
		rows, err := engine.DiagnosticSearch(ctx, id, query)
		hideDiagnosticKeys(rows, keys)
		return rows, err
	}
	var pool *provider.KeyPool
	if len(keys) > 0 {
		var err error
		pool, err = provider.NewKeyPool(keys)
		if err != nil {
			return nil, err
		}
		pool.SetSearchQuota(id, budget)
	}
	adapter := mode.NewDiagnosticSource(conf, id, pool)
	if adapter == nil {
		return nil, ErrDiagnosticSource
	}
	rows, err := adapter.SearchRawContext(ctx, query)
	hideDiagnosticKeys(rows, keys)
	return rows, err
}

func diagnosticProviderKeys(conf config.Config, id string) []string {
	switch id {
	case "baidu":
		return conf.Baidu.EffectiveSKList()
	case "tavily":
		return conf.Tavily.EffectiveSKList()
	case "exa":
		return conf.Exa.EffectiveSKList()
	case "anysearch":
		return conf.Anysearch.EffectiveSKList()
	case "doubao":
		return conf.Doubao.EffectiveSKList()
	case "yuanbao":
		return conf.Yuanbao.EffectiveSKList()
	case "tinyfish":
		return conf.TinyFish.EffectiveSKList()
	case "keenable":
		return conf.Keenable.EffectiveSKList()
	case "github":
		return conf.GitHub.EffectiveTokenList()
	case "openalex":
		if conf.Academic.OpenAlexAPIKey != "" {
			return []string{conf.Academic.OpenAlexAPIKey}
		}
	case "semantic_scholar":
		if conf.Academic.SemanticScholarAPIKey != "" {
			return []string{conf.Academic.SemanticScholarAPIKey}
		}
	}
	return nil
}

func hideDiagnosticKeys(rows []SearchResult, keys []string) {
	for i := range rows[:min(len(rows), 3)] {
		for _, key := range keys {
			if key = strings.TrimSpace(key); key != "" {
				rows[i].Title = strings.ReplaceAll(rows[i].Title, key, "[secret]")
			}
		}
	}
}
