package proxy

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	// wininet.dll 既不在 KnownDLLs 也不在 Go 的 sysdll 白名单里，
	// syscall.NewLazyDLL 会按默认搜索顺序解析，exe 目录放同名 DLL 即可劫持。
	// NewLazySystemDLL 走 LOAD_LIBRARY_SEARCH_SYSTEM32，只从 System32 加载。
	modwininet            = windows.NewLazySystemDLL("wininet.dll")
	procInternetGetOption = modwininet.NewProc("InternetQueryOptionW")
)

const (
	internetOptionProxy = 38
)

// internetProxyInfo 对应 Windows INTERNET_PROXY_INFO 结构。
type internetProxyInfo struct {
	AccessType  uint32
	Proxy       *uint16
	ProxyBypass *uint16
}

// proxyFromOS 读取 Windows 系统代理设置。
// 对应 "设置 → 网络和 Internet → 代理 → 使用代理服务器"。
func proxyFromOS() string {
	// 先检查注册表（更可靠，不依赖 wininet 的当前状态）
	if ep := proxyFromRegistry(); ep != "" {
		return ep
	}
	// 回退到 WinHTTP API
	return proxyFromWinHTTP()
}

// proxyFromRegistry 从注册表读取 Windows 系统代理。
// 对应 HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings\ProxyEnable / ProxyServer。
func proxyFromRegistry() string {
	// advapi32 在 Go 的 sysdll 白名单内，syscall 会固定从 System32 加载。
	hkey, err := regOpenKey(syscall.HKEY_CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if err != nil {
		return ""
	}
	defer syscall.RegCloseKey(hkey)

	// 检查 ProxyEnable
	enabled, ok := regQueryDWORD(hkey, "ProxyEnable")
	if !ok || enabled == 0 {
		return ""
	}

	// 读取 ProxyServer
	server, ok := regQueryString(hkey, "ProxyServer")
	if !ok || server == "" {
		return ""
	}

	// 解析格式: 可能是 "host:port" 或 "http=host:port;https=host:port"
	return normalizeProxyAddr(server)
}

// normalizeProxyAddr 将 Windows 代理地址转为标准 URL。
// 支持格式: "host:port"、"http=host:port;https=host:port"。
func normalizeProxyAddr(raw string) string {
	// 如果包含分号，取 http= 部分（Clash 默认只设一个，但保险起见）
	if idx := indexOf(raw, '='); idx >= 0 {
		// 格式: "http=host:port;https=..."
		parts := splitSemicolon(raw)
		for _, p := range parts {
			if eqIdx := indexOf(p, '='); eqIdx >= 0 {
				val := p[eqIdx+1:]
				if val != "" {
					return ensureScheme(val)
				}
			}
		}
		return ""
	}
	// 简单格式: "host:port"
	return ensureScheme(raw)
}

func ensureScheme(addr string) string {
	if addr == "" {
		return ""
	}
	// 已有协议前缀
	if len(addr) > 7 && (addr[:7] == "http://" || addr[:8] == "https://") {
		return addr
	}
	// socks5
	if len(addr) > 9 && addr[:9] == "socks5://" {
		return addr
	}
	return "http://" + addr
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func splitSemicolon(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// regOpenKey 打开注册表键。
func regOpenKey(root syscall.Handle, path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	err = syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &h)
	if err != nil {
		return 0, err
	}
	return h, nil
}

// regQueryDWORD 读取注册表 DWORD 值。
func regQueryDWORD(h syscall.Handle, name string) (uint32, bool) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var val uint32
	var size uint32 = 4
	var typ uint32
	err = syscall.RegQueryValueEx(h, n, nil, &typ, (*byte)(unsafe.Pointer(&val)), &size)
	if err != nil || typ != syscall.REG_DWORD {
		return 0, false
	}
	return val, true
}

// regQueryString 读取注册表字符串值。
func regQueryString(h syscall.Handle, name string) (string, bool) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	var buf [1024]byte
	bufLen := uint32(len(buf))
	var typ uint32
	err = syscall.RegQueryValueEx(h, n, nil, &typ, &buf[0], &bufLen)
	if err != nil || typ != syscall.REG_SZ {
		return "", false
	}
	// UTF-16LE → string
	u16 := (*[512]uint16)(unsafe.Pointer(&buf[0]))[: bufLen/2 : bufLen/2]
	// 去掉尾部 NUL
	for i, c := range u16 {
		if c == 0 {
			u16 = u16[:i]
			break
		}
	}
	return syscall.UTF16ToString(u16), true
}

// proxyFromWinHTTP 通过 WinHTTP InternetQueryOption 读取系统代理。
func proxyFromWinHTTP() string {
	var bufLen uint32
	// 第一次调用获取所需缓冲区大小
	procInternetGetOption.Call(0, internetOptionProxy, 0, uintptr(unsafe.Pointer(&bufLen)))
	if bufLen == 0 {
		return ""
	}

	buf := make([]byte, bufLen)
	ret, _, _ := procInternetGetOption.Call(
		0,
		internetOptionProxy,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)
	if ret == 0 {
		return ""
	}

	// 缓冲区至少容纳 INTERNET_PROXY_INFO 结构，否则无法安全读取
	if bufLen < uint32(unsafe.Sizeof(internetProxyInfo{})) {
		return ""
	}

	info := (*internetProxyInfo)(unsafe.Pointer(&buf[0]))
	if info.Proxy == nil {
		return ""
	}

	// 在 buf 范围内从 info.Proxy 读 UTF-16 至 NUL（按缓冲区边界截断，禁止定长强转越界）
	proxy := utf16PtrToString(info.Proxy, buf)
	if proxy == "" {
		return ""
	}

	return normalizeProxyAddr(proxy)
}

func init() {
	// 注册 Windows 平台的系统代理检测函数
	systemProxyDetector = func() string {
		return proxyFromOS()
	}
}
