package adapter

import (
	"context"

	"websearch/pkg/search/core"
)

// ──────────────────────────────────────────────────────────────────────────────
// BaiduWithFallback SK 调用失败时自动回退到百度网页搜索
// ──────────────────────────────────────────────────────────────────────────────

// BaiduWithFallback 包装百度千帆 SK 引擎，SK 调用失败时自动回退到百度网页搜索引擎。
type BaiduWithFallback struct {
	primary  core.SearchInf // 千帆 SK 引擎
	fallback core.SearchInf // 百度网页搜索引擎
}

// NewBaiduWithFallback 创建带回退的百度搜索引擎。
func NewBaiduWithFallback(primary, fallback core.SearchInf) *BaiduWithFallback {
	return &BaiduWithFallback{primary: primary, fallback: fallback}
}

func (b *BaiduWithFallback) Name() string { return "baidu_api" }

func (b *BaiduWithFallback) Search(query string) (string, error) {
	results, err := b.SearchRaw(query)
	if err != nil {
		return "", err
	}
	return b.MergeContent(query, results)
}

func (b *BaiduWithFallback) SearchRaw(query string) ([]core.SearchResult, error) {
	return b.SearchRawContext(context.Background(), query)
}

// SearchRawContext 实现 core.ContextSearcher：ctx 透传给主引擎与回退引擎。
func (b *BaiduWithFallback) SearchRawContext(ctx context.Context, query string) ([]core.SearchResult, error) {
	results, err := searchWithContext(ctx, b.primary, query)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // 请求已取消/超时，不再触发回退
		}
		// SK 调用失败，回退到百度网页搜索
		return searchWithContext(ctx, b.fallback, query)
	}
	return results, nil
}

// searchWithContext 对实现 core.ContextSearcher 的引擎走 ctx 入口，其余保持旧接口。
func searchWithContext(ctx context.Context, engine core.SearchInf, query string) ([]core.SearchResult, error) {
	if cs, ok := engine.(core.ContextSearcher); ok {
		return cs.SearchRawContext(ctx, query)
	}
	return engine.SearchRaw(query)
}

func (b *BaiduWithFallback) MergeContent(query string, results []core.SearchResult) (string, error) {
	return b.primary.MergeContent(query, results)
}
