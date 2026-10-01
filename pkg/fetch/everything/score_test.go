package everything

import (
	"testing"
)

// ── 二次过滤：词汇对齐重排 + 噪声降权 + 阈值过滤 ─────────────────────────────

func TestEnhanceItemsRanksNameHitsFirst(t *testing.T) {
	items := []Item{
		{Name: "utils.go", Path: `d:\code\ai\proj\internal\server\handler\`, DateModified: "134353000000000000"},
		{Name: "factory.go", Path: `d:\code\ai\proj\pkg\search\`, DateModified: "134353169800707240"},
		{Name: "readme.txt", Path: `d:\code\ai\docs\`, DateModified: "134353000000000000"},
	}
	got := EnhanceItems(items, "factory", EnhanceOptions{ReRank: true, NoiseDirs: NoiseDirSet(nil)})
	if len(got) != 3 {
		t.Fatalf("不应有结果被剔除: %d", len(got))
	}
	if got[0].Name != "factory.go" {
		t.Errorf("文件名命中的应排第一, got %s", got[0].Name)
	}
}

func TestEnhanceItemsNoiseDemotion(t *testing.T) {
	items := []Item{
		{Name: "factory.go", Path: `d:\code\ai\proj\node_modules\leftpad\`},
		{Name: "factory.go", Path: `d:\code\ai\proj\pkg\search\`},
	}
	got := EnhanceItems(items, "factory", EnhanceOptions{ReRank: true, NoiseDirs: NoiseDirSet(nil)})
	// 同分（文件名+路径对齐率相同）下非噪声路径应排前
	if got[0].Path != `d:\code\ai\proj\pkg\search\` {
		t.Errorf("噪声目录应降权, got %s", got[0].Path)
	}
}

func TestEnhanceItemsIsNoiseComponentMatch(t *testing.T) {
	noise := NoiseDirSet(nil)
	if !isNoise(`d:\proj\bin\debug\`, noise) {
		t.Error("bin 组件应命中噪声")
	}
	if isNoise(`d:\proj\robin\main.go`, noise) {
		t.Error("路径片段含 bin 但组件名是 robin，不应命中")
	}
}

func TestEnhanceItemsMinAlignFilters(t *testing.T) {
	items := []Item{
		{Name: "factory.go", Path: `d:\code\ai\proj\`},
		{Name: "readme.txt", Path: `d:\code\ai\docs\`},
	}
	got := EnhanceItems(items, "factory", EnhanceOptions{ReRank: true, NoiseDirs: NoiseDirSet(nil), MinAlign: 0.5})
	if len(got) != 1 || got[0].Name != "factory.go" {
		t.Errorf("阈值过滤应只保留 factory.go: %+v", got)
	}
	// 阈值 0 = 只重排不过滤
	got = EnhanceItems(items, "factory", EnhanceOptions{ReRank: true, NoiseDirs: NoiseDirSet(nil)})
	if len(got) != 2 {
		t.Errorf("阈值为 0 不应剔除: %d", len(got))
	}
}

func TestEnhanceItemsReRankFalseKeepsServerOrder(t *testing.T) {
	// 用户显式指定 sort 时尊重服务端顺序：即使 factory.go 对齐分更低也不重排
	items := []Item{
		{Name: "readme.txt", Path: `d:\code\ai\docs\`},
		{Name: "factory.go", Path: `d:\code\ai\proj\pkg\search\`},
	}
	got := EnhanceItems(items, "factory", EnhanceOptions{ReRank: false, NoiseDirs: NoiseDirSet(nil)})
	if got[0].Name != "readme.txt" {
		t.Errorf("ReRank=false 应保留服务端顺序, got %s", got[0].Name)
	}
}

func TestNoiseDirSetEmptyArrayDisables(t *testing.T) {
	if set := NoiseDirSet([]string{}); len(set) != 0 {
		t.Errorf("显式空数组应关闭降权: %v", set)
	}
	if set := NoiseDirSet(nil); len(set) == 0 {
		t.Error("nil 应使用内置默认噪声目录")
	}
}
