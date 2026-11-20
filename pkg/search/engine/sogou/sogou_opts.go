package sogou

import (
	"net/http"
	"time"

	"websearch/pkg/antirobot"
)

// SogouOpts 搜狗网页搜索配置（HTML 抓取，无需 API Key）。
type SogouOpts struct {
	Enabled bool
	Blocked []string // 屏蔽域名
	PerSec  int      // 每秒限流，默认 2
	PerMin  int      // 每分钟限流，默认 30
}

// NewSogou 创建搜狗搜索引擎。
func NewSogou(opts SogouOpts) antirobot.Engine {
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = 2
	}
	if perMin <= 0 {
		perMin = 30
	}
	return &sogouEngine{
		opts:    opts,
		limiter: antirobot.NewRateLimiter(perSec, perMin),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}
