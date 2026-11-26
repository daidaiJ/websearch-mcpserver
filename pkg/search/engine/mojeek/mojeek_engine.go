// Package mojeek Mojeek 网页搜索（mojeek.com）——独立英文索引，HTML 抓取，零 Key。
// 页面可能返回 altcha PoW 挑战；本实现按上游 searxng/searx/engines/mojeek.py
// 的流程求解（PBKDF2/SHA-256），成功后缓存 chllg cookie。
package mojeek

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"websearch/pkg/admission"
	"websearch/pkg/antirobot"
	webclient "websearch/pkg/client"

	"github.com/PuerkitoBio/goquery"
)

const (
	mojeekBaseURL  = "https://www.mojeek.com"
	mojeekTokenTTL = 30 * time.Minute
	mojeekMaxTries = 1000
)

var mojeekUAs = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

const mojeekMaxBackoff = 60 * time.Second

var (
	mojeekBaseDelay = 500 * time.Millisecond
	mojeekJitter    = 800 * time.Millisecond
)

type mojeekEngine struct {
	opts     MojeekOpts
	baseURL  string // 测试可覆写
	limiter  *antirobot.RateLimiter
	client   *http.Client
	tokenMu  sync.Mutex
	token    string
	tokenExp time.Time

	mu          sync.Mutex
	backoff     time.Duration
	consecFails int
	stateEpoch  uint64
}

type altchaParameters struct {
	Algorithm string `json:"algorithm"`
	Cost      int    `json:"cost"`
	KeyLength int    `json:"keyLength"`
	KeyPrefix string `json:"keyPrefix"`
	Nonce     string `json:"nonce"`
	Salt      string `json:"salt"`
}

type altchaChallenge struct {
	Parameters altchaParameters `json:"parameters"`
	Signature  string           `json:"signature"`
}

type altchaAnswer struct {
	Counter    int    `json:"counter"`
	DerivedKey string `json:"derivedKey"`
	Time       int    `json:"time"`
}

type altchaSolution struct {
	Challenge json.RawMessage `json:"challenge"`
	Solution  altchaAnswer    `json:"solution"`
}

func (e *mojeekEngine) Name() string                    { return "mojeek" }
func (e *mojeekEngine) Region() antirobot.NetworkRegion { return antirobot.RegionInternational }

func (e *mojeekEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	return e.SearchContext(context.Background(), query, page, timeRange)
}

func (e *mojeekEngine) SearchContext(ctx context.Context, query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !e.limiter.Allow() {
		return antirobot.LocalLimitSkipped("mojeek")
	}

	gen := e.stateGen()
	if err := e.preDelay(ctx); err != nil {
		return nil, err
	}

	var token string
	if !admission.IsDiagnostic(ctx) {
		var err error
		token, err = e.ensureToken(ctx)
		if err != nil {
			if ctx.Err() == nil {
				e.recordFail()
			}
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", e.buildURL(query, page, timeRange), nil)
	if err != nil {
		return nil, err
	}
	e.setHeaders(req, token)

	resp, err := webclient.Do(e.client, req)
	if err != nil {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, err
	}
	html := string(body)

	if isMojeekChallenge(html) {
		e.invalidateToken()
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, fmt.Errorf("blocked by Mojeek anti-bot (altcha challenge)")
	}
	if resp.StatusCode != 200 {
		if ctx.Err() == nil {
			e.recordFail()
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if ctx.Err() == nil {
		e.recordSuccess(gen)
	}
	results := e.parseResults(html)
	if len(e.opts.Blocked) > 0 {
		results = e.filterBlocked(results)
	}
	return &antirobot.SearchResponse{Engine: "mojeek", Results: results}, nil
}

var _ antirobot.ContextEngine = (*mojeekEngine)(nil)

func (e *mojeekEngine) base() string {
	if e.baseURL != "" {
		return strings.TrimRight(e.baseURL, "/")
	}
	return mojeekBaseURL
}

func (e *mojeekEngine) buildURL(query string, page int, timeRange antirobot.TimeRange) string {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("q", e.applyBlocked(query))
	if e.opts.SafeSearch > 0 {
		q.Set("safe", "1")
	} else {
		q.Set("safe", "0")
	}
	// 第一页显式带 s=0 会触发限流（上游注释），只在第 2 页起带。
	if page > 1 {
		q.Set("s", strconv.Itoa(10*(page-1)))
	}
	if since := mojeekSince(timeRange); since != "" {
		q.Set("since", since)
	}
	return e.base() + "/search?" + q.Encode()
}

func mojeekSince(tr antirobot.TimeRange) string {
	now := time.Now()
	switch tr {
	case antirobot.TimeRangeDay:
		now = now.AddDate(0, 0, -1)
	case antirobot.TimeRangeWeek:
		now = now.AddDate(0, 0, -7)
	case antirobot.TimeRangeMonth:
		now = now.AddDate(0, -1, 0)
	case antirobot.TimeRangeYear:
		now = now.AddDate(-1, 0, 0)
	default:
		return ""
	}
	return now.Format("20060102")
}

func (e *mojeekEngine) setHeaders(req *http.Request, token string) {
	req.Header.Set("User-Agent", mojeekUAs[rand.Intn(len(mojeekUAs))])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Referer", "https://www.mojeek.com/")
	req.Header.Set("Cookie", "lb=en; arc=us; chllg="+token)
}

func (e *mojeekEngine) preDelay(ctx context.Context) error {
	e.mu.Lock()
	bo := e.backoff
	e.mu.Unlock()
	delay := mojeekBaseDelay + time.Duration(rand.Int63n(int64(mojeekJitter)))
	if bo > 0 {
		delay += bo
	}
	return antirobot.SleepCtx(ctx, delay)
}

func (e *mojeekEngine) stateGen() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateEpoch
}

func (e *mojeekEngine) recordSuccess(since uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stateEpoch != since {
		return
	}
	e.backoff = 0
	e.consecFails = 0
}

func (e *mojeekEngine) recordFail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecFails++
	e.stateEpoch++
	bo := mojeekBaseDelay * time.Duration(1<<uint(e.consecFails))
	if bo > mojeekMaxBackoff {
		bo = mojeekMaxBackoff
	}
	e.backoff = bo
}

// ── altcha ──

func (e *mojeekEngine) ensureToken(ctx context.Context) (string, error) {
	e.tokenMu.Lock()
	defer e.tokenMu.Unlock()
	if e.token != "" && time.Now().Before(e.tokenExp) {
		return e.token, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", e.base()+"/captcha/challenge", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", mojeekUAs[rand.Intn(len(mojeekUAs))])
	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mojeek challenge: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return "", readErr
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("mojeek challenge HTTP %d", resp.StatusCode)
	}

	var challenge altchaChallenge
	if err := json.Unmarshal(body, &challenge); err != nil {
		return "", fmt.Errorf("mojeek challenge decode: %w", err)
	}
	key, counter, err := solveAltcha(challenge.Parameters)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(altchaSolution{
		Challenge: json.RawMessage(body),
		Solution:  altchaAnswer{Counter: counter, DerivedKey: key, Time: 100},
	})
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(payload)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	field, err := mw.CreateFormField("altcha")
	if err != nil {
		return "", err
	}
	if _, err := field.Write([]byte(encoded)); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	verifyReq, err := http.NewRequestWithContext(ctx, "POST", e.base()+"/captcha/verify", &buf)
	if err != nil {
		return "", err
	}
	verifyReq.Header.Set("Content-Type", mw.FormDataContentType())
	verifyReq.Header.Set("User-Agent", mojeekUAs[rand.Intn(len(mojeekUAs))])
	verifyResp, err := e.client.Do(verifyReq)
	if err != nil {
		return "", fmt.Errorf("mojeek verify: %w", err)
	}
	verifyResp.Body.Close()

	token := ""
	for _, c := range verifyResp.Cookies() {
		if c.Name == "chllg" {
			token = c.Value
			break
		}
	}
	if token == "" {
		return "", fmt.Errorf("mojeek verify: missing chllg cookie")
	}
	e.token = token
	e.tokenExp = time.Now().Add(mojeekTokenTTL)
	return token, nil
}

func (e *mojeekEngine) invalidateToken() {
	e.tokenMu.Lock()
	e.token = ""
	e.tokenExp = time.Time{}
	e.tokenMu.Unlock()
}

// solveAltcha 按 altcha 参数做 PBKDF2 前缀碰撞，返回 derivedKey 与 counter。
func solveAltcha(p altchaParameters) (string, int, error) {
	if p.Cost <= 0 || p.KeyLength <= 0 {
		return "", 0, fmt.Errorf("mojeek altcha: invalid cost/keyLength")
	}
	var newHash func() hash.Hash
	switch strings.ToLower(p.Algorithm) {
	case "pbkdf2/sha-256", "sha-256", "sha256":
		newHash = sha256.New
	case "pbkdf2/sha-1", "sha-1", "sha1":
		newHash = sha1.New
	default:
		return "", 0, fmt.Errorf("mojeek altcha: unsupported algorithm %q", p.Algorithm)
	}

	nonce, err := hex.DecodeString(p.Nonce)
	if err != nil {
		return "", 0, fmt.Errorf("mojeek altcha nonce: %w", err)
	}
	salt, err := hex.DecodeString(p.Salt)
	if err != nil {
		return "", 0, fmt.Errorf("mojeek altcha salt: %w", err)
	}
	prefix, err := hex.DecodeString(p.KeyPrefix)
	if err != nil {
		return "", 0, fmt.Errorf("mojeek altcha prefix: %w", err)
	}

	counterBytes := make([]byte, 4)
	for counter := 0; counter < mojeekMaxTries; counter++ {
		binary.BigEndian.PutUint32(counterBytes, uint32(counter))
		secret := append(append([]byte{}, nonce...), counterBytes...)
		key, err := pbkdf2.Key(newHash, string(secret), salt, p.Cost, p.KeyLength)
		if err != nil {
			return "", 0, err
		}
		if bytes.HasPrefix(key, prefix) {
			return hex.EncodeToString(key), counter, nil
		}
	}
	return "", 0, fmt.Errorf("mojeek altcha: no solution in %d tries", mojeekMaxTries)
}

func isMojeekChallenge(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "altcha") ||
		strings.Contains(lower, "<title>captcha") ||
		strings.Contains(lower, "javascript is required to complete this challenge")
}

// ── HTML 解析 ──

func (e *mojeekEngine) parseResults(htmlText string) []antirobot.Result {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	items := doc.Find("ul.results-standard li")
	if items.Length() == 0 {
		items = doc.Find("ul.results li")
	}

	var results []antirobot.Result
	items.Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find("a.ob").First()
		if link.Length() == 0 {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if strings.HasPrefix(href, "/") {
			href = e.base() + href
		}
		title := antirobot.CollapseSpace(strings.TrimSpace(sel.Find("h2 a").First().Text()))
		if title == "" {
			title = antirobot.CollapseSpace(strings.TrimSpace(link.Text()))
		}
		if href == "" || title == "" || !strings.HasPrefix(href, "http") {
			return
		}
		content := antirobot.CollapseSpace(strings.TrimSpace(sel.Find("p.s").First().Text()))
		results = append(results, antirobot.Result{
			Type: antirobot.ResultWeb, Title: title, URL: href,
			Content: content, Engine: "mojeek",
		})
	})
	return results
}

// ── 站点屏蔽 ──

func (e *mojeekEngine) applyBlocked(query string) string {
	if len(e.opts.Blocked) > 5 {
		return query
	}
	var sb strings.Builder
	sb.WriteString(query)
	for _, d := range e.opts.Blocked {
		sb.WriteString(" -site:")
		sb.WriteString(d)
	}
	return sb.String()
}

func (e *mojeekEngine) filterBlocked(results []antirobot.Result) []antirobot.Result {
	blocked := make(map[string]struct{}, len(e.opts.Blocked))
	for _, d := range e.opts.Blocked {
		blocked[strings.ToLower(d)] = struct{}{}
	}
	filtered := make([]antirobot.Result, 0, len(results))
	for _, r := range results {
		host := extractHost(r.URL)
		hit := false
		for d := range blocked {
			if host == d || strings.HasSuffix(host, "."+d) {
				hit = true
				break
			}
		}
		if !hit {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}
