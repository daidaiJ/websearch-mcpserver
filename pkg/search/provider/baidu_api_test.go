package provider

import (
	"testing"
)

// ── 集成测试（从 config.test.yaml 加载 API Key） ──

// skipIfLiveBaiduUnavailable 当百度实时 API 因环境因素（网络不可达 / 鉴权 / 配额 / 空结果）
// 返回错误时跳过测试，避免 go test ./... 因外部依赖不可用而失败；happy path 仍会执行断言。
func skipIfLiveBaiduUnavailable(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Skipf("跳过: 百度实时 API 不可用（环境因素）: %v", err)
	}
}

func TestBaiduSearchImpl_SearchRaw_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadBaiduAPIKey(t)

	baidu := NewBaiduSeach(newTestKeyPool(t, apiKey), []string{"csdn.net"})
	results, err := baidu.SearchRaw("Go programming language")
	skipIfLiveBaiduUnavailable(t, err)
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	for i, r := range results {
		t.Logf("[%d] %s - %s", i+1, r.Title, r.Url)
		if r.Title == "" {
			t.Error("result title should not be empty")
		}
		if r.Url == "" {
			t.Error("result url should not be empty")
		}
		if r.Engine != "baidu_api" {
			t.Errorf("expected engine 'baidu_api', got %s", r.Engine)
		}
	}
}

func TestBaiduSearchImpl_Search_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadBaiduAPIKey(t)

	baidu := NewBaiduSeach(newTestKeyPool(t, apiKey), nil)
	output, err := baidu.Search("latest AI news")
	skipIfLiveBaiduUnavailable(t, err)
	if output == "" {
		t.Fatal("expected non-empty output")
	}
	t.Logf("Search output:\n%s", output)
}

func TestBaiduSearchImpl_SearchRawWithTimeRange_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试: -short 模式")
	}
	apiKey := loadBaiduAPIKey(t)

	baidu := NewBaiduSeach(newTestKeyPool(t, apiKey), nil)

	// 测试时间范围: day
	results, err := baidu.SearchRawWithTimeRange("AI news", 1)
	skipIfLiveBaiduUnavailable(t, err)
	t.Logf("day 范围结果数: %d", len(results))

	// 测试时间范围: week
	results, err = baidu.SearchRawWithTimeRange("AI news", 7)
	skipIfLiveBaiduUnavailable(t, err)
	t.Logf("week 范围结果数: %d", len(results))

	// 测试时间范围: month
	results, err = baidu.SearchRawWithTimeRange("AI news", 30)
	skipIfLiveBaiduUnavailable(t, err)
	t.Logf("month 范围结果数: %d", len(results))

	// 测试时间范围: semiyear
	results, err = baidu.SearchRawWithTimeRange("AI news", 180)
	skipIfLiveBaiduUnavailable(t, err)
	t.Logf("semiyear 范围结果数: %d", len(results))

	// 测试不限时间范围（省略筛选字段）
	results, err = baidu.SearchRawWithTimeRange("AI news", 0)
	skipIfLiveBaiduUnavailable(t, err)
	t.Logf("不限时间范围结果数: %d", len(results))
}

func TestLookbackDaysToRecency(t *testing.T) {
	tests := []struct {
		days int
		want string
	}{
		{0, "semiyear"}, // 默认
		{1, "day"},
		{3, "week"},
		{7, "week"},
		{15, "month"},
		{30, "month"},
		{90, "semiyear"},
		{180, "semiyear"},
		{365, "year"},
	}
	for _, tt := range tests {
		got := lookbackDaysToRecency(tt.days)
		if got != tt.want {
			t.Errorf("lookbackDaysToRecency(%d) = %s, want %s", tt.days, got, tt.want)
		}
	}
}
