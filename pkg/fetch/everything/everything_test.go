package everything

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// ── FILETIME 转换 ────────────────────────────────────────────────────────────

func TestFiletimeToTime(t *testing.T) {
	// 134353169800707240 → 2026-10-01 16:29:40（本地时区实测值）
	got, ok := filetimeToTime("134353169800707240")
	if !ok {
		t.Fatal("期望解析成功")
	}
	if got.Format("2006-01-02 15:04:05") != "2026-10-01 16:29:40" {
		t.Errorf("FILETIME 转换错误: got %s", got.Format("2006-01-02 15:04:05"))
	}
	for _, bad := range []string{"", "abc", "0", "-123"} {
		if _, ok := filetimeToTime(bad); ok {
			t.Errorf("期望 %q 解析失败", bad)
		}
	}
}

// ── ScopeQuery 目录白名单 ────────────────────────────────────────────────────

var testRoots = []string{`D:\CODE\ai`, `D:\CODE\pro`}

func TestScopeQueryFolderInRoots(t *testing.T) {
	for _, tc := range []struct{ folder, want string }{
		// 路径项保留原始大小写（match_case 时 Everything 对路径项也大小写敏感）
		{`D:\CODE\ai`, `D:\CODE\ai\ *.go`},
		{`d:\code\ai\`, `d:\code\ai\ *.go`},
		{`D:/CODE/ai`, `D:\CODE\ai\ *.go`},
		// Git Bash 风格自动转盘符风格
		{`/d/CODE/ai`, `D:\CODE\ai\ *.go`},
		{`/d/code/ai/`, `D:\code\ai\ *.go`},
		// 子目录以传入目录本身为范围（组件边界校验其仍在白名单内）
		{`D:\CODE\ai\websearch-mcpserver`, `D:\CODE\ai\websearch-mcpserver\ *.go`},
	} {
		got, err := ScopeQuery(testRoots, tc.folder, `*.go`)
		if err != nil {
			t.Fatalf("folder %q 应在白名单内: %v", tc.folder, err)
		}
		if got != tc.want {
			t.Errorf("folder %q: got %q, want %q", tc.folder, got, tc.want)
		}
	}
}

func TestScopeQueryFolderOutsideRoots(t *testing.T) {
	for _, folder := range []string{`C:\Windows`, `D:\CODE\ai2`, `E:\`, `/c/Windows`} {
		if _, err := ScopeQuery(testRoots, folder, `*.go`); err == nil {
			t.Errorf("folder %q 应被白名单拒绝", folder)
		}
	}
}

func TestScopeQueryNoFolderJoinsRoots(t *testing.T) {
	got, err := ScopeQuery(testRoots, "", `factory`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `D:\CODE\ai\|D:\CODE\pro\ factory`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScopeQueryNoRoots(t *testing.T) {
	got, err := ScopeQuery(nil, `C:/anywhere`, `*.pdf`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `C:\anywhere\ *.pdf`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, _ := ScopeQuery(nil, "", `*.pdf`); got != `*.pdf` {
		t.Errorf("无白名单无 folder 时应原样透传: got %q", got)
	}
}

// ── 网络冒烟（-short 跳过；凭据读仓库根 config.test.yaml，该文件不入库） ─────

// testClient 从仓库根 config.test.yaml 的 everything 段构造客户端；
// 文件或段缺失时返回 nil（调用方跳过实机测试）。
func testClient(t *testing.T) *Client {
	t.Helper()
	v := viper.New()
	v.SetConfigFile(filepath.Join("..", "..", "..", "config.test.yaml"))
	if err := v.ReadInConfig(); err != nil {
		t.Skipf("config.test.yaml 不可用，跳过实机测试: %v", err)
	}
	var conf struct {
		Everything struct {
			URL      string   `mapstructure:"url"`
			Username string   `mapstructure:"username"`
			Password string   `mapstructure:"password"`
			Roots    []string `mapstructure:"roots"`
		} `mapstructure:"everything"`
	}
	if err := v.Unmarshal(&conf); err != nil || conf.Everything.URL == "" {
		t.Skipf("config.test.yaml 缺少 everything 段，跳过实机测试")
	}
	return New(conf.Everything.URL, conf.Everything.Username, conf.Everything.Password, 5*time.Second)
}

func TestProbeAndSearchLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test")
	}
	client := testClient(t)
	if client == nil {
		t.Skip("no client")
	}
	if err := client.Probe(context.Background()); err != nil {
		t.Skipf("Everything HTTP Server 不可用: %v", err)
	}

	res, err := client.Search(context.Background(), `D:\CODE\ai\ ext:go`, SearchOptions{Count: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total == 0 || len(res.Items) == 0 {
		t.Fatalf("期望命中 D:\\CODE\\ai 下的 go 文件, got total=%d", res.Total)
	}
	for _, it := range res.Items {
		if it.Name == "" || it.Path == "" {
			t.Fatalf("结果缺少 name/path: %+v", it)
		}
	}
}
