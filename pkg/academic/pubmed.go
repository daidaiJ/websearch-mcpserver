package academic

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"websearch/pkg/antirobot"
)

// ──────────────────────────────────────────────────────────────────────────────
// PubMed 生物医学文献搜索引擎（国内友好）
// ──────────────────────────────────────────────────────────────────────────────

const pubmedBaseURL = "https://www.ncbi.nlm.nih.gov/pubmed/"
const eutilsAPI = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"

type pubmedEngine struct {
	client *http.Client
}

// NewPubMed 创建 PubMed 引擎。client 为 nil 时使用默认客户端。
func NewPubMed(_ antirobot.PubMedOpts, client *http.Client) antirobot.Engine {
	if client == nil {
		client = defaultHTTPClient
	}
	return &pubmedEngine{client: client}
}

func (e *pubmedEngine) Name() string                    { return "pubmed" }
func (e *pubmedEngine) Region() antirobot.NetworkRegion { return antirobot.RegionChina }

func (e *pubmedEngine) Search(query string, page int, timeRange antirobot.TimeRange) (*antirobot.SearchResponse, error) {
	offset := (page - 1) * 10
	if offset < 0 {
		offset = 0
	}

	// ── esearch: 获取 PMID 列表 ──
	esearchURL := fmt.Sprintf("%s/esearch.fcgi?db=pubmed&term=%s&retstart=%d&retmax=10&usehistory=y",
		eutilsAPI, url.QueryEscape(query), offset)

	esearchResp, err := e.client.Get(esearchURL)
	if err != nil {
		return nil, fmt.Errorf("pubmed esearch: %w", err)
	}
	defer esearchResp.Body.Close()

	esearchBody, err := io.ReadAll(esearchResp.Body)
	if err != nil {
		return nil, err
	}

	pmids, err := parseEsearchPMIDs(esearchBody)
	if err != nil {
		return nil, err
	}
	if len(pmids) == 0 {
		return &antirobot.SearchResponse{Engine: "pubmed", Results: []antirobot.Result{}}, nil
	}

	// ── efetch: 获取论文详情 ──
	efetchURL := fmt.Sprintf("%s/efetch.fcgi?db=pubmed&retmode=xml&id=%s",
		eutilsAPI, strings.Join(pmids, ","))

	efetchResp, err := e.client.Get(efetchURL)
	if err != nil {
		return nil, fmt.Errorf("pubmed efetch: %w", err)
	}
	defer efetchResp.Body.Close()

	efetchBody, err := io.ReadAll(efetchResp.Body)
	if err != nil {
		return nil, err
	}

	return parseEfetchResponse(efetchBody)
}

// ── XML 数据结构 ──

type pubmedESearchResult struct {
	XMLName xml.Name     `xml:"eSearchResult"`
	IDList  pubmedIDList `xml:"IdList"`
}

type pubmedIDList struct {
	IDs []string `xml:"Id"`
}

type pubmedArticleSet struct {
	XMLName  xml.Name         `xml:"PubmedArticleSet"`
	Articles []pubmedArticle  `xml:"PubmedArticle"`
}

type pubmedArticle struct {
	MedlineCitation pubmedCitation `xml:"MedlineCitation"`
}

type pubmedCitation struct {
	PMID    string            `xml:"PMID"`
	Article pubmedArticleData `xml:"Article"`
}

type pubmedArticleData struct {
	Title        string            `xml:"ArticleTitle"`
	Abstract     pubmedAbstract    `xml:"Abstract"`
	Journal      pubmedJournal     `xml:"Journal"`
	AuthorList   pubmedAuthorList  `xml:"AuthorList"`
	ELocationIDs []pubmedELocation `xml:"ELocationID"`
}

type pubmedAbstract struct {
	Texts []pubmedAbstractText `xml:"AbstractText"`
}

type pubmedAbstractText struct {
	InnerXML string `xml:",innerxml"`
}

type pubmedJournal struct {
	Title string `xml:"Title"`
	ISSN  string `xml:"ISSN"`
}

type pubmedAuthorList struct {
	Authors []pubmedAuthor `xml:"Author"`
}

type pubmedAuthor struct {
	ForeName string `xml:"ForeName"`
	LastName string `xml:"LastName"`
}

type pubmedELocation struct {
	EIdType string `xml:"EIdType,attr"`
	Value   string `xml:",chardata"`
}

// ── 解析函数 ──

func parseEsearchPMIDs(data []byte) ([]string, error) {
	var result pubmedESearchResult
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("pubmed esearch parse: %w", err)
	}
	return result.IDList.IDs, nil
}

func parseEfetchResponse(data []byte) (*antirobot.SearchResponse, error) {
	var articleSet pubmedArticleSet
	if err := xml.Unmarshal(data, &articleSet); err != nil {
		return nil, fmt.Errorf("pubmed efetch parse: %w", err)
	}

	results := make([]antirobot.Result, 0, len(articleSet.Articles))
	for _, article := range articleSet.Articles {
		cit := article.MedlineCitation
		pmid := cit.PMID
		title := antirobot.CollapseSpace(strings.TrimSpace(cit.Article.Title))
		if title == "" || pmid == "" {
			continue
		}

		resultURL := pubmedBaseURL + pmid

		// 摘要
		var abstractParts []string
		for _, at := range cit.Article.Abstract.Texts {
			txt := antirobot.StripXMLTags(antirobot.CollapseSpace(strings.TrimSpace(at.InnerXML)))
			if txt != "" {
				abstractParts = append(abstractParts, txt)
			}
		}
		abstract := strings.Join(abstractParts, " ")

		// DOI
		doi := ""
		for _, eloc := range cit.Article.ELocationIDs {
			if eloc.EIdType == "doi" {
				doi = strings.TrimSpace(eloc.Value)
				break
			}
		}

		// 期刊
		journal := antirobot.CollapseSpace(strings.TrimSpace(cit.Article.Journal.Title))

		// 作者
		authors := make([]string, 0, len(cit.Article.AuthorList.Authors))
		for _, a := range cit.Article.AuthorList.Authors {
			name := strings.TrimSpace(a.ForeName + " " + a.LastName)
			if name != "" {
				authors = append(authors, name)
			}
		}

		results = append(results, antirobot.Result{
			Type:    antirobot.ResultPaper,
			Title:   title,
			URL:     resultURL,
			Content: abstract,
			Authors: strings.Join(authors, ", "),
			DOI:     doi,
			Journal: journal,
			Engine:  "pubmed",
		})
	}

	return &antirobot.SearchResponse{Engine: "pubmed", Results: results}, nil
}
