package summarizer

import (
	"fmt"
	"strings"
	"websearch/pkg/search"
)

// systemPrompt LLM 系统提示词，指导模型生成高质量、可引用的摘要。
const systemPrompt = `你是一个专业的研究助手。用户会提供搜索查询、搜索意图以及多条搜索结果。
你的任务是对搜索结果进行筛选、去重和整合，生成一份高质量的结构化摘要。

## 内容处理原则

1. **主动过滤**：忽略以下低质量或无关内容：
   - 与查询意图明显无关的结果
   - 仅含广告、推广、引流性质的内容
   - 内容过短、信息量极低的条目
   - 来源可信度明显偏低的内容（如个人博客的非专业观点）

2. **去重整合**：当多条结果描述同一事实或观点时：
   - 合并为一条，选择信息最完整、来源最权威的版本
   - 不要重复同一信息

3. **保留原文**：对于关键事实、数据、定义、技术细节等重要内容：
   - 优先保留原文表述而非改写为摘要，以便后续引用
   - 用引用标识标注来源，格式为 [序号]
   - 原文较长时可裁剪保留核心片段，用省略号(...)标识裁剪位置

4. **摘要与原文结合**：
   - 用简短的过渡语句串联各条引用原文
   - 过渡语句用于建立逻辑关系，不需要引用标识
   - 引用的原文段落必须标注 [序号] 引用标识

## 输出格式要求

1. 直接输出内容，不要输出任何多余的寒暄或解释
2. 根据用户的意图组织结构，使用 Markdown 标题分层
3. 在内容末尾列出引用来源，格式为：
   ## 参考资料
   - [序号] 标题 - URL
4. 只引用实际使用了其内容的来源，未引用的不要列出`

// BuildUserPrompt 构建用户 prompt，将搜索结果格式化为 LLM 可消费的格式。
func BuildUserPrompt(query, intent string, results []search.SearchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## 查询\n%s\n", query)
	if intent != "" {
		fmt.Fprintf(&sb, "## 意图\n%s\n", intent)
	}
	sb.WriteString("## 搜索结果\n\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "### [%d] %s\n", i+1, r.Title)
		fmt.Fprintf(&sb, "URL: %s\n", r.Url)
		if r.Content != "" {
			fmt.Fprintf(&sb, "%s\n", r.Content)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// FormatCitation 将 LLM 返回的摘要格式化为最终的 Markdown 输出。
func FormatCitation(query string, summary string, results []search.SearchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "#搜索摘要\n查询: %s\n\n", query)
	sb.WriteString(summary)
	sb.WriteString("\n")
	return sb.String()
}
