package academic

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"websearch/pkg/antirobot"

	"github.com/PuerkitoBio/goquery"
)

// ──────────────────────────────────────────────────────────────────────────────
// Google Scholar 学术搜索（海外优先）
// ──────────────────────────────────────────────────────────────────────────────

const defaultScholarDomain = "scholar.google.com"

// URL 模板与退避基数抽为包级变量，单测可指向 httptest 服务 / 缩短等待。
var (
	scholarURLFormat = "https://%s/scholar?%s"
	scholarRetryBase = 1500 * time.Millisecond // 指数退避基数：1.5s → 3s（叠加 rand 0~0.5s）
)

// scholarUserAgents 每次请求（含重试）随机轮换的桌面浏览器 UA。
var scholarUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
}

// scholarRandIntn 抽离随机源，单测可固定序列。
var scholarRandIntn = rand.Intn

type googleScholarEngine struct {
	client *http.Client
	domain string
}

// NewGoogleScholar 创建 Google Scholar 引擎。client 为 nil 时使用默认客户端。
func NewGoogleScholar(opts antirobot.GoogleScholarOpts, client *http.Client) antirobot.Engine {
	domain := opts.Domain
	if domain == "" {
		domain = defaultScholarDomain
	}
	if client == nil {
		client = defaultHTTPClient
	}
	return &googleScholarEngine{
		client: client,
		domain: domain,
	}
}

func (e *googleScholarEngine) Name() string                    { return "google_scholar" }
func (e *googleScholarEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

func (e *googleScholarEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	start := (page - 1) * 10
	if start < 0 {
		start = 0
	}

	params := url.Values{
		"q":      {query},
		"start":  {strconv.Itoa(start)},
		"as_sdt": {"2007"},
		"as_vis": {"0"},
		"hl":     {"en"},
	}
	if year := scholarTimeRangeYear(timeRange); year > 0 {
		params.Set("as_ylo", strconv.Itoa(year))
	}

	// 重试策略（受 Searcher.execOne 默认 10s per-engine 超时约束，预算 ≤ 8s）：
	//   403/429/503 → 指数退避 1.5s→3s（+0~0.5s 抖动）换 UA 重试，最多 2 次
	//   CAPTCHA（/sorry 跳转或 gs_captcha_f 表单）不浪费重试，直接报错交上层处理
	// GS 走 dynamicProxyTransport，transport 对 429 已按 Retry-After 自动重试一次；
	// 引擎内退避发生在 transport 重试之后仍 429 的场景，两者不冲突但叠加后
	// 单引擎最多 3 次上游请求——可接受，勿再加大次数。
	for attempt := 0; ; attempt++ {
		scholarURL := fmt.Sprintf(scholarURLFormat, e.domain, params.Encode())
		req, err := http.NewRequest("GET", scholarURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", scholarUserAgents[scholarRandIntn(len(scholarUserAgents))])
		req.Header.Set("Referer", "https://scholar.google.com/")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")

		resp, err := e.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("google scholar request: %w", err)
		}

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if strings.Contains(loc, "/sorry") {
				return nil, fmt.Errorf("google scholar: access denied (CAPTCHA redirect)")
			}
			return nil, fmt.Errorf("google scholar HTTP %d", resp.StatusCode)
		}

		if resp.StatusCode == 200 {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			return parseScholarHTML(body)
		}
		resp.Body.Close()

		retryable := resp.StatusCode == 403 || resp.StatusCode == 429 || resp.StatusCode == 503
		if retryable && attempt < 2 {
			time.Sleep(scholarRetryBase<<attempt + time.Duration(rand.Int63n(int64(500*time.Millisecond))))
			continue
		}
		return nil, fmt.Errorf("google scholar HTTP %d", resp.StatusCode)
	}
}

// ── HTML 解析 ──

func parseScholarHTML(data []byte) (*antirobot.SearchResponse, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("google scholar parse: %w", err)
	}

	if doc.Find("form#gs_captcha_f").Length() > 0 {
		return nil, fmt.Errorf("google scholar: CAPTCHA detected")
	}

	var results []antirobot.Result
	doc.Find("div[data-rp]").Each(func(_ int, sel *goquery.Selection) {
		title := strings.TrimSpace(sel.Find("h3").First().Find("a").First().Text())
		if title == "" {
			return
		}

		href, _ := sel.Find("h3").First().Find("a").First().Attr("href")

		content := antirobot.CollapseSpace(strings.TrimSpace(sel.Find("div.gs_rs").Text()))

		// GS 无原生 DOI 字段：从出版商落地页 URL 提取，回退摘要内文引用
		doi := antirobot.ExtractDOI(href)
		if doi == "" {
			doi = antirobot.ExtractDOI(content)
		}

		authorsStr := sel.Find("div.gs_a").Text()
		authors, journal, _, pubDate := parseScholarMeta(authorsStr)

		citedByText := sel.Find("div.gs_fl a").FilterFunction(func(_ int, s *goquery.Selection) bool {
			h, _ := s.Attr("href")
			return strings.HasPrefix(h, "/scholar?cites=")
		}).Text()
		citedBy := parseCitedByText(citedByText)

		pdfURL := ""
		docLink := sel.Find("div.gs_or_ggsm a")
		if docLink.Length() > 0 {
			docHref, _ := docLink.First().Attr("href")
			docType := strings.TrimSpace(sel.Find("span.gs_ctg2").Text())
			if docType == "[PDF]" {
				pdfURL = docHref
			}
		}

		title = antirobot.CollapseSpace(title)
		results = append(results, antirobot.Result{
			Type:        antirobot.ResultPaper,
			Title:       title,
			URL:         href,
			Content:     content,
			Authors:     authors,
			Journal:     journal,
			PublishedAt: pubDate,
			DateSource:  antirobot.DateSourceSnippet,
			CitedBy:     citedBy,
			PDFURL:      pdfURL,
			DOI:         doi,
			Engine:      "google_scholar",
		})
	})

	return &antirobot.SearchResponse{Engine: "google_scholar", Results: results}, nil
}

func parseScholarMeta(text string) (authors string, journal string, publisher string, pubDate string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", "", ""
	}

	parts := strings.SplitN(text, " - ", 3)

	authorList := strings.Split(parts[0], ", ")
	for i := range authorList {
		authorList[i] = strings.TrimSpace(authorList[i])
	}
	authors = strings.Join(authorList, ", ")

	if len(parts) < 2 {
		return authors, "", "", ""
	}

	publisher = strings.TrimSpace(parts[len(parts)-1])

	if len(parts) < 3 {
		return authors, "", publisher, ""
	}

	middle := strings.TrimSpace(parts[1])
	commaIdx := strings.LastIndex(middle, ",")
	if commaIdx > 0 {
		journal = strings.TrimSpace(middle[:commaIdx])
		yearStr := strings.TrimSpace(middle[commaIdx+1:])
		if y, err := strconv.Atoi(yearStr); err == nil && y > 1900 && y < 2100 {
			pubDate = strconv.Itoa(y)
		}
	} else {
		if y, err := strconv.Atoi(middle); err == nil && y > 1900 && y < 2100 {
			pubDate = strconv.Itoa(y)
		}
	}

	return authors, journal, publisher, pubDate
}

func parseCitedByText(text string) int {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "Cited by")
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	n, _ := strconv.Atoi(text)
	return n
}

func scholarTimeRangeYear(tr antirobot.TimeRange) int {
	if tr == antirobot.TimeRangeNone {
		return 0
	}
	return time.Now().Year() - 1
}
