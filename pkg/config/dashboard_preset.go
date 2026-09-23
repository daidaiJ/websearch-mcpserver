package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// dashboardExplicit 用户是否在配置里显式声明过控制中心（主配置 dashboard:
// 块或独立 dashboard.yaml 任一存在即视为显式）。用于「默认启用控制台」
// 的判定：显式配置过的部署绝不静默追加启用配置。
var dashboardExplicit bool

// DashboardExplicitlyConfigured 报告用户是否显式配置过控制中心。
func DashboardExplicitlyConfigured() bool { return dashboardExplicit }

const dashboardPresetTemplate = `# ==============================================
# 控制中心配置（默认启用）
# ==============================================
# 不需要时：把 enabled 改为 false，或直接删除本文件并重启 ——
# 运行时即回到零遥测开销；主 config.yaml 无需任何改动。
# 完整可配置项见 dashboard.example.yaml 或 docs/configuration.md。
dashboard:
  enabled: true
  # 快捷方式落位（Windows）：desktop（桌面，默认）/ start（开始菜单，开始屏幕
  # 可搜索并手动固定）/ both / off（不创建）。完整配置项见 docs/dashboard.md。
  shortcut: desktop
  # 本机管理口令：写操作（设置 / API Key / 重启 / 清缓存 / 额度管理）需要它，
  # 仅限本机使用；如需更换请手改本文件后重启（WebUI 无法修改口令）。
  admin_password: %q
  # 可选管理员用户名：配置后写请求还需携带匹配的 X-Admin-User；
  # 不配置 = 只验证口令（与上游行为一致）。同样 WebUI 无法读取或修改。
  # admin_username: "admin"
  storage_path: "./data/dashboard.db"
  retention_days: 30
  # 只读放行网段（CIDR），默认仅本机；Docker 部署按需放行 bridge 网段
  # allowed_networks: ["192.168.1.0/24"]
  quotas:
    reset: monthly        # monthly / weekly / daily / none
    limits: { tavily: 1000, exa: 1000, anysearch: 1000, doubao: 1000 }
  brand:
    title: ""             # 控制台标题；空 = "WebSearch 控制中心"
    theme: ""             # green（默认）/ blue / mono
`

// randomPassword 生成 128-bit 随机口令（十六进制，20 字符截取）。
func randomPassword() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)[:20]
}

// EnsureDashboardFile 在 dir 下生成默认启用的控制中心配置（dashboard.yaml）。
// 约定：全新部署开箱即有 WebUI；不需要时改 enabled: false 或删文件。
// 幂等：文件已存在时不做任何修改（不覆盖用户口令）。返回是否新创建。
func EnsureDashboardFile(dir string) (bool, string, error) {
	path := filepath.Join(dir, "dashboard.yaml")
	if _, err := os.Stat(path); err == nil {
		return false, path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, path, err
	}
	password := randomPassword()
	if password == "" {
		// 随机源不可用的极端场景：宁可留空（写操作保持禁用）也不写死默认口令
		password = ""
	}
	content := fmt.Sprintf(dashboardPresetTemplate, password)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return false, path, err
	}
	return true, path, nil
}
