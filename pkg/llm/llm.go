package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"websearch/pkg/client"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

// streamChunk OpenAI-compatible SSE 流式响应的单个 chunk。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	// timeout 单次调用（含流式全程）的整体超时；0 = 不额外设限，
	// 此时沿用共享 HTTP 客户端自身的超时（默认 30s）。
	timeout time.Duration
}

func NewClient(baseURL, apiKey, model_id string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model_id,
	}
}

// NewClientWithTimeout 同 NewClient，但为每次调用设置整体超时。
// 调用方 ctx 已带更早 deadline 时不覆盖（取更早者）。
func NewClientWithTimeout(baseURL, apiKey, model_id string, timeout time.Duration) *Client {
	c := NewClient(baseURL, apiKey, model_id)
	c.timeout = timeout
	return c
}

// withDeadline 确保 ctx 带上本端超时；调用方已有不晚于本端超时的 deadline
// 时遵循调用方，否则按 c.timeout 派生（两者恒取更早者）。
func (c *Client) withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return ctx, nil
	}
	if dl, ok := ctx.Deadline(); ok && !dl.After(time.Now().Add(c.timeout)) {
		return ctx, nil
	}
	return context.WithTimeout(ctx, c.timeout)
}

// Chat 以非流式方式调用 LLM，返回完整回答。
func (c *Client) Chat(systemPrompt, userPrompt string) (string, error) {
	return c.ChatWithContext(context.Background(), systemPrompt, userPrompt)
}

// ChatWithContext 同 Chat，但支持 ctx 取消（如客户端断开时中止请求）。
func (c *Client) ChatWithContext(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := c.withDeadline(ctx)
	if cancel != nil {
		defer cancel()
	}
	reqBody := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("search.llm.marshal_failed"), err)
	}

	var resp ChatResponse
	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	res, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", c.apiKey)).
		SetHeader("Content-Type", "application/json").
		SetBody(bytes.NewReader(body)).
		SetResult(&resp).
		Post(url)
	if err != nil {
		log.ErrorT("log.llm.llm_req_failed", "error", err.Error())
		return "", fmt.Errorf("%s: %w", i18n.T("search.llm.call_failed"), err)
	}
	if res.StatusCode() != 200 {
		return "", errors.New(i18n.T("search.llm.bad_status", "Status", res.StatusCode()))
	}
	if len(resp.Choices) == 0 {
		return "", errors.New(i18n.T("search.llm.empty_result"))
	}
	return resp.Choices[0].Message.Content, nil
}

// ChatStream 以流式方式调用 LLM，通过 ch 逐 token 返回增量内容。
// 完成契约：先关闭 ch，再向 errCh 发送一次结果（nil 表示成功），
// 消费端排干 ch 后即可阻塞读取 errCh，token 与完成信号严格有序。
// 调用方负责消费 ch；ctx 取消（如客户端断开）时中止并报告 ctx.Err()。
func (c *Client) ChatStream(ctx context.Context, systemPrompt, userPrompt string, ch chan<- string, errCh chan<- error) {
	var streamErr error
	defer func() {
		close(ch)
		errCh <- streamErr
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := c.withDeadline(ctx)
	if cancel != nil {
		defer cancel()
	}

	reqBody := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: true,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		streamErr = fmt.Errorf("%s: %w", i18n.T("search.llm.marshal_failed"), err)
		return
	}

	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	resp, err := client.DefaultClient.R().
		SetContext(ctx).
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", c.apiKey)).
		SetHeader("Content-Type", "application/json").
		SetBody(bytes.NewReader(body)).
		SetResponseDoNotParse(true).
		Post(url)
	if err != nil {
		log.ErrorT("log.llm.llm_stream_req_failed", "error", err.Error())
		streamErr = fmt.Errorf("%s: %w", i18n.T("search.llm.call_failed"), err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode() != 200 {
		streamErr = errors.New(i18n.T("search.llm.bad_status", "Status", resp.StatusCode()))
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 支持较长行
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			streamErr = ctx.Err()
			return
		default:
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			select {
			case ch <- chunk.Choices[0].Delta.Content:
			case <-ctx.Done():
				streamErr = ctx.Err()
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.ErrorT("log.llm.llm_stream_read_failed", "error", err.Error())
		streamErr = fmt.Errorf("%s: %w", i18n.T("search.llm.stream_read_failed"), err)
		return
	}
}
