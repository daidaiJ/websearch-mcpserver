package mojeek

import (
	"net/http"
	"time"

	"websearch/pkg/antirobot"
)

// MojeekOpts Mojeek 网页搜索配置（HTML 抓取 + altcha PoW，无需 API Key）。
type MojeekOpts struct {
	Enabled    bool
	Blocked    []string // 屏蔽域名
	PerSec     int      // 每秒限流，默认 1
	PerMin     int      // 每分钟限流，默认 20
	SafeSearch int      // 0=关, 1/2=开（请求 safe 参数）
}

// NewMojeek 创建 Mojeek 搜索引擎。
func NewMojeek(opts MojeekOpts) antirobot.Engine {
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = 1
	}
	if perMin <= 0 {
		perMin = 20
	}
	return &mojeekEngine{
		opts:    opts,
		limiter: antirobot.NewRateLimiter(perSec, perMin),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}
