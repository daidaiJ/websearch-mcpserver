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
	fetchcontent "websearch/pkg/fetch"
	"websearch/pkg/fetch/mineru"
	"websearch/pkg/i18n"
	"websearch/pkg/log"

	webfetch "github.com/daidaiJ/go-webfetch"
)

// Result 封装 go-webfetch 的返回结果。
type Result struct {
	Title       string
	Mode        string // "inline" 或 "saved_to_file"
	Markdown    string
	FilePath    string
	TotalLines  int
	TotalChars  int
	AgentHint   string
	PageCount   int    // PDF 内容：文档总页数（非 PDF 为 0）
	Preamble    string // 页截断/选择说明，工具输出时置于正文前
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
	PageLimit() int
	ParseURL(ctx context.Context, fileURL string) (string, error)
	ParseURLWithPages(ctx context.Context, fileURL string, pages []int, maxPages int) (string, error)
	ParseStandardFile(ctx context.Context, filePath string) (string, error)
	ResumeTask(ctx context.Context, taskRef string) (string, error)
	ParseFileWithPages(ctx context.Context, filePath string, pages []int, maxPages int) (string, error)
}

// Fetcher 封装 go-webfetch Engine。
type Fetcher struct {
	engine          fetchEngine
	mineru          mineruParser
	mineruOCR       bool // 本地 PDF 库读不到文本时是否回退 MinerU OCR
	mineruRemotePDF bool // 远程 PDF URL 是否走 MinerU 精准 API（配置 pdf_parser.mineru_remote_pdf）

	outputDir      string        // 大文本落盘目录（清理范围仅限此目录）
	fileTTL        time.Duration // 落盘文件保留时长
	maxInlineLines int
	maxInlineChars int

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

	log.InfoT("log.fetch.webfetch.webfetch_engine_enabled_output_dir_ttl", "outputDir", outputDir, "fileTTL", fileTTL, "maxInlineLines", maxInlineLines, "timeout", timeout)

	f := &Fetcher{
		engine:          engine,
		mineruOCR:       pdfCfg.MinerUOCREnabled(),
		mineruRemotePDF: pdfCfg.MinerURemotePDF,
		outputDir:       outputDir,
		fileTTL:         fileTTL,
		maxInlineLines:  maxInlineLines,
		maxInlineChars:  cfg.MaxInlineChars,
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
			pdfCfg.GetMinerUPageLimit(),
		)
		if pdfCfg.MinerUOcr {
			log.InfoT("log.fetch.webfetch.mineru_ocr_fallback_enabled_used_when", "minerUModel", pdfCfg.GetMinerUModel())
		} else if pdfCfg.MinerUToken != "" {
			log.InfoT("log.fetch.webfetch.mineru_precise_parsing_api_enabled_remote", "minerUModel", pdfCfg.GetMinerUModel(), "minerUPageLimit", pdfCfg.GetMinerUPageLimit())
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
		log.InfoT("log.fetch.webfetch.mineru_precise_api_matched_a_pdf", "rawURL", rawURL)
		md, err := f.mineru.ParseURL(ctx, rawURL)
		if err == nil {
			return &Result{
				Mode:        "inline",
				Markdown:    md,
				ParseEngine: "mineru-remote",
			}, nil
		}
		log.InfoT("log.fetch.webfetch.mineru_precise_api_parsing_failed_falling", "err", err)
	}

	res, err := f.engine.Fetch(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", classifyError(err), err)
	}
	if res.Mode == "inline" && !fetchcontent.HasPageBody(res.Markdown, res.Title) {
		return nil, fmt.Errorf("%s: %w", i18n.T("fetch.webfetch.empty_body_js"), &webfetch.EmptyContentError{URL: rawURL, Title: res.Title, CharCount: res.TotalChars})
	}
	return &Result{
		Title:       res.Title,
		Mode:        res.Mode,
		Markdown:    res.Markdown,
		FilePath:    res.FilePath,
		TotalLines:  res.TotalLines,
		TotalChars:  res.TotalChars,
		AgentHint:   cleanAgentHint(res.AgentHint),
		PageCount:   res.PageCount,
		ParseEngine: "pdf-lib",
	}, nil
}

// isPDFURL 判断远程 URL 是否指向 PDF 文件。
// Recognize .pdf paths and arXiv's canonical extensionless PDF endpoint.
var arxivPDFPath = regexp.MustCompile(`^/pdf/(?:[0-9]{4}\.[0-9]{4,5}|[a-zA-Z][a-zA-Z0-9.-]*/[0-9]{7})(?:v[1-9][0-9]*)?$`)

func isPDFURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf") ||
		(strings.EqualFold(u.Hostname(), "arxiv.org") && arxivPDFPath.MatchString(u.Path))
}

// CanUseMinerURemotePDF reports whether this URL takes the precise MinerU route.
func (f *Fetcher) CanUseMinerURemotePDF(rawURL string) bool {
	return f.mineru != nil && f.mineru.HasToken() && f.mineruRemotePDF && isPDFURL(rawURL)
}

// ParseMinerUPDFWithPages attempts only the precise API, so callers can choose a local fallback.
func (f *Fetcher) ParseMinerUPDFWithPages(ctx context.Context, rawURL string, pages []int, maxPages int) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()
	if !f.CanUseMinerURemotePDF(rawURL) {
		return nil, fmt.Errorf("MinerU precise PDF route is not enabled for this URL")
	}
	log.InfoT("log.fetch.webfetch.mineru_precise_api_matched_a_pdf", "rawURL", rawURL)
	md, err := f.mineru.ParseURLWithPages(ctx, rawURL, pages, maxPages)
	if err != nil {
		return nil, err
	}
	return &Result{Mode: "inline", Markdown: md, ParseEngine: "mineru-remote",
		Preamble: mineruPageNote(pages, maxPages, false, f.mineru.PageLimit())}, nil
}

// ParseCroppedPDF uploads a locally selected PDF through MinerU's precise API.
func (f *Fetcher) ParseCroppedPDF(ctx context.Context, path string) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()
	if f.mineru == nil || !f.mineru.HasToken() {
		return nil, errors.New(i18n.T("fetch.webfetch.mineru_token_required"))
	}
	md, err := f.mineru.ParseStandardFile(ctx, path)
	if err != nil {
		return nil, err
	}
	return &Result{Mode: "inline", Markdown: md, ParseEngine: "mineru-remote"}, nil
}

// ResumeMinerUTask retrieves the original task's selected pages without resubmission.
func (f *Fetcher) ResumeMinerUTask(ctx context.Context, taskRef string) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()
	if f.mineru == nil {
		return nil, errors.New(i18n.T("fetch.webfetch.mineru_disabled"))
	}
	md, err := f.mineru.ResumeTask(ctx, taskRef)
	if err != nil {
		return nil, err
	}
	engine := "mineru-remote"
	if strings.HasPrefix(taskRef, "agent:") {
		engine = "mineru-ocr"
	}
	result := &Result{Mode: "inline", Markdown: md, ParseEngine: engine,
		Preamble: i18n.T("fetch.webfetch.mineru_task_resumed_preamble")}
	if err := f.StoreLargePDFResult(result, taskRef); err != nil {
		return nil, fmt.Errorf("%s: %w (task_ref=%q)", i18n.T("fetch.webfetch.mineru_result_save_failed"), err, taskRef)
	}
	return result, nil
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
// MinerU 路径分别使用精准 API 的 page_ranges 和轻量 API 的 page_range。
// 无页约束时与 Fetch 的 PDF 分支行为一致。
func (f *Fetcher) FetchPDFWithPages(ctx context.Context, rawURL string, pages []int, maxPages int) (*Result, error) {
	f.beginActivity()
	defer f.endActivity()

	if strings.HasPrefix(rawURL, "file://") {
		return f.parseLocalPDFWithPages(ctx, localPathFromFileURL(rawURL), pages, maxPages)
	}

	// 远程 PDF：MinerU 精准 API 优先（与 Fetch 一致）
	var mineruErr error
	if f.CanUseMinerURemotePDF(rawURL) {
		result, err := f.ParseMinerUPDFWithPages(ctx, rawURL, pages, maxPages)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w; %s: %w", i18n.T("fetch.webfetch.mineru_parse_failed"), err, i18n.T("fetch.webfetch.context_ended"), ctx.Err())
		}
		if errors.Is(err, mineru.ErrPageLimit) || errors.Is(err, mineru.ErrRemoteURLRejected) {
			return nil, err
		}
		mineruErr = err
		log.InfoT("log.fetch.webfetch.mineru_precise_api_parsing_failed_falling", "err", err)
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
		if mineruErr != nil {
			return nil, fmt.Errorf("%s: %w; %s: %s: %w", i18n.T("fetch.webfetch.mineru_parse_failed"), mineruErr, i18n.T("fetch.webfetch.text_fallback_failed"), classifyError(err), err)
		}
		return nil, fmt.Errorf("%s: %w", classifyError(err), err)
	}

	out := &Result{
		Title:       res.Title,
		Mode:        res.Mode,
		Markdown:    res.Markdown,
		FilePath:    res.FilePath,
		TotalLines:  res.TotalLines,
		TotalChars:  res.TotalChars,
		AgentHint:   cleanAgentHint(res.AgentHint),
		PageCount:   res.PageCount,
		ParseEngine: "pdf-lib",
	}
	out.ParsedPages = parsedPDFPageCount(pages, maxPages, out.PageCount)
	out.Preamble = pdfPagesPreamble(out, pages, maxPages)
	if mineruErr != nil {
		out.Preamble = i18n.T("fetch.webfetch.mineru_imprecise_preamble", "Err", mineruErr) + "\n\n" + out.Preamble
	}
	return out, nil
}

// mineruPageNote 说明已提交的选页请求；MinerU 不保证返回原文件总页数。
// lightweight 区分轻量 API（20 页硬上限、无图片包）与精准 API 文案。
func mineruPageNote(pages []int, maxPages int, lightweight bool, limit int) string {
	if lightweight {
		if len(pages) > 0 {
			return i18n.T("fetch.webfetch.mineru_light_pages", "Count", len(pages))
		}
		if maxPages > 0 {
			if maxPages > limit {
				maxPages = limit
			}
			return i18n.T("fetch.webfetch.mineru_light_first_pages", "Count", maxPages)
		}
		return ""
	}
	if len(pages) > 0 {
		return i18n.T("fetch.webfetch.mineru_pages", "Count", len(pages), "Limit", limit)
	}
	if maxPages > 0 {
		if maxPages > limit {
			maxPages = limit
		}
		return i18n.T("fetch.webfetch.mineru_first_pages", "Count", maxPages, "Limit", limit)
	}
	return ""
}

// pdfPagesPreamble 截断说明：仅「未指定 pages 且按 max_pages 截断」时返回非空。
func pdfPagesPreamble(result *Result, pages []int, maxPages int) string {
	if len(pages) > 0 || maxPages <= 0 || result.PageCount <= 0 || result.PageCount <= maxPages {
		return ""
	}
	end := maxPages * 2
	if end > result.PageCount {
		end = result.PageCount
	}
	return i18n.T("fetch.webfetch.pdf_pages_truncated",
		"Total", result.PageCount, "Max", maxPages, "From", maxPages+1, "To", end)
}

// parseLocalPDF 本地 PDF：优先 ledongthuc/pdf 文本提取；无文本且开启 mineru_ocr 时回退 MinerU。
func (f *Fetcher) parseLocalPDF(ctx context.Context, localPath string) (*Result, error) {
	return f.parseLocalPDFWithPages(ctx, localPath, nil, 0)
}

// parseLocalPDFWithPages parseLocalPDF 的按页版本：pages 非空时按指定页提取，
// 否则 maxPages>0 时截前 N 页。扫描件回退 MinerU OCR 时传递选页范围。
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
		return nil, fmt.Errorf("%w%s", err, i18n.T("fetch.webfetch.scanned_pdf_hint"))
	}

	log.InfoT("log.fetch.webfetch.local_pdf_library_extracted_no_text", "localPath", localPath)
	md, mineruErr := f.mineru.ParseFileWithPages(ctx, localPath, pages, maxPages)
	if mineruErr == nil {
		res := &Result{
			Title:       filepath.Base(localPath),
			Mode:        "inline",
			Markdown:    md,
			ParseEngine: "mineru-ocr",
		}
		res.Preamble = mineruPageNote(pages, maxPages, true, 20)
		return res, nil
	}
	if errors.Is(mineruErr, mineru.ErrFileTooLarge) {
		return nil, fmt.Errorf("%w%s", err, i18n.T("fetch.webfetch.ocr_fallback_too_large"))
	}
	return nil, fmt.Errorf("%w%s: %w", err, i18n.T("fetch.webfetch.ocr_fallback_failed"), mineruErr)
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
		return nil, fmt.Errorf("%s: %w", i18n.T("fetch.webfetch.pdf_parse_failed"), err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("%s: %s", i18n.T("fetch.webfetch.pdf_parse_failed"), res.Error)
	}
	mode := res.Mode
	if mode == "" && res.Markdown != "" {
		mode = "inline"
	}
	return &Result{
		Title:       res.Title,
		Mode:        mode,
		Markdown:    res.Markdown,
		FilePath:    res.FilePath,
		TotalLines:  res.TotalLines,
		TotalChars:  res.TotalChars,
		AgentHint:   cleanAgentHint(res.AgentHint),
		PageCount:   res.PageCount,
		ParseEngine: "pdf-lib",
		ParsedPages: len(res.Pages),
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
					log.WarnT("log.fetch.webfetch.webfetch_expired_file_cleanup_failed", "err", err)
				} else if n > 0 {
					log.InfoT("log.fetch.webfetch.webfetch_idle_cleanup_finished_deleted_expired", "n", n, "ttl", ttl)
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
			log.WarnT("log.fetch.webfetch.webfetch_expired_file_deletion_failed", "name", entry.Name(), "rmErr", rmErr)
		}
	}
	return count, nil
}

// classifyError 将 go-webfetch 的错误分类为面向用户的错误信息（按当前语言渲染）。
func classifyError(err error) string {
	var notFound *webfetch.NotFoundError
	var waf *webfetch.WAFError
	var empty *webfetch.EmptyContentError
	var ssrf *webfetch.SSRFError
	var timeout *webfetch.TimeoutError

	switch {
	case errors.As(err, &notFound):
		return i18n.T("fetch.webfetch.err_page_not_found", "Status", notFound.StatusCode)
	case errors.As(err, &waf):
		return i18n.T("fetch.webfetch.err_waf")
	case errors.As(err, &empty):
		return i18n.T("fetch.webfetch.err_empty_maybe_antibot")
	case errors.As(err, &ssrf):
		return i18n.T("fetch.webfetch.err_private_address")
	case errors.As(err, &timeout):
		return i18n.T("fetch.webfetch.err_timeout")
	default:
		return i18n.T("fetch.webfetch.err_fetch_failed", "Err", err)
	}
}
