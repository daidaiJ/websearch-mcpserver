// Package everything 提供对 Everything (voidtools) HTTP Server 的只读检索客户端。
// 仅做文件名/路径索引检索（探测、搜索），不提供文件下载能力。
// Everything 1.5 需安装官方 http_server 插件（建议仅绑定 127.0.0.1 并配置鉴权）。
package everything

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 探测/检索失败的分类错误：
//   - ErrUnavailable：HTTP Server 不可达（未启动、端口不对）
//   - ErrAuth：鉴权失败（配置的用户名/口令与服务端不一致）
// 调用方据此决定是否暴露 file_search 工具及给出可行动的提示。
var (
	ErrUnavailable = errors.New("everything http server 不可达")
	ErrAuth        = errors.New("everything http server 鉴权失败")
)

// Client Everything HTTP Server 客户端。
type Client struct {
	baseURL  string
	username string
	password string
	timeout  time.Duration
	http     *http.Client
}

// New 创建客户端。baseURL 为 HTTP Server 地址（如 http://127.0.0.1:4180）；
// username/password 为 Basic 鉴权凭据，服务端未配置鉴权时留空；timeout 为单次请求超时。
func New(baseURL, username, password string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		timeout:  timeout,
		http:     &http.Client{Timeout: timeout},
	}
}

// SearchOptions 一次检索的返回控制与原生过滤参数。
type SearchOptions struct {
	Offset      int    // 起始条目（分页）
	Count       int    // 返回条数上限（必须显式设置：JSON 模式下服务端默认返回全量）
	Sort        string // name / path / size / date_modified（空 = 服务端默认 name）
	Descending  bool   // 配合 Sort 使用
	MatchCase   bool   // 区分大小写（原生 i 参数）
	WholeWord   bool   // 全字匹配（原生 w 参数）
	Regex       bool   // 正则检索（原生 r 参数）
	Diacritics  bool   // 区分变音符号（原生 m 参数）
}

// Item 一条索引命中。
type Item struct {
	Type         string `json:"type"`          // file / folder
	Name         string `json:"name"`          // 文件名
	Path         string `json:"path"`          // 所在目录
	Size         string `json:"size"`          // 字节数（服务端以字符串返回）
	DateModified string `json:"date_modified"` // Windows FILETIME 字符串（1601 起 100ns）
}

// Result 一次检索的结果集。
type Result struct {
	Total int64  `json:"totalResults"` // 命中总数
	Items []Item `json:"results"`
}

// ModifiedTime 将 FILETIME 字符串转为本地时间；无效值返回零值与 false。
func (it Item) ModifiedTime() (time.Time, bool) {
	return filetimeToTime(it.DateModified)
}

// Probe 探测 HTTP Server 可用性：连通、鉴权通过、JSON 输出正常三者齐备才可用。
// 返回 ErrUnavailable / ErrAuth 便于区分提示；服务端正常返回 nil。
func (c *Client) Probe(ctx context.Context) error {
	_, err := c.doQuery(ctx, url.Values{"s": {""}, "j": {"1"}, "c": {"1"}})
	return err
}

// Search 执行检索。query 为最终查询串（调用方负责拼装路径限定项与用户词）。
func (c *Client) Search(ctx context.Context, query string, opt SearchOptions) (*Result, error) {
	q := url.Values{
		"s":             {query},
		"j":             {"1"}, // JSON 输出；不带时返回 HTML
		"c":             {strconv.Itoa(opt.Count)},
		"o":             {strconv.Itoa(opt.Offset)},
		"path_column":          {"1"},
		"size_column":          {"1"},
		"date_modified_column": {"1"},
	}
	if opt.Sort != "" {
		q.Set("sort", opt.Sort)
		if opt.Descending {
			q.Set("ascending", "0")
		}
	}
	for _, flag := range []struct {
		on    bool
		param string
	}{
		{opt.MatchCase, "i"}, {opt.WholeWord, "w"},
		{opt.Regex, "r"}, {opt.Diacritics, "m"},
	} {
		if flag.on {
			q.Set(flag.param, "1")
		}
	}
	return c.doQuery(ctx, q)
}

// doQuery 发起查询并解析 JSON 响应；网络错误归类为 ErrUnavailable，
// 401/403 归类为 ErrAuth，响应体不含 JSON 结果结构视为协议异常。
func (c *Client) doQuery(ctx context.Context, q url.Values) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w（请核对 everything.username/password 与 Everything HTTP Server 的凭据）", ErrAuth)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: 响应不是预期的 JSON 结果（HTTP %d）", ErrUnavailable, resp.StatusCode)
	}
	return &result, nil
}

// filetimeToTimeEpoch 偏移：Windows FILETIME 纪元 1601-01-01 到 Unix 纪元的 100ns 数。
const filetimeToTimeEpoch = 116444736000000000

func filetimeToTime(ft string) (time.Time, bool) {
	raw, err := strconv.ParseInt(strings.TrimSpace(ft), 10, 64)
	if err != nil || raw <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, (raw-filetimeToTimeEpoch)*100).Local(), true
}
