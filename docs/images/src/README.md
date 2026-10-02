# README 配图渲染源

README 顶部四张配图（`hero-banner` / `architecture` / `toolchain` / `pipeline`）由本目录的 HTML 渲染而来，产物直接落在上一级 `docs/images/`。

| 文件 | 作用 |
|------|------|
| `readme-diagrams.html` | 单页四张图，各占一个 `<section class="board">`，id 为 `hero` / `architecture` / `toolchain` / `pipeline` |
| `render.py` | Playwright 逐 section 截图，输出 1280px 宽、2 倍缩放的 PNG |

改图流程：

```bash
# 依赖：Python + playwright（python -m playwright install chromium）
python docs/images/src/render.py
```

- 图上文案与 README 正文的一致性靠人工维护，改 README 图注时同步检查 HTML 内的文字。
- 需要调色或改版式时改 HTML 顶部的 CSS 变量，不要手工编辑 `docs/images/*.png`。
- 根 `.gitignore` 里的 `*.html` 全局忽略带 `!docs/**/*.html` 例外，本文件因此可入库。
