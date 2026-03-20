package md

import "fmt"

func FormatMD(id int, title, url, context string) string {
	return fmt.Sprintf("## 结果 %d \n**标题**: %s  \n**url**: %s  \n**内容**: %s  \n", id, title, url, context)
}

func MDSearchHeader(query string, count int) string {
	return fmt.Sprintf("#搜索结果  \n查询: %s  \n 结果数: %d  \n", query, count)
}
