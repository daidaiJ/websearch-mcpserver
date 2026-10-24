package github

import (
	"os"
	"testing"

	"websearch/internal/testenv"
	"websearch/pkg/antirobot"
)

// TestGitHubSearchNetwork 真网集成测试：默认 auto（可达才跑），-short 跳过，
// WS_TEST_NETWORK=on 强制执行。配 GITHUB_TOKEN 时走提额档并断言限流档位。
func TestGitHubSearchNetwork(t *testing.T) {
	testenv.Require(t, testenv.GitHub)
	eng := NewGitHub(GitHubOpts{Enabled: true, Tokens: splitTokens(os.Getenv("GITHUB_TOKEN"))})
	resp, err := eng.Search("websearch mcpserver", 1, antirobot.TimeRangeNone)
	if testenv.HandleSearchError(t, err) {
		return
	}
	if len(resp.Results) == 0 {
		t.Fatal("真网搜索应至少命中 1 条")
	}
	for _, r := range resp.Results {
		if r.Engine != "github" || r.URL == "" || r.Title == "" {
			t.Fatalf("结果字段不完整: %+v", r)
		}
	}
}

func splitTokens(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}
