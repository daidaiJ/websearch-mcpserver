package ddg

import (
	"net/http"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/log"
	"websearch/pkg/proxy"
)

// 内置限流约束（实测校准，见 ddg_engine.go 顶部说明）：DDG 服务端 202 触发阈值很低，
// ~6s 内第 2 次请求即 202，恢复窗口 15~25s。全局 rate_limit 配置（默认 3/s·60/min）
// 远超服务端容忍度，用户配置比内置上限宽松时钳制到上限，避免频繁 202。
const (
	ddgMaxPerSec = 1 // 每秒最多 1 次
	ddgMaxPerMin = 6 // 每分钟最多 6 次（≈10s 间隔，实测安全节奏）
)

// DuckDuckGoOpts DuckDuckGo 搜索配置。
type DuckDuckGoOpts struct {
	Enabled      bool
	Blocked      []string            // 屏蔽域名
	PerSec       int                 // 每秒限流（默认/上限 1，宽松配置被钳制）
	PerMin       int                 // 每分钟限流（默认/上限 6，宽松配置被钳制）
	ProxyResolve proxy.ProxyResolver // 代理端点动态解析函数（每次请求实时获取）
	Timeout      time.Duration       // 单次搜索超时预算（对齐 Searcher.PerEngineTimeout，默认 10s）；202/429 避让重试据此决定等待重试或放弃
}

// NewDuckDuckGo 创建 DuckDuckGo 引擎（需代理访问）。
func NewDuckDuckGo(opts DuckDuckGoOpts) antirobot.Engine {
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = ddgMaxPerSec
	}
	if perMin <= 0 {
		perMin = ddgMaxPerMin
	}
	// 钳制宽松配置：用户/全局 rate_limit 更宽松时以内置上限为准（实测 202 阈值）
	if perSec > ddgMaxPerSec {
		log.Warnf("duckduckgo: per_sec=%d 超过内置上限，钳制为 %d（DDG 实测 202 限流阈值）", perSec, ddgMaxPerSec)
		perSec = ddgMaxPerSec
	}
	if perMin > ddgMaxPerMin {
		log.Warnf("duckduckgo: per_min=%d 超过内置上限，钳制为 %d（DDG 实测 202 限流阈值）", perMin, ddgMaxPerMin)
		perMin = ddgMaxPerMin
	}
	e := &ddgEngine{
		opts:    opts,
		limiter: antirobot.NewRateLimiter(perSec, perMin),
	}
	e.adoptPersistedCooldown()
	e.rotateSession()
	return e
}

func (o DuckDuckGoOpts) newHTTPClient() *http.Client {
	return proxy.NewDynamicHTTPClient(o.ProxyResolve, 15*time.Second)
}
