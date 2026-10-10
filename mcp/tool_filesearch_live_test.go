package mcpserver

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"websearch/pkg/config"
	"websearch/pkg/fetch/everything"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestFileSearchEverythingPaging 真机集成测试：对真实 Everything HTTP Server 验证
// 分页连续性与单页条数收敛。默认跳过（不配置环境变量就不跑，CI 与无 Everything 的
// 机器上不产生假失败）；本地显式验证：
//
//	WS_EVERYTHING_URL=http://127.0.0.1:4180 WS_EVERYTHING_USER=u WS_EVERYTHING_PASS=p \
//	  WS_EVERYTHING_ROOT='D:\CODE\ai' WS_EVERYTHING_QUERY='ext:go' \
//	  go test -run TestFileSearchEverythingPaging -v ./mcp/
func TestFileSearchEverythingPaging(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("WS_EVERYTHING_URL"))
	if url == "" {
		t.Skip("未设置 WS_EVERYTHING_URL，跳过 Everything 真机集成测试")
	}
	root := strings.TrimSpace(os.Getenv("WS_EVERYTHING_ROOT"))
	query := strings.TrimSpace(os.Getenv("WS_EVERYTHING_QUERY"))
	if query == "" {
		query = "ext:go"
	}

	oldInst, oldRoots, oldMax := everythingInst, everythingRoots, everythingMaxResults
	oldNoise, oldAlign := everythingNoise, everythingMinAlign
	t.Cleanup(func() {
		everythingInst, everythingRoots, everythingMaxResults = oldInst, oldRoots, oldMax
		everythingNoise, everythingMinAlign = oldNoise, oldAlign
	})

	everythingInst = everything.New(url, os.Getenv("WS_EVERYTHING_USER"), os.Getenv("WS_EVERYTHING_PASS"), 10*time.Second)
	everythingRoots = nil
	if root != "" {
		everythingRoots = []string{root}
	}
	everythingMaxResults = 10
	everythingNoise = everything.NoiseDirSet(nil)
	everythingMinAlign = 0

	call := func(params *FileSearchParams) string {
		t.Helper()
		res, _, err := FileSearch(t.Context(), &mcp.CallToolRequest{}, params)
		if err != nil {
			t.Fatalf("FileSearch(page=%d max=%d) 失败: %v", params.Page, params.MaxResults, err)
		}
		text, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("非文本响应: %#v", res.Content[0])
		}
		return text.Text
	}
	lines := func(text string) []string {
		var out []string
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "- `") {
				out = append(out, l)
			}
		}
		return out
	}

	// 默认单页 10 条
	p1 := call(&FileSearchParams{Query: query})
	first := lines(p1)
	if len(first) == 0 {
		t.Fatalf("默认查询无结果，无法验证分页: %q", p1)
	}
	if len(first) > 10 {
		t.Errorf("默认单页条数超限: %d\n%s", len(first), p1)
	}

	// 翻页与第 1 页不重复
	p2 := call(&FileSearchParams{Query: query, Page: 2})
	second := lines(p2)
	if len(second) == 0 {
		t.Skipf("命中不足两页（第 1 页 %d 条），跳过翻页断言", len(first))
	}
	seen := map[string]bool{}
	for _, l := range first {
		seen[l] = true
	}
	for _, l := range second {
		if seen[l] {
			t.Errorf("第 2 页与第 1 页重复条目: %s", l)
		}
	}

	// 单页条数收敛：agent 传 50 生效 20
	big := call(&FileSearchParams{Query: query, MaxResults: 50})
	if got := len(lines(big)); got > 20 {
		t.Errorf("max_results=50 未被硬上限收敛: %d 条\n%s", got, big)
	}
	if !strings.Contains(big, "超过硬上限 20") {
		t.Errorf("缺少收敛提示: %q", big)
	}

	// 调小后每页条数如实生效
	small := call(&FileSearchParams{Query: query, MaxResults: 3})
	if got := len(lines(small)); got > 3 {
		t.Errorf("max_results=3 未生效: %d 条\n%s", got, small)
	}

	// 深页码：要么有结果，要么给出空页/触顶说明，不能报错也不能重复第 1 页
	deep := call(&FileSearchParams{Query: query, Page: 100})
	if len(lines(deep)) > 20 {
		t.Errorf("深页码单页条数超限: %d", len(lines(deep)))
	}
}

// TestFileSearchEverythingToolSchema agent 面向契约：file_search 注册后，
// 工具描述与输入 schema 必须暴露 max_results 与 page（含硬上限提示），
// 需与分页测试同一套环境变量（探测通过才会注册该工具）。
func TestFileSearchEverythingToolSchema(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("WS_EVERYTHING_URL"))
	if url == "" {
		t.Skip("未设置 WS_EVERYTHING_URL，跳过 Everything 真机集成测试")
	}
	oldLazy := everythingLazyCfg
	t.Cleanup(func() { everythingLazyCfg = oldLazy })
	everythingLazyCfg = &config.EverythingConfig{
		URL:        url,
		Username:   os.Getenv("WS_EVERYTHING_USER"),
		Password:   os.Getenv("WS_EVERYTHING_PASS"),
		TimeoutSec: 5,
	}

	tool := toolByName(t, config.Config{}, "file_search")
	if !strings.Contains(tool.Description, "page") {
		t.Errorf("工具描述未提示翻页: %q", tool.Description)
	}
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("序列化 input schema 失败: %v", err)
	}
	for _, want := range []string{"max_results", "page", "硬上限 20"} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("input schema 缺少 %q: %s", want, schema)
		}
	}
}

// 分页头如实标注页码且不出现翻页提示（需与分页测试同一套环境变量）。
func TestFileSearchEverythingSinglePageFallback(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("WS_EVERYTHING_URL"))
	if url == "" {
		t.Skip("未设置 WS_EVERYTHING_URL，跳过 Everything 真机集成测试")
	}
	oldInst, oldRoots, oldMax := everythingInst, everythingRoots, everythingMaxResults
	oldNoise, oldAlign := everythingNoise, everythingMinAlign
	t.Cleanup(func() {
		everythingInst, everythingRoots, everythingMaxResults = oldInst, oldRoots, oldMax
		everythingNoise, everythingMinAlign = oldNoise, oldAlign
	})

	everythingInst = everything.New(url, os.Getenv("WS_EVERYTHING_USER"), os.Getenv("WS_EVERYTHING_PASS"), 10*time.Second)
	everythingRoots = nil
	if root := strings.TrimSpace(os.Getenv("WS_EVERYTHING_ROOT")); root != "" {
		everythingRoots = []string{root}
	}
	everythingMaxResults = 10
	everythingNoise = everything.NoiseDirSet(nil)
	everythingMinAlign = 0

	res, _, err := FileSearch(t.Context(), &mcp.CallToolRequest{}, &FileSearchParams{Query: "agent-guide-不存在的关键词-xyzzy"})
	if err != nil {
		t.Fatalf("FileSearch 失败: %v", err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "没有匹配") {
		t.Errorf("零命中应给空结果提示: %q", text)
	}
}
