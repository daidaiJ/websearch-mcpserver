//go:build windows

package i18n

import (
	"syscall"
	"unsafe"

	"golang.org/x/text/language"
)

// localeNameMaxLength 是 Windows 的 LOCALE_NAME_MAX_LENGTH。
const localeNameMaxLength = 85

// procGetUserDefaultLocaleName 直接走 kernel32，与 pkg/proxy 的 Windows 探测同口径，
// 不引入 golang.org/x/sys/windows。
var procGetUserDefaultLocaleName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")

// systemLocale 读取 Windows 用户默认区域（如 zh-CN）；失败返回 language.Und。
func systemLocale() language.Tag {
	buf := make([]uint16, localeNameMaxLength)
	n, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(localeNameMaxLength))
	if n == 0 {
		return language.Und
	}
	// 返回值含结尾 NUL。
	return language.Make(syscall.UTF16ToString(buf[:n-1]))
}
