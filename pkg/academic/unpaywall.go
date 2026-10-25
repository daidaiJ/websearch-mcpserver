package academic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type unpaywallResp struct {
	IsOA           bool          `json:"is_oa"`
	BestOALocation *unpaywallLoc `json:"best_oa_location"`
}

type unpaywallLoc struct {
	URLForPDF string `json:"url_for_pdf"`
}

// UnpaywallPDF 查询合法 OA PDF。email 为空时不发请求。
func UnpaywallPDF(ctx context.Context, doi, email string) (string, error) {
	email = strings.TrimSpace(email)
	doi = stringsTrimDOI(doi)
	if email == "" || doi == "" {
		return "", nil
	}
	select {
	case unpaywallGate <- struct{}{}:
		defer func() { <-unpaywallGate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	u := fmt.Sprintf("%s/%s?email=%s", unpaywallAPI, url.PathEscape(doi), url.QueryEscape(email))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := lookupClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unpaywall HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed unpaywallResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if !parsed.IsOA || parsed.BestOALocation == nil {
		return "", nil
	}
	return strings.TrimSpace(parsed.BestOALocation.URLForPDF), nil
}
