# README 配图渲染源

README 顶部四张配图（`hero-banner` / `architecture` / `toolchain` / `pipeline`）由本目录的 HTML 渲染而来，产物直接落在上一级 `docs/images/`。

| 文件 | 作用 |
|------|------|
| `readme-diagrams.html` | 单页四张图，各占一个 `<section class="board">`，id 为 `hero` / `architecture` / `toolchain` / `pipeline` |
| `render.py` | Playwright 逐 section 截图，输出 1280px 宽、2 倍缩放的 PNG |
| `render_dashboard.py` | 用已提交的 `pkg/dashboard/web/dist` 加 mock API 数据，渲染 1400×875 的控制中心截图（亮色主题） |

改图流程：

```bash
# 依赖：Python + playwright（python -m playwright install chromium）
python docs/images/src/render.py
```

- 图上文案与 README 正文的一致性靠人工维护，改 README 图注时同步检查 HTML 内的文字。
- 需要调色或改版式时改 HTML 顶部的 CSS 变量，不要手工编辑 `docs/images/*.png`。
- `transport-modes.svg` / `transport-modes.en.svg` 是手写 SVG，配色与上面四张图及控制中心亮色主题保持一致；改色直接改 SVG。
- 根 `.gitignore` 里的 `*.html` 全局忽略带 `!docs/**/*.html` 例外，本文件因此可入库。

## 控制中心截图

`render_dashboard.py` 直接从已提交的前端构建产物渲染，不依赖 `frontend/node_modules` 或运行中的 Go 服务；API 响应由脚本内的 mock 数据提供，使用亮色主题和中文界面。改控制中心布局或示例数据后重渲染：

```bash
# 依赖：Python + playwright + Pillow
python docs/images/src/render_dashboard.py
```

产物为 `docs/images/dashboard-overview.jpg` / `dashboard-sources.jpg` / `dashboard-usage.jpg` / `dashboard-settings.jpg`。截图只用于 README 和文档示意，不包含真实遥测数据。

## PDF 解析状态图

`pdf-parser-state.md` 是 [`docs/pdf-parser.md`](../../pdf-parser.md) 里 `pdf_parser` 处理状态图的 Mermaid 源，产物为 `docs/images/pdf-parser-state.png`。用 mmdx 渲染：

```bash
mmdx docs/images/src/pdf-parser-state.md -o docs/images/pdf-parser-state.png \
  --width 1280 --scale 2 --title "pdf_parser 处理状态"
```

改流程或 PDFium / MinerU 分支时同步改 Mermaid 源并重渲染，不要手工编辑 PNG。
