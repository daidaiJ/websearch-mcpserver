package mineru

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"websearch/pkg/log"
	"websearch/pkg/proxy"

	"resty.dev/v3"
)

const (
	baseURL         = "https://mineru.net"
	standardAPIPath = "/api/v4"
	agentAPIPath    = "/api/v1/agent"

	pollInterval   = 3 * time.Second
	pollTimeout    = 5 * time.Minute
	maxAgentSizeMB = 10

	defaultModelVersion = "pipeline"
	defaultLang         = "ch"

	// DefaultPageLimit MinerU 精准 API 单任务页数上限（官方文档 2026-10：200MB / 600 页）。
	DefaultPageLimit = 600
)

// ErrFileTooLarge 文件超过 Agent 轻量 API 限制（10MB）。
var ErrFileTooLarge = errors.New("file exceeds MinerU Agent API size limit (10MB)")

// ErrPageLimit means MinerU rejected the original document's page count.
var ErrPageLimit = errors.New("MinerU PDF 页数超过接口限制")

// ErrRemoteURLRejected means MinerU cannot fetch a URL that the local service may still read.
var ErrRemoteURLRejected = errors.New("MinerU 无法直接读取远程 PDF URL")

// Client MinerU 文档解析 API 客户端。
type Client struct {
	token        string
	endpoint     string
	modelVersion string
	ocr          bool
	formula      bool
	table        bool
	lang         string
	pageLimit    int
	client       *resty.Client
}

// NewFromConfig 根据配置创建 MinerU 客户端。
// Token 为空时仍可创建（Agent 轻量 API 可用）；pageLimit <= 0 时取 DefaultPageLimit。
func NewFromConfig(token, modelVersion, lang string, ocr, formula, table bool, proxyURL string, pageLimit int) *Client {
	if modelVersion == "" {
		modelVersion = defaultModelVersion
	}
	if lang == "" {
		lang = defaultLang
	}
	if pageLimit <= 0 {
		pageLimit = DefaultPageLimit
	}

	var rc *resty.Client
	if proxyURL != "" {
		httpClient := proxy.NewDynamicHTTPClient(func() string { return proxyURL }, 60*time.Second)
		rc = resty.NewWithClient(httpClient)
	} else {
		rc = resty.New()
	}
	rc.SetTimeout(60 * time.Second)

	return &Client{
		token:        token,
		endpoint:     baseURL,
		modelVersion: modelVersion,
		ocr:          ocr,
		formula:      formula,
		table:        table,
		lang:         lang,
		pageLimit:    pageLimit,
		client:       rc,
	}
}

// PageLimit 返回精准 API 单任务页数上限。
func (c *Client) PageLimit() int { return c.pageLimit }

// HasToken 返回是否配置了 Token（可使用精准解析 API）。
func (c *Client) HasToken() bool {
	return c.token != ""
}

// ParseURL 通过精准解析 API 解析远程文件 URL。需要 Token。
func (c *Client) ParseURL(ctx context.Context, fileURL string) (string, error) {
	return c.ParseURLWithPages(ctx, fileURL, nil, 0)
}

// ParseURLWithPages 向 MinerU 传递选页范围；省略选页时使用 maxPages。
func (c *Client) ParseURLWithPages(ctx context.Context, fileURL string, pages []int, maxPages int) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("MinerU 精准解析 API 需要 Token")
	}
	pageRanges, err := standardPageRanges(pages, maxPages, c.pageLimit)
	if err != nil {
		return "", err
	}

	taskID, err := c.createTask(ctx, fileURL, pageRanges)
	if err != nil {
		return "", err
	}
	log.Infof("MinerU 精准 API 任务已提交: task_id=%s", taskID)

	zipURL, err := c.pollStandardTask(ctx, taskID)
	if err != nil {
		return "", err
	}

	md, err := c.downloadZIP(ctx, zipURL)
	if err != nil {
		return "", fmt.Errorf("MinerU 结果下载失败: %w", err)
	}
	return fmt.Sprintf("> **MinerU 完整结果 ZIP（含图片资源）**：%s。Markdown 中的相对图片路径需与 ZIP 内的 images/ 一起使用。\n\n%s", zipURL, md), nil
}

// standardPageRanges 将已排序的 MCP 页码转成精准 API 的 page_ranges。
// API 对原文件有页数上限（limit），指定页范围也不能解除该限制；
// 页码超出上限时返回 ErrPageLimit，由调用方决定是否本地裁切后重试。
func standardPageRanges(pages []int, maxPages, limit int) (string, error) {
	if len(pages) == 0 {
		if maxPages <= 0 {
			return "", nil
		}
		if maxPages > limit {
			maxPages = limit
		}
		return fmt.Sprintf("1-%d", maxPages), nil
	}
	if len(pages) > limit || pages[len(pages)-1] > limit {
		return "", fmt.Errorf("%w：精准 API 单次最多 %d 页", ErrPageLimit, limit)
	}
	var parts []string
	for i := 0; i < len(pages); {
		start, end := pages[i], pages[i]
		for i+1 < len(pages) && pages[i+1] == end+1 {
			i++
			end = pages[i]
		}
		if start == end {
			parts = append(parts, fmt.Sprint(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, end))
		}
		i++
	}
	return strings.Join(parts, ","), nil
}

// ParseFile 通过 Agent 轻量 API 解析本地文件（签名上传模式）。
// 文件大小超过 10MB 时返回 ErrFileTooLarge。
func (c *Client) ParseFile(ctx context.Context, filePath string) (string, error) {
	return c.ParseFileWithPages(ctx, filePath, nil, 0)
}

// ParseFileWithPages 先裁切所选页，再通过轻量 API 上传不超过 20 页的子 PDF。
func (c *Client) ParseFileWithPages(ctx context.Context, filePath string, pages []int, maxPages int) (string, error) {
	if len(pages) > 0 || maxPages > 0 {
		cropped, _, _, cleanup, err := CropPDF(ctx, filePath, pages, maxPages, 20)
		if err != nil {
			return "", err
		}
		defer cleanup()
		filePath = cropped
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("文件不存在: %w", err)
	}
	if info.Size() > maxAgentSizeMB*1024*1024 {
		return "", ErrFileTooLarge
	}

	taskID, uploadURL, err := c.createFileTask(ctx, filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	log.Infof("MinerU Agent API 任务已创建: task_id=%s", taskID)

	if err := c.uploadFile(ctx, uploadURL, filePath); err != nil {
		return "", fmt.Errorf("MinerU 文件上传失败: %w", err)
	}
	log.Infof("MinerU 文件上传完成，等待解析: task_id=%s", taskID)

	mdURL, err := c.pollAgentTask(ctx, taskID)
	if err != nil {
		return "", err
	}

	md, err := c.downloadMarkdown(ctx, mdURL)
	if err != nil {
		return "", fmt.Errorf("MinerU 结果下载失败: %w", err)
	}
	if strings.Contains(md, "images/") {
		md = "> 注：MinerU 轻量 API 仅提供 Markdown 链接，未提供可下载的图片包；文中的 images/ 相对路径可能无法读取。需要完整配图时请使用带 Token 的精准 API。\n\n" + md
	}
	return md, nil
}

// ── 精准解析 API（/api/v4）────────────────────────────────────────────────

type createTaskResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

func (c *Client) createTask(ctx context.Context, fileURL, pageRanges string) (string, error) {
	body := map[string]any{
		"url":            fileURL,
		"model_version":  c.modelVersion,
		"is_ocr":         c.ocr,
		"enable_formula": c.formula,
		"enable_table":   c.table,
		"language":       c.lang,
	}
	if pageRanges != "" {
		body["page_ranges"] = pageRanges
	}

	var resp createTaskResp
	res, err := c.client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+c.token).
		SetBody(body).
		SetResult(&resp).
		Post(c.endpoint + standardAPIPath + "/extract/task")
	if err != nil {
		return "", fmt.Errorf("MinerU 服务连接失败: %w", err)
	}
	if res.StatusCode() != http.StatusOK {
		if res.StatusCode() == http.StatusUnauthorized {
			return "", fmt.Errorf("MinerU Token 无效或已过期 (HTTP 401)，请在 https://mineru.net/apiManage 重新获取")
		}
		return "", fmt.Errorf("MinerU 服务异常 (HTTP %d)", res.StatusCode())
	}
	if resp.Code != 0 {
		if err := classifyStandardError(resp.Code, resp.Msg); err != nil {
			return "", err
		}
		return "", fmt.Errorf("MinerU 任务提交失败: %s", mapAPIError(resp.Code, resp.Msg))
	}
	return resp.Data.TaskID, nil
}

// classifyStandardError 把精准 API 中「可本地补救」的错误码转成哨兵错误：
// -60006 页数超限（本地裁切可解）、-60023 源 URL 被拒（本地下载上传可解）。
func classifyStandardError(code int, msg string) error {
	switch code {
	case -60006:
		return fmt.Errorf("%w: %s", ErrPageLimit, mapAPIError(code, msg))
	case -60023:
		return fmt.Errorf("%w: %s", ErrRemoteURLRejected, mapAPIError(code, msg))
	default:
		return nil
	}
}

type taskStatusResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID          string `json:"task_id"`
		State           string `json:"state"`
		FullZipURL      string `json:"full_zip_url"`
		ErrMsg          string `json:"err_msg"`
		ErrCode         int    `json:"err_code"`
		ExtractProgress struct {
			ExtractedPages int    `json:"extracted_pages"`
			TotalPages     int    `json:"total_pages"`
			StartTime      string `json:"start_time"`
		} `json:"extract_progress"`
	} `json:"data"`
}

func (c *Client) pollStandardTask(ctx context.Context, taskID string) (string, error) {
	deadline, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	for {
		select {
		case <-deadline.Done():
			return "", fmt.Errorf("MinerU 解析超时（超过 %s），请稍后重试 task_id: %s", pollTimeout, taskID)
		default:
		}

		var resp taskStatusResp
		res, err := c.client.R().
			SetContext(deadline).
			SetHeader("Authorization", "Bearer "+c.token).
			SetResult(&resp).
			Get(fmt.Sprintf("%s%s/extract/task/%s", c.endpoint, standardAPIPath, taskID))
		if err != nil {
			return "", fmt.Errorf("MinerU 查询失败: %w", err)
		}
		if res.StatusCode() != http.StatusOK {
			if res.StatusCode() == http.StatusUnauthorized {
				return "", fmt.Errorf("MinerU Token 无效或已过期 (HTTP 401)，请在 https://mineru.net/apiManage 重新获取")
			}
			return "", fmt.Errorf("MinerU 服务异常 (HTTP %d)", res.StatusCode())
		}
		if resp.Code != 0 {
			if err := classifyStandardError(resp.Code, resp.Msg); err != nil {
				return "", err
			}
			return "", fmt.Errorf("MinerU 查询失败: %s", mapAPIError(resp.Code, resp.Msg))
		}

		switch resp.Data.State {
		case "done":
			if resp.Data.FullZipURL == "" {
				return "", fmt.Errorf("MinerU 解析完成但未返回结果链接")
			}
			return resp.Data.FullZipURL, nil
		case "failed":
			if err := classifyStandardError(resp.Data.ErrCode, resp.Data.ErrMsg); err != nil {
				return "", err
			}
			return "", fmt.Errorf("MinerU 解析失败: %s", mapAPIError(resp.Data.ErrCode, resp.Data.ErrMsg))
		case "pending", "running", "converting":
			ep := resp.Data.ExtractProgress
			if ep.TotalPages > 0 {
				log.Infof("MinerU 解析进度: %d/%d 页", ep.ExtractedPages, ep.TotalPages)
			}
			time.Sleep(pollInterval)
		default:
			time.Sleep(pollInterval)
		}
	}
}

// ── Agent 轻量 API（/api/v1/agent）─────────────────────────────────────────

type agentCreateResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID  string `json:"task_id"`
		FileURL string `json:"file_url"`
	} `json:"data"`
}

func (c *Client) createFileTask(ctx context.Context, fileName string) (taskID, uploadURL string, err error) {
	body := map[string]any{
		"file_name":      fileName,
		"language":       c.lang,
		"is_ocr":         c.ocr,
		"enable_formula": c.formula,
		"enable_table":   c.table,
	}

	var resp agentCreateResp
	res, err := c.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		SetResult(&resp).
		Post(c.endpoint + agentAPIPath + "/parse/file")
	if err != nil {
		return "", "", fmt.Errorf("MinerU 服务连接失败: %w", err)
	}
	if res.StatusCode() != http.StatusOK {
		return "", "", fmt.Errorf("MinerU 服务异常 (HTTP %d)", res.StatusCode())
	}
	if resp.Code != 0 {
		return "", "", fmt.Errorf("MinerU 创建任务失败: %s", mapAgentError(resp.Code, resp.Msg))
	}
	return resp.Data.TaskID, resp.Data.FileURL, nil
}

func (c *Client) uploadFile(ctx context.Context, uploadURL, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 签名上传使用原生 http.Client，避免 resty 添加额外请求头
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, file)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("上传失败，HTTP %d", resp.StatusCode)
	}
	return nil
}

type agentStatusResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID      string `json:"task_id"`
		State       string `json:"state"`
		MarkdownURL string `json:"markdown_url"`
		ErrMsg      string `json:"err_msg"`
		ErrCode     int    `json:"err_code"`
	} `json:"data"`
}

func (c *Client) pollAgentTask(ctx context.Context, taskID string) (string, error) {
	deadline, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	for {
		select {
		case <-deadline.Done():
			return "", fmt.Errorf("MinerU 解析超时（超过 %s），请稍后重试 task_id: %s", pollTimeout, taskID)
		default:
		}

		var resp agentStatusResp
		res, err := c.client.R().
			SetContext(deadline).
			SetResult(&resp).
			Get(fmt.Sprintf("%s%s/parse/%s", c.endpoint, agentAPIPath, taskID))
		if err != nil {
			return "", fmt.Errorf("MinerU 查询失败: %w", err)
		}
		if res.StatusCode() != http.StatusOK {
			return "", fmt.Errorf("MinerU 服务异常 (HTTP %d)", res.StatusCode())
		}
		if resp.Code != 0 {
			return "", fmt.Errorf("MinerU 查询失败: %s", mapAgentError(resp.Code, resp.Msg))
		}

		switch resp.Data.State {
		case "done":
			if resp.Data.MarkdownURL == "" {
				return "", fmt.Errorf("MinerU 解析完成但未返回结果链接")
			}
			return resp.Data.MarkdownURL, nil
		case "failed":
			return "", fmt.Errorf("MinerU 解析失败: %s", mapAgentError(resp.Data.ErrCode, resp.Data.ErrMsg))
		case "waiting-file", "uploading", "pending", "running":
			time.Sleep(pollInterval)
		default:
			time.Sleep(pollInterval)
		}
	}
}

// ── 下载与提取 ──────────────────────────────────────────────────────────────

func (c *Client) downloadZIP(ctx context.Context, zipURL string) (string, error) {
	res, err := c.client.R().SetContext(ctx).Get(zipURL)
	if err != nil {
		return "", err
	}
	if res.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("下载失败，HTTP %d", res.StatusCode())
	}
	return extractFullMD(res.Bytes())
}

func (c *Client) downloadMarkdown(ctx context.Context, mdURL string) (string, error) {
	res, err := c.client.R().SetContext(ctx).Get(mdURL)
	if err != nil {
		return "", err
	}
	if res.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("下载失败，HTTP %d", res.StatusCode())
	}
	return string(res.Bytes()), nil
}

func extractFullMD(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("ZIP 解压失败: %w", err)
	}
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "full.md") {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			content, err := io.ReadAll(rc)
			if err != nil {
				return "", err
			}
			return string(content), nil
		}
	}
	return "", fmt.Errorf("ZIP 中未找到 full.md 文件")
}

// ── 错误映射 ────────────────────────────────────────────────────────────────

// mapAPIError 将精准解析 API 错误码翻译为用户友好的中文提示。
func mapAPIError(code int, msg string) string {
	if code == 0 {
		return ""
	}
	if msg == "" {
		msg = "未知错误"
	}
	switch code {
	case -500:
		return "请求参数格式错误"
	case -10001:
		return "MinerU 服务暂时不可用，请稍后重试"
	case -10002:
		return "请求参数错误"
	case -60001:
		return "MinerU 服务繁忙，请稍后重试"
	case -60002:
		return "不支持的文件格式"
	case -60003:
		return "文件读取失败，请检查文件是否损坏"
	case -60004:
		return "文件为空"
	case -60005:
		return "文件超过大小限制 (200MB)"
	case -60006:
		return "文件页数超过限制 (600页)"
	case -60007:
		return "MinerU 模型服务暂不可用，请稍后重试"
	case -60008:
		return "文件 URL 访问超时，请检查链接是否可用"
	case -60009:
		return "MinerU 任务队列已满，请稍后重试"
	case -60010:
		return "文件解析失败"
	case -60015, -60016:
		return "文件格式转换失败，建议转为 PDF 后重试"
	case -60017:
		return "MinerU 重试次数已达上限，请稍后重试"
	case -60018:
		return "MinerU 每日解析额度已用完"
	case -60022:
		return "MinerU 无法访问文件 URL，可能是网络限制"
	default:
		// A0xxx 系列 Token 错误（API 文档中的错误码格式）
		if isTokenError(msg) {
			return "MinerU Token 无效或已过期，请更新配置"
		}
		return fmt.Sprintf("MinerU 错误 (%d): %s", code, msg)
	}
}

// mapAgentError 将 Agent 轻量 API 错误码翻译为用户友好的中文提示。
func mapAgentError(code int, msg string) string {
	if code == 0 {
		return ""
	}
	switch code {
	case -30001:
		return "文件超过轻量 API 大小限制 (10MB)"
	case -30002:
		return "轻量 API 不支持该文件格式"
	case -30003:
		return "文件页数超过轻量 API 限制 (20页)；可用 pages 指定 20 页以内范围"
	case -30004:
		return "请求参数错误"
	default:
		return mapAPIError(code, msg)
	}
}

// isTokenError 通过错误消息判断是否为 Token 相关错误。
func isTokenError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "token") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "unauthorized")
}
