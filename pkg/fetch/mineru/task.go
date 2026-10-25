package mineru

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"websearch/pkg/log"
)

type taskWaitKey struct{}
type taskProgressKey struct{}

// WithTaskWait bounds polling per submitted task; zero queries once.
func WithTaskWait(ctx context.Context, wait time.Duration) context.Context {
	return context.WithValue(ctx, taskWaitKey{}, wait)
}

// WithTaskProgress reports remote state changes without creating another task.
func WithTaskProgress(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, taskProgressKey{}, report)
}

// MinerU uses both numeric and string error codes across its API variants.
type apiCode string

func (c *apiCode) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*c = apiCode(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*c = apiCode(number.String())
	return nil
}

func (c apiCode) number() int {
	n, _ := strconv.Atoi(string(c))
	return n
}

func (c apiCode) message(msg string) string {
	if c != "" && c != "0" && c.number() == 0 {
		return fmt.Sprintf("MinerU 错误 (%s): %s", c, msg)
	}
	if c.number() == 0 {
		if msg == "" {
			return "未知解析错误"
		}
		return msg
	}
	return mapAgentError(c.number(), msg)
}

type taskStatus struct {
	TaskID          string  `json:"task_id"`
	State           string  `json:"state"`
	FullZipURL      string  `json:"full_zip_url"`
	MarkdownURL     string  `json:"markdown_url"`
	ErrMsg          string  `json:"err_msg"`
	ErrCode         apiCode `json:"err_code"`
	ExtractProgress struct {
		ExtractedPages int `json:"extracted_pages"`
		TotalPages     int `json:"total_pages"`
	} `json:"extract_progress"`
}

func (s taskStatus) progress() string {
	label := map[string]string{"pending": "排队中", "running": "解析中", "converting": "格式转换中", "uploading": "文件下载/上传中", "waiting-file": "等待文件上传"}[s.State]
	if label == "" {
		label = s.State
	}
	if s.ExtractProgress.TotalPages > 0 {
		label += fmt.Sprintf("，%d/%d 页", s.ExtractProgress.ExtractedPages, s.ExtractProgress.TotalPages)
	}
	return label
}

// TaskPendingError preserves an unfinished upstream job for a later status query.
type TaskPendingError struct {
	TaskRef string
	Status  string
	Wait    time.Duration
}

func (e *TaskPendingError) Error() string {
	return fmt.Sprintf("MinerU 等待结束（%s），任务尚未完成；最后状态: %s；task_ref=%q。可用 pdf_parser 的 task_ref 继续查询原任务（省略 path/pages），无需重新提交", e.Wait, e.Status, e.TaskRef)
}

var taskRefPattern = regexp.MustCompile(`^(standard|batch|agent):[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func splitTaskRef(ref string) (string, string, error) {
	if !taskRefPattern.MatchString(ref) {
		return "", "", fmt.Errorf("task_ref 格式无效，应为 standard:<task_id>、batch:<batch_id> 或 agent:<task_id>")
	}
	kind, id, _ := strings.Cut(ref, ":")
	return kind, id, nil
}

func (c *Client) queryTask(ctx context.Context, ref string) (taskStatus, error) {
	kind, id, err := splitTaskRef(ref)
	if err != nil {
		return taskStatus{}, err
	}
	if kind != "agent" && !c.HasToken() {
		return taskStatus{}, fmt.Errorf("MinerU 精准解析任务查询需要 Token")
	}
	route := standardAPIPath + "/extract/task/" + id
	if kind == "batch" {
		route = standardAPIPath + "/extract-results/batch/" + id
	}
	if kind == "agent" {
		route = agentAPIPath + "/parse/" + id
	}
	var result struct {
		Code apiCode `json:"code"`
		Msg  string  `json:"msg"`
		Data struct {
			taskStatus
			ExtractResult []taskStatus `json:"extract_result"`
		} `json:"data"`
	}
	req := c.client.R().SetContext(ctx).SetResult(&result)
	if kind != "agent" {
		req.SetHeader("Authorization", "Bearer "+c.token)
	}
	resp, err := req.Get(c.endpoint + route)
	if err != nil {
		return taskStatus{}, fmt.Errorf("MinerU 查询失败: %w；task_ref=%q", err, ref)
	}
	if resp.StatusCode() == http.StatusUnauthorized && kind != "agent" {
		return taskStatus{}, fmt.Errorf("MinerU Token 无效或已过期 (HTTP 401)，请在 https://mineru.net/apiManage 重新获取；task_ref=%q", ref)
	}
	if resp.StatusCode() != http.StatusOK {
		return taskStatus{}, fmt.Errorf("MinerU 服务异常 (HTTP %d)；task_ref=%q", resp.StatusCode(), ref)
	}
	if result.Code != "" && result.Code != "0" {
		if kind != "agent" {
			if err := classifyStandardError(result.Code.number(), result.Msg); err != nil {
				return taskStatus{}, fmt.Errorf("%w；task_ref=%q", err, ref)
			}
		}
		return taskStatus{}, fmt.Errorf("MinerU 查询失败: %s；task_ref=%q", result.Code.message(result.Msg), ref)
	}
	status := result.Data.taskStatus
	if kind == "batch" {
		if len(result.Data.ExtractResult) != 1 {
			return taskStatus{}, fmt.Errorf("MinerU 上传任务应返回一个文件结果；task_ref=%q", ref)
		}
		status = result.Data.ExtractResult[0]
	}
	return status, nil
}

func (c *Client) pollTask(ctx context.Context, ref string) (string, error) {
	wait := pollTimeout
	if configured, ok := ctx.Value(taskWaitKey{}).(time.Duration); ok {
		wait = configured
	}
	if wait < 0 {
		return "", fmt.Errorf("MinerU 等待时间不能为负")
	}
	pollCtx := ctx
	if wait > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	last := "尚未取得任务状态"
	interval := 2 * time.Second
	for {
		status, err := c.queryTask(pollCtx, ref)
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w；task_ref=%q", ctx.Err(), ref)
		}
		if err != nil {
			if pollCtx.Err() != nil {
				return "", &TaskPendingError{ref, last, wait}
			}
			return "", err
		}
		progress := status.progress()
		if progress != last {
			last = progress
			log.InfoT("log.fetch.mineru.mineru_task_status_task_ref", "ref", ref, "progress", progress)
			if report, ok := ctx.Value(taskProgressKey{}).(func(string)); ok {
				report(progress)
			}
		}
		switch status.State {
		case "done":
			resultURL := status.FullZipURL
			if strings.HasPrefix(ref, "agent:") {
				resultURL = status.MarkdownURL
			}
			if resultURL == "" {
				return "", fmt.Errorf("MinerU 解析完成但未返回结果链接；task_ref=%q", ref)
			}
			return resultURL, nil
		case "failed":
			if err := classifyStandardError(status.ErrCode.number(), status.ErrMsg); err != nil {
				return "", fmt.Errorf("%w；task_ref=%q", err, ref)
			}
			return "", fmt.Errorf("MinerU 解析失败: %s；task_ref=%q", status.ErrCode.message(status.ErrMsg), ref)
		case "pending", "running", "converting", "uploading", "waiting-file":
		default:
			if status.State == "" {
				return "", fmt.Errorf("MinerU 未返回任务状态；task_ref=%q", ref)
			}
		}
		if wait == 0 {
			return "", &TaskPendingError{ref, last, wait}
		}
		select {
		case <-pollCtx.Done():
			if ctx.Err() != nil {
				return "", fmt.Errorf("%w；task_ref=%q", ctx.Err(), ref)
			}
			return "", &TaskPendingError{ref, last, wait}
		case <-time.After(interval):
		}
		interval = min(interval*2, 30*time.Second)
	}
}

// ResumeTask queries an existing task; it never submits or uploads a document.
func (c *Client) ResumeTask(ctx context.Context, ref string) (string, error) {
	kind, _, err := splitTaskRef(ref)
	if err != nil {
		return "", err
	}
	if ctx.Value(taskWaitKey{}) == nil {
		ctx = WithTaskWait(ctx, 0)
	}
	resultURL, err := c.pollTask(ctx, ref)
	if err != nil {
		return "", err
	}
	if kind == "agent" {
		md, err := c.downloadMarkdown(ctx, resultURL)
		if err != nil {
			return "", fmt.Errorf("MinerU 结果下载失败: %w；task_ref=%q", err, ref)
		}
		return "> MinerU 轻量结果不包含可下载的图片包。\n\n" + md, nil
	}
	md, err := c.downloadZIP(ctx, resultURL)
	if err != nil {
		return "", fmt.Errorf("MinerU 结果下载失败: %w；task_ref=%q", err, ref)
	}
	return fmt.Sprintf("> **MinerU 完整结果 ZIP（含图片资源）**：%s。Markdown 中的相对图片路径需与 ZIP 内的 images/ 一起使用。\n\n%s", resultURL, md), nil
}
