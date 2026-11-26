package mcpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
	"websearch/pkg/config"
	"websearch/pkg/fetch"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
	"websearch/pkg/search"
)

// smartsearch 渲染响应超限落盘（P1-11 移交小项，2026-10-06 用户拍板口径）：
// 渲染结果超过 smartsearch.inline_max_chars 时整体写临时文件（零丢失，agent 分段读取），
// 响应内保留溯源头 / 统计 / 读取提示 / 失败清单（失败清单永不因落盘裁剪）。
// 目录沿用 cleanfetch file_output_dir 约定（未配置时 exe 同目录 fetchdata/），
// 惰性清理超 7 天的 search-*.md（仅在落盘发生时清理，不占后台协程）。
// 动机：堵 Tavily raw_content 默认开启的响应体积失控口——超限用落盘零丢失，不用截断。

// searchDumpDir smartsearch 超限落盘目录（applySearchEngine 注入 cleanfetch.file_output_dir 原始配置值）。
var searchDumpDir string

// dumpSearchResultsIfOversize keeps the highest-ranked bodies inline when the
// rendered response exceeds smartsearch.inline_max_chars. The complete render is
// still written to a temp file and registered as a result_ref, so nothing is
// lost; tail entries carry a marker that points at the saved copy. When the
// fixed overhead alone exceeds the limit, it falls back to the previous
// whole-response preview.
func dumpSearchResultsIfOversize(query string, results []search.SearchResult, rendered string) (string, bool) {
	var format func(string, []search.SearchResult) (string, error)
	if searchapi != nil {
		format = formatRawResults
	}
	return dumpSearchResultsIfOversizeWithFormatter(query, results, rendered, format)
}

func dumpSearchResultsIfOversizeWithFormatter(query string, results []search.SearchResult, rendered string, format func(string, []search.SearchResult) (string, error)) (string, bool) {
	limit, enabled := smartSearchConf.InlineMaxCharsOrDefault()
	if !enabled || rendered == "" {
		return "", false
	}
	charCount := utf8.RuneCountInString(rendered)
	if charCount <= limit {
		return "", false
	}
	path, err := writeSearchDump(rendered)
	if err != nil {
		log.ErrorT("log.mcp.smartsearch_overflow_persist_failed_falling_back", "err", err)
		return "", false
	}
	hint, err := resultFileNotice(path, fetch.SearchOutputTTL)
	if err != nil {
		log.ErrorT("log.mcp.smartsearch_result_reference_creation_failed_falling", "err", err)
		return "", false
	}
	log.InfoT("log.mcp.smartsearch_rendered_result_of_chars_exceeds", "charCount", charCount, "limit", limit, "path", path)
	if inline, kept, truncated := buildRankedInline(query, results, charCount, limit, format); inline != "" {
		return formatRankedDumpNotice(len(results), charCount, limit, kept, truncated, path, hint) + "\n\n---\n\n" + inline, true
	}
	return formatDumpPreview(results, charCount, limit, path, hint), true
}

// buildRankedInline rebuilds the response with a rank-aware content budget.
// It returns an empty string when a smaller render cannot be produced or when
// the formatter ignores result bodies (the mock-based delivery tests rely on
// this path to keep their whole-response preview assertions).
func buildRankedInline(query string, results []search.SearchResult, renderedChars, limit int, format func(string, []search.SearchResult) (string, error)) (string, int, int) {
	if format == nil || renderedChars <= limit || !hasInlineContent(results) {
		return "", 0, 0
	}

	skeleton := make([]search.SearchResult, len(results))
	copy(skeleton, results)
	for i := range skeleton {
		if !skeleton[i].ContentSaved {
			skeleton[i].Content = ""
		}
	}
	skeletonText, err := format(query, skeleton)
	if err != nil {
		return "", 0, 0
	}
	skeletonChars := utf8.RuneCountInString(skeletonText)
	if skeletonChars >= limit {
		return "", 0, 0
	}

	marker := i18n.T("tool.dump.ranked_entry_omitted")
	// Reserve space for the notice, path and result_ref hint plus one marker per
	// entry. The reserve is deliberately conservative: a ranked render that still
	// exceeds the limit falls back to the preview below.
	const noticeReserve = 768
	markerReserve := utf8.RuneCountInString(marker) * len(results)
	contentBudget := limit - skeletonChars - noticeReserve - markerReserve
	if contentBudget < minSearchContentChars {
		return "", 0, 0
	}

	budgeted := applyContentBudgetWithMarker(results, contentBudget, marker)
	inline, err := format(query, budgeted)
	if err != nil {
		return "", 0, 0
	}
	inlineChars := utf8.RuneCountInString(inline)
	if inlineChars >= renderedChars || inlineChars > limit {
		return "", 0, 0
	}

	kept, truncated := 0, 0
	for i := range results {
		if results[i].ContentSaved || strings.TrimSpace(results[i].Content) == "" {
			continue
		}
		if budgeted[i].Content == results[i].Content {
			kept++
		} else {
			truncated++
		}
	}
	if kept == 0 && truncated == 0 {
		return "", 0, 0
	}
	return inline, kept, truncated
}

func hasInlineContent(results []search.SearchResult) bool {
	for _, result := range results {
		if !result.ContentSaved && strings.TrimSpace(result.Content) != "" {
			return true
		}
	}
	return false
}

func formatRankedDumpNotice(results, chars, limit, kept, truncated int, path, hint string) string {
	var sb strings.Builder
	sb.WriteString(i18n.T("tool.dump.ranked_notice", "Results", results, "Chars", chars, "Limit", limit, "Kept", kept, "Truncated", truncated))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.T("tool.dump.file_path", "Path", path))
	sb.WriteString("\n\n")
	sb.WriteString(hint)
	return sb.String()
}

func formatDumpPreview(results []search.SearchResult, chars, limit int, path, hint string) string {
	var sb strings.Builder
	sb.WriteString(i18n.T("tool.dump.overflow_notice", "Results", len(results), "Chars", chars, "Limit", limit))
	sb.WriteString("\n\n")
	for i, candidate := range results {
		snippet := []rune(strings.TrimSpace(candidate.Content))
		if len(snippet) > 160 {
			snippet = append(snippet[:160], '.', '.', '.')
		}
		fmt.Fprintf(&sb, "%d. %s\n   URL: %s\n", i+1, candidate.Title, candidate.Url)
		if candidate.Engine != "" {
			sb.WriteString(i18n.T("tool.dump.engine_line", "Engine", candidate.Engine))
			sb.WriteString("\n")
		}
		sb.WriteString(i18n.T("tool.dump.snippet_line", "Snippet", string(snippet)))
		sb.WriteString("\n\n")
	}
	sb.WriteString(i18n.T("tool.dump.file_path", "Path", path))
	sb.WriteString("\n\n")
	sb.WriteString(hint)
	return sb.String()
}

// writeSearchDump 写入落盘文件并触发惰性清理，返回文件绝对路径。
func writeSearchDump(content string) (string, error) {
	dir := (config.CleanFetchConfig{FileOutputDir: searchDumpDir}).OutputDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cleanupSearchDumps(dir)

	var err error
	// O_EXCL 独占创建，随机后缀冲突时重试（同秒并发落盘概率极低，3 次足够）
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("search-%s-%s.md", time.Now().Format("20060102_150405"), randHex8())
		p := filepath.Join(dir, name)
		f, cerr := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if cerr != nil {
			err = cerr
			continue
		}
		_, werr := f.WriteString(content)
		ferr := f.Close()
		if werr != nil {
			err = werr
			continue
		}
		if ferr != nil {
			err = ferr
			continue
		}
		return p, nil
	}
	if err != nil {
		return "", err
	}
	return "", errors.New(i18n.T("tool.dump.create_exhausted"))
}

// cleanupSearchDumps 清理目录中超 TTL 的 search-*.md（只动本程序命名模式的文件，
// 目录里其它文件一律不动）。
func cleanupSearchDumps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-fetch.SearchOutputTTL)
	for _, e := range entries {
		if !e.Type().IsRegular() || !fetch.IsSearchOutputName(e.Name()) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(deadline) {
			continue
		}
		if rmErr := os.Remove(filepath.Join(dir, e.Name())); rmErr == nil {
			log.InfoT("log.mcp.smartsearch_expired_persisted_files_cleaned_up", "name", e.Name())
		}
	}
}

// randHex8 生成 8 位随机十六进制后缀。
func randHex8() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见；退化为纳秒时间戳保证可用性
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b)
}
