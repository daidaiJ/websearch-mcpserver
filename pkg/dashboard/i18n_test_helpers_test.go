package dashboard

import (
	"websearch/pkg/config"
	"websearch/pkg/i18n"
)

// testLoc 返回固定中文的 Localizer：控制中心文案断言以 zh 为基准，
// 不受运行测试的机器语言影响（i18n.For 不做全局状态变更）。
func testLoc() *i18n.Localizer { return i18n.For(i18n.Zh) }

// applySettingsForTest 是 applySettingsFile 的测试包装，固定中文 Localizer。
func applySettingsForTest(c config.Config, req SettingsRequest, path string, clearCache func() error) (SettingsResult, error) {
	return applySettingsFile(c, req, path, clearCache, testLoc())
}

// settingsFileViewForTest 是 settingsFileView 的测试包装，固定中文 Localizer。
func settingsFileViewForTest(c config.Config, path string) map[string]any {
	return settingsFileView(c, path, testLoc())
}

// validateSourceCombinationForTest 是 validateSourceCombination 的测试包装。
func validateSourceCombinationForTest(c config.Config) error {
	return validateSourceCombination(c, testLoc())
}
