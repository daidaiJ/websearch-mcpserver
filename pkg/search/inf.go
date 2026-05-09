package search

var DefaultSearchInf SearchInf

type SearchResult struct {
	Title       string `json:"title"`
	Url         string `json:"url"`
	Content     string `json:"content"`
	PublishDate string `json:"publishedDate"`
}

type SearchInf interface {
	Search(query string) (string, error)
	SearchRaw(query string) ([]SearchResult, error)
	MergeContent(query string, results []SearchResult) (string, error)
}

// AcademicSearcher 支持学术搜索的引擎可实现此接口。
// 实现方应返回学术论文类结果（arXiv、Crossref、OpenAlex、Semantic Scholar 等）。
type AcademicSearcher interface {
	SearchAcademicRaw(query string) ([]SearchResult, error)
}
