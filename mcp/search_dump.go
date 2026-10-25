package mcpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	"websearch/pkg/config"
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

const (
	searchDumpFileTTL = 7 * 24 * time.Hour // 落盘文件保留时长
)

// searchDumpDir smartsearch 超限落盘目录（applySearchEngine 注入 cleanfetch.file_output_dir 原始配置值）。
var searchDumpDir string

// searchDumpFileRe 匹配本程序落盘的文件名（search-日期_时间-8位随机hex.md）。
// 清理只删除匹配该模式的文件，与 webfetch 的 savedFileRe（日期开头命名）互不重叠，
// 用户放在输出目录里的其它文件一律不动。
var searchDumpFileRe = regexp.MustCompile(`^search-\d{8}_\d{6}-[0-9a-f]{8}\.md$`)

// dumpSearchResultsIfOversize 渲染结果超过内联上限时整体落盘，返回替代响应文本
// （统计 + 文件路径 + 读取提示）。未超限 / 落盘禁用时返回 ("", false)；
// 写入失败按原样内联返回（fail-open：落盘只是体积保护，不应让搜索整体失败）。
func dumpSearchResultsIfOversize(results []search.SearchResult, rendered string) (string, bool) {
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
	hint, err := resultFileNotice(path, searchDumpFileTTL)
	if err != nil {
		log.ErrorT("log.mcp.smartsearch_result_reference_creation_failed_falling", "err", err)
		return "", false
	}
	log.InfoT("log.mcp.smartsearch_rendered_result_of_chars_exceeds", "charCount", charCount, "limit", limit, "path", path)
	var sb strings.Builder
	sb.WriteString(i18n.T("tool.dump.overflow_notice", "Results", len(results), "Chars", charCount, "Limit", limit))
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
	return sb.String(), true
}

// writeSearchDump 写入落盘文件并触发惰性清理，返回文件绝对路径。
func writeSearchDump(content string) (string, error) {
	dir := searchDumpDir
	if dir == "" {
		dir = filepath.Join(config.ExeBaseDir(), "fetchdata")
	}
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
// 目录里其它文件一律不动——与 webfetch savedFileRe 同纪律）。
func cleanupSearchDumps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-searchDumpFileTTL)
	for _, e := range entries {
		if e.IsDir() || !searchDumpFileRe.MatchString(e.Name()) {
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
