package academic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"websearch/pkg/antirobot"
)

var (
	openalexAPI   = "https://api.openalex.org"
	unpaywallAPI  = "https://api.unpaywall.org/v2"
	lookupClient  = defaultHTTPClient
	unpaywallGate = make(chan struct{}, 4)
)

// LookupDOI 并发打 OpenAlex + Crossref 单篇端点，不走 Engine.Search()。
func LookupDOI(ctx context.Context, doi string) []antirobot.Result {
	doi = stringsTrimDOI(doi)
	if doi == "" {
		return nil
	}
	var (
		mu   sync.Mutex
		all  []antirobot.Result
		wg   sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if rs := lookupOpenAlexDOI(ctx, doi); len(rs) > 0 {
			mu.Lock()
			all = append(all, rs...)
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		if rs := lookupCrossrefDOI(ctx, doi); len(rs) > 0 {
			mu.Lock()
			all = append(all, rs...)
			mu.Unlock()
		}
	}()
	wg.Wait()
	return antirobot.DeduplicateResults(all)
}

// LookupArxivID 用 id_list 取单篇，不走关键词 Search()。
func LookupArxivID(ctx context.Context, id string) []antirobot.Result {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	u := fmt.Sprintf("%s?id_list=%s", arxivEndpoint, url.QueryEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	resp, err := lookupClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	parsed, err := parseArxivFeed(body)
	if err != nil || parsed == nil {
		return nil
	}
	return parsed.Results
}

func lookupOpenAlexDOI(ctx context.Context, doi string) []antirobot.Result {
	u := openalexAPI + "/works/" + url.PathEscape("https://doi.org/"+doi)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	resp, err := lookupClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var w openalexWork
	if err := json.Unmarshal(body, &w); err != nil {
		return nil
	}
	r, ok := openalexWorkToResult(w)
	if !ok {
		return nil
	}
	return []antirobot.Result{r}
}

func lookupCrossrefDOI(ctx context.Context, doi string) []antirobot.Result {
	u := stringsTrimRightSlash(crossrefPath) + "/" + url.PathEscape(doi)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "websearch/1.0 (mailto:search@example.com)")
	resp, err := lookupClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var wrap struct {
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil
	}
	var item crossrefItem
	if err := json.Unmarshal(wrap.Message, &item); err != nil {
		return nil
	}
	fake, err := json.Marshal(crossrefResp{Message: crossrefMessage{Items: []crossrefItem{item}}})
	if err != nil {
		return nil
	}
	parsed, err := (&crossrefEngine{}).parse(fake)
	if err != nil || parsed == nil {
		return nil
	}
	return parsed.Results
}

func stringsTrimDOI(doi string) string {
	s := strings.TrimSpace(doi)
	s = strings.TrimPrefix(s, "https://doi.org/")
	s = strings.TrimPrefix(s, "http://doi.org/")
	return strings.TrimSpace(s)
}

func stringsTrimRightSlash(s string) string {
	return strings.TrimRight(s, "/")
}
