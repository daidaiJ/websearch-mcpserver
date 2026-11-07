package apipool

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"websearch/pkg/search/core"
	"websearch/pkg/search/provider"
)

// ── poolAwareEngine mock：调用 pool.Next() 模拟真实引擎行为 ──────────────

type poolAwareEngine struct {
	name   string
	pool   *provider.KeyPool
	result []core.SearchResult
	errFn  func(key string) error // 根据 key 决定是否报错
	calls  int                    // SearchRaw 实际到达引擎的次数（上游调用计数）
}

func (p *poolAwareEngine) Name() string { return p.name }
func (p *poolAwareEngine) Search(query string) (string, error) {
	return "", nil
}
func (p *poolAwareEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	key, ok := p.pool.NextUsable()
	if !ok {
		return nil, provider.NoKeyError(p.name)
	}
	p.calls++ // 通过 key 检查后才算上游调用（全冷却时打不到上游）
	if p.errFn != nil {
		if err := p.errFn(key); err != nil {
			return nil, err
		}
	}
	return p.result, nil
}
func (p *poolAwareEngine) MergeContent(query string, results []core.SearchResult) (string, error) {
	return "", nil
}

// ── ApipoolSearchImpl ──────────────────────────────────────────────────────

func TestApipool_FirstProviderSucceeds(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, result: []core.SearchResult{{Title: "b1", Url: "http://b1.com", Engine: "baidu"}}}
	e2 := &poolAwareEngine{name: "tavily", pool: pool2, result: []core.SearchResult{{Title: "t1", Url: "http://t1.com", Engine: "tavily"}}}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Title != "b1" {
		t.Errorf("expected b1, got %v", results)
	}
}

// ── 同供应商 SK 重试 ──────────────────────────────────────────────────────

func TestApipool_IntraPool_RetryNextSK(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1_bad", "k2_good"})
	// k1_bad 报鉴权错（B3：只有 auth 类才冷却并换 key），k2_good 成功
	e1 := &poolAwareEngine{
		name:   "baidu",
		pool:   pool1,
		result: []core.SearchResult{{Title: "b1", Url: "http://b1.com", Engine: "baidu"}},
		errFn: func(key string) error {
			if key == "k1_bad" {
				return provider.NewKeyError(provider.KeyErrorAuth, key, fmt.Errorf("auth fail"))
			}
			return nil
		},
	}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Title != "b1" {
		t.Errorf("expected b1, got %s", results[0].Title)
	}
	// k1_bad 被标记失效，k2_good 仍可用
	if pool1.Available() != 1 {
		t.Errorf("expected 1 available key, got %d", pool1.Available())
	}
}

func TestApipool_IntraPool_AllSKsFail_FallbackToNext(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1", "k2"})
	pool2, _ := provider.NewKeyPool([]string{"k3"})
	// baidu 全部 key 都鉴权失败（B3：模拟的失败形态必须显式分类才会冷却）
	e1 := &poolAwareEngine{
		name: "baidu", pool: pool1,
		errFn: func(key string) error {
			return provider.NewKeyError(provider.KeyErrorAuth, key, fmt.Errorf("auth fail"))
		},
	}
	e2 := &poolAwareEngine{
		name: "tavily", pool: pool2,
		result: []core.SearchResult{{Title: "t1", Url: "http://t1.com", Engine: "tavily"}},
	}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Title != "t1" {
		t.Errorf("expected t1, got %s", results[0].Title)
	}
	if pool1.Available() != 0 {
		t.Errorf("expected pool1 exhausted, got %d available", pool1.Available())
	}
}

// ── priority 策略 ─────────────────────────────────────────────────────────

func TestApipool_Priority_AlwaysStartFromFirst(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, result: []core.SearchResult{{Title: "b1", Engine: "baidu"}}}
	e2 := &poolAwareEngine{name: "tavily", pool: pool2, result: []core.SearchResult{{Title: "t1", Engine: "tavily"}}}
	ap := NewApipoolSearch("priority",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	for i := 0; i < 5; i++ {
		results, err := ap.SearchRaw("test")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if results[0].Engine != "baidu" {
			t.Errorf("call %d: expected baidu, got %s", i, results[0].Engine)
		}
	}
}

func TestApipool_Priority_FallbackWhenFirstExhausted(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, errFn: func(string) error { return fmt.Errorf("fail") }}
	e2 := &poolAwareEngine{name: "tavily", pool: pool2, result: []core.SearchResult{{Title: "t1", Engine: "tavily"}}}
	ap := NewApipoolSearch("priority",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Title != "t1" {
		t.Errorf("expected t1, got %s", results[0].Title)
	}
}

// ── round-robin 策略 ──────────────────────────────────────────────────────

func TestApipool_RoundRobin_RotatesProvider(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, result: []core.SearchResult{{Title: "b1", Engine: "baidu"}}}
	e2 := &poolAwareEngine{name: "tavily", pool: pool2, result: []core.SearchResult{{Title: "t1", Engine: "tavily"}}}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	r1, _ := ap.SearchRaw("test")
	r2, _ := ap.SearchRaw("test")
	r3, _ := ap.SearchRaw("test")
	if r1[0].Engine != "baidu" {
		t.Errorf("call 1: expected baidu, got %s", r1[0].Engine)
	}
	if r2[0].Engine != "tavily" {
		t.Errorf("call 2: expected tavily, got %s", r2[0].Engine)
	}
	if r3[0].Engine != "baidu" {
		t.Errorf("call 3: expected baidu, got %s", r3[0].Engine)
	}
}

func TestApipool_RoundRobin_FallbackOnFail(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	pool3, _ := provider.NewKeyPool([]string{"k3"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, errFn: func(string) error { return fmt.Errorf("fail") }}
	e2 := &poolAwareEngine{name: "tavily", pool: pool2, result: []core.SearchResult{{Title: "t1", Engine: "tavily"}}}
	e3 := &poolAwareEngine{name: "exa", pool: pool3, result: []core.SearchResult{{Title: "e1", Engine: "exa"}}}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
		ApipoolProvider{engine: e3, pool: pool3},
	)
	// start=0, baidu 失败 → fallback tavily
	r1, _ := ap.SearchRaw("test")
	if r1[0].Engine != "tavily" {
		t.Errorf("call 1: expected tavily, got %s", r1[0].Engine)
	}
	// start=1, tavily 直接成功
	r2, _ := ap.SearchRaw("test")
	if r2[0].Engine != "tavily" {
		t.Errorf("call 2: expected tavily, got %s", r2[0].Engine)
	}
}

// ── 边界情况 ──────────────────────────────────────────────────────────────

func TestApipool_AllFail(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, errFn: func(string) error { return fmt.Errorf("fail1") }}
	e2 := &poolAwareEngine{name: "tavily", pool: pool2, errFn: func(string) error { return fmt.Errorf("fail2") }}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	_, err := ap.SearchRaw("test")
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestApipool_NilPool_NoPanic(t *testing.T) {
	e1 := &mockEngine{name: "baidu", err: fmt.Errorf("fail")}
	e2 := &mockEngine{name: "baiduWeb", results: []core.SearchResult{{Title: "w1", Url: "http://w1.com", Engine: "baiduWeb"}}}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: nil},
		ApipoolProvider{engine: e2, pool: nil},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Title != "w1" {
		t.Errorf("expected w1, got %s", results[0].Title)
	}
}

func TestApipool_MaxSize(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	e1 := &poolAwareEngine{name: "baidu", pool: pool1, result: []core.SearchResult{
		{Title: "b1", Engine: "baidu"}, {Title: "b2", Engine: "baidu"}, {Title: "b3", Engine: "baidu"},
	}}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e1, pool: pool1})
	ap.SetMaxSize(2)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}
}

func TestApipool_Name(t *testing.T) {
	ap := NewApipoolSearch("round-robin")
	if ap.Name() != "apipool" {
		t.Errorf("expected apipool, got %s", ap.Name())
	}
}

func TestApipool_DefaultStrategy(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	e := &poolAwareEngine{name: "a", pool: pool1, result: []core.SearchResult{{Title: "a1", Engine: "a"}}}
	ap := NewApipoolSearch("", ApipoolProvider{engine: e, pool: pool1})
	if ap.strategy != "round-robin" {
		t.Errorf("expected round-robin, got %s", ap.strategy)
	}
}

func TestApipool_SearchRawWithTimeRange(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	trEngine := &timeRangeRecorder{name: "baidu", results: []core.SearchResult{{Title: "b1", Engine: "baidu"}}}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: trEngine, pool: pool1})
	results, err := ap.SearchRawWithTimeRange("test", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if trEngine.lastLookback != 7 {
		t.Errorf("expected lookbackDays=7, got %d", trEngine.lastLookback)
	}
}

// ── provider.KeyError 精确标记 ──────────────────────────────────────────────────────

// keyErrorEngine 模拟真实引擎：Next() 取 key，失败时返回携带该 key 的 provider.KeyError。
type keyErrorEngine struct {
	pool     *provider.KeyPool
	failKeys map[string]bool
	gotKey   chan struct{} // 每次 Next 后发信号（非 nil 时）
	release  chan struct{} // 放行失败（非 nil 时）
}

func (e *keyErrorEngine) Name() string { return "keyerr" }
func (e *keyErrorEngine) Search(query string) (string, error) {
	return "", nil
}
func (e *keyErrorEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	key, ok := e.pool.NextUsable()
	if !ok {
		return nil, provider.NoKeyError("keyerr")
	}
	if e.gotKey != nil {
		e.gotKey <- struct{}{}
	}
	if e.release != nil {
		<-e.release
	}
	if e.failKeys[key] {
		return nil, provider.NewKeyError(provider.KeyErrorAuth, key, fmt.Errorf("boom"))
	}
	return []core.SearchResult{{Title: "ok", Url: "http://ok.com", Engine: "keyerr"}}, nil
}
func (e *keyErrorEngine) MergeContent(query string, results []core.SearchResult) (string, error) {
	return "", nil
}

// TestKeyError_ErrorHidesKey provider.KeyError.Error() 不得泄漏 Key 本身（日志安全）。
func TestKeyError_ErrorHidesKey(t *testing.T) {
	ke := provider.NewKeyError(provider.KeyErrorAuth, "sk-secret-123", fmt.Errorf("auth fail"))
	if strings.Contains(ke.Error(), "sk-secret-123") {
		t.Errorf("provider.KeyError.Error() 不应包含 Key: %s", ke.Error())
	}
	var got *provider.KeyError
	if !errors.As(fmt.Errorf("wrap: %w", ke), &got) || got.Key != "sk-secret-123" {
		t.Error("errors.As 应取出 provider.KeyError 及 Key")
	}
}

// TestApipool_KeyError_MarksUsedKey 单 goroutine：provider.KeyError 的 key 被精确冷却。
func TestApipool_KeyError_MarksUsedKey(t *testing.T) {
	pool, _ := provider.NewKeyPool([]string{"k1", "k2"})
	e := &keyErrorEngine{pool: pool, failKeys: map[string]bool{"k1": true}}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e, pool: pool})
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("k2 应重试成功, got err=%v", err)
	}
	if len(results) != 1 || results[0].Title != "ok" {
		t.Fatalf("expected ok result, got %v", results)
	}
	// k1 被冷却，k2 仍可用
	if pool.Available() != 1 {
		t.Fatalf("expected 1 available (k2), got %d", pool.Available())
	}
	if got := pool.Next(); got != "k2" {
		t.Errorf("expected next key k2, got %s", got)
	}
}

// TestApipool_ConcurrentKeyError_CoolsOwnKey 并发：两个 goroutine 交错 Next + 失败，
// 各自冷却自己的 key，不误伤对方（B9：旧 MarkLastInvalid 会标错 key，已删除）。
func TestApipool_ConcurrentKeyError_CoolsOwnKey(t *testing.T) {
	pool, _ := provider.NewKeyPool([]string{"k1", "k2"})
	e := &keyErrorEngine{
		pool:     pool,
		failKeys: map[string]bool{"k1": true}, // k1 坏，k2 好
		gotKey:   make(chan struct{}, 4),
		release:  make(chan struct{}),
	}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e, pool: pool})

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = ap.SearchRaw("test")
		}()
	}
	// 等两个 goroutine 都完成首次 Next（交错完成），再放行失败
	<-e.gotKey
	<-e.gotKey
	close(e.release)
	wg.Wait()

	// 只有 k1（坏 key）被冷却，k2（好 key）必须保持可用
	if pool.Available() != 1 {
		t.Fatalf("expected only k1 cooled (1 available), got %d", pool.Available())
	}
	if got := pool.Next(); got != "k2" {
		t.Errorf("expected k2 still available, got %s", got)
	}
}

// ── B3：KeyError 分类决定冷却与换 key 行为 ────────────────────────────────

// TestApipool_TransportErrorDoesNotCoolKeys transport 类失败不冷却任何 key、
// 不换 key，直接级联下一个供应商（B3 修复 (a)）。
func TestApipool_TransportErrorDoesNotCoolKeys(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool2, _ := provider.NewKeyPool([]string{"k2"})
	e1 := &poolAwareEngine{
		name: "tavily", pool: pool1,
		errFn: func(key string) error {
			return provider.NewKeyError(provider.KeyErrorTransport, key, fmt.Errorf("dial tcp: connection reset"))
		},
	}
	e2 := &poolAwareEngine{name: "exa", pool: pool2, result: []core.SearchResult{{Title: "e1", Engine: "exa"}}}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("transport 失败应级联到 exa 成功: %v", err)
	}
	if results[0].Engine != "exa" {
		t.Fatalf("expected fallback to exa, got %s", results[0].Engine)
	}
	// key 完全未被冷却，且引擎只被调了一次（未换 key 重试）
	if got := pool1.Available(); got != 1 {
		t.Errorf("transport 失败不得冷却任何 key: Available=%d, want 1", got)
	}
	if e1.calls != 1 {
		t.Errorf("transport 失败不得换 key 重试: calls=%d, want 1", e1.calls)
	}
}

// TestApipool_AuthErrorCoolsKey auth 类失败冷却该 key 并换下一个 key 重试（B3 修复 (b)）。
func TestApipool_AuthErrorCoolsKey(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1", "k2"})
	calls := 0
	e1 := &poolAwareEngine{
		name: "tavily", pool: pool1,
		errFn: func(key string) error {
			calls++
			if key == "k1" {
				return provider.NewKeyError(provider.KeyErrorAuth, key, fmt.Errorf("401 unauthorized"))
			}
			return nil
		},
		result: []core.SearchResult{{Title: "t1", Engine: "tavily"}},
	}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e1, pool: pool1})
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Title != "t1" {
		t.Fatalf("expected t1, got %+v", results)
	}
	if got := pool1.Available(); got != 1 {
		t.Errorf("auth 失败应冷却 k1（剩 k2 可用）: Available=%d, want 1", got)
	}
	if calls != 2 {
		t.Errorf("auth 失败应换 key 重试一次: calls=%d, want 2", calls)
	}
}

// TestApipool_RateLimitErrorShortCooldown rate_limit 类失败只短冷却该 key：
// 冷却后 key 在注入的短 TTL 之后即恢复可用（不等 5 分钟真实时长）。
func TestApipool_RateLimitErrorShortCooldown(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1", "k2"})
	e1 := &poolAwareEngine{
		name: "tavily", pool: pool1,
		errFn: func(key string) error {
			if key == "k1" {
				return provider.NewKeyError(provider.KeyErrorRateLimit, key, fmt.Errorf("429 too many requests"))
			}
			return nil
		},
		result: []core.SearchResult{{Title: "t1", Engine: "tavily"}},
	}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e1, pool: pool1})
	ap.rateLimitTTL = 30 * time.Millisecond // 注入短 TTL，避免等真实 5 分钟
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Title != "t1" {
		t.Fatalf("expected t1, got %+v", results)
	}
	// 立即看：k1 短冷却中，k2 可用
	if got := pool1.Available(); got != 1 {
		t.Errorf("rate_limit 失败应短冷却 k1（剩 k2 可用）: Available=%d, want 1", got)
	}
	if got := pool1.Next(); got != "k2" {
		t.Errorf("expected next key k2, got %s", got)
	}
	// 短 TTL 过后 k1 惰性恢复
	time.Sleep(40 * time.Millisecond)
	if got := pool1.Available(); got != 2 {
		t.Errorf("短冷却到期后 k1 应恢复: Available=%d, want 2", got)
	}
}

// TestApipool_ContentErrorDoesNotCoolKeys content 类失败（空结果等）不冷却任何 key（B3）。
func TestApipool_ContentErrorDoesNotCoolKeys(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	e1 := &poolAwareEngine{
		name: "tavily", pool: pool1,
		errFn: func(key string) error {
			return provider.NewKeyError(provider.KeyErrorContent, key, fmt.Errorf("no results"))
		},
	}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e1, pool: pool1})
	if _, err := ap.SearchRaw("test"); err == nil {
		t.Fatal("content 失败应返回错误")
	}
	if got := pool1.Available(); got != 1 {
		t.Errorf("content 失败不得冷却 key: Available=%d, want 1", got)
	}
}

// TestApipool_NonKeyErrorDoesNotCoolKeys 非 KeyError 失败不冷却任何 key
// （B9 收口：旧实现回退 MarkLastInvalid 会误冻池级 lastKey）。
func TestApipool_NonKeyErrorDoesNotCoolKeys(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	e1 := &poolAwareEngine{
		name: "tavily", pool: pool1,
		errFn: func(string) error { return fmt.Errorf("plain failure") },
	}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e1, pool: pool1})
	if _, err := ap.SearchRaw("test"); err == nil {
		t.Fatal("expected error")
	}
	if got := pool1.Available(); got != 1 {
		t.Errorf("非 KeyError 失败不得冷却 key: Available=%d, want 1", got)
	}
}

// ── B8：池内无可用 key 时不打上游 ─────────────────────────────────────────

// TestApipool_AllKeysCooling_NoUpstreamCall 全部 key 冷却中：Next() 返回 ""、
// NextUsable() ok=false，callProviderWithRetry 报错且上游调用次数为 0。
func TestApipool_AllKeysCooling_NoUpstreamCall(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1", "k2"})
	pool1.MarkInvalid("k1")
	pool1.MarkInvalid("k2")
	if got := pool1.Available(); got != 0 {
		t.Fatalf("precondition: all keys cooling, Available=%d", got)
	}
	if got := pool1.Next(); got != "" {
		t.Errorf("全部冷却时 Next() 必须返回空串（不下发已知失效 key）, got %q", got)
	}
	if key, ok := pool1.NextUsable(); ok {
		t.Errorf("全部冷却时 NextUsable() 必须 ok=false, got key=%q ok=true", key)
	}
	pool2, _ := provider.NewKeyPool([]string{"k3"})
	e1 := &poolAwareEngine{name: "tavily", pool: pool1}
	e2 := &poolAwareEngine{name: "exa", pool: pool2, result: []core.SearchResult{{Title: "e1", Engine: "exa"}}}
	ap := NewApipoolSearch("round-robin",
		ApipoolProvider{engine: e1, pool: pool1},
		ApipoolProvider{engine: e2, pool: pool2},
	)
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("应级联到 exa 成功: %v", err)
	}
	if results[0].Engine != "exa" {
		t.Fatalf("expected exa, got %s", results[0].Engine)
	}
	if e1.calls != 0 {
		t.Errorf("全部冷却时不得打上游: calls=%d, want 0", e1.calls)
	}
}

// TestApipool_AllKeysCooling_AllProvidersDown 全供应商全冷却：整体报错且含
// ErrNoAvailableKey 哨兵，上游零调用。
func TestApipool_AllKeysCooling_AllProvidersDown(t *testing.T) {
	pool1, _ := provider.NewKeyPool([]string{"k1"})
	pool1.MarkInvalid("k1")
	e1 := &poolAwareEngine{name: "tavily", pool: pool1}
	ap := NewApipoolSearch("round-robin", ApipoolProvider{engine: e1, pool: pool1})
	_, err := ap.SearchRaw("test")
	if err == nil {
		t.Fatal("expected error when all providers' keys are cooling")
	}
	if !errors.Is(err, provider.ErrNoAvailableKey) {
		t.Errorf("聚合错误应包含 ErrNoAvailableKey 哨兵, got %v", err)
	}
	if e1.calls != 0 {
		t.Errorf("全部冷却时不得打上游: calls=%d, want 0", e1.calls)
	}
}

// TestKeyPool_MarkInvalidFor_ZeroTTLRecovers MarkInvalidFor 的 ttl<=0 视为立即恢复。
func TestKeyPool_MarkInvalidFor_ZeroTTLRecovers(t *testing.T) {
	pool, _ := provider.NewKeyPool([]string{"k1", "k2"})
	pool.MarkInvalidFor("k1", 0)
	if got := pool.Available(); got != 2 {
		t.Errorf("ttl=0 应清零冷却: Available=%d, want 2", got)
	}
}

// ── 辅助类型 ──────────────────────────────────────────────────────────────

// timeRangeRecorder 记录 SearchRawWithTimeRange 调用参数。
type timeRangeRecorder struct {
	name         string
	results      []core.SearchResult
	err          error
	lastLookback int
}

func (t *timeRangeRecorder) Name() string                        { return t.name }
func (t *timeRangeRecorder) Search(query string) (string, error) { return "", nil }
func (t *timeRangeRecorder) SearchRaw(query string) ([]core.SearchResult, error) {
	return t.results, t.err
}
func (t *timeRangeRecorder) MergeContent(_ string, _ []core.SearchResult) (string, error) {
	return "", nil
}
func (t *timeRangeRecorder) SearchRawWithTimeRange(query string, lookbackDays int) ([]core.SearchResult, error) {
	t.lastLookback = lookbackDays
	return t.results, t.err
}

// ── weighted 策略 ─────────────────────────────────────────────────────────

// TestApipool_Weighted_ProportionalSelection 加权随机：选择频率应与有效权重比例一致。
// 有效权重 anysearch:baidu = 30000:1500 = 20:1，2000 次中 anysearch 应占 90% 以上。
func TestApipool_Weighted_ProportionalSelection(t *testing.T) {
	poolA, _ := provider.NewKeyPool([]string{"ka"})
	poolB, _ := provider.NewKeyPool([]string{"kb"})
	ea := &poolAwareEngine{name: "anysearch", pool: poolA, result: []core.SearchResult{{Title: "a", Engine: "anysearch"}}}
	eb := &poolAwareEngine{name: "baidu", pool: poolB, result: []core.SearchResult{{Title: "b", Engine: "baidu"}}}
	ap := NewApipoolSearch("weighted",
		ApipoolProvider{name: "anysearch", engine: ea, pool: poolA},
		ApipoolProvider{name: "baidu", engine: eb, pool: poolB},
	)
	ap.SetWeights(map[string]int{"anysearch": 30000, "baidu": 1500})

	const total = 2000
	counts := map[string]int{}
	for range total {
		results, err := ap.SearchRaw("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[results[0].Engine]++
	}
	if got := counts["anysearch"]; got < total*85/100 {
		t.Errorf("anysearch 应占约 95%%（20:1 加权），2000 次中仅 %d 次", got)
	}
	if got := counts["baidu"]; got > total*15/100 {
		t.Errorf("baidu 应占约 5%%（20:1 加权），2000 次中达 %d 次", got)
	}
}

// TestApipool_Weighted_KeysAccumulate 权重按可用 Key 数累加：
// 两供应商配置权重相同，一方 2 个可用 Key、一方 1 个，比例应为 2:1。
func TestApipool_Weighted_KeysAccumulate(t *testing.T) {
	poolA, _ := provider.NewKeyPool([]string{"a1", "a2"})
	poolB, _ := provider.NewKeyPool([]string{"b1"})
	ea := &poolAwareEngine{name: "tavily", pool: poolA, result: []core.SearchResult{{Title: "a", Engine: "tavily"}}}
	eb := &poolAwareEngine{name: "exa", pool: poolB, result: []core.SearchResult{{Title: "b", Engine: "exa"}}}
	ap := NewApipoolSearch("weighted",
		ApipoolProvider{name: "tavily", engine: ea, pool: poolA},
		ApipoolProvider{name: "exa", engine: eb, pool: poolB},
	)
	ap.SetWeights(map[string]int{"tavily": 100, "exa": 100})

	const total = 2000
	countA := 0
	for range total {
		results, err := ap.SearchRaw("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results[0].Engine == "tavily" {
			countA++
		}
	}
	// 期望 2/3 ≈ 66.7%，3σ 容差约 ±3.2%
	if countA < total*60/100 || countA > total*73/100 {
		t.Errorf("tavily(2 keys) 应占约 66%%，2000 次中 %d 次", countA)
	}
}

// TestApipool_Weighted_ZeroWeightSkipped 权重为 0 的供应商不作为起始选择，
// 但仍保留在失败切换链路中。
func TestApipool_Weighted_ZeroWeightSkipped(t *testing.T) {
	poolA, _ := provider.NewKeyPool([]string{"ka"})
	poolB, _ := provider.NewKeyPool([]string{"kb"})
	ea := &poolAwareEngine{name: "tavily", pool: poolA, result: []core.SearchResult{{Title: "a", Engine: "tavily"}}}
	eb := &poolAwareEngine{name: "exa", pool: poolB, result: []core.SearchResult{{Title: "b", Engine: "exa"}}}
	ap := NewApipoolSearch("weighted",
		ApipoolProvider{name: "tavily", engine: ea, pool: poolA},
		ApipoolProvider{name: "exa", engine: eb, pool: poolB},
	)
	ap.SetWeights(map[string]int{"tavily": 0, "exa": 100})

	for range 20 {
		results, err := ap.SearchRaw("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results[0].Engine != "exa" {
			t.Fatalf("权重 0 的供应商不应被起始选中，got %s", results[0].Engine)
		}
	}
}

// TestApipool_Weighted_AllZero_FallsBackRoundRobin 全部权重为 0 时退化为 round-robin。
func TestApipool_Weighted_AllZero_FallsBackRoundRobin(t *testing.T) {
	poolA, _ := provider.NewKeyPool([]string{"ka"})
	poolB, _ := provider.NewKeyPool([]string{"kb"})
	ea := &poolAwareEngine{name: "tavily", pool: poolA, result: []core.SearchResult{{Title: "a", Engine: "tavily"}}}
	eb := &poolAwareEngine{name: "exa", pool: poolB, result: []core.SearchResult{{Title: "b", Engine: "exa"}}}
	ap := NewApipoolSearch("weighted",
		ApipoolProvider{name: "tavily", engine: ea, pool: poolA},
		ApipoolProvider{name: "exa", engine: eb, pool: poolB},
	)
	ap.SetWeights(map[string]int{"tavily": 0, "exa": 0})

	seen := map[string]bool{}
	for range 4 {
		results, err := ap.SearchRaw("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[results[0].Engine] = true
	}
	if len(seen) != 2 {
		t.Errorf("退化为 round-robin 后应轮转到两家供应商，got %v", seen)
	}
}

// TestApipool_Weighted_FreeEngineWeightOne 免费引擎（pool 为 nil）固定权重 1，保持兜底定位。
func TestApipool_Weighted_FreeEngineWeightOne(t *testing.T) {
	poolA, _ := provider.NewKeyPool([]string{"ka"})
	ea := &poolAwareEngine{name: "anysearch", pool: poolA, result: []core.SearchResult{{Title: "a", Engine: "anysearch"}}}
	free := &mockEngine{name: "baiduWeb", results: []core.SearchResult{{Title: "w", Engine: "baiduWeb"}}}
	ap := NewApipoolSearch("weighted",
		ApipoolProvider{name: "anysearch", engine: ea, pool: poolA},
		ApipoolProvider{name: "baidu_web", engine: free, pool: nil},
	)
	ap.SetWeights(map[string]int{"anysearch": 30000})

	const total = 500
	for range total {
		results, err := ap.SearchRaw("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results[0].Engine != "anysearch" {
			t.Fatalf("免费兜底引擎权重 1（30000:1）不应被起始选中，got %s", results[0].Engine)
		}
	}
}

// TestApipool_Weighted_FallbackOnFail weighted 下起始供应商失败仍切换到下一家。
func TestApipool_Weighted_FallbackOnFail(t *testing.T) {
	poolA, _ := provider.NewKeyPool([]string{"ka"})
	poolB, _ := provider.NewKeyPool([]string{"kb"})
	ea := &poolAwareEngine{name: "tavily", pool: poolA, errFn: func(string) error { return fmt.Errorf("fail") }}
	eb := &poolAwareEngine{name: "exa", pool: poolB, result: []core.SearchResult{{Title: "b", Engine: "exa"}}}
	ap := NewApipoolSearch("weighted",
		ApipoolProvider{name: "tavily", engine: ea, pool: poolA},
		ApipoolProvider{name: "exa", engine: eb, pool: poolB},
	)
	ap.SetWeights(map[string]int{"tavily": 0, "exa": 0}) // 全 0 退化 round-robin：先 tavily（失败）→ exa
	results, err := ap.SearchRaw("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Engine != "exa" {
		t.Errorf("expected fallback to exa, got %s", results[0].Engine)
	}
}

// mockEngine 测试用 SearchInf 替身。
type mockEngine struct {
	name    string
	results []core.SearchResult
	err     error
}

func (m *mockEngine) Name() string                        { return m.name }
func (m *mockEngine) Search(query string) (string, error) { return "", nil }
func (m *mockEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}
func (m *mockEngine) MergeContent(query string, results []core.SearchResult) (string, error) {
	return "", nil
}
