// Package tuning replays fixed per-engine result lists through the scoring
// pipeline so RRF / Boost / MMR parameters can be compared offline.
//
// The default corpus comes from the public benchmark published by
// fangchenghao/dsh-web-search (MIT): 20 bilingual queries with real captured
// engine rankings. See testdata/dsh-web-search/NOTICE.md for provenance and
// license.
package tuning

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"websearch/pkg/search/core"
)

//go:embed testdata/dsh-web-search/queries.json testdata/dsh-web-search/fixtures/*.json
var embeddedReference embed.FS

// Dataset is a replay corpus: each case carries the raw per-engine result
// lists and the annotated expected domains.
type Dataset struct {
	Name  string `json:"name"`
	K     int    `json:"k"`
	Cases []Case `json:"cases"`
}

// Case is one query with per-engine rankings.
type Case struct {
	Name            string   `json:"name"`
	Query           string   `json:"query"`
	Language        string   `json:"language,omitempty"`
	Category        string   `json:"category,omitempty"`
	Engines         []Engine `json:"engines"`
	ExpectedDomains []string `json:"expected_domains"`
}

// Engine is one search engine's ranked result list.
type Engine struct {
	Name    string              `json:"name"`
	Weight  float64             `json:"weight"`
	Results []core.SearchResult `json:"results"`
}

type referenceQuery struct {
	ID              string   `json:"id"`
	Query           string   `json:"query"`
	Language        string   `json:"language"`
	Category        string   `json:"category"`
	ExpectedDomains []string `json:"expectedDomains"`
}

type referenceFixture struct {
	OK              bool     `json:"ok"`
	ID              string   `json:"id"`
	Query           string   `json:"query"`
	Language        string   `json:"language"`
	Category        string   `json:"category"`
	ExpectedDomains []string `json:"expectedDomains"`
	Engines         []struct {
		ID      string  `json:"id"`
		Weight  float64 `json:"weight"`
		Sources []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Snippet     string `json:"snippet"`
			PublishedAt string `json:"publishedAt"`
		} `json:"sources"`
	} `json:"engines"`
}

// ReferenceDataset loads the embedded copy of the public dsh-web-search
// benchmark (20 queries, real engine snapshots).
func ReferenceDataset() (Dataset, error) {
	return loadReferenceFS(embeddedReference, "testdata/dsh-web-search")
}

// LoadReferenceDataset reads a directory containing queries.json and a
// fixtures/ subdirectory in the reference format. Use it to replay a dataset
// captured on another machine.
func LoadReferenceDataset(dir string) (Dataset, error) {
	return loadReferenceDir(dir)
}

func loadReferenceFS(fsys embed.FS, root string) (Dataset, error) {
	raw, err := fsys.ReadFile(root + "/queries.json")
	if err != nil {
		return Dataset{}, err
	}
	var queries []referenceQuery
	if err := json.Unmarshal(raw, &queries); err != nil {
		return Dataset{}, fmt.Errorf("parse queries.json: %w", err)
	}
	cases := make([]Case, 0, len(queries))
	for _, q := range queries {
		body, err := fsys.ReadFile(root + "/fixtures/" + q.ID + ".json")
		if err != nil {
			return Dataset{}, fmt.Errorf("fixture %s: %w", q.ID, err)
		}
		tc, err := decodeReferenceFixture(body)
		if err != nil {
			return Dataset{}, fmt.Errorf("fixture %s: %w", q.ID, err)
		}
		tc.Name, tc.Query, tc.Language, tc.Category = q.ID, q.Query, q.Language, q.Category
		tc.ExpectedDomains = normalizeDomains(q.ExpectedDomains)
		cases = append(cases, tc)
	}
	return Dataset{Name: "dsh-web-search-0.24.2", K: 8, Cases: cases}, nil
}

func loadReferenceDir(dir string) (Dataset, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "queries.json"))
	if err != nil {
		return Dataset{}, err
	}
	var queries []referenceQuery
	if err := json.Unmarshal(raw, &queries); err != nil {
		return Dataset{}, fmt.Errorf("parse queries.json: %w", err)
	}
	cases := make([]Case, 0, len(queries))
	for _, q := range queries {
		body, err := os.ReadFile(filepath.Join(dir, "fixtures", q.ID+".json"))
		if err != nil {
			return Dataset{}, fmt.Errorf("fixture %s: %w", q.ID, err)
		}
		tc, err := decodeReferenceFixture(body)
		if err != nil {
			return Dataset{}, fmt.Errorf("fixture %s: %w", q.ID, err)
		}
		tc.Name, tc.Query, tc.Language, tc.Category = q.ID, q.Query, q.Language, q.Category
		tc.ExpectedDomains = normalizeDomains(q.ExpectedDomains)
		cases = append(cases, tc)
	}
	return Dataset{Name: "dsh-web-search-custom", K: 8, Cases: cases}, nil
}

func decodeReferenceFixture(body []byte) (Case, error) {
	var fx referenceFixture
	if err := json.Unmarshal(body, &fx); err != nil {
		return Case{}, err
	}
	if !fx.OK {
		return Case{}, fmt.Errorf("fixture marked ok=false")
	}
	engines := make([]Engine, 0, len(fx.Engines))
	for _, e := range fx.Engines {
		weight := e.Weight
		if weight <= 0 {
			weight = 1
		}
		results := make([]core.SearchResult, 0, len(e.Sources))
		for _, s := range e.Sources {
			if strings.TrimSpace(s.URL) == "" {
				continue
			}
			results = append(results, core.SearchResult{
				Title:       s.Title,
				Url:         s.URL,
				Content:     s.Snippet,
				PublishDate: s.PublishedAt,
			})
		}
		if len(results) == 0 {
			continue
		}
		engines = append(engines, Engine{Name: normalizeEngineName(e.ID), Weight: weight, Results: results})
	}
	if len(engines) == 0 {
		return Case{}, fmt.Errorf("no usable engines")
	}
	return Case{
		Engines:         engines,
		ExpectedDomains: normalizeDomains(fx.ExpectedDomains),
	}, nil
}

func normalizeEngineName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cn.bing":
		return "bing"
	case "ddg-html":
		return "duckduckgo"
	case "anysearch":
		return "anysearch"
	case "exa":
		return "exa"
	case "firecrawl":
		return "firecrawl"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	out := make([]string, 0, len(domains))
	for _, raw := range domains {
		d := strings.ToLower(strings.TrimSpace(raw))
		d = strings.TrimPrefix(d, "www.")
		d = strings.TrimSuffix(d, ".")
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
