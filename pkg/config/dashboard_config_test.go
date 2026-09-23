package config

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyAdminPassword(t *testing.T) {
	sha := "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8" // sha256("password")
	conf := DashboardConfig{AdminPassword: "secret1"}
	if !conf.AdminPasswordConfigured() {
		t.Fatal("明文口令应视为已配置")
	}
	if !conf.VerifyAdminPassword("secret1") {
		t.Fatal("正确明文口令应通过")
	}
	if conf.VerifyAdminPassword("wrong") || conf.VerifyAdminPassword("") {
		t.Fatal("错误或空口令不应通过")
	}

	hashed := DashboardConfig{AdminPasswordSHA256: sha}
	if !hashed.VerifyAdminPassword("password") {
		t.Fatal("正确 SHA-256 口令应通过")
	}
	if hashed.VerifyAdminPassword("password1") {
		t.Fatal("错误口令不应通过 SHA-256 校验")
	}

	empty := DashboardConfig{}
	if empty.AdminPasswordConfigured() || empty.VerifyAdminPassword("x") {
		t.Fatal("未配置口令时写闸门应整体关闭")
	}
}

func TestDashboardBrandAccessors(t *testing.T) {
	b := BrandConfig{Theme: "BLUE", Accent: "#1E5DB8"}
	if b.GetTheme() != "blue" {
		t.Fatalf("theme 应规范化为 blue, got %q", b.GetTheme())
	}
	if b.GetAccent() != "#1e5db8" {
		t.Fatalf("accent 应规范化小写, got %q", b.GetAccent())
	}
	if (BrandConfig{Accent: "#abc"}).GetAccent() != "#aabbcc" {
		t.Fatal("#RGB 应扩展为 #rrggbb")
	}
	for _, bad := range []string{"", "14633f", "#12345", "#12zz56"} {
		if (BrandConfig{Accent: bad}).GetAccent() != "" {
			t.Fatalf("非法 accent %q 应返回空", bad)
		}
	}
	if (BrandConfig{}).GetTheme() != "green" || (BrandConfig{}).GetTitle() == "" {
		t.Fatal("空品牌配置应回退内置默认")
	}
}

func TestQuotaPeriodWindow(t *testing.T) {
	q := QuotasConfig{}
	if q.GetReset() != "monthly" || q.GetResetDay() != 1 || q.GetLimit("tavily") != DefaultQuotaLimit {
		t.Fatal("空额度配置应回退默认：monthly / 1 号 / 1000 上限")
	}
	// 每月 5 号重置：3 月 3 日属于 2 月 5 日开始的周期
	q.ResetDay = 5
	start := q.PeriodStart(time.Date(2026, 3, 3, 10, 0, 0, 0, time.Local))
	if start.Month() != time.February || start.Day() != 5 {
		t.Fatalf("月初未到重置日应回退上月, got %v", start)
	}
	start = q.PeriodStart(time.Date(2026, 3, 6, 10, 0, 0, 0, time.Local))
	if start.Month() != time.March || start.Day() != 5 {
		t.Fatalf("重置日之后应取本月, got %v", start)
	}
	if next := q.NextReset(start); next.Month() != time.April || next.Day() != 5 {
		t.Fatalf("下次重置应为下月 5 号, got %v", next)
	}
	if !(QuotasConfig{Reset: "none"}).PeriodStart(time.Now()).IsZero() {
		t.Fatal("none 策略不应产生自动周期起点")
	}
}

func TestParseAllowedNetworks(t *testing.T) {
	nets, err := DashboardConfig{AllowedNetworks: []string{"192.168.1.0/24", "10.0.0.3"}}.ParseAllowedNetworks()
	if err != nil || len(nets) != 2 {
		t.Fatalf("合法 CIDR 与裸 IP 应解析成功: %v %v", nets, err)
	}
	if !nets[1].Contains(net.ParseIP("10.0.0.3")) || nets[1].Contains(net.ParseIP("10.0.0.4")) {
		t.Fatal("裸 IP 应视为单地址网段")
	}
	if _, err := (DashboardConfig{AllowedNetworks: []string{"bad"}}).ParseAllowedNetworks(); err == nil {
		t.Fatal("非法条目必须报错（调用方 fail-closed）")
	}
}

func TestDashboardOverlayMerge(t *testing.T) {
	dir := t.TempDir()
	mainYAML := "port: 8338\ndashboard:\n  enabled: false\nlegacy_key: 1\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(mainYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := `dashboard:
  enabled: true
  admin_password: "pw-1"
  allowed_networks: ["192.168.1.0/24"]
  quotas:
    reset: weekly
    limits: { tavily: 2000 }
  brand:
    title: My Console
    theme: blue
    accent: "#1e5db8"
`
	if err := os.WriteFile(filepath.Join(dir, "dashboard.yaml"), []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	conf, err := Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	d := conf.Dashboard
	if !d.Enabled || d.AdminPassword != "pw-1" || len(d.AllowedNetworks) != 1 {
		t.Fatal("overlay 应覆盖主配置的 dashboard 块")
	}
	if d.Quotas.Reset != "weekly" || d.Quotas.Limits["tavily"] != 2000 {
		t.Fatal("overlay 额度配置应生效")
	}
	if d.Brand.GetTitle() != "My Console" || d.Brand.GetTheme() != "blue" || d.Brand.GetAccent() != "#1e5db8" {
		t.Fatal("overlay 品牌配置应生效")
	}
	if GetDashboardOverlayFile() == "" {
		t.Fatal("应记录 overlay 文件路径")
	}
}

func TestDashboardOverlayMissingAndBroken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("port: 8338\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 无 overlay：加载成功，dashboard 关闭（老配置完全兼容）
	conf, err := Load(filepath.Join(dir, "config.yaml"))
	if err != nil || conf.Dashboard.Enabled || conf.Dashboard.AdminPasswordConfigured() {
		t.Fatalf("老配置应零改动兼容: %v %v", conf.Dashboard, err)
	}
	// 损坏的 overlay：警告并忽略，不拖垮主服务
	if err := os.WriteFile(filepath.Join(dir, "dashboard.yaml"), []byte("dashboard: [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf, err = Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("损坏的 overlay 不应导致加载失败: %v", err)
	}
	if conf.Dashboard.Enabled {
		t.Fatal("损坏的 overlay 应被整体忽略")
	}
}

func TestDashboardShortcutAndFooterDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("port: 8338\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf, err := Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// 零值兼容：旧配置无 shortcut/footer 字段时，落位 = desktop、关于块显示
	if got := conf.Dashboard.GetShortcut(); got != "desktop" {
		t.Fatalf("默认 shortcut 应为 desktop, got %q", got)
	}
	if !conf.Dashboard.Brand.GetFooter() {
		t.Fatal("brand.footer 零值应为 true（默认显示）")
	}
	if err := os.WriteFile(filepath.Join(dir, "dashboard.yaml"), []byte("dashboard:\n  shortcut: start\n  brand:\n    footer: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf, err = Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := conf.Dashboard.GetShortcut(); got != "start" {
		t.Fatalf("overlay shortcut=start 未生效, got %q", got)
	}
	if conf.Dashboard.Brand.GetFooter() {
		t.Fatal("brand.footer=false 未生效")
	}
	// 非法值回退 desktop；别名归一化
	for raw, want := range map[string]string{
		"both": "both", "off": "off", "none": "off", "start-menu": "start", "bogus": "desktop",
	} {
		c := DashboardConfig{Shortcut: raw}
		if got := c.GetShortcut(); got != want {
			t.Fatalf("GetShortcut(%q) = %q, want %q", raw, got, want)
		}
	}
}
