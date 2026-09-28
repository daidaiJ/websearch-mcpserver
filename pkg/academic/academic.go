package academic

import (
	"net/http"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/proxy"
)

// defaultHTTPClient 是各引擎未注入 client 时的共享兜底（测试/独立构造场景）；
// 生产路径由 BuildAcademic 统一注入 direct/intl 动态代理客户端。
var defaultHTTPClient = &http.Client{Timeout: 15 * time.Second}

// BuildAcademic 根据配置创建学术搜索引擎列表。
// ProxyResolve 非 nil 时，对 RegionInternational 引擎使用动态代理。
func BuildAcademic(opts struct {
	Network         antirobot.NetworkRegion
	Arxiv           antirobot.ArxivOpts
	Crossref        antirobot.CrossrefOpts
	OpenAlex        antirobot.OpenAlexOpts
	SemanticScholar antirobot.SemanticScholarOpts
	PubMed          antirobot.PubMedOpts
	GoogleScholar   antirobot.GoogleScholarOpts
	EuropePMC       antirobot.EuropePMCOpts
	DBLP            antirobot.DBLPOpts
	DOAJ            antirobot.DOAJOpts
	ProxyResolve    proxy.ProxyResolver // 代理端点动态解析函数
}) []antirobot.Engine {
	var engines []antirobot.Engine

	// 国内引擎：不走代理，但带 429 Retry-After 自动重试
	directClient := proxy.NewDynamicHTTPClient(nil, 15*time.Second)

	if opts.Crossref.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewCrossref(opts.Crossref, directClient))
	}
	if opts.OpenAlex.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewOpenAlex(opts.OpenAlex, directClient))
	}
	if opts.PubMed.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewPubMed(opts.PubMed, directClient))
	}
	if opts.EuropePMC.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewEuropePMC(opts.EuropePMC, directClient))
	}
	if opts.DBLP.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewDBLP(opts.DBLP, directClient))
	}
	if opts.DOAJ.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewDOAJ(opts.DOAJ, directClient))
	}

	// 国内引擎：arXiv 国内可直连
	if opts.Arxiv.Enabled && opts.Network >= antirobot.RegionChina {
		engines = append(engines, NewArxiv(opts.Arxiv, directClient))
	}

	// 国际引擎：Semantic Scholar 和 Google Scholar 通过动态代理客户端访问
	if opts.ProxyResolve != nil {
		intlClient := proxy.NewDynamicHTTPClient(opts.ProxyResolve, 15*time.Second)
		if opts.SemanticScholar.Enabled && opts.Network >= antirobot.RegionInternational {
			engines = append(engines, NewSemanticScholar(opts.SemanticScholar, intlClient))
		}
		if opts.GoogleScholar.Enabled && opts.Network >= antirobot.RegionInternational {
			engines = append(engines, NewGoogleScholar(opts.GoogleScholar, intlClient))
		}
	}

	return engines
}
