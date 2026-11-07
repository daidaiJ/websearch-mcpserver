package antirobot

import "context"

// Engine 搜索引擎接口。
type Engine interface {
	Name() string
	Region() NetworkRegion
	Search(query string, page int, timeRange TimeRange) (*SearchResponse, error)
}

// ContextEngine 支持请求级 context 的搜索引擎可选接口。
//
// 编排层在软超时或客户端断连后会放弃等待，实现者据此取消在飞的 HTTP 请求，
// 并且不再把本次（已被放弃的）调用的结果写进共享状态——冷却档位、会话轮换计数、
// 退避都只应由仍在服务请求的调用推进（B4：软超时后 goroutine 继续改状态）。
// 未实现此接口的引擎继续走旧入口，由编排层兜底放弃等待。
type ContextEngine interface {
	SearchContext(ctx context.Context, query string, page int, timeRange TimeRange) (*SearchResponse, error)
}
