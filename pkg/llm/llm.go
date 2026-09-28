package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"websearch/pkg/client"
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
}

func NewClient(baseURL, apiKey, model_id string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model_id,
	}
}

// Chat 以非流式方式调用 LLM，返回完整回答。
func (c *Client) Chat(systemPrompt, userPrompt string) (string, error) {
	return c.ChatWithContext(context.Background(), systemPrompt, userPrompt)
}

// ChatWithContext 同 Chat，但支持 ctx 取消（如客户端断开时中止请求）。
func (c *Client) ChatWithContext(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("llm request marshal 失败: %w", err)
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
		log.Errf("llm req failed : %s", err.Error())
		return "", fmt.Errorf("llm api 调用失败: %w", err)
	}
	if res.StatusCode() != 200 {
		return "", fmt.Errorf("llm api 返回错误状态码: %d", res.StatusCode())
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm api 返回空结果")
	}
	return resp.Choices[0].Message.Content, nil
}

// ChatStream 以流式方式调用 LLM，通过 ch 逐 token 返回增量内容。
// 流结束时关闭 ch，并在 errCh 发送一次结果（nil 表示成功）。
// 调用方负责消费 ch；ctx 取消（如客户端断开）时中止并报告 ctx.Err()。
func (c *Client) ChatStream(ctx context.Context, systemPrompt, userPrompt string, ch chan<- string, errCh chan<- error) {
	defer close(ch)

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
		errCh <- fmt.Errorf("llm request marshal 失败: %w", err)
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
		log.Errf("llm stream req failed : %s", err.Error())
		errCh <- fmt.Errorf("llm api 调用失败: %w", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode() != 200 {
		errCh <- fmt.Errorf("llm api 返回错误状态码: %d", resp.StatusCode())
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 支持较长行
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
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
				errCh <- ctx.Err()
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Errf("llm stream read failed : %s", err.Error())
		errCh <- fmt.Errorf("llm 流式读取失败: %w", err)
		return
	}
	errCh <- nil
}
