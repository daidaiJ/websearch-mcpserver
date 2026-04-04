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
