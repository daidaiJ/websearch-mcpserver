package summarizer

import (
	"fmt"
	"websearch/pkg/llm"
	"websearch/pkg/log"
	"websearch/pkg/search"
)

type Summarizer struct {
	llm *llm.Client
}

func NewSummarizer(baseURL, apiKey string) *Summarizer {
	return &Summarizer{
		llm: llm.NewClient(baseURL, apiKey),
	}
}

// Summarize 搜索完成后，根据 query + intent + 搜索结果调用 LLM 生成摘要
func (s *Summarizer) Summarize(query, intent string, results []search.SearchResult) (string, error) {
	userPrompt := BuildUserPrompt(query, intent, results)
	log.Infof("调用 LLM 生成摘要, query=%s, intent=%s", query, intent)

	summary, err := s.llm.Chat(systemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("LLM 摘要生成失败: %w", err)
	}

	output := FormatCitation(query, summary, results)
	return output, nil
}
