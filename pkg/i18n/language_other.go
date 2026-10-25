//go:build !windows

package i18n

import "golang.org/x/text/language"

// systemLocale 在类 Unix 上没有额外的系统查询通道：语言由 POSIX 环境变量
// （LC_ALL / LC_MESSAGES / LANG）承担，见 systemLanguage。返回 Und 走 fallback。
// 注：macOS 由图形界面启动的进程不继承 shell 的 LANG，此时落到 fallback；
// 需要固定语言时显式配置 language: zh|en。
func systemLocale() language.Tag { return language.Und }
