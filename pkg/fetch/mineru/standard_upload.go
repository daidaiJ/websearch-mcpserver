package mineru

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"websearch/pkg/i18n"
)

// ParseStandardFile sends a locally cropped PDF through MinerU's signed upload API.
func (c *Client) ParseStandardFile(ctx context.Context, filePath string) (string, error) {
	if c.token == "" {
		return "", errors.New(i18n.T("fetch.mineru.token_required"))
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	if info.Size() > 200*1024*1024 {
		return "", errors.New(i18n.T("fetch.mineru.cropped_too_large"))
	}
	batchID, uploadURL, err := c.createStandardUpload(ctx, filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if err := c.uploadFile(ctx, uploadURL, filePath); err != nil {
		return "", fmt.Errorf("%s: %w (task_ref=%q)", i18n.T("fetch.mineru.upload_failed"), err, "batch:"+batchID)
	}
	zipURL, err := c.pollStandardUpload(ctx, batchID)
	if err != nil {
		return "", err
	}
	md, err := c.downloadZIP(ctx, zipURL)
	if err != nil {
		return "", fmt.Errorf("%s: %w (task_ref=%q)", i18n.T("fetch.mineru.result_download_failed"), err, "batch:"+batchID)
	}
	return i18n.T("fetch.mineru.zip_preamble", "URL", zipURL) + "\n\n" + md, nil
}

type standardUploadResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		BatchID  string   `json:"batch_id"`
		FileURLs []string `json:"file_urls"`
	} `json:"data"`
}

func (c *Client) createStandardUpload(ctx context.Context, name string) (string, string, error) {
	body := map[string]any{
		"files":          []map[string]any{{"name": name, "is_ocr": c.ocr}},
		"model_version":  c.modelVersion,
		"enable_formula": c.formula,
		"enable_table":   c.table,
		"language":       c.lang,
	}
	var result standardUploadResponse
	resp, err := c.client.R().SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", "Bearer "+c.token).
		SetBody(body).SetResult(&result).
		Post(c.endpoint + standardAPIPath + "/file-urls/batch")
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", i18n.T("fetch.mineru.service_connect_failed"), err)
	}
	if resp.StatusCode() != http.StatusOK {
		return "", "", errors.New(i18n.T("fetch.mineru.service_error", "Status", resp.StatusCode()))
	}
	if result.Code != 0 {
		return "", "", fmt.Errorf("%s: %s", i18n.T("fetch.mineru.upload_task_failed"), mapAPIError(result.Code, result.Msg))
	}
	if result.Data.BatchID == "" || len(result.Data.FileURLs) != 1 || result.Data.FileURLs[0] == "" {
		return "", "", errors.New(i18n.T("fetch.mineru.no_upload_url"))
	}
	return result.Data.BatchID, result.Data.FileURLs[0], nil
}

func (c *Client) pollStandardUpload(ctx context.Context, batchID string) (string, error) {
	return c.pollTask(ctx, "batch:"+batchID)
}
