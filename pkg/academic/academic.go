package academic

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"websearch/pkg/antirobot"
)

// BuildAcademic 根据配置创建学术搜索引擎列表。
// proxyEndpoint 非空时，仅对 RegionInternational 引擎启用代理。
func BuildAcademic(opts struct {
	Network         antirobot.NetworkRegion
	Arxiv           antirobot.ArxivOpts
	Crossref        antirobot.CrossrefOpts
	OpenAlex        antirobot.OpenAlexOpts
	SemanticScholar antirobot.SemanticScholarOpts
	PubMed          antirobot.PubMedOpts
	GoogleScholar   antirobot.GoogleScholarOpts
	Proxy           string // 代理端点，如 http://127.0.0.1:7897
}) []antirobot.Engine {
	var engines []antirobot.Engine

	// 国内引擎：不走代理
	if opts.Crossref.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewCrossref(opts.Crossref, nil))
	}
	if opts.OpenAlex.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewOpenAlex(opts.OpenAlex, nil))
	}
	if opts.PubMed.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewPubMed(opts.PubMed, nil))
	}

	// 国内引擎：arXiv 国内可直连
	if opts.Arxiv.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewArxiv(opts.Arxiv, nil))
	}

	// 国际引擎：Semantic Scholar 和 Google Scholar 仅在配置代理时启用（国内无代理不可达）
	if opts.Proxy != "" {
		intlClient := buildProxyClient(opts.Proxy)
		if opts.SemanticScholar.Enabled && opts.Network >= antirobot.RegionInternational {
			engines = append(engines, NewSemanticScholar(opts.SemanticScholar, intlClient))
		}
		if opts.GoogleScholar.Enabled && opts.Network >= antirobot.RegionInternational {
			engines = append(engines, NewGoogleScholar(opts.GoogleScholar, intlClient))
		}
	}

	return engines
}

// buildProxyClient 创建带代理的 HTTP 客户端，proxy 为空时返回 nil（使用默认客户端）。
func buildProxyClient(proxy string) *http.Client {
	if proxy == "" {
		return nil
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return nil
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}
}
