package ddg

import (
	"os"
	"testing"
	"time"

	"websearch/pkg/i18n"
)

// TestMain 把进程语言钉成 zh：本包测试断言的是中文文案，而 language 的默认值
// auto 会跟随运行机器的系统语言（CI 为英文），不钉就会变成依赖机器的不稳定用例。
func TestMain(m *testing.M) {
	i18n.Configure(i18n.Zh)
	// 缩短前置退避等待：并发用例不应被真实 sleep（800ms~1.8s）拖慢
	ddgBaseDelay, ddgJitter = time.Millisecond, time.Millisecond
	os.Exit(m.Run())
}
