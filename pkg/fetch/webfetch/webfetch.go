package webfetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"websearch/pkg/config"
	"websearch/pkg/log"
	"websearch/pkg/fetch/mineru"

	webfetch "github.com/daidaiJ/go-webfetch"
)

// Result 封装 go-webfetch 的返回结果。
type Result struct {
	Title      string
	Mode       string // "inline" 或 "saved_to_file"
	Markdown   string
	FilePath   string
	TotalLines int
	TotalChars int
	AgentHint  string
	PageCount  int    // PDF 内容：文档总页数（非 PDF 为 0）
	Preamble   string // 页截断/选择说明，工具输出时置于正文前
	ParseEngine string // PDF：实际解析器（pdf-lib / mineru-remote / mineru-ocr）
	ParsedPages int    // PDF：本次实际解析页数；0 表示解析器未返回页数
}

// fetchEngine 抽象 go-webfetch 抓取引擎，便于测试注入替身。
type fetchEngine interface {
	Fetch(ctx context.Context, rawURL string) (*webfetch.FetchResult, error)
	FetchWithOpts(ctx context.Context, rawURL string, opts webfetch.FetchOptions) (*webfetch.FetchResult, error)
	ParsePDFFile(ctx context.Context, filePath string, opts ...webfetch.PDFOption) (*webfetch.PDFResult, error)
	Close() error
}

// mineruParser 抽象 MinerU 客户端，便于测试注入替身。
type mineruParser interface {
	HasToken() bool
	ParseURL(ctx context.Context, fileURL string) (string, error)
	ParseFile(ctx context.Context, filePath string) (string, error)
}

// Fetcher 封装 go-webfetch Engine。
type Fetcher struct {
	engine          fetchEngine
	mineru          mineruParser
	mineruOCR       bool // 本地 PDF 库读不到文本时是否回退 MinerU OCR
	mineruRemotePDF bool // 远程 PDF URL 是否走 MinerU 精准 API（配置 pdf_parser.mineru_remote_pdf）

	outputDir string        // 大文本落盘目录（清理范围仅限此目录）
	fileTTL   time.Duration // 落盘文件保留时长

	cleanupStop  chan struct{} // 关闭以停止过期文件清理协程
	inflight     atomic.Int64  // 进行中的抓取数（空闲判定用）
	lastActivity atomic.Int64  // 最近一次抓取活动的 UnixNano 时间戳
}

// 文件清理只在空闲期执行：距上次抓取活动超过 idleThreshold 且无进行中抓取。
const (
	fileCleanupIdleThreshold = 1 * time.Minute
	fileCleanupCheckInterval = 1 * time.Minute
)

// savedFileRe 匹配本程序落盘的文件名（buildFilename：日期_时间_标题slug_6位hash.md，
// slug 由上游 slugify 生成、不含下划线）。清理只删除匹配该模式的过期文件，
// 用户放在输出目录里的其它文件（任意 .md/.txt 等）一律不动。
var savedFileRe = regexp.MustCompile(`^\d{8}_\d{6}_.+_[0-9a-f]{6}\.md$`)

// NewFromConfig 根据配置创建 Fetcher。proxyURL 为代理地址，空字符串表示不使用代理（仍回退到环境变量）。
func NewFromConfig(cfg config.CleanFetchConfig, pdfCfg config.PDFParserConfig, proxyURL string) (*Fetcher, error) {
	outputDir := cfg.FileOutputDir
	if outputDir == "" {
		// 默认 exe 同目录的 fetchdata 子目录（配置未指定时）
		outputDir = filepath.Join(config.ExeBaseDir(), "fetchdata")
	}

	fileTTL := time.Duration(cfg.FileTTL) * time.Hour
	if fileTTL <= 0 {
		fileTTL = 24 * time.Hour
	}

	maxInlineLines := cfg.MaxInlineLines
	if maxInlineLines <= 0 {
		maxInlineLines = 100
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	engine, err := webfetch.New(webfetch.Config{
		BlockPrivateIP: true,
		Timeout:        timeout,
		MaxInlineLines: maxInlineLines,
		MaxInlineChars: cfg.MaxInlineChars,
		FileOutputDir:  outputDir,
		FileTTL:        fileTTL,
		ProxyURL:       proxyURL,
		UseSystemProxy: cfg.UseSystemProxy,
		MaxRetries:     cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("webfetch engine init failed: %w", err)
	}

	log.Infof("WebFetch 引擎已启用 (output_dir=%s, ttl=%s, max_inline_lines=%d, timeout=%s)", outputDir, fileTTL, maxInlineLines, timeout)

	f := &Fetcher{
		engine:          engine,
		mineruOCR:       pdfCfg.MinerUOCREnabled(),
		mineruRemotePDF: pdfCfg.MinerURemotePDF,
		outputDir:       outputDir,
		fileTTL:         fileTTL,
	}
	f.startFileJanitor(fileTTL)

	// 初始化 MinerU 客户端（有 Token 或开启 OCR 回退时）
	if pdfCfg.MinerUEnabled() {
		f.mineru = mineru.NewFromConfig(
			pdfCfg.MinerUToken,
			pdfCfg.GetMinerUModel(),
			pdfCfg.GetMinerULang(),
			pdfCfg.MinerUOcr,
			pdfCfg.GetMinerUFormula(),
			pdfCfg.GetMinerUTable(),
			proxyURL,
		)
		if pdfCfg.MinerUOcr {
			log.Infof("MinerU OCR 回退已启用 (本地 PDF 库读不到文本时使用, model=%s)", pdfCfg.GetMinerUModel())
		} else if pdfCfg.MinerUToken != "" {
			log.Infof("MinerU 精准解析 API 已启用 (远程 URL, model=%s)", pdfCfg.GetMinerUModel())
		}
	}

	return f, nil
}

// Fetch 抓取网页或解析 PDF（自动检测 file:// 路径）。
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()

	// 本地 PDF 文件：先本地 PDF 库抽文本，读不到再按需走 MinerU OCR
	if strings.HasPrefix(rawURL, "file://") {
		return f.parseLocalPDF(ctx, localPathFromFileURL(rawURL))
	}

	// 远程 URL：仅当配置开启且 URL 指向 PDF 文件、有 Token 时优先尝试 MinerU 精准 API
	if f.mineru != nil && f.mineru.HasToken() && f.mineruRemotePDF && isPDFURL(rawURL) {
		log.Infof("MinerU 精准 API 命中 PDF URL: %s", rawURL)
		md, err := f.mineru.ParseURL(ctx, rawURL)
		if err == nil {
			return &Result{
				Mode:        "inline",
				Markdown:    md,
				ParseEngine: "mineru-remote",
			}, nil
		}
		log.Infof("MinerU 精准 API 解析失败(%v)，回退到 webfetch", err)
	}

	res, err := f.engine.Fetch(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s", classifyError(err))
	}
	return &Result{
		Title:      res.Title,
		Mode:       res.Mode,
		Markdown:   res.Markdown,
		FilePath:   res.FilePath,
		TotalLines: res.TotalLines,
		TotalChars: res.TotalChars,
		AgentHint:  cleanAgentHint(res.AgentHint),
		PageCount:  res.PageCount,
		ParseEngine: "pdf-lib",
	}, nil
}

// isPDFURL 判断远程 URL 是否指向 PDF 文件。
// 仅按 URL path（忽略 query/fragment）的 .pdf 后缀判断，大小写不敏感。
func isPDFURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

// localPathFromFileURL 将 file:// URL 规范为本地文件路径（兼容 Windows 三斜杠格式）。
func localPathFromFileURL(rawURL string) string {
	localPath := strings.TrimPrefix(rawURL, "file://")
	// 处理 Windows 三斜杠格式 file:///C:/...
	if len(localPath) > 2 && localPath[0] == '/' && localPath[2] == ':' {
		localPath = localPath[1:]
	}
	return strings.ReplaceAll(localPath, "/", string(os.PathSeparator))
}

// FetchPDFWithPages 解析 PDF 的指定页（pdf_parser 专用）。
// pages 非空时按指定页（1-based）；为空且 maxPages>0 时只取前 maxPages 页，
// 发生截断时经 Result.Preamble 说明总页数与用 pages 继续的方式。
// MinerU 路径（远程精准 API / 本地 OCR 回退）无页范围 API，返回全文并加说明。
// 无页约束时与 Fetch 的 PDF 分支行为一致。
func (f *Fetcher) FetchPDFWithPages(ctx context.Context, rawURL string, pages []int, maxPages int) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()

	if strings.HasPrefix(rawURL, "file://") {
		return f.parseLocalPDFWithPages(ctx, localPathFromFileURL(rawURL), pages, maxPages)
	}

	// 远程 PDF：MinerU 精准 API 优先（与 Fetch 一致）
	if f.mineru != nil && f.mineru.HasToken() && f.mineruRemotePDF && isPDFURL(rawURL) {
		log.Infof("MinerU 精准 API 命中 PDF URL: %s", rawURL)
		md, err := f.mineru.ParseURL(ctx, rawURL)
		if err == nil {
			res := &Result{Mode: "inline", Markdown: md, ParseEngine: "mineru-remote"}
			if len(pages) > 0 || maxPages > 0 {
				res.Preamble = pagesUnsupportedNote
			}
			return res, nil
		}
		log.Infof("MinerU 精准 API 解析失败(%v)，回退到 webfetch", err)
	}

	// 其余远程 URL：webfetch 管线按 Content-Type 分流到 PDF 解析，透传页选项
	opts := webfetch.FetchOptions{}
	constrained := false
	if len(pages) > 0 {
		opts.PDFPages = pages
		constrained = true
	} else if maxPages > 0 {
		n := maxPages
		opts.PDFMaxPages = &n
		constrained = true
	}

	var res *webfetch.FetchResult
	var err error
	if constrained {
		res, err = f.engine.FetchWithOpts(ctx, rawURL, opts)
	} else {
		res, err = f.engine.Fetch(ctx, rawURL)
	}
	if err != nil {
		return nil, fmt.Errorf("%s", classifyError(err))
	}

	out := &Result{
		Title:      res.Title,
		Mode:       res.Mode,
		Markdown:   res.Markdown,
		FilePath:   res.FilePath,
		TotalLines: res.TotalLines,
		TotalChars: res.TotalChars,
		AgentHint:  cleanAgentHint(res.AgentHint),
		PageCount:  res.PageCount,
		ParseEngine: "pdf-lib",
	}
	out.ParsedPages = parsedPDFPageCount(pages, maxPages, out.PageCount)
	out.Preamble = pdfPagesPreamble(out, pages, maxPages)
	return out, nil
}

// pagesUnsupportedNote MinerU 解析路径的页约束说明。
const pagesUnsupportedNote = "> 注：该 PDF 经 MinerU 解析，MinerU 暂不支持按页截断/选择，以下为全文。"

// pdfPagesPreamble 截断说明：仅「未指定 pages 且按 max_pages 截断」时返回非空。
func pdfPagesPreamble(result *Result, pages []int, maxPages int) string {
	if len(pages) > 0 || maxPages <= 0 || result.PageCount <= 0 || result.PageCount <= maxPages {
		return ""
	}
	end := maxPages * 2
	if end > result.PageCount {
		end = result.PageCount
	}
	return fmt.Sprintf("> 注：PDF 全文共 %d 页，已按 pdf_parser.max_pages=%d 只解析前 %d 页；后续页请用 pages 参数指定页码（如 \"%d-%d\"）。",
		result.PageCount, maxPages, maxPages, maxPages+1, end)
}

// parseLocalPDF 本地 PDF：优先 ledongthuc/pdf 文本提取；无文本且开启 mineru_ocr 时回退 MinerU。
func (f *Fetcher) parseLocalPDF(ctx context.Context, localPath string) (*Result, error) {
	return f.parseLocalPDFWithPages(ctx, localPath, nil, 0)
}

// parseLocalPDFWithPages parseLocalPDF 的按页版本：pages 非空时按指定页提取，
// 否则 maxPages>0 时截前 N 页。扫描件回退 MinerU OCR 时无页能力，返回全文加说明。
func (f *Fetcher) parseLocalPDFWithPages(ctx context.Context, localPath string, pages []int, maxPages int) (*Result, error) {
	var engineOpts []webfetch.PDFOption
	if len(pages) > 0 {
		engineOpts = append(engineOpts, webfetch.WithPDFPages(pages))
	} else if maxPages > 0 {
		engineOpts = append(engineOpts, webfetch.WithPDFMaxPages(maxPages))
	}

	result, err := f.parsePDFFileOpts(ctx, localPath, engineOpts)
	if err == nil {
		result.Preamble = pdfPagesPreamble(result, pages, maxPages)
		return result, nil
	}

	if !needsOCRFallback(err) {
		return nil, err
	}

	if f.mineru == nil || !f.mineruOCR {
		return nil, fmt.Errorf("%w（可能是扫描件/图片型 PDF，本地库无法提取文本；可在配置中开启 pdf_parser.mineru_ocr 以使用 MinerU OCR）", err)
	}

	log.Infof("本地 PDF 库未提取到文本，尝试 MinerU OCR: %s", localPath)
	md, mineruErr := f.mineru.ParseFile(ctx, localPath)
	if mineruErr == nil {
		res := &Result{
			Title:       filepath.Base(localPath),
			Mode:        "inline",
			Markdown:    md,
			ParseEngine: "mineru-ocr",
		}
		if len(pages) > 0 || maxPages > 0 {
			res.Preamble = pagesUnsupportedNote
		}
		return res, nil
	}
	if errors.Is(mineruErr, mineru.ErrFileTooLarge) {
		return nil, fmt.Errorf("%w；MinerU OCR 回退失败: 文件超过 Agent API 限制(10MB)", err)
	}
	return nil, fmt.Errorf("%w；MinerU OCR 回退失败: %v", err, mineruErr)
}

// needsOCRFallback 判断本地解析失败是否因无文本层（扫描件等），适合 OCR 回退。
func needsOCRFallback(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no text extracted") ||
		strings.Contains(msg, "scanned/image-based PDF")
}

// parsePDFFile 解析本地 PDF 文件（引擎默认选项）。
func (f *Fetcher) parsePDFFile(ctx context.Context, filePath string) (*Result, error) {
	return f.parsePDFFileOpts(ctx, filePath, nil)
}

// parsePDFFileOpts 解析本地 PDF 文件，opts 传递页选择/页上限给底层引擎。
func (f *Fetcher) parsePDFFileOpts(ctx context.Context, filePath string, opts []webfetch.PDFOption) (*Result, error) {
	res, err := f.engine.ParsePDFFile(ctx, filePath, opts...)
	if err != nil {
		return nil, fmt.Errorf("PDF 解析失败: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("PDF 解析失败: %s", res.Error)
	}
	mode := res.Mode
	if mode == "" && res.Markdown != "" {
		mode = "inline"
	}
	return &Result{
		Title:      res.Title,
		Mode:       mode,
		Markdown:   res.Markdown,
		FilePath:   res.FilePath,
		TotalLines: res.TotalLines,
		TotalChars: res.TotalChars,
		AgentHint:  cleanAgentHint(res.AgentHint),
		PageCount:  res.PageCount,
		ParseEngine: "pdf-lib",
	}, nil
}

// parsedPDFPageCount reports how many pages the PDF pipeline actually read.
// Explicit page selections are authoritative; maxPages truncation is capped by
// the document's own page count. MinerU-only results leave this at zero because
// that path does not report a reliable parsed-page count.
func parsedPDFPageCount(pages []int, maxPages, pageCount int) int {
	if len(pages) > 0 {
		if pageCount <= 0 {
			return len(pages)
		}
		n := 0
		for _, p := range pages {
			if p >= 1 && p <= pageCount {
				n++
			}
		}
		return n
	}
	if maxPages <= 0 || pageCount <= 0 {
		return pageCount
	}
	if pageCount < maxPages {
		return pageCount
	}
	return maxPages
}

// cleanAgentHint 去掉 AgentHint 中的预览部分（空白行和分隔线污染）。
func cleanAgentHint(hint string) string {
	if idx := strings.Index(hint, "预览（"); idx != -1 {
		return strings.TrimRight(hint[:idx], "\n")
	}
	return hint
}

// Close 停止过期文件清理协程并关闭引擎。
func (f *Fetcher) Close() error {
	if f.cleanupStop != nil {
		close(f.cleanupStop)
		f.cleanupStop = nil
	}
	return f.engine.Close()
}

// beginActivity / endActivity 记录抓取活动，供清理协程判定空闲。
func (f *Fetcher) beginActivity() {
	f.inflight.Add(1)
	f.lastActivity.Store(time.Now().UnixNano())
}

func (f *Fetcher) endActivity() {
	f.inflight.Add(-1)
	f.lastActivity.Store(time.Now().UnixNano())
}

// idleForCleanup 判定当前是否处于可清理的空闲态：无进行中抓取，
// 且距上次抓取活动超过 fileCleanupIdleThreshold（lastActivity 零值视为长期空闲）。
func (f *Fetcher) idleForCleanup() bool {
	return f.inflight.Load() == 0 &&
		time.Since(time.Unix(0, f.lastActivity.Load())) >= fileCleanupIdleThreshold
}

// startFileJanitor 启动空闲期过期文件清理协程。上游引擎从不自行清理，
// TTL 若无人驱动就形同虚设：本协程按固定节奏检查，仅在空闲态
// （idleForCleanup）时执行清理，避免与正常抓取抢磁盘 I/O。
func (f *Fetcher) startFileJanitor(ttl time.Duration) {
	f.cleanupStop = make(chan struct{})
	stop := f.cleanupStop
	go func() {
		ticker := time.NewTicker(fileCleanupCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !f.idleForCleanup() {
					continue
				}
				n, err := f.CleanExpiredFiles()
				if err != nil {
					log.Warnf("webfetch 过期文件清理失败: %v", err)
				} else if n > 0 {
					log.Infof("webfetch 空闲期清理完成: 删除 %d 个过期文件 (ttl=%s)", n, ttl)
				}
			}
		}
	}()
}

// CleanExpiredFiles 立即清理输出目录中超过 TTL 的落盘文件，返回清理数量。
// 只删除文件名匹配本程序保存模式（savedFileRe）的 .md 文件；
// 目录、子目录、其它名字或扩展名的文件一律不动，防止误伤。
func (f *Fetcher) CleanExpiredFiles() (int, error) {
	entries, err := os.ReadDir(f.outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	deadline := time.Now().Add(-f.fileTTL)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !savedFileRe.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(deadline) {
			continue
		}
		if rmErr := os.Remove(filepath.Join(f.outputDir, entry.Name())); rmErr == nil {
			count++
		} else {
			log.Warnf("webfetch 过期文件删除失败: %s: %v", entry.Name(), rmErr)
		}
	}
	return count, nil
}

// classifyError 将 go-webfetch 的错误分类为用户友好的错误信息。
func classifyError(err error) string {
	var notFound *webfetch.NotFoundError
	var waf *webfetch.WAFError
	var empty *webfetch.EmptyContentError
	var ssrf *webfetch.SSRFError
	var timeout *webfetch.TimeoutError

	switch {
	case errors.As(err, &notFound):
		return fmt.Sprintf("页面不存在(%d)", notFound.StatusCode)
	case errors.As(err, &waf):
		return "被网站反爬机制拦截(WAF)"
	case errors.As(err, &empty):
		return "页面内容为空(可能被反爬)"
	case errors.As(err, &ssrf):
		return "不允许访问内网地址"
	case errors.As(err, &timeout):
		return "请求超时"
	default:
		return fmt.Sprintf("抓取失败: %v", err)
	}
}
