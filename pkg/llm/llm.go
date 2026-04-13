package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
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
}

type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
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

func (c *Client) Chat(systemPrompt, userPrompt string) (string, error) {
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
