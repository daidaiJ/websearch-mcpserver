package quota

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/telemetry"
)

// tContext 是 Get 所需的最小上下文。
func tContext() context.Context { return context.Background() }

func newLocalService(t *testing.T, conf config.Config) (*Service, *telemetry.Store) {
	t.Helper()
	store, err := telemetry.Open(filepath.Join(t.TempDir(), "telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return newService(conf, tavilyUsageEndpoint, nil, store), store
}

func recordN(t *testing.T, store *telemetry.Store, provider string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := store.Record(telemetry.Event{Kind: "provider", Provider: provider, Query: "q", Success: true}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalQuotaCountsRealCalls(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.Quotas.Limits = map[string]int{"tavily": 100}
	svc, store := newLocalService(t, conf)
	recordN(t, store, "tavily", 3)
	recordN(t, store, "doubao", 5)

	items := map[string]Item{}
	for _, it := range svc.Get(tContext()) {
		items[it.Provider] = it
	}
	tav, ok := items["tavily"]
	if !ok || tav.Source != "local" || tav.Used == nil || *tav.Used != 3 {
		t.Fatalf("tavily 本地用量应为 3, got %+v", tav)
	}
	if tav.Limit == nil || *tav.Limit != 100 {
		t.Fatalf("tavily 上限应来自配置 100, got %+v", tav.Limit)
	}
	if tav.AutoReset != "monthly" {
		t.Fatalf("默认重置周期应为 monthly, got %q", tav.AutoReset)
	}
	// doubao 未配置 Key 也未显式给上限 → 不生成条目（不给免费来源编造上限）
	if _, ok := items["doubao"]; ok {
		t.Fatalf("未配置来源不应出现条目, got %+v", items["doubao"])
	}
}

func TestQuotaLimitMultipliedByKeyCount(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.Quotas.Limits = map[string]int{"anysearch": 100, "baidu": 500}
	conf.Anysearch = config.AnysearchConfig{SKList: []string{"k1", "k2"}}
	conf.Baidu = config.BaiduConfig{WebEnabled: true} // 有显式上限但没配 Key
	svc, store := newLocalService(t, conf)
	recordN(t, store, "anysearch", 3)
	recordN(t, store, "baidu", 1)

	items := map[string]Item{}
	for _, it := range svc.Get(tContext()) {
		items[it.Provider] = it
	}
	any, ok := items["anysearch"]
	if !ok {
		t.Fatal("anysearch 应生成条目")
	}
	// 两把 Key：展示上限 = 单 Key 100 × 2 = 200（与 apipool 实际消耗容量对齐）。
	if any.Limit == nil || *any.Limit != 200 {
		t.Fatalf("多 Key 上限应自动换算为 200, got %+v", any.Limit)
	}
	// 无 Key 的显式上限不放大（没有 Key 就没有对应消耗容量）。
	bd := items["baidu"]
	if bd.Limit == nil || *bd.Limit != 500 {
		t.Fatalf("无 Key 时上限应保持 500, got %+v", bd.Limit)
	}
}

func TestQuotaResetAndAdjust(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.Quotas.Limits = map[string]int{"exa": 10}
	svc, store := newLocalService(t, conf)
	recordN(t, store, "exa", 7)

	if err := svc.ResetProvider("exa"); err != nil {
		t.Fatal(err)
	}
	items := svc.Get(tContext())
	var exa Item
	for _, it := range items {
		if it.Provider == "exa" {
			exa = it
		}
	}
	if exa.Used == nil || *exa.Used != 0 {
		t.Fatalf("手动重置后用量应归零, got %+v", exa)
	}

	// 修正为 4：真实记录 7 条不动，修正量 = 4-7 = -3
	if err := svc.SetProviderUsed("exa", 4); err != nil {
		t.Fatal(err)
	}
	for _, it := range rangeItems(t, svc) {
		if it.Provider == "exa" && (it.Used == nil || *it.Used != 4) {
			t.Fatalf("修正后展示用量应为 4, got %+v", it)
		}
	}
	// 修正不会删除真实事件，但重置会开新窗口：重置之后的新调用才计入。
	// 窗口起点是重置秒+1（occurred_at 按秒落库），sleep 跨秒保证确定性。
	if err := svc.ResetProvider("exa"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	recordN(t, store, "exa", 2)
	for _, it := range rangeItems(t, svc) {
		if it.Provider == "exa" && (it.Used == nil || *it.Used != 2) {
			t.Fatalf("重置后应回到真实计数 2, got %+v", it)
		}
	}
}

func TestTavilyOfficialPriority(t *testing.T) {
	conf := config.Config{}
	conf.Dashboard.Quotas.Limits = map[string]int{"tavily": 100}
	svc, store := newLocalService(t, conf)
	recordN(t, store, "tavily", 3)

	// 官方端点不可用（无 Key）→ status not_configured → 本地条目保留
	for _, it := range rangeItems(t, svc) {
		if it.Provider == "tavily" && it.Source != "local" {
			t.Fatalf("官方未配置时应回退本地, got %+v", it)
		}
	}
}

// tContext / rangeItems 是小工具，让断言写得更短。
func rangeItems(t *testing.T, svc *Service) []Item {
	t.Helper()
	return svc.Get(tContext())
}
