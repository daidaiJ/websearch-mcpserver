package bing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Searcher 多引擎搜索编排器，支持并行和串行两种策略。
type Searcher struct {
	opts    Options
	engines []Engine
}

// NewSearcher 根据 Options 创建 Searcher，自动注册已启用且网络可达的引擎。
func NewSearcher(opts Options) *Searcher {
	s := &Searcher{opts: opts}
	s.buildEngines()
	return s
}

func (s *Searcher) buildEngines() {
	if !s.opts.Academic {
		if s.opts.Bing.Enabled {
			s.engines = append(s.engines, NewBing(s.opts.Bing))
		}
		return
	}

	// 学术模式：优先学术引擎，Bing 仅作补充
	if s.opts.Crossref.Enabled && s.opts.Network >= RegionChina {
		s.engines = append(s.engines, NewCrossref(s.opts.Crossref))
	}
	if s.opts.OpenAlex.Enabled && s.opts.Network >= RegionChina {
		s.engines = append(s.engines, NewOpenAlex(s.opts.OpenAlex))
	}
	if s.opts.Arxiv.Enabled && s.opts.Network >= RegionInternational {
		s.engines = append(s.engines, NewArxiv(s.opts.Arxiv))
	}
	if s.opts.SemanticScholar.Enabled && s.opts.Network >= RegionInternational {
		s.engines = append(s.engines, NewSemanticScholar(s.opts.SemanticScholar))
	}
	// Bing 放在最后，学术引擎无结果时才启用
	if s.opts.Bing.Enabled && s.opts.BingFallback {
		s.engines = append(s.engines, NewBing(s.opts.Bing))
	}
}

// Search 根据配置的策略执行多引擎搜索。
func (s *Searcher) Search(ctx context.Context, query string, page int) []SearchResponse {
	if len(s.engines) == 0 {
		return nil
	}
	switch s.opts.Strategy {
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

	concurrency := s.opts.Concurrency
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
	if s.opts.MaxResults > 0 && len(results) > s.opts.MaxResults {
		return results[:s.opts.MaxResults]
	}
	return results
}

// ── 通用执行 ──

func (s *Searcher) execOne(parent context.Context, eng Engine, query string, page int) SearchResponse {
	timeout := s.opts.PerEngineTimeout
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
		resp, err := eng.Search(query, page, s.opts.TimeRange)
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
