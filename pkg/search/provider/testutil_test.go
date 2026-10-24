package provider

import (
	"websearch/pkg/search/core"

	"os"
	"testing"

	"websearch/pkg/config"
)

// loadTestConfig 从 config.test.yaml 加载测试配置。
// 如果文件不存在或加载失败，跳过测试。
func loadTestConfig(t *testing.T) config.Config {
	t.Helper()

	// 设置测试配置文件路径
	os.Setenv("WEBSEARCH_CONFIG", "../../../config.test.yaml")

	conf, err := config.Load("")
	if err != nil {
		t.Skipf("跳过: 无法加载测试配置: %v", err)
	}
	return *conf
}

// newTestKeyPool 创建单 key 测试用 KeyPool。
func newTestKeyPool(t *testing.T, key string) *KeyPool {
	t.Helper()
	pool, err := NewKeyPool([]string{key})
	if err != nil {
		t.Fatalf("创建 KeyPool 失败: %v", err)
	}
	return pool
}

// loadBaiduAPIKey 从测试配置加载百度 API Key。
func loadBaiduAPIKey(t *testing.T) string {
	t.Helper()
	conf := loadTestConfig(t)
	if conf.Baidu.APIKey == "" {
		t.Skip("跳过: 未配置 baidu.api_key")
	}
	return conf.Baidu.APIKey
}

// loadTavilyAPIKey 从测试配置加载 Tavily API Key。
func loadTavilyAPIKey(t *testing.T) string {
	t.Helper()
	conf := loadTestConfig(t)
	if conf.Tavily.APIKey == "" {
		t.Skip("跳过: 未配置 tavily.api_key")
	}
	return conf.Tavily.APIKey
}

// loadExaAPIKey 从测试配置加载 Exa API Key。
func loadExaAPIKey(t *testing.T) string {
	t.Helper()
	conf := loadTestConfig(t)
	if conf.Exa.APIKey == "" {
		t.Skip("跳过: 未配置 exa.api_key")
	}
	return conf.Exa.APIKey
}

// loadAnysearchAPIKey 从测试配置加载 AnySearch API Key。
func loadAnysearchAPIKey(t *testing.T) string {
	t.Helper()
	conf := loadTestConfig(t)
	if conf.Anysearch.APIKey == "" {
		t.Skip("跳过: 未配置 anysearch.api_key")
	}
	return conf.Anysearch.APIKey
}

// loadDoubaoAPIKey 从测试配置加载豆包联网搜索 API Key。
func loadDoubaoAPIKey(t *testing.T) string {
	t.Helper()
	conf := loadTestConfig(t)
	if conf.Doubao.APIKey == "" {
		t.Skip("跳过: 未配置 doubao.api_key")
	}
	return conf.Doubao.APIKey
}

// loadYuanbaoAPIKey 从测试配置加载腾讯云元宝搜索 API Key。
func loadYuanbaoAPIKey(t *testing.T) string {
	t.Helper()
	conf := loadTestConfig(t)
	if conf.Yuanbao.APIKey == "" {
		t.Skip("跳过: 未配置 yuanbao.api_key")
	}
	return conf.Yuanbao.APIKey
}

// mockEngine 测试用 SearchInf 替身。
type mockEngine struct {
	name    string
	results []core.SearchResult
	err     error
}

func (m *mockEngine) Name() string { return m.name }
func (m *mockEngine) Search(query string) (string, error) {
	return "", nil
}
func (m *mockEngine) SearchRaw(query string) ([]core.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}
func (m *mockEngine) MergeContent(query string, results []core.SearchResult) (string, error) {
	return "", nil
}
