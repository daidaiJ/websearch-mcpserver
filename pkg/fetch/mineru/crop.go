package mineru

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"websearch/pkg/i18n"
)

// PDFPageCount 用 qpdf 读取 PDF 总页数（需安装 qpdf）。
func PDFPageCount(ctx context.Context, source string) (int, error) {
	countOutput, err := exec.CommandContext(ctx, "qpdf", "--show-npages", source).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%s: %w: %s", i18n.T("fetch.mineru.page_count_failed"), err, strings.TrimSpace(string(countOutput)))
	}
	total, err := strconv.Atoi(strings.TrimSpace(string(countOutput)))
	if err != nil || total <= 0 {
		return 0, errors.New(i18n.T("fetch.mineru.page_count_invalid", "Out", strings.TrimSpace(string(countOutput))))
	}
	return total, nil
}

// CropPDF creates a short-lived PDF containing the requested original pages.
// qpdf preserves page resources, including images, without rendering pages.
func CropPDF(ctx context.Context, source string, pages []int, maxPages, limit int) (string, int, int, func(), error) {
	total, err := PDFPageCount(ctx, source)
	if err != nil {
		return "", 0, 0, nil, err
	}
	selected := pages
	if len(selected) == 0 {
		end := maxPages
		if end <= 0 || end > total {
			end = total
		}
		if end > limit {
			end = limit
		}
		selected = make([]int, end)
		for i := range selected {
			selected[i] = i + 1
		}
	}
	if len(selected) == 0 || len(selected) > limit {
		return "", total, 0, nil, errors.New(i18n.T("fetch.mineru.crop_over_limit", "Limit", limit))
	}
	ranges := make([]string, len(selected))
	for i, page := range selected {
		if page < 1 || page > total {
			return "", total, 0, nil, errors.New(i18n.T("fetch.mineru.page_out_of_range", "Page", page, "Total", total))
		}
		ranges[i] = strconv.Itoa(page)
	}
	dir, err := os.MkdirTemp("", "mineru-pages-")
	if err != nil {
		return "", total, 0, nil, err
	}
	output := filepath.Join(dir, "selected.pdf")
	cleanup := func() {
		_ = os.Remove(output)
		_ = os.Remove(dir)
	}
	cmd := exec.CommandContext(ctx, "qpdf", "--empty", "--pages", source, strings.Join(ranges, ","), "--", output)
	if result, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", total, 0, nil, fmt.Errorf("%s: %w: %s", i18n.T("fetch.mineru.crop_failed"), err, strings.TrimSpace(string(result)))
	}
	return output, total, len(selected), cleanup, nil
}
