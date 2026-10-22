package mcphub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// toolResult 一次 MCP tools/call 的结果：结构化内容优先，纯文本兜底。
type toolResult struct {
	Structured map[string]any
	Text       string
}

// toolCaller 抽出 MCP 调用，便于轮转/降级逻辑单测。
type toolCaller interface {
	Call(ctx context.Context, s *server, tool string, args map[string]any, timeout time.Duration, key string) (toolResult, error)
}

// headerTransport 为每个请求加 User-Agent 与可选鉴权头。
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", userAgent)
	for k, v := range t.headers {
		if v != "" {
			clone.Header.Set(k, v)
		}
	}
	return t.base.RoundTrip(clone)
}

type mcpCaller struct {
	base http.RoundTripper
}

func (c *mcpCaller) Call(ctx context.Context, s *server, tool string, args map[string]any, timeout time.Duration, key string) (toolResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	headers := map[string]string{}
	if key != "" && s.authHeader != "" {
		headers[s.authHeader] = s.authPrefix + key
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "websearch-mcphub", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             s.url,
		HTTPClient:           &http.Client{Transport: &headerTransport{base: c.base, headers: headers}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		return toolResult{}, fmt.Errorf("%s: MCP 连接失败: %w", s.name, err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return toolResult{}, fmt.Errorf("%s: MCP 调用 %s 失败: %w", s.name, tool, err)
	}
	text := contentText(res.Content)
	if res.IsError {
		return toolResult{}, fmt.Errorf("%s: %s 返回错误: %s", s.name, tool, truncateErr(text))
	}
	structured, _ := res.StructuredContent.(map[string]any)
	return toolResult{Structured: structured, Text: text}, nil
}

func contentText(content []mcp.Content) string {
	var b strings.Builder
	for _, c := range content {
		if t, ok := c.(*mcp.TextContent); ok && t.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		return s[:240] + "..."
	}
	return s
}

// shouldCooldown 只对连接/限流/鉴权类错误冷却；站点级“不支持抓取”不应连坐整个 server。
func shouldCooldown(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"timeout", "timed out", "deadline exceeded", "connection", "connect:", "no such host",
		"eof", "tls", "429", "too many requests", "rate limit", "401", "403", "unauthorized",
		"forbidden", "502", "503", "504", "server closed",
		"keyless access is unavailable", "quota", "monthly limit", "monthly cap",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "invalid api key")
}
