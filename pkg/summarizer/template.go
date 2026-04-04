package summarizer

import (
	"fmt"
	"strings"
	"websearch/pkg/search"
)

// PromptTemplate 构建 LLM system prompt，指导模型生成带引用的摘要
const systemPrompt = `你是一个专业的研究助手。用户会提供搜索查询、搜索意图以及多条搜索结果。
请根据用户的查询和意图，综合所有搜索结果，生成一份结构清晰的摘要。
要求：
1. 直接输出摘要内容，不要输出任何多余的寒暄或解释
2. 摘要需要根据用户的意图来总结相关事实内容
3. 在摘要中引用来源时，使用 [序号] 格式标注引用
4. 在摘要末尾，列出所有引用的来源链接，格式为：
   ## 参考资料
   - [序号] 标题 - URL
5. 只引用实际使用了其内容的来源`

// BuildUserPrompt 使用 Go text/template 风格格式化用户 prompt
func BuildUserPrompt(query, intent string, results []search.SearchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## 查询\n%s\n", query)
	if intent != "" {
		fmt.Fprintf(&sb, "## 意图\n%s\n", intent)
	}
	sb.WriteString("## 搜索结果\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "### [%d] %s\nURL: %s\n%s\n\n", i+1, r.Title, r.Url, r.Content)
	}
	return sb.String()
}

// FormatCitation 将 LLM 返回的摘要与引用格式化为最终的 Markdown 输出
func FormatCitation(query string, summary string, results []search.SearchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "#搜索摘要\n查询: %s\n\n", query)
	sb.WriteString(summary)
	sb.WriteString("\n\n")
	return sb.String()
}
