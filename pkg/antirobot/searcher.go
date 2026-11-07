package antirobot

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ── 搜索策略 ──

// Strategy 多引擎检索策略。
type Strategy int

const (
	// StrategyParallel 并行：同时请求所有引擎，汇总全部结果。
	StrategyParallel Strategy = iota
	// StrategySerial 串行：依次请求引擎，首个返回有效结果即停止。
	StrategySerial
)

// ── Searcher 编排器 ──

// Searcher 多引擎搜索编排器，支持并行和串行两种策略。
type Searcher struct {
	Strategy         Strategy
	MaxResults       int
	TimeRange        TimeRange
	Concurrency      int
	PerEngineTimeout time.Duration

	engines []Engine
}

// NewSearcher 创建 Searcher。
func NewSearcher(strategy Strategy, engines []Engine) *Searcher {
	return &Searcher{
		Strategy:         strategy,
		Concurrency:      5,
		PerEngineTimeout: 10 * time.Second,
		engines:          engines,
	}
}

// Search 根据配置的策略执行多引擎搜索。
func (s *Searcher) Search(ctx context.Context, query string, page int) []SearchResponse {
	if len(s.engines) == 0 {
		return nil
	}
	switch s.Strategy {
	case StrategySerial:
		return s.searchSerial(ctx, query, page)
	default:
		return s.searchParallel(ctx, query, page)
	}
}

// Engines 返回已注册的引擎名称列表。
func (s *Searcher) Engines() []string {
	names := make([]string, len(s.engines))
	for i, e := range s.engines {
		names[i] = e.Name()
	}
	return names
}

// ── 并行策略 ──

func (s *Searcher) searchParallel(ctx context.Context, query string, page int) []SearchResponse {
	n := len(s.engines)
	results := make([]SearchResponse, n)

	concurrency := s.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, eng := range s.engines {
		wg.Add(1)
		go func(idx int, e Engine) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = s.execOne(ctx, e, query, page)
		}(i, eng)
	}

	wg.Wait()
	return results
}

// ── 串行策略 ──

func (s *Searcher) searchSerial(ctx context.Context, query string, page int) []SearchResponse {
	var results []SearchResponse

	for _, eng := range s.engines {
		if ctx.Err() != nil {
			break
		}
		resp := s.execOne(ctx, eng, query, page)
		resp.Results = s.truncateResults(resp.Results)
		results = append(results, resp)
		if resp.HasResults() {
			break
		}
	}
	return results
}

func (s *Searcher) truncateResults(results []Result) []Result {
	if s.MaxResults > 0 && len(results) > s.MaxResults {
		return results[:s.MaxResults]
	}
	return results
}

// ── 通用执行 ──

func (s *Searcher) execOne(parent context.Context, eng Engine, query string, page int) SearchResponse {
	timeout := s.PerEngineTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	type outcome struct {
		resp *SearchResponse
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		var resp *SearchResponse
		var err error
		// 优先走 ctx 入口：软超时或客户端断连后引擎立即中止在飞请求，
		// 不再把已被放弃的调用结果写进冷却 / 会话轮换等共享状态（B4）。
		if ce, ok := eng.(ContextEngine); ok {
			resp, err = ce.SearchContext(ctx, query, page, s.TimeRange)
		} else {
			resp, err = eng.Search(query, page, s.TimeRange)
		}
		ch <- outcome{resp, err}
	}()

	select {
	case o := <-ch:
		if o.err != nil {
			return SearchResponse{Engine: eng.Name(), Error: o.err.Error()}
		}
		if o.resp == nil {
			return SearchResponse{Engine: eng.Name(), Results: []Result{}}
		}
		return *o.resp
	case <-ctx.Done():
		return SearchResponse{
			Engine: eng.Name(),
			Error:  fmt.Sprintf("timeout after %s", timeout),
		}
	}
}
