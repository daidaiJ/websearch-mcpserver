package webfetch

import (
	"os"
	"testing"

	"websearch/pkg/i18n"
)

// TestMain 把进程语言钉成 zh：本包测试断言的是中文文案，而 language 的默认值
// auto 会跟随运行机器的系统语言（CI 为英文），不钉就会变成依赖机器的不稳定用例。
func TestMain(m *testing.M) {
	i18n.Configure(i18n.Zh)
	os.Exit(m.Run())
}
