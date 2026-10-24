package sitefetch

import (
	"context"
	"os"
	"strings"
	"testing"

	"websearch/internal/testenv"
)

// TestGitHubFetcherRepoNetwork 真网集成测试：仓库根（元数据 + README）与 blob→contents API 两条路径。
func TestGitHubFetcherRepoNetwork(t *testing.T) {
	testenv.Require(t, testenv.GitHub)
	f := NewGitHubFetcher(splitTokenList(os.Getenv("GITHUB_TOKEN")))
	text, err := f.Fetch(context.Background(), "https://github.com/golang/go")
	if testenv.HandleSearchError(t, err) {
		return
	}
	for _, want := range []string{"# golang/go", "The Go programming language"} {
		if !strings.Contains(text, want) {
			t.Errorf("仓库页输出缺 %q", want)
		}
	}
	raw, err := f.Fetch(context.Background(), "https://github.com/golang/go/blob/master/README.md")
	if testenv.HandleSearchError(t, err) {
		return
	}
	if strings.TrimSpace(raw) == "" {
		t.Error("blob→contents API 抓取正文为空")
	}
}

func splitTokenList(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}
