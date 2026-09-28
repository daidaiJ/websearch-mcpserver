package mineru

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// CropPDF creates a short-lived PDF containing the requested original pages.
// qpdf preserves page resources, including images, without rendering pages.
func CropPDF(ctx context.Context, source string, pages []int, maxPages, limit int) (string, int, int, func(), error) {
	countOutput, err := exec.CommandContext(ctx, "qpdf", "--show-npages", source).CombinedOutput()
	if err != nil {
		return "", 0, 0, nil, fmt.Errorf("读取 PDF 页数失败（文件可能不是有效 PDF；需安装 qpdf）: %w: %s", err, strings.TrimSpace(string(countOutput)))
	}
	total, err := strconv.Atoi(strings.TrimSpace(string(countOutput)))
	if err != nil || total <= 0 {
		return "", 0, 0, nil, fmt.Errorf("PDF 页数无效: %q", strings.TrimSpace(string(countOutput)))
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
		return "", total, 0, nil, fmt.Errorf("一次最多裁切 %d 页；请缩小 pages 范围", limit)
	}
	ranges := make([]string, len(selected))
	for i, page := range selected {
		if page < 1 || page > total {
			return "", total, 0, nil, fmt.Errorf("请求第 %d 页，但 PDF 只有 %d 页", page, total)
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
		return "", total, 0, nil, fmt.Errorf("裁切 PDF 失败: %w: %s", err, strings.TrimSpace(string(result)))
	}
	return output, total, len(selected), cleanup, nil
}
