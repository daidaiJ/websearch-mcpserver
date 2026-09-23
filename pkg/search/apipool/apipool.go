package apipool

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"
	"websearch/pkg/search/core"
	"websearch/pkg/search/provider"

	"websearch/pkg/log"
	"websearch/pkg/telemetry"
)

// ApipoolProvider 单个供应商：搜索引擎 + 对应的 KeyPool（免费引擎 pool 为 nil）。
// name 为供应商配置名（baidu/tavily/exa/anysearch/doubao），weighted 策略权重匹配用；
// 为空时回退 engine.Name()。
type ApipoolProvider struct {
	name   string
	engine core.SearchInf
	pool   *provider.KeyPool
}

// NewApipoolProvider 创建 apipool 供应商条目（供编排层工厂组装）。
func NewApipoolProvider(name string, engine core.SearchInf, pool *provider.KeyPool) ApipoolProvider {
	return ApipoolProvider{name: name, engine: engine, pool: pool}
}

// ApipoolSearchImpl API Key 池轮转搜索引擎。
//
// 支持三种策略：
//   - round-robin（默认）：跨请求轮转起始供应商，同一次请求内先用完当前供应商所有可用 SK 再 fallback
//   - priority：始终从第一个供应商开始，用完所有 SK → 下一个供应商 → web 兜底
//   - weighted：按权重加权随机选择起始供应商（权重按可用 SK 数累加），失败切换链路相同
//
// 三种策略都支持同供应商内 SK 重试：当前 key 失败 → 标记 invalid → 尝试同 pool 下一个可用 key。
type ApipoolSearchImpl struct {
	providers []ApipoolProvider
	strategy  string         // "round-robin" / "priority" / "weighted"
	idx       atomic.Uint64  // round-robin 轮转游标
	maxSize   int            // 全局最大结果数，0 = 不限
	weights   map[string]int // weighted 策略：供应商名 → 单 Key 权重
}

// NewApipoolSearch 创建 ApipoolSearchImpl，providers 顺序即为优先级顺序。
func NewApipoolSearch(strategy string, providers ...ApipoolProvider) *ApipoolSearchImpl {
	switch strategy {
	case "priority", "weighted":
	default:
		strategy = "round-robin"
	}
	return &ApipoolSearchImpl{providers: providers, strategy: strategy}
}

// SetWeights 设置 weighted 策略的供应商权重（供应商名 → 单 Key 权重）。
// 供应商有效权重 = 配置权重 × 当前可用 SK 数（免费引擎无 pool，固定为 1）。
func (a *ApipoolSearchImpl) SetWeights(w map[string]int) {
	a.weights = w
}

// SetMaxSize 设置全局最大结果数。
func (a *ApipoolSearchImpl) SetMaxSize(n int) {
	a.maxSize = n
}

func (a *ApipoolSearchImpl) Name() string { return "apipool" }

func (a *ApipoolSearchImpl) Search(query string) (string, error) {
	results, err := a.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return a.MergeContent(query, results)
}

func (a *ApipoolSearchImpl) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	n := len(a.providers)
	if n == 0 {
		return nil, fmt.Errorf("apipool: 无可用供应商")
	}

	start := a.pickStartIndex()

	var lastErr error
	for i := 0; i < n; i++ {
		p := a.providers[(start+i)%n]
		results, err := a.callProviderWithRetry(p, query, lookbackDays)
		if err == nil {
			return results, nil
		}
		log.Infof("apipool: %s 所有 SK 均不可用，切换下一个供应商: %v", p.engine.Name(), err)
		lastErr = err
	}
	return nil, fmt.Errorf("apipool: 所有供应商均失败，最后错误: %w", lastErr)
}

// pickStartIndex 确定起始供应商索引。
func (a *ApipoolSearchImpl) pickStartIndex() int {
	n := len(a.providers)
	if a.strategy == "priority" {
		return 0
	}
	if a.strategy == "weighted" {
		if start, ok := a.pickWeighted(); ok {
			return start
		}
		// 所有供应商有效权重均为 0 时退化为 round-robin
	}
	return int(a.idx.Add(1)-1) % n
}

// pickWeighted 加权随机选择起始供应商（权重比例分配，无状态，突发请求天然分散）。
// 供应商有效权重 = 配置权重 × 当前可用 SK 数；pool 为 nil 的免费引擎固定为 1，
// 保持"百度网页搜索兜底"的定位。返回 false 表示全部权重为 0。
func (a *ApipoolSearchImpl) pickWeighted() (int, bool) {
	weights := make([]int, len(a.providers))
	total := 0
	for i, p := range a.providers {
		weights[i] = a.effectiveWeight(p)
		total += weights[i]
	}
	if total <= 0 {
		return 0, false
	}
	r := rand.IntN(total)
	for i, w := range weights {
		if r < w {
			return i, true
		}
		r -= w
	}
	return 0, false
}

// effectiveWeight 计算供应商有效权重（配置权重 × 可用 SK 数）。
// 权重表未收录的供应商按 1 计；显式配置 0（或负数）表示不参与加权起始选择。
func (a *ApipoolSearchImpl) effectiveWeight(p ApipoolProvider) int {
	if p.pool == nil {
		return 1
	}
	w := 1
	if a.weights != nil {
		name := p.name
		if name == "" {
			name = p.engine.Name()
		}
		if v, ok := a.weights[name]; ok {
			if v > 0 {
				w = v
			} else {
				w = 0
			}
		}
	}
	return w * p.pool.Available()
}

func (a *ApipoolSearchImpl) SearchRaw(query string) ([]core.SearchResult, error) {
	return a.SearchRawWithTimeRange(query, 0)
}

// callProviderWithRetry 调用单个供应商，自动重试同 pool 内的其他 SK。
// 当前 key 失败 → 标记 invalid → 检查 pool 是否还有可用 key → 有则重试，无则返回错误。
func (a *ApipoolSearchImpl) callProviderWithRetry(p ApipoolProvider, query string, lookbackDays int) ([]core.SearchResult, error) {
	// 免费引擎无 pool，只调一次
	if p.pool == nil {
		return a.callSingle(p, query, lookbackDays)
	}

	var lastErr error
	for {
		results, err := a.callSingle(p, query, lookbackDays)
		if err == nil {
			return results, nil
		}
		// 优先按 provider.KeyError 精确标记本次实际使用的 key（并发安全）；
		// 无 provider.KeyError（如免费引擎/内容为空等非 key 错误）时回退 MarkLastInvalid。
		var ke *provider.KeyError
		if errors.As(err, &ke) {
			p.pool.MarkInvalid(ke.Key)
		} else {
			p.pool.MarkLastInvalid()
		}
		lastErr = err
		if p.pool.Available() == 0 {
			return nil, fmt.Errorf("%s 所有 SK 均失败: %w", p.engine.Name(), lastErr)
		}
		log.Infof("apipool: %s 当前 SK 失败，尝试下一个 SK", p.engine.Name())
	}
}

// callSingle 调用一次搜索引擎。
func (a *ApipoolSearchImpl) callSingle(p ApipoolProvider, query string, lookbackDays int) ([]core.SearchResult, error) {
	started := time.Now()
	var results []core.SearchResult
	var err error
	if lookbackDays > 0 {
		if tr, ok := p.engine.(core.SearchTimeRanger); ok {
			results, err = tr.SearchRawWithTimeRange(query, lookbackDays)
		} else {
			results, err = p.engine.SearchRaw(query)
		}
	} else {
		results, err = p.engine.SearchRaw(query)
	}
	name := p.name
	if name == "" {
		name = p.engine.Name()
	}
	telemetry.Record(telemetry.Event{Kind: "provider", Provider: name, Query: query, Success: err == nil, Duration: time.Since(started), ResultCount: len(results), Error: err})
	if err != nil {
		return nil, err
	}
	if a.maxSize > 0 && len(results) > a.maxSize {
		results = results[:a.maxSize]
	}
	return results, nil
}

func (a *ApipoolSearchImpl) MergeContent(query string, results []core.SearchResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("没有搜索结果可以合并")
	}
	var buf strings.Builder
	buf.Grow(1024 * len(results))
	buf.WriteString(core.MDSearchHeader(query, len(results)))
	for i, val := range results {
		if core.ShowMeta {
			buf.WriteString(core.FormatMDScore(i+1, val.Title, val.Url, val.Engine, core.FormatScore(val.Score), val.Content))
		} else {
			buf.WriteString(core.FormatMD(i+1, val.Title, val.Url, val.Content))
		}
	}
	return buf.String(), nil
}
