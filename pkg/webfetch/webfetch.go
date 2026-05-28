package webfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"websearch/pkg/config"
	"websearch/pkg/log"

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
}

// Fetcher 封装 go-webfetch Engine。
type Fetcher struct {
	engine *webfetch.Engine
}

// NewFromConfig 根据配置创建 Fetcher。
func NewFromConfig(cfg config.CleanFetchConfig) (*Fetcher, error) {
	outputDir := cfg.FileOutputDir
	if outputDir == "" {
		outputDir = filepath.Join(os.TempDir(), "webfetch")
	}

	fileTTL := time.Duration(cfg.FileTTL) * time.Hour
	if fileTTL <= 0 {
		fileTTL = 24 * time.Hour
	}

	maxInlineLines := cfg.MaxInlineLines
	if maxInlineLines <= 0 {
		maxInlineLines = 100
	}

	engine, err := webfetch.New(webfetch.Config{
		BlockPrivateIP:  true,
		MaxInlineLines:  maxInlineLines,
		MaxInlineChars:  cfg.MaxInlineChars,
		FileOutputDir:   outputDir,
		FileTTL:         fileTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("webfetch engine init failed: %w", err)
	}

	log.Infof("WebFetch 引擎已启用 (output_dir=%s, ttl=%s, max_inline_lines=%d)", outputDir, fileTTL, maxInlineLines)
	return &Fetcher{engine: engine}, nil
}

// Fetch 抓取网页或解析 PDF（自动检测 file:// 路径）。
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*Result, error) {
	// 本地 PDF 文件：使用 ParsePDFFile
	if strings.HasPrefix(rawURL, "file://") {
		localPath := strings.TrimPrefix(rawURL, "file://")
		// 处理 Windows 三斜杠格式 file:///C:/...
		if len(localPath) > 0 && localPath[0] == '/' && len(localPath) > 2 && localPath[2] == ':' {
			localPath = localPath[1:] // 去掉前导 /
		}
		localPath = strings.ReplaceAll(localPath, "/", string(os.PathSeparator))
		return f.parsePDFFile(ctx, localPath)
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
	}, nil
}

// parsePDFFile 解析本地 PDF 文件。
func (f *Fetcher) parsePDFFile(ctx context.Context, filePath string) (*Result, error) {
	res, err := f.engine.ParsePDFFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("PDF 解析失败: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("PDF 解析失败: %s", res.Error)
	}
	return &Result{
		Title:      res.Title,
		Mode:       res.Mode,
		Markdown:   res.Markdown,
		FilePath:   res.FilePath,
		TotalLines: res.TotalLines,
		TotalChars: res.TotalChars,
		AgentHint:  cleanAgentHint(res.AgentHint),
	}, nil
}

// cleanAgentHint 去掉 AgentHint 中的预览部分（空白行和分隔线污染）。
func cleanAgentHint(hint string) string {
	if idx := strings.Index(hint, "预览（"); idx != -1 {
		return strings.TrimRight(hint[:idx], "\n")
	}
	return hint
}

// Close 关闭引擎。
func (f *Fetcher) Close() error {
	return f.engine.Close()
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
