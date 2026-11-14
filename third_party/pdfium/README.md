# PDFium 预编译动态库

本地 PDF 文本解析的可选实验性后端。平台压缩包按架构携带一份 PDFium 动态库；服务启动时不加载，只在首次需要解析本地 PDF 时按需打开。库缺失或加载失败会回退到纯 Go 解析，不影响默认行为。

## 目录约定

| 目录 | 对应平台 | 动态库 |
|------|----------|--------|
| `win-x64/` | Windows x86_64 | `pdfium.dll` |
| `linux-x64/` | Linux x86_64 | `libpdfium.so` |
| `linux-arm64/` | Linux ARM64（容器/手工部署） | `libpdfium.so` |
| `mac-x64/` | macOS Intel | `libpdfium.dylib` |
| `mac-arm64/` | macOS Apple Silicon | `libpdfium.dylib` |

每个目录同时保留 PDFium 的 `LICENSE`、`licenses/` 与 `VERSION`。

## 获取

```bash
bash third_party/pdfium/fetch.sh              # 全部架构
bash third_party/pdfium/fetch.sh win-x64      # 只取指定架构
```

脚本从 [bblanchon/pdfium-binaries](https://github.com/bblanchon/pdfium-binaries) 下载固定版本 `chromium/8152`（PDFium 159.0.8152.0），按脚本内固定的 SHA-256 校验后解压。Release CI 在打包平台压缩包时调用同一脚本，把对应架构的库放进 `third_party/pdfium/<架构>/`；使用整包的用户解压即得，不需要自己运行脚本。下载物被根目录 `.gitignore` 的 `*.dll` / `*.so` / `*.dylib` 忽略，不进入版本库。

## 运行时查找顺序

设置 `pdf_parser.pdfium.path` 时按该路径查找；为空时使用平台默认文件名。相对路径依次在以下目录查找：

1. 可执行文件目录
2. 可执行文件目录下的 `third_party/pdfium/<架构>/`
3. 配置目录
4. 配置目录下的 `third_party/pdfium/<架构>/`

放在可执行文件或配置目录根部的库优先于 `third_party` 内的标准副本，便于手工替换。省略 `pdf_parser.pdfium.enabled` 时自动检测；显式 `true` 强制启用并在缺失时告警，`false` 关闭。

## 许可与构建

PDFium 为 BSD-3-Clause（Chromium 项目），预编译包内附 `LICENSE` 与第三方 `licenses/`。主程序仍为纯 Go、无 CGO、MIT；动态库通过 purego 在运行时加载，不链接进二进制。分发时保留动态库旁的许可文件。
