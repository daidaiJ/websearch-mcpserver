// Package wikipedia 维基百科搜索引擎——MediaWiki API（零 Key），低风险参考型来源。
// 国内出口需代理（2026-10-06 实测：直连超时，代理 200 + 正常 JSON），
// 与 DDG 同策略：无可用代理解析时跳过注册。
package wikipedia

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
	"websearch/pkg/proxy"
)

// wikipediaEndpoint 单测可指向 httptest 服务（%s 为语言版本）。
var wikipediaEndpoint = "https://%s.wikipedia.org/w/api.php"

// WikipediaOpts 维基百科引擎配置。
type WikipediaOpts struct {
	Enabled      bool
	Lang         string              // 语言版本（默认 zh）
	NumResults   int                 // 单次结果数（默认 10，API 上限 50）
	PerSec       int                 // 每秒限流（默认 1，MediaWiki 礼仪取保守值）
	PerMin       int                 // 每分钟限流（默认 30）
	ProxyResolve proxy.ProxyResolver // 代理端点动态解析函数（每次请求实时获取）
}

// NewWikipedia 创建维基百科引擎（需代理访问）。
func NewWikipedia(opts WikipediaOpts) antirobot.Engine {
	lang := strings.ToLower(strings.TrimSpace(opts.Lang))
	if lang == "" {
		lang = "zh"
	}
	numResults := opts.NumResults
	if numResults <= 0 {
		numResults = 10
	}
	if numResults > 50 {
		numResults = 50
	}
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = 1
	}
	if perMin <= 0 {
		perMin = 30
	}
	return &wikipediaEngine{
		opts:    opts,
		lang:    lang,
		num:     numResults,
		limiter: antirobot.NewRateLimiter(perSec, perMin),
		client:  proxy.NewDynamicHTTPClient(opts.ProxyResolve, 10*time.Second),
	}
}

type wikipediaEngine struct {
	opts    WikipediaOpts
	lang    string
	num     int
	limiter *antirobot.RateLimiter
	client  *http.Client
}

func (e *wikipediaEngine) Name() string                    { return "wikipedia" }
func (e *wikipediaEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

// wikiSearchResp MediaWiki list=search 响应（仅取用到的字段）。
type wikiSearchResp struct {
	Error struct {
		Info string `json:"info"`
		Code string `json:"code"`
	} `json:"error"`
	Query struct {
		Search []struct {
			Title   string `json:"title"`
			PageID  int    `json:"pageid"`
			Snippet string `json:"snippet"` // 含 <span class="searchmatch"> 高亮 HTML
		} `json:"search"`
	} `json:"query"`
}

func (e *wikipediaEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	// MediaWiki list=search 无时间过滤参数，freshness 请求退化为普通搜索
	if !e.limiter.Allow() {
		return &antirobot.SearchResponse{Engine: "wikipedia", Results: []antirobot.Result{}}, nil
	}

	// MediaWiki list=search 不支持 site:/lang:/after:/before: 等网页搜索操作符，
	// 调用方的检索语法会把这些操作符拼进 query，原样传入会被当成普通词面导致零命中。
	// 这里只剥离操作符 token，不含操作符的查询原样透传。
	rawQuery := query
	query = sanitizeWikiQuery(query)

	u := e.buildURL(query, page)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "websearch-mcpserver/1.0 (MediaWiki search engine)")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var parsed wikiSearchResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("wikipedia API 响应解析失败: %w", err)
	}
	if parsed.Error.Code != "" {
		return nil, fmt.Errorf("wikipedia API 错误: %s", parsed.Error.Info)
	}

	results := make([]antirobot.Result, 0, len(parsed.Query.Search))
	for _, item := range parsed.Query.Search {
		title := strings.TrimSpace(item.Title)
		if title == "" || item.PageID == 0 {
			continue
		}
		results = append(results, antirobot.Result{
			Type:    antirobot.ResultWeb,
			Title:   title,
			URL:     e.articleURL(title),
			Content: antirobot.CollapseSpace(stripHTML(item.Snippet)),
			Engine:  "wikipedia",
		})
	}
	if len(results) == 0 {
		if query != rawQuery {
			log.Debugf("wikipedia: %q 无结果（已剥离检索操作符，实际检索串 %q）", rawQuery, query)
		} else {
			log.Debugf("wikipedia: %q 无结果", query)
		}
	}
	return &antirobot.SearchResponse{Engine: "wikipedia", Results: results}, nil
}

// buildURL 构造 MediaWiki list=search 请求 URL；page>1 用 sroffset 翻页（每页 num 条）。
// 端点模板用 %s 占位语言版本（测试替换端点时无占位也安全，故用 Replace 而非 Sprintf）。
func (e *wikipediaEngine) buildURL(query string, page int) string {
	q := url.Values{}
	q.Set("action", "query")
	q.Set("list", "search")
	q.Set("srsearch", query)
	q.Set("format", "json")
	q.Set("srlimit", fmt.Sprintf("%d", e.num))
	if page > 1 {
		q.Set("sroffset", fmt.Sprintf("%d", (page-1)*e.num))
	}
	return strings.Replace(wikipediaEndpoint, "%s", e.lang, 1) + "?" + q.Encode()
}

// articleURL 由标题构造条目链接（空格转 wiki 惯用的下划线 + 路径转义）。
func (e *wikipediaEngine) articleURL(title string) string {
	return fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", e.lang, url.PathEscape(strings.ReplaceAll(title, " ", "_")))
}

// stripHTML 去掉 MediaWiki snippet 里的高亮 HTML 标签并解码实体（&nbsp; 等）。
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	var sb strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			sb.WriteRune(r)
		}
	}
	return htmlUnescape(sb.String())
}

// htmlUnescape 解码 snippet 中常见的 HTML 实体。
func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
	)
	return r.Replace(s)
}

// wikiUnsupportedOperator 匹配以网页搜索操作符开头的 token：
// site: / -site: / lang: / after: / before:（大小写不敏感，允许空值如裸 site:）。
// 这些操作符由调用方的检索语法带入，MediaWiki list=search 不认识，会被当成普通词面，
// 导致整条查询零命中。
var wikiUnsupportedOperator = regexp.MustCompile(`(?i)^-?(site|lang|after|before):`)

// splitWikiTokens 按空白切分查询，但双引号短语整体作为一个 token（短语内部不拆）。
func splitWikiTokens(query string) []string {
	tokens := make([]string, 0, 8)
	var cur strings.Builder
	inQuote := false
	for _, r := range query {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// sanitizeWikiQuery 剥离 MediaWiki 不支持的网页搜索操作符 token，保留自然语言词与
// 双引号短语。不含操作符的查询原样返回（不改写任何空白）；清洗后为空时回退原查询，
// 避免纯操作符查询退化成空检索。
func sanitizeWikiQuery(query string) string {
	tokens := splitWikiTokens(query)
	kept := make([]string, 0, len(tokens))
	removed := false
	for _, tok := range tokens {
		if strings.HasPrefix(tok, `"`) { // 双引号短语整体保留，不拆短语内部
			kept = append(kept, tok)
			continue
		}
		if wikiUnsupportedOperator.MatchString(tok) {
			removed = true
			continue
		}
		kept = append(kept, tok)
	}
	if !removed {
		return query
	}
	cleaned := strings.Join(kept, " ")
	if cleaned == "" {
		return query
	}
	return cleaned
}
