package mcphub

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"websearch/pkg/i18n"
	"websearch/pkg/search/core"
)

type parallelSearchResponse struct {
	Results []parallelResult `json:"results"`
}

type parallelResult struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	PublishDate *string  `json:"publish_date"`
	Excerpts    []string `json:"excerpts"`
	FullContent string   `json:"full_content"`
}

type firecrawlSearchResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Web []firecrawlResult `json:"web"`
	} `json:"data"`
}

type firecrawlResult struct {
	URL         string         `json:"url"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Metadata    map[string]any `json:"metadata"`
}

func parseSearch(name string, res toolResult, max int) ([]core.SearchResult, error) {
	switch name {
	case "parallel":
		return parseParallelSearch(res, max)
	case "firecrawl":
		return parseFirecrawlSearch(res, max)
	case "exa":
		return parseExaSearch(res.Text, max)
	case "anysearch":
		return parseAnysearchSearch(res.Text, max)
	default:
		return nil, errors.New(i18n.T("provider.mcphub.unknown_preset", "Name", name))
	}
}

func parseParallelSearch(res toolResult, max int) ([]core.SearchResult, error) {
	var out parallelSearchResponse
	if res.Structured != nil {
		if err := remarshal(res.Structured, &out); err != nil {
			return nil, err
		}
	} else if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("provider.mcphub.parallel_unparsable"), err)
	}
	return parallelResults(out.Results, max), nil
}

func parallelResults(items []parallelResult, max int) []core.SearchResult {
	out := make([]core.SearchResult, 0, len(items))
	for _, r := range items {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		content := strings.TrimSpace(r.FullContent)
		if content == "" {
			content = strings.Join(r.Excerpts, "\n\n")
		}
		item := core.SearchResult{
			Title:   strings.TrimSpace(r.Title),
			Url:     strings.TrimSpace(r.URL),
			Content: content,
			Engine:  "parallel_mcp",
		}
		if r.PublishDate != nil && strings.TrimSpace(*r.PublishDate) != "" {
			item.PublishDate = strings.TrimSpace(*r.PublishDate)
			item.DateSource = core.DateSourceStructured
		}
		out = append(out, item)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func parseFirecrawlSearch(res toolResult, max int) ([]core.SearchResult, error) {
	var out firecrawlSearchResponse
	if res.Structured != nil {
		if err := remarshal(res.Structured, &out); err != nil {
			return nil, err
		}
	} else if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("provider.mcphub.firecrawl_unparsable"), err)
	}
	results := make([]core.SearchResult, 0, len(out.Data.Web))
	for _, r := range out.Data.Web {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		title := firstNonEmpty(r.Title, stringField(r.Metadata, "title"))
		date := firstNonEmpty(stringField(r.Metadata, "publishedDate"), stringField(r.Metadata, "publishedTime"))
		item := core.SearchResult{
			Title:   strings.TrimSpace(title),
			Url:     strings.TrimSpace(r.URL),
			Content: strings.TrimSpace(r.Description),
			Engine:  "firecrawl_mcp",
		}
		if date != "" {
			item.PublishDate = date
			item.DateSource = core.DateSourceStructured
		}
		results = append(results, item)
		if max > 0 && len(results) >= max {
			break
		}
	}
	return results, nil
}

// parseExaSearch 解析 Exa MCP 的纯文本输出：
//
//	Title: ...
//	URL: ...
//	Published: ...
//	Author: ...
//	Highlights:
//	<正文>
func parseExaSearch(text string, max int) ([]core.SearchResult, error) {
	var out []core.SearchResult
	var cur *core.SearchResult
	var content []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Content = strings.TrimSpace(strings.Join(content, "\n"))
		if cur.Url != "" {
			out = append(out, *cur)
		}
		cur = nil
		content = nil
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "Title: "):
			flush()
			cur = &core.SearchResult{Title: strings.TrimSpace(strings.TrimPrefix(line, "Title: ")), Engine: "exa_mcp"}
		case cur != nil && strings.HasPrefix(line, "URL: "):
			cur.Url = strings.TrimSpace(strings.TrimPrefix(line, "URL: "))
		case cur != nil && strings.HasPrefix(line, "Published: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "Published: "))
			if v != "" && !strings.EqualFold(v, "N/A") {
				cur.PublishDate = v
				cur.DateSource = core.DateSourceStructured
			}
		case cur != nil && strings.HasPrefix(line, "Author: "):
			// Exa 的 Author 字段当前不映射到 SearchResult。
		case cur != nil && strings.HasPrefix(line, "Highlights:"):
			// 后续行均为正文。
		case cur != nil:
			content = append(content, line)
		}
	}
	flush()
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	if len(out) == 0 {
		return nil, errors.New(i18n.T("provider.mcphub.exa_unrecognized"))
	}
	return out, nil
}

// parseAnysearchSearch 解析 AnySearch MCP 的 Markdown 输出：
//
//	### 1. Title
//	- **URL**: https://...
//	- 摘要/正文
func parseAnysearchSearch(text string, max int) ([]core.SearchResult, error) {
	var out []core.SearchResult
	var cur *core.SearchResult
	var content []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Content = strings.TrimSpace(strings.Join(content, "\n"))
		if cur.Url != "" {
			out = append(out, *cur)
		}
		cur = nil
		content = nil
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			if dot := strings.Index(title, ". "); dot > 0 && dot <= 3 {
				if _, err := strconv.Atoi(title[:dot]); err == nil {
					title = title[dot+2:]
				}
			}
			cur = &core.SearchResult{Title: title, Engine: "anysearch_mcp"}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "- **URL**:"):
			cur.Url = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **URL**:"))
		case strings.HasPrefix(trimmed, "- **Published**:"):
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Published**:"))
			if v != "" {
				cur.PublishDate = v
				cur.DateSource = core.DateSourceStructured
			}
		case strings.HasPrefix(trimmed, "- **Date**:"):
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Date**:"))
			if v != "" {
				cur.PublishDate = v
				cur.DateSource = core.DateSourceStructured
			}
		case trimmed != "":
			content = append(content, strings.TrimPrefix(trimmed, "- "))
		}
	}
	flush()
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	if len(out) == 0 {
		return nil, errors.New(i18n.T("provider.mcphub.anysearch_unrecognized"))
	}
	return out, nil
}

func parseFetch(name string, res toolResult) (string, error) {
	switch name {
	case "parallel":
		return parseParallelFetch(res)
	case "firecrawl":
		return parseFirecrawlFetch(res)
	case "exa":
		return parseExaFetch(res.Text)
	case "anysearch":
		return parseAnysearchFetch(res)
	default:
		return "", errors.New(i18n.T("provider.mcphub.unknown_preset", "Name", name))
	}
}

func parseParallelFetch(res toolResult) (string, error) {
	var out parallelSearchResponse
	if res.Structured != nil {
		if err := remarshal(res.Structured, &out); err != nil {
			return "", err
		}
	} else if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("provider.mcphub.parallel_fetch_unparsable"), err)
	}
	if len(out.Results) == 0 {
		return "", errors.New(i18n.T("provider.mcphub.parallel_fetch_empty"))
	}
	r := out.Results[0]
	content := strings.TrimSpace(r.FullContent)
	if content == "" {
		content = strings.TrimSpace(strings.Join(r.Excerpts, "\n\n"))
	}
	if content == "" {
		return "", errors.New(i18n.T("provider.mcphub.parallel_fetch_no_body"))
	}
	var b strings.Builder
	if r.Title != "" {
		fmt.Fprintf(&b, "# %s\n\n", r.Title)
	}
	if r.PublishDate != nil && *r.PublishDate != "" {
		fmt.Fprintf(&b, "%s\n\n", i18n.T("provider.mcphub.published_time", "Time", *r.PublishDate))
	}
	b.WriteString(content)
	return b.String(), nil
}

func parseFirecrawlFetch(res toolResult) (string, error) {
	m := res.Structured
	if m == nil {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(res.Text), &parsed); err != nil {
			return "", fmt.Errorf("%s: %w", i18n.T("provider.mcphub.firecrawl_fetch_unparsable"), err)
		}
		m = parsed
	}
	content := firstNonEmpty(stringField(m, "markdown"), stringField(m, "content"), stringField(m, "text"))
	if content == "" {
		return "", errors.New(i18n.T("provider.mcphub.firecrawl_fetch_no_body"))
	}
	title := ""
	if meta, ok := m["metadata"].(map[string]any); ok {
		title = stringField(meta, "title")
	}
	if title != "" && !strings.HasPrefix(strings.TrimSpace(content), "# ") {
		content = "# " + title + "\n\n" + content
	}
	return strings.TrimSpace(content), nil
}

func parseAnysearchFetch(res toolResult) (string, error) {
	m := res.Structured
	if m == nil {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(res.Text), &parsed); err == nil {
			m = parsed
		}
	}
	content := firstNonEmpty(stringField(m, "content"), stringField(m, "markdown"), stringField(m, "text"), res.Text)
	if strings.TrimSpace(content) == "" {
		return "", errors.New(i18n.T("provider.mcphub.anysearch_fetch_empty"))
	}
	title := stringField(m, "title")
	if title != "" && !strings.HasPrefix(strings.TrimSpace(content), "# ") {
		content = "# " + title + "\n\n" + content
	}
	return strings.TrimSpace(content), nil
}

func parseExaFetch(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New(i18n.T("provider.mcphub.exa_fetch_empty"))
	}
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(line, "URL: http") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n")), nil
}

func remarshal(in any, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
