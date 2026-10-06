package core

import (
	"fmt"
	"strings"
	"time"
)

func FormatMD(id int, title, url, context string) string {
	return fmt.Sprintf("## 结果 %d \n**标题**: %s  \n**url**: %s  \n**内容**: %s  \n", id, title, url, context)
}

// FormatDateSource 渲染带来源注记的日期文本（无日期返回空串）：
// structured 直接标注来源，snippet 额外标注"弱"——提示 agent 采信前读原页。
func FormatDateSource(date, source string) string {
	state := DateSourceState(SearchResult{PublishDate: date, DateSource: source})
	if state == DateSourceUndated {
		return ""
	}
	if state == DateSourceSnippet {
		return fmt.Sprintf("%s（snippet，弱）", date)
	}
	return fmt.Sprintf("%s（structured）", date)
}

// FormatMDScore 格式化带引擎来源、相关性分数与日期注记的搜索结果。
// dateStr 由 FormatDateSource 预渲染，空串表示无日期不显示该行。
func FormatMDScore(id int, title, url, engine, scoreStr, dateStr, context string) string {
	s := fmt.Sprintf("## 结果 %d \n**标题**: %s  \n**url**: %s  \n", id, title, url)
	if engine != "" {
		s += fmt.Sprintf("**来源**: %s  \n", engine)
	}
	if scoreStr != "" {
		s += fmt.Sprintf("**相关性**: %s  \n", scoreStr)
	}
	if dateStr != "" {
		s += fmt.Sprintf("**日期**: %s  \n", dateStr)
	}
	s += fmt.Sprintf("**内容**: %s  \n", context)
	return s
}

func FormatPaperMD(id int, title, url, authors, doi, journal, pubDate, pdfURL, citedBy, content string) string {
	s := fmt.Sprintf("## 结果 %d \n**标题**: %s  \n**url**: %s  \n", id, title, url)
	if authors != "" {
		s += fmt.Sprintf("**作者**: %s  \n", authors)
	}
	if journal != "" {
		s += fmt.Sprintf("**期刊**: %s  \n", journal)
	}
	if pubDate != "" {
		s += fmt.Sprintf("**发表日期**: %s  \n", pubDate)
	}
	if doi != "" {
		s += fmt.Sprintf("**DOI**: %s  \n", doi)
	}
	if citedBy != "" {
		s += fmt.Sprintf("**引用次数**: %s  \n", citedBy)
	}
	if pdfURL != "" {
		s += fmt.Sprintf("**PDF**: %s  \n", pdfURL)
	}
	s += fmt.Sprintf("**内容**: %s  \n", content)
	return s
}

func MDSearchHeader(query string, count int) string {
	return fmt.Sprintf("#搜索结果  \n查询: %s  \n 结果数: %d  \n", query, count)
}

// ProvenanceHeader 响应溯源头（P1-4 结果来源注记）：retrieved_at 为结果检索时间
// （缓存命中时为原检索时间），cache_age_seconds 仅缓存命中（cacheAge > 0）时出现；
// usage_note 固定行提醒 snippet 日期的可信边界。retrievedAt 为零值时返回空串。
func ProvenanceHeader(retrievedAt time.Time, cacheAge time.Duration) string {
	if retrievedAt.IsZero() {
		return ""
	}
	var sb strings.Builder
	if cacheAge > 0 {
		fmt.Fprintf(&sb, "> 🕒 retrieved_at: %s ｜ cache_age_seconds: %d\n", retrievedAt.Format(time.RFC3339), int64(cacheAge.Seconds()))
	} else {
		fmt.Fprintf(&sb, "> 🕒 retrieved_at: %s\n", retrievedAt.Format(time.RFC3339))
	}
	sb.WriteString("> 📌 usage_note: snippet 日期仅用于定位来源，日期、金额、版本等细节必须读原页核实。\n")
	return sb.String()
}
