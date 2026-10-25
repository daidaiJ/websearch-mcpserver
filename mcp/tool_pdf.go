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
	"websearch/pkg/i18n"
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
		return nil, nil, errors.New(i18n.T("tool.pdf.wait_seconds_invalid"))
	}
	if params.WaitSeconds != nil {
		ctx = mineru.WithTaskWait(ctx, time.Duration(*params.WaitSeconds)*time.Second)
	}
	ctx = mineru.WithTaskProgress(ctx, func(status string) {
		notifyProgress(ctx, req, 0, 1, "MinerU: "+status)
	})
	if params.TaskRef != "" {
		if params.Path != "" || params.Pages != "" {
			return nil, nil, errors.New(i18n.T("tool.pdf.taskref_page_hint"))
		}
		if webfetchInst == nil {
			return nil, nil, errors.New(i18n.T("tool.pdf.webfetch_not_initialized"))
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
		return textResult(i18n.T("tool.pdf.parser_label", "Engine", parseEngine) + "\n\n" + formatWebFetchResult(result)), nil, nil
	}
	if params.Path == "" {
		return nil, nil, errors.New(i18n.T("tool.pdf.path_required"))
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
		return nil, nil, errors.New(i18n.T("tool.pdf.pages_over_limit", "Count", len(pages), "Max", maxPages))
	}

	if webfetchInst == nil {
		return nil, nil, errors.New(i18n.T("tool.pdf.webfetch_not_initialized"))
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
			return nil, nil, fmt.Errorf("%s: %w", i18n.T("tool.pdf.cancelled"), errors.Join(ctx.Err(), mineruErr))
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
					err = fmt.Errorf("%s: %w; %s: %w", i18n.T("tool.pdf.mineru_failed"), mineruErr, i18n.T("tool.pdf.text_fallback_failed"), err)
				} else {
					result.Preamble = i18n.T("tool.pdf.mineru_imprecise_preamble", "Err", mineruErr) + "\n\n" + result.Preamble
				}
			}
		}
	} else {
		result, err = webfetchInst.FetchPDFWithPages(ctx, pdfPath, pages, maxPages)
	}
	if err != nil {
		if !remote {
			return nil, nil, fmt.Errorf("%s: %w%s", i18n.T("tool.pdf.parse_failed"), err, i18n.T("tool.pdf.local_path_hint"))
		}
		return nil, nil, fmt.Errorf("%s: %w", i18n.T("tool.pdf.parse_failed"), err)
	}
	parseEngine = result.ParseEngine
	parsedPages = result.ParsedPages
	text := formatWebFetchResult(result)
	if parseEngine != "" {
		text = i18n.T("tool.pdf.parser_label", "Engine", parseEngine) + "\n\n" + text
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
		result = textResult(i18n.T("tool.pdf.task_status_no_body") + "\n\n" + message)
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
	notifyProgress(ctx, req, 0, 1, i18n.T("tool.pdf.mineru_downloading_for_crop"))
	source, removeSource, err := downloadPDFForCrop(ctx, pdfPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("tool.pdf.crop_prepare_failed"), err)
	}
	defer removeSource()

	total, err := mineru.PDFPageCount(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("tool.pdf.crop_failed"), err)
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
		return nil, errors.New(i18n.T("tool.pdf.crop_no_pages", "Budget", pdfMineruPageBudget))
	}

	var merged strings.Builder
	parsed := 0
	for i, batch := range batches {
		notifyProgress(ctx, req, float64(parsed), float64(len(selected)),
			i18n.T("tool.pdf.batch_start", "Index", i+1, "Total", len(batches), "From", batch[0], "To", batch[len(batch)-1]))
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
						ParsedPages: parsed, ParseEngine: "mineru-remote", Preamble: i18n.T(
							"tool.pdf.batch_pending",
							"DoneBatches", i, "DonePages", parsed, "Index", i+1, "Total", len(batches),
							"From", batch[0], "To", batch[len(batch)-1], "Message", pending.Error())}
					if saveErr := webfetchInst.StoreLargePDFResult(partial, pdfPath); saveErr != nil {
						return nil, fmt.Errorf("%s: %w; %w", i18n.T("tool.pdf.save_batches_failed"), saveErr, err)
					}
					return partial, err
				}
				return nil, fmt.Errorf("%s: %w", i18n.T("tool.pdf.batch_failed",
					"Index", i+1, "Total", len(batches), "From", batch[0], "To", batch[len(batch)-1], "DonePages", parsed), err)
			}
			return nil, err
		}
		parsed += len(batch)
		notifyProgress(ctx, req, float64(parsed), float64(len(selected)),
			i18n.T("tool.pdf.batch_done", "Index", i+1, "Total", len(batches), "From", batch[0], "To", batch[len(batch)-1], "Parsed", parsed, "Selected", len(selected)))
		if len(batches) == 1 {
			merged.WriteString(part.Markdown)
			continue
		}
		fmt.Fprintf(&merged, "\n\n%s\n\n%s",
			i18n.T("tool.pdf.merged_section", "From", batch[0], "To", batch[len(batch)-1], "Index", i+1, "Total", len(batches)), part.Markdown)
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
		result.Preamble = i18n.T("tool.pdf.preamble_cropped", "Total", total, "Parsed", parsed)
	} else {
		result.Preamble = i18n.T("tool.pdf.preamble_batched",
			"Total", total, "Batches", len(batches), "BatchSize", pdfMineruPageBatch,
			"From", batches[0][0], "To", last[len(last)-1], "Pages", parsed)
	}
	if truncated {
		next := last[len(last)-1] + 1
		result.Preamble += i18n.T("tool.pdf.page_budget_used",
			"Budget", pdfMineruPageBudget, "From", next, "To", min(next+pdfMineruPageBatch-1, total))
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
				return nil, errors.New(i18n.T("tool.pdf.pages_spec_invalid", "Spec", spec))
			}
			if to-from+1 > widthCap {
				return nil, errors.New(i18n.T("tool.pdf.pages_range_over_limit", "From", from, "To", to, "Cap", widthCap))
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
			return nil, errors.New(i18n.T("tool.pdf.pages_spec_invalid", "Spec", spec))
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out, nil
}
