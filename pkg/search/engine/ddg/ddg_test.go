package ddg

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"websearch/internal/testenv"

	"websearch/pkg/antirobot"
	"websearch/pkg/proxy"
)

func TestDuckDuckGoSearch(t *testing.T) {
	testenv.Require(t, testenv.DuckDuckGo)
	resolver := func() string { return proxy.DetectSystemProxy() }
	if resolver() == "" {
		t.Skip("跳过: 未检测到系统代理，DuckDuckGo 需要代理")
	}

	eng := NewDuckDuckGo(DuckDuckGoOpts{
		Enabled:      true,
		ProxyResolve: resolver,
	})

	resp, err := eng.Search("golang tutorial", 1, antirobot.TimeRangeNone)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("搜索失败: %v", err)
	}
	if resp.Engine != "duckduckgo" {
		t.Errorf("引擎名错误: got %q", resp.Engine)
	}
	if len(resp.Results) == 0 {
		t.Fatal("期望结果，得到 0 条")
	}

	for i, r := range resp.Results {
		if i >= 3 {
			break
		}
		fmt.Printf("[%d] %s\n    %s\n    %s\n\n", i+1, r.Title, r.URL, r.Content[:min(len(r.Content), 80)])
	}
	fmt.Printf("共 %d 条结果\n", len(resp.Results))
}

func TestDuckDuckGoTimeRange(t *testing.T) {
	testenv.Require(t, testenv.DuckDuckGo)
	resolver := func() string { return proxy.DetectSystemProxy() }
	if resolver() == "" {
		t.Skip("跳过: 未检测到系统代理")
	}

	eng := NewDuckDuckGo(DuckDuckGoOpts{
		Enabled:      true,
		ProxyResolve: resolver,
	})

	resp, err := eng.Search("AI news", 1, antirobot.TimeRangeWeek)
	if err != nil {
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("搜索失败: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("期望结果，得到 0 条")
	}
	fmt.Printf("最近一周 AI news 结果: %d 条\n", len(resp.Results))
}

func TestDuckDuckGoRateLimit(t *testing.T) {
	testenv.Require(t, testenv.DuckDuckGo)
	resolver := func() string { return proxy.DetectSystemProxy() }
	if resolver() == "" {
		t.Skip("跳过: 未检测到系统代理")
	}

	eng := NewDuckDuckGo(DuckDuckGoOpts{
		Enabled:      true,
		ProxyResolve: resolver,
		PerSec:       1,
		PerMin:       5,
	})

	// 等待 2s 避免前序测试触发 DDG 服务端限流
	time.Sleep(2 * time.Second)

	// 第一次应该成功
	resp1, err := eng.Search("test1", 1, antirobot.TimeRangeNone)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 202") {
			t.Logf("⚠️ DDG 服务端限流(HTTP 202)，跳过限流测试: %v", err)
			return
		}
		// CAPTCHA / 代理不可用等环境类失败按 testenv 口径跳过（场景不可用时不误报）
		if testenv.HandleSearchError(t, err) {
			return
		}
		t.Fatalf("第一次搜索失败: %v", err)
	}
	fmt.Printf("第一次: %d 条结果\n", len(resp1.Results))

	// 立即第二次应该被限流（返回空结果）
	time.Sleep(100 * time.Millisecond)
	resp2, _ := eng.Search("test2", 1, antirobot.TimeRangeNone)
	fmt.Printf("第二次（限流）: %d 条结果\n", len(resp2.Results))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
