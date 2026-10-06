package so360

import (
	"regexp"

	"websearch/pkg/antirobot"
)

// so360CaptchaRe 验证码/反爬页标记（wappass 为 360 验证码域，verify 为安全验证路径）。
var so360CaptchaRe = regexp.MustCompile(`wappass\.so\.com|/verify/| captcha`)

// So360Opts 360 搜索引擎配置。
type So360Opts struct {
	Enabled    bool
	Blocked    []string // 屏蔽域名
	PerSec     int      // 每秒限流，默认 3
	PerMin     int      // 每分钟限流，默认 60
	SafeSearch int      // 0=关, 1/2=开（secure=1）
}

// NewSo360 创建 360 搜索引擎。
func NewSo360(opts So360Opts) antirobot.Engine {
	perSec, perMin := opts.PerSec, opts.PerMin
	if perSec <= 0 {
		perSec = 3
	}
	if perMin <= 0 {
		perMin = 60
	}
	return &so360Engine{
		opts:    opts,
		limiter: antirobot.NewRateLimiter(perSec, perMin),
	}
}
