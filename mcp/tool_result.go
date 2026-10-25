package mcpserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"websearch/pkg/fetch/webfetch"
	"websearch/pkg/i18n"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxResultReadChars = 8192

type ReadResultParams struct {
	projectParams
	Ref    string `json:"ref" jsonschema:"mcp.param.result.ref"`
	Offset int64  `json:"offset,omitempty" jsonschema:"mcp.param.result.offset"`
	Limit  int    `json:"limit,omitempty" jsonschema:"mcp.param.result.limit"`
}

type ReadResultOutput struct {
	Ref        string `json:"ref"`
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	EOF        bool   `json:"eof"`
}

type resultFile struct {
	path    string
	info    os.FileInfo
	expires time.Time
	// cacheChar/cacheByte 记录一次成功分页读取的终点（下一页首字符的字符
	// 偏移与字节偏移），顺序续读直接 Seek 跳过逐 rune 扫描。cacheByte<0 表示
	// 缓存未建立。任意 (char,byte) 边界对都正确，并发读者最后写入者胜。
	cacheChar int64
	cacheByte int64
}

// References are process-local capabilities, independent of MCP sessions and project labels.
var resultFiles = struct {
	sync.Mutex
	files map[string]resultFile
}{files: make(map[string]resultFile)}

// errResultUnavailable 是稳定哨兵（errors.Is 可用），对外文案按当前语言渲染。
var errResultUnavailable = errors.New("result_unavailable")

func resultUnavailableErr() error {
	return fmt.Errorf("%s: %w", i18n.T("tool.result.unavailable"), errResultUnavailable)
}

func savedResultReadNotice(result *webfetch.Result) string {
	ttl := 24 * time.Hour
	if webfetchLazyCfg != nil && webfetchLazyCfg.CleanFetch.FileTTL > 0 {
		ttl = time.Duration(webfetchLazyCfg.CleanFetch.FileTTL) * time.Hour
	}
	notice, err := resultFileNotice(result.FilePath, ttl)
	if err != nil {
		return i18n.T("tool.result.read_failed", "Err", err.Error())
	}
	return notice
}

func resultFileNotice(path string, ttl time.Duration) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", resultUnavailableErr()
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", resultUnavailableErr()
	}
	expires := info.ModTime().Add(ttl)
	if !time.Now().Before(expires) {
		return "", resultUnavailableErr()
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", errors.New(i18n.T("tool.result.ref_create_failed"))
	}
	ref := "result:" + hex.EncodeToString(token[:])
	resultFiles.Lock()
	for key, file := range resultFiles.files {
		if !time.Now().Before(file.expires) {
			delete(resultFiles.files, key)
		}
	}
	resultFiles.files[ref] = resultFile{path: path, info: info, expires: expires}
	resultFiles.Unlock()
	return i18n.T("tool.result.ref_notice", "Ref", ref, "Expires", expires.UTC().Format(time.RFC3339)), nil
}

func readSavedResult(ctx context.Context, _ *mcp.CallToolRequest, params *ReadResultParams) (result *mcp.CallToolResult, output *ReadResultOutput, err error) {
	if params.Offset < 0 || params.Limit < 0 || params.Limit > maxResultReadChars {
		return nil, nil, errors.New(i18n.T("tool.result.offset_invalid"))
	}
	limit := params.Limit
	if limit == 0 {
		limit = 4096
	}
	resultFiles.Lock()
	file, ok := resultFiles.files[params.Ref]
	resultFiles.Unlock()
	if !ok || !time.Now().Before(file.expires) {
		return nil, nil, resultUnavailableErr()
	}
	info, err := os.Lstat(file.path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, resultUnavailableErr()
	}
	// Confine symlink resolution and verify the opened file is the one we issued.
	f, err := os.OpenInRoot(filepath.Dir(file.path), filepath.Base(file.path))
	if err != nil {
		return nil, nil, resultUnavailableErr()
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !os.SameFile(file.info, info) || info.Size() != file.info.Size() || !info.ModTime().Equal(file.info.ModTime()) {
		return nil, nil, resultUnavailableErr()
	}
	reader := bufio.NewReader(f)
	var base int64     // 计数起点字节偏移（缓存命中时为缓存点，扫描路径为 0）
	var consumed int64 // 自 base 起累计消费的字节数
	if params.Offset == file.cacheChar {
		// 顺序续读命中缓存（含 offset 0）：直接按字节偏移定位，跳过逐 rune 扫描。
		if _, err := f.Seek(file.cacheByte, io.SeekStart); err != nil {
			return nil, nil, resultUnavailableErr()
		}
		base = file.cacheByte
	} else {
		for i := int64(0); i < params.Offset; i++ {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			_, size, err := reader.ReadRune()
			if err == io.EOF {
				return nil, nil, errors.New(i18n.T("tool.result.offset_beyond_end"))
			}
			if err != nil {
				return nil, nil, resultUnavailableErr()
			}
			consumed += int64(size)
		}
	}
	var body strings.Builder
	count := 0
	for count < limit {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		r, size, err := reader.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil || (r == utf8.RuneError && size == 1) {
			return nil, nil, resultUnavailableErr()
		}
		body.WriteRune(r)
		consumed += int64(size)
		count++
	}
	_, err = reader.Peek(1)
	if err != nil && err != io.EOF {
		return nil, nil, resultUnavailableErr()
	}
	if err != io.EOF {
		// 未到文件尾：记录下一页首字符的字符/字节偏移，供顺序续读 Seek 直达。
		resultFiles.Lock()
		if entry, ok := resultFiles.files[params.Ref]; ok && entry.expires.Equal(file.expires) {
			entry.cacheChar = params.Offset + int64(count)
			entry.cacheByte = base + consumed
			resultFiles.files[params.Ref] = entry
		}
		resultFiles.Unlock()
	}
	return nil, &ReadResultOutput{Ref: params.Ref, Content: body.String(), Offset: params.Offset, NextOffset: params.Offset + int64(count), EOF: err == io.EOF}, nil
}
