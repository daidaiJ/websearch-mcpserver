package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"websearch/pkg/fetch/mineru"
	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/telemetry"
)

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchParamsWithIntent LLM 摘要启用时使用的参数（含 intent）。

// pdf_parser 工具：本地/远程 PDF 解析与页码范围。
// PDFParserParams pdf_parser 工具参数。
type PDFParserParams struct {
	projectParams
	Path        string `json:"path,omitempty" jsonschema:"mcp.param.pdf.path"`
	Pages       string `json:"pages,omitempty" jsonschema:"mcp.param.pdf.pages"`
	TaskRef     string `json:"task_ref,omitempty" jsonschema:"mcp.param.pdf.task_ref"`
	WaitSeconds *int   `json:"wait_seconds,omitempty" jsonschema:"mcp.param.pdf.wait_seconds"`
}

// CleanFetch 通过 go-webfetch 抓取网页，失败时回退到 Jina Reader。
// 支持 url + urls 批量（合并去重，最多 5 个）：并发抓取，单条失败不影响其它。

// http(s) 远程地址原样返回且 remote=true，禁止再拼 file://。
func resolvePDFPath(path string) (fetchURL string, remote bool) {
	p := strings.TrimSpace(path)
	lower := strings.ToLower(p)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return normalizePDFDownloadURL(p), true
	}
	if strings.HasPrefix(p, "file://") {
		return p, false
	}
	return "file:///" + strings.ReplaceAll(p, `\`, "/"), false
}

// PDFParserHandler PDF 解析 tool handler：本地路径 / file:// / 远程 http(s) URL。
// 支持可选 pages 页码范围；省略时受 pdf_parser.max_pages（默认 20）约束并提示截断。
func PDFParserHandler(ctx context.Context, req *mcp.CallToolRequest, params *PDFParserParams) (resultOut *mcp.CallToolResult, extra any, err error) {
	requestID := telemetry.NewRequestID()
	ctx = telemetry.WithRequestID(ctx, requestID)
	started := time.Now()
	parsedPages := 0
	parseEngine := ""
	var pending *mineru.TaskPendingError
	defer func() {
		telemetry.RecordEventContext(ctx, telemetry.Event{
			Kind:         "tool",
			Tool:         "pdf_parser",
			Provider:     "pdf_pipeline",
			Query:        params.Path,
			Success:      err == nil && (resultOut == nil || !resultOut.IsError),
			Duration:     time.Since(started),
			ResultCount:  parsedPages,
			Detail:       parseEngine,
			Error:        err,
			RequestID:    requestID,
			AttemptChain: recentAttemptChain(started, 10),
		})
	}()
	defer func() {
		if ctx.Err() == nil {
			if pending == nil {
				errors.As(err, &pending)
			}
			if pending != nil && resultOut == nil {
				parseEngine = "mineru-task"
			}
			resultOut, err = pdfTaskResult(resultOut, pending, err)
		}
	}()
	if params.WaitSeconds != nil && (*params.WaitSeconds < 0 || *params.WaitSeconds > 1800) {
		return nil, nil, fmt.Errorf("wait_seconds 必须在 0-1800 秒之间")
	}
	if params.WaitSeconds != nil {
		ctx = mineru.WithTaskWait(ctx, time.Duration(*params.WaitSeconds)*time.Second)
	}
	ctx = mineru.WithTaskProgress(ctx, func(status string) {
		notifyProgress(ctx, req, 0, 1, "MinerU: "+status)
	})
	if params.TaskRef != "" {
		if params.Path != "" || params.Pages != "" {
			return nil, nil, fmt.Errorf("task_ref 查询沿用原任务页码，请省略 path 和 pages")
		}
		if webfetchInst == nil {
			return nil, nil, fmt.Errorf("webfetch 未初始化，请先启用 cleanfetch")
		}
		result, resumeErr := webfetchInst.ResumeMinerUTask(ctx, params.TaskRef)
		if errors.As(resumeErr, &pending) {
			parseEngine = "mineru-task"
			status, statusErr := pdfTaskResult(nil, pending, resumeErr)
			return status, nil, statusErr
		}
		if resumeErr != nil {
			return nil, nil, resumeErr
		}
		parseEngine = result.ParseEngine
		return textResult(fmt.Sprintf("> PDF 解析器: %s\n\n%s", parseEngine, formatWebFetchResult(result))), nil, nil
	}
	if params.Path == "" {
		return nil, nil, fmt.Errorf("path 参数不能为空")
	}

	pages, err := parsePagesSpec(params.Pages, pageSpecWidthCap)
	if err != nil {
		return nil, nil, err
	}
	maxPages := pdfMaxPages
	if maxPages <= 0 {
		maxPages = 20
	}
	if len(pages) > maxPages {
		return nil, nil, fmt.Errorf("pages 指定了 %d 页，超过单次上限 max_pages=%d，请拆分多次调用（如 pages=\"1-%d\"）", len(pages), maxPages, maxPages)
	}

	if webfetchInst == nil {
		return nil, nil, fmt.Errorf("webfetch 未初始化，请先启用 cleanfetch")
	}

	pdfPath, remote := resolvePDFPath(params.Path)
	if remote {
		if err := validateURLSecurity(pdfPath); err != nil {
			return nil, nil, err
		}
		maxSizeMB := cleanFetchMaxSizeMB
		if maxSizeMB <= 0 {
			maxSizeMB = 10
		}
		if webfetchInst.CanUseMinerURemotePDF(pdfPath) {
			maxSizeMB = 200
		}
		if err := headCheckWithLimit(ctx, pdfPath, maxSizeMB); err != nil {
			return nil, nil, err
		}
	}

	var result *webfetch.Result
	if remote {
		var mineruErr error
		if webfetchInst.CanUseMinerURemotePDF(pdfPath) {
			result, mineruErr = webfetchInst.ParseMinerUPDFWithPages(ctx, pdfPath, pages, maxPages)
		}
		if errors.Is(mineruErr, mineru.ErrPageLimit) || errors.Is(mineruErr, mineru.ErrRemoteURLRejected) {
			result, mineruErr = parseRemotePDFWithCrop(ctx, req, pdfPath, pages, maxPages)
		}
		errors.As(mineruErr, &pending)
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("PDF 解析已取消: %w", errors.Join(ctx.Err(), mineruErr))
		}
		if result == nil {
			maxSizeMB := cleanFetchMaxSizeMB
			if maxSizeMB <= 0 {
				maxSizeMB = 10
			}
			source, cleanup, downloadErr := downloadPDFWithLimit(ctx, pdfPath, int64(maxSizeMB)*1024*1024)
			err = downloadErr
			if err == nil {
				defer cleanup()
				localURL, _ := resolvePDFPath(source)
				result, err = webfetchInst.FetchPDFWithPages(ctx, localURL, pages, maxPages)
				if err == nil {
					err = webfetchInst.StoreLargePDFResult(result, pdfPath)
				}
			}
			if mineruErr != nil {
				if err != nil {
					err = fmt.Errorf("MinerU 解析失败: %w；PDF 文本回退失败: %w", mineruErr, err)
				} else {
					result.Preamble = fmt.Sprintf("> MinerU 未取得精准结果（%v），本次返回 PDF 文本提取；图片未随文本返回。\n\n%s", mineruErr, result.Preamble)
				}
			}
		}
	} else {
		result, err = webfetchInst.FetchPDFWithPages(ctx, pdfPath, pages, maxPages)
	}
	if err != nil {
		if !remote {
			return nil, nil, fmt.Errorf("PDF 解析失败: %w；本地路径须对服务进程可见，Docker 中须使用容器内或已挂载路径，不能直接使用 file_search 的宿主机路径", err)
		}
		return nil, nil, fmt.Errorf("PDF 解析失败: %w", err)
	}
	parseEngine = result.ParseEngine
	parsedPages = result.ParsedPages
	text := formatWebFetchResult(result)
	if parseEngine != "" {
		text = fmt.Sprintf("> PDF 解析器: %s\n\n%s", parseEngine, text)
	}
	return textResult(text), nil, nil
}

// pdfTaskResult keeps unfinished task state separate from document content.
func pdfTaskResult(result *mcp.CallToolResult, pending *mineru.TaskPendingError, err error) (*mcp.CallToolResult, error) {
	if pending == nil {
		errors.As(err, &pending)
	}
	if pending == nil {
		return result, err
	}
	if result == nil {
		message := pending.Error()
		if err != nil {
			message = err.Error()
		}
		result = textResult("> 任务状态（尚无正文）\n\n" + message)
		result.IsError = true
		err = nil
	}
	result.Meta = mcp.Meta{"websearch/mineru_task": map[string]any{
		"task_ref": pending.TaskRef, "status": pending.Status, "completed": false,
	}}
	return result, err
}

// parseRemotePDFWithCrop 远程 PDF 的本地裁切回退：MinerU 拒绝原文件（页数超限
// 或源 URL 不可读）时，下载原件、用 qpdf 裁出所选页再走签名上传。
// mineru_page_batch_size > 0 时按批切分串行提交；累计页数受 mineru_page_budget
// 预算约束，预算耗尽返回已解析部分并在 Preamble 说明续读页码（元数据先行、
// 按需续读，避免一次调用烧掉当天全部 MinerU 额度）。
// 每批开始/结束经 MCP progress notification 推送进度，agent 无需整批盲等。
func parseRemotePDFWithCrop(ctx context.Context, req *mcp.CallToolRequest, pdfPath string, pages []int, maxPages int) (*webfetch.Result, error) {
	notifyProgress(ctx, req, 0, 1, "MinerU 无法直接解析原文件，正在下载原件准备本地裁切…")
	source, removeSource, err := downloadPDFForCrop(ctx, pdfPath)
	if err != nil {
		return nil, fmt.Errorf("MinerU 远程 PDF 本地裁切准备失败: %w", err)
	}
	defer removeSource()

	total, err := mineru.PDFPageCount(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("MinerU 远程 PDF 本地裁切失败: %w", err)
	}
	selected := pages
	if len(selected) == 0 {
		end := maxPages
		if end <= 0 || end > total {
			end = total
		}
		for p := 1; p <= end; p++ {
			selected = append(selected, p)
		}
	}
	batches, truncated := mineru.PlanPageBatches(selected, pdfMineruPageBatch, pdfMineruPageBudget)
	if len(batches) == 0 {
		return nil, fmt.Errorf("MinerU 远程 PDF 本地裁切失败: 页预算 %d 内没有可解析的页码", pdfMineruPageBudget)
	}

	var merged strings.Builder
	parsed := 0
	for i, batch := range batches {
		notifyProgress(ctx, req, float64(parsed), float64(len(selected)),
			fmt.Sprintf("开始第 %d/%d 批：裁切原页码 %d-%d 并上传 MinerU…", i+1, len(batches), batch[0], batch[len(batch)-1]))
		cropped, _, _, removeCropped, cropErr := mineru.CropPDF(ctx, source, batch, 0, pdfMineruPageLimit)
		if cropErr != nil {
			return nil, cropErr
		}
		batchCtx := mineru.WithTaskProgress(ctx, func(status string) {
			notifyProgress(ctx, req, float64(parsed), float64(len(selected)), "MinerU: "+status)
		})
		part, err := webfetchInst.ParseCroppedPDF(batchCtx, cropped)
		removeCropped()
		if err != nil {
			if parsed > 0 {
				var pending *mineru.TaskPendingError
				if errors.As(err, &pending) {
					partial := &webfetch.Result{Mode: "inline", Markdown: merged.String(), PageCount: total,
						ParsedPages: parsed, ParseEngine: "mineru-remote", Preamble: fmt.Sprintf(
							"> 已取得前 %d 批（%d 页）的精准正文；第 %d/%d 批原页码 %d-%d 尚未完成。续查引用只返回该批，批内页码从 1 编号。\n> %s",
							i, parsed, i+1, len(batches), batch[0], batch[len(batch)-1], pending.Error())}
					if saveErr := webfetchInst.StoreLargePDFResult(partial, pdfPath); saveErr != nil {
						return nil, fmt.Errorf("保存已完成批次失败: %w；%w", saveErr, err)
					}
					return partial, err
				}
				return nil, fmt.Errorf("第 %d/%d 批（原页码 %d-%d）解析失败（前 %d 页已成功，可用 pages 续读）: %w",
					i+1, len(batches), batch[0], batch[len(batch)-1], parsed, err)
			}
			return nil, err
		}
		parsed += len(batch)
		notifyProgress(ctx, req, float64(parsed), float64(len(selected)),
			fmt.Sprintf("第 %d/%d 批完成（原页码 %d-%d），累计已解析 %d/%d 页", i+1, len(batches), batch[0], batch[len(batch)-1], parsed, len(selected)))
		if len(batches) == 1 {
			merged.WriteString(part.Markdown)
			continue
		}
		fmt.Fprintf(&merged, "\n\n### 原文第 %d-%d 页（第 %d/%d 批，批内页码从 1 重新编号）\n\n%s",
			batch[0], batch[len(batch)-1], i+1, len(batches), part.Markdown)
	}

	result := &webfetch.Result{
		Mode:        "inline",
		Markdown:    merged.String(),
		PageCount:   total,
		ParsedPages: parsed,
		ParseEngine: "mineru-remote",
	}
	last := batches[len(batches)-1]
	if len(batches) == 1 {
		result.Preamble = fmt.Sprintf("> 原 PDF 共 %d 页，已在本地裁切所需的 %d 页后上传 MinerU；结果页码从 1 重新编号。", total, parsed)
	} else {
		result.Preamble = fmt.Sprintf("> 原 PDF 共 %d 页，已本地裁切并分 %d 批上传 MinerU（每批最多 %d 页，串行提交）；各批页码从 1 重新编号，原页码见各节标题。本次解析原页码 %d-%d（共 %d 页）。",
			total, len(batches), pdfMineruPageBatch, batches[0][0], last[len(last)-1], parsed)
	}
	if truncated {
		next := last[len(last)-1] + 1
		result.Preamble += fmt.Sprintf("\n> 单次页预算 %d 页已用完，其余页请再次调用并用 pages 指定（如 \"%d-%d\"）。",
			pdfMineruPageBudget, next, min(next+pdfMineruPageBatch-1, total))
	}
	return result, nil
}

// pageSpecWidthCap 页码区间展开宽度的硬顶（F2）：防止 pages="1-2147483647"
// 这类巨大递增区间在 max_pages 校验前被展开分配导致 OOM。
const pageSpecWidthCap = 1000

// parsePagesSpec 解析页码表达式："3"、"1-10"、"1,3,5-7"（1-based）。
// 返回去重升序页码列表；非法格式或区间宽度超过 widthCap 返回错误。
func parsePagesSpec(spec string, widthCap int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			from, err1 := strconv.Atoi(strings.TrimSpace(lo))
			to, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 != nil || err2 != nil || from < 1 || to < from {
				return nil, fmt.Errorf("pages 参数格式非法: %q，示例：1-10 或 1,3,5-7", spec)
			}
			if to-from+1 > widthCap {
				return nil, fmt.Errorf("pages 区间 %d-%d 展开后超过 %d 页上限，请用 pages 指定具体页码（如 \"1-%d\"）", from, to, widthCap, widthCap)
			}
			for p := from; p <= to; p++ {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
			continue
		}
		p, err := strconv.Atoi(part)
		if err != nil || p < 1 {
			return nil, fmt.Errorf("pages 参数格式非法: %q，示例：1-10 或 1,3,5-7", spec)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out, nil
}
