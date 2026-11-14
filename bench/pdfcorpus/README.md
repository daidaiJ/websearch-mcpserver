# PDF 病态语料对照

对照结论（ledongthuc 日常够用、病态偏弱；PDFium DLL / wasm 不挂死但代价不同）写在 [`docs/pdf-parser.md`](../../docs/pdf-parser.md)。这里只说明怎么复跑。

合成 PDF 来自 `ledongthuc/pdf` 等上游的 crash/hang/OOM issue。每个 `(引擎, 用例)` 在独立子进程里跑，5 秒杀掉。本目录是独立 `go.mod`，不会进主模块。

C 内核用捆绑库 / 官方 wheel / 预编译 CLI，不在本机从源码编译。

## 引擎

| 引擎 | 接入 | 依赖 |
|------|------|------|
| `ledongthuc` / `pdfcpu` / `aspose` / `razvan` / `gofastpdf` | 库 | 随 `go.mod` |
| `go-fitz` | MuPDF C | 仅 CGO 构建；CGO=0 时自动跳过 |
| `pdfium-dll` | `purego` 运行时加载 `pdfium.dll` / `libpdfium.so` | `PDFCORPUS_PDFIUM_DLL` 或 exe 旁的 `pdfium.dll` |
| `pdfium-wasm` | `klippa-app/go-pdfium` 的 wazero 后端 | 内嵌 wasm，无外部文件 |
| `pymupdf` / `pypdfium2` / `pypdf` / `pdfminer` | 本机 Python | `extract.py` |
| `pdfjs` | Node + pdfjs-dist | `PDFCORPUS_JSROOT` |
| `pdfcpu-cli` / `poppler` / `litho` | 预编译 CLI | 对应环境变量 |

## 复跑

```bash
cd bench/pdfcorpus
go build -o D:/tmp/pdfcorpus.exe .

# 最少：Go 引擎（ledongthuc / pdfcpu / aspose / razvan / gofastpdf / pdfium-wasm）
D:/tmp/pdfcorpus.exe

# 只跑 PDFium 对照（推荐）
PDFCORPUS_PDFIUM_DLL=D:/tmp/pdfcorpus-tools/pdfium/bin/pdfium.dll \
PDFCORPUS_ENGINES=ledongthuc,pdfium-dll,pdfium-wasm \
D:/tmp/pdfcorpus.exe

# 全量（路径按本机实际）
PDFCORPUS_JSROOT=D:/tmp/pdfcorpus-js \
PDFCORPUS_PDFCPU=D:/tmp/pdfcorpus-tools/pdfcpu_0.16.4_Windows_x86_64/pdfcpu.exe \
PDFCORPUS_POPPLER=D:/tmp/pdfcorpus-tools/poppler-26.09.0/Library/bin/pdftotext.exe \
D:/tmp/pdfcorpus.exe
```

无 CGO 构建：

```bash
CGO_ENABLED=0 go build -o D:/tmp/pdfcorpus-nocgo.exe .
```

`go-fitz` 在 CGO=0 构建里不注册；`pdfium-dll` 与 `pdfium-wasm` 都不需要 CGO。

## 环境变量

| 变量 | 作用 |
|------|------|
| `PDFCORPUS_ENGINES` | 逗号分隔的引擎白名单，例如 `ledongthuc,pdfium-dll,pdfium-wasm`；`PDFCORPUS_ONLY` 为同义别名 |
| `PDFCORPUS_PDFIUM_DLL` | `pdfium.dll` / `libpdfium.so` 路径；不设时找 exe 旁 |
| `PDFCORPUS_JSROOT` | 含 `extract.mjs` + `node_modules/pdfjs-dist` 的目录 |
| `PDFCORPUS_PDFCPU` | 官方预编译 `pdfcpu.exe` |
| `PDFCORPUS_POPPLER` | Poppler `pdftotext.exe`（与 PATH 上的 xpdf `pdftotext` 分开） |
| `PDFCORPUS_TIMEOUT_MS` | 病态用例的抽取超时，默认 5000；用于区分「慢」和「挂死」 |
| `PDFCORPUS_STRESS` | 引擎名；跑长跑模式 |
| `PDFCORPUS_STRESS_ITERS` | 长跑次数，默认 3000 |
| `PDFCORPUS_BENCH` / `PDFCORPUS_BENCH_DIR` | 内部：warm bench 子进程参数 |

## 判定与计时

- `OK` = 打开且保留预期子串；`miss` = 打开但预期子串不在；`err` = 进程返回错误；`TIMEOUT` = 抽取阶段被杀；`OK+form` = 额外抽出 Form XObject 文本。
- PDFium 两种后端先在 worker 内完成库加载 / wasm 编译，再向 stderr 打 `PDFCORPUS_READY`；父进程从 READY 起计 5 秒，避免把 wasm 约 4 秒的冷编译误判为挂死。
- 常态性能分两张表：
  - **warm process**：引擎加载一次，每个 fixture 预热后跑 7 轮；1 页每轮 500 次、20 页每轮 20 次，取每轮平均再取中位。文件读取在计时外，ledongthuc 用 `pdf.NewReader` 走内存。
  - **process-inclusive**：Python / Node / CLI 保留「启动 + 抽取」的真实成本，不与 warm 表直接比较。
- 长跑模式每 500 次打印 `STRESS|iterations|chars|elapsed_ms`，可用外部采样器（如 psutil）同步记录 RSS。

Python 引擎在 `extract.py`：`pymupdf` / `pypdfium2` / `pypdf` / `pdfminer`。Tsinghua 镜像可能没有 PyMuPDF，装 wheel 走 `https://pypi.org/simple`。
