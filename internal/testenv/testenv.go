// Package testenv 集成测试的网络场景门控。
//
// 网络型集成测试（真实引擎搜索、真实抓取）按「网络场景」动态启用或跳过，
// 避免在无外网 / 出口 IP 被反爬拦截的环境里产生持续性的假失败。
//
// 模式由环境变量 WS_TEST_NETWORK 控制（默认 auto）：
//
//	auto  探测网络场景：目标可达 → 运行；不可达 / 被反爬拦截 → Skip（附原因）
//	on    强制执行：目标不可达或被拦截 → t.Fatalf（特殊 case 被禁用即报 fail，
//	        用于 CI 或本地显式验证，防止 Skip 静默掩盖引擎回归）
//	off   一律 Skip
//
// 另受 go test -short 约束：short 模式下一律 Skip（优先级高于 WS_TEST_NETWORK=on）。
//
// 用法：
//
//	func TestBaiduSearch(t *testing.T) {
//	    testenv.Require(t, testenv.Baidu)
//	    resp, err := engine.Search(...)
//	    if testenv.HandleSearchError(t, err) {
//	        return // 场景不可用已被 Skip/Fatal 处理
//	    }
//	    ...正常断言
//	}
package testenv

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// 网络场景目标。Require 按目标做连通性探测（进程内缓存探测结果）。
const (
	Arxiv           = "arxiv"
	Crossref        = "crossref"
	OpenAlex        = "openalex"
	SemanticScholar = "semantic_scholar"
	Baidu           = "baidu"
	Bing            = "bing"
	So360           = "so360"
	Wikipedia       = "wikipedia"
	GoogleNews      = "googlenews"
	Google          = "google"
	DuckDuckGo      = "duckduckgo"
	Web             = "web"          // 通用外网连通性
	Ruanyifeng      = "ruanyifeng"   // ruanyifeng.com 博客
	WmySkxz         = "wmyskxz"      // wmyskxz.cn
)

var probeURLs = map[string]string{
	Arxiv:           "https://export.arxiv.org/api/query?search_query=all:test&max_results=1",
	Crossref:        "https://api.crossref.org/works?rows=0",
	OpenAlex:        "https://api.openalex.org/works?per-page=1",
	SemanticScholar: "https://api.semanticscholar.org/graph/v1/paper/search?query=test&limit=1",
	Baidu:           "https://www.baidu.com",
	Bing:            "https://www.bing.com",
	So360:           "https://www.so.com",
	Wikipedia:       "https://zh.wikipedia.org",
	GoogleNews:      "https://news.google.com/rss",
	Google:          "https://www.google.com/generate_204",
	DuckDuckGo:      "https://duckduckgo.com/",
	Web:             "https://example.com",
	Ruanyifeng:      "https://www.ruanyifeng.com/blog/",
	WmySkxz:         "https://wmyskxz.cn/",
}

type mode int

const (
	modeAuto mode = iota
	modeOn
	modeOff
)

func getMode() mode {
	switch strings.ToLower(os.Getenv("WS_TEST_NETWORK")) {
	case "on", "force", "1", "true":
		return modeOn
	case "off", "0", "false":
		return modeOff
	default:
		return modeAuto
	}
}

var (
	probeMu    sync.Mutex
	probeCache = map[string]bool{}
)

// probe 探测目标连通性：任意 HTTP 响应（含 4xx/5xx）即视为网络可达；
// 只有传输层失败（DNS/超时/拒连/TLS）才算不可达。结果进程内缓存。
func probe(target string) bool {
	probeMu.Lock()
	defer probeMu.Unlock()
	if ok, seen := probeCache[target]; seen {
		return ok
	}
	url, ok := probeURLs[target]
	if !ok {
		probeCache[target] = false
		return false
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err == nil {
		resp.Body.Close()
	}
	reachable := err == nil
	probeCache[target] = reachable
	return reachable
}

// shortOrOff 判断是否无条件跳过：-short 或 WS_TEST_NETWORK=off。
func shortOrOff(t *testing.T) bool {
	if testing.Short() {
		t.Skipf("short 模式跳过网络集成测试（%s）", strings.Join(targetsOf(t), ","))
		return true
	}
	if getMode() == modeOff {
		t.Skip("WS_TEST_NETWORK=off 跳过网络集成测试")
		return true
	}
	return false
}

// targetsOf 从 t.Name() 无法反查目标，仅用于 skip 文案占位；
// Require 内部直接拼接目标名。
func targetsOf(t *testing.T) []string { return nil }

// Require 声明测试依赖的网络场景（连通性探测）。
// 见包注释的 WS_TEST_NETWORK 模式说明。
func Require(t *testing.T, targets ...string) {
	t.Helper()
	if testing.Short() {
		t.Skipf("short 模式跳过网络集成测试（%s）", strings.Join(targets, ","))
		return
	}
	if getMode() == modeOff {
		t.Skip("WS_TEST_NETWORK=off 跳过网络集成测试")
		return
	}
	for _, target := range targets {
		if !probe(target) {
			if getMode() == modeOn {
				t.Fatalf("WS_TEST_NETWORK=on 强制执行，但网络场景 %q 不可达（禁用的特殊 case 应报 fail 而非跳过）", target)
			}
			t.Skipf("网络场景 %q 不可达，跳过（WS_TEST_NETWORK=on 可强制执行）", target)
			return
		}
	}
}

// HandleSearchError 对引擎/抓取集成测试的错误做场景分类：
//   - 反爬拦截（CAPTCHA/WAF/sorry）或网络传输层错误 → 场景类错误：
//     auto → Skip（附原因），on → t.Fatalf（禁用即报 fail），off → Skip；
//     返回 true，测试应立即 return。
//   - 其它错误（引擎逻辑缺陷、断言失败）→ 返回 false，由调用方继续原有
//     t.Fatalf 断言，不会被静默吞掉。
func HandleSearchError(t *testing.T, err error) bool {
	t.Helper()
	if err == nil {
		return false
	}
	if getMode() == modeOff {
		t.Skip("WS_TEST_NETWORK=off 跳过网络集成测试")
		return true
	}
	kind := classify(err)
	if kind == "" {
		return false // 非场景类错误，交给调用方断言
	}
	if getMode() == modeOn {
		t.Fatalf("WS_TEST_NETWORK=on 强制执行，但网络场景不可用（%s）：%v", kind, err)
	}
	t.Skipf("网络场景不可用（%s）：%v；WS_TEST_NETWORK=on 可强制执行", kind, err)
	return true
}

// classify 错误场景分类：返回 "blocked"（反爬拦截）/"network"（传输层）/
// ""（非场景类）。子串匹配按宽松规则覆盖两类引擎错误。
func classify(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// 反爬拦截：HTTP 可达但被识别（CAPTCHA/WAF/sorry 页）
	for _, s := range []string{
		"CAPTCHA", "captcha", "blocked by", "WAF", "sorry",
		"验证码", "反爬", "challenge",
	} {
		if strings.Contains(msg, s) {
			return "blocked/出口 IP 被反爬拦截"
		}
	}

	// 传输层：不可达 / 超时 / DNS / 代理
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "network/网络不可达或超时"
	}
	for _, s := range []string{
		"timeout", "Timeout", "connection refused", "connection reset",
		"forcibly closed", "no such host", "i/o timeout", "EOF",
		"TLS handshake", "proxyconnect", "dial tcp", "read tcp", "write tcp",
		"context deadline exceeded", "请求超时", "网络",
	} {
		if strings.Contains(msg, s) {
			return "network/网络不可达或超时"
		}
	}
	return ""
}
