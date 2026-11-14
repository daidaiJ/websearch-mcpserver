package mineru

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"websearch/pkg/i18n"
)

func (c *Client) saveLocalZIP(ctx context.Context, url, dir string) (string, []string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	response, err := c.client.R().SetContext(ctx).SetResponseDoNotParse(true).Get(url)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode() != http.StatusOK {
		return "", nil, errors.New(i18n.T("fetch.mineru.download_http_failed", "Status", response.StatusCode()))
	}
	file, err := os.Create(filepath.Join(dir, "result.zip"))
	if err != nil {
		return "", nil, err
	}
	const maxZIP = 200 * 1024 * 1024
	n, copyErr := io.Copy(file, io.LimitReader(response.Body, maxZIP+1))
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", nil, err
	}
	if n > maxZIP {
		return "", nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
	}
	return extractLocalZIP(filepath.Join(dir, "result.zip"), dir)
}

func extractLocalZIP(path, dir string) (string, []string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = archive.Close() }()
	if err := os.MkdirAll(filepath.Join(dir, "content"), 0700); err != nil {
		return "", nil, err
	}
	root, err := os.OpenRoot(filepath.Join(dir, "content"))
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = root.Close() }()
	var markdown string
	files := []string{"result.zip"}
	var expanded uint64
	for _, entry := range archive.File {
		name := filepath.FromSlash(strings.ReplaceAll(entry.Name, `\`, "/"))
		if !filepath.IsLocal(name) || entry.Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 > 1024*1024*1024-expanded {
			return "", nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
		}
		expanded += entry.UncompressedSize64
		if entry.FileInfo().IsDir() {
			if err := root.MkdirAll(name, 0700); err != nil {
				return "", nil, err
			}
			continue
		}
		if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return "", nil, err
		}
		input, err := entry.Open()
		if err != nil {
			return "", nil, err
		}
		output, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			_ = input.Close()
			return "", nil, err
		}
		_, copyErr := io.Copy(output, input)
		inErr, outErr := input.Close(), output.Close()
		if err := errors.Join(copyErr, inErr, outErr); err != nil {
			return "", nil, err
		}
		files = append(files, filepath.Join("content", name))
		if filepath.Base(name) == "full.md" && entry.UncompressedSize64 > 0 {
			if markdown != "" {
				return "", nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
			}
			markdown = filepath.Join("content", name)
		}
	}
	if markdown == "" {
		return "", nil, errors.New(i18n.T("fetch.mineru.zip_full_md_missing"))
	}
	return markdown, files, nil
}

// Relative links must remain valid after Markdown from separate batches is merged.
var localAssetLink = regexp.MustCompile(`(\]\(|(?:src|href)=["'])([^"'()\s<>]+)`)

func rewriteLocalLinks(markdown, prefix string) string {
	return localAssetLink.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := localAssetLink.FindStringSubmatch(match)
		link, err := url.Parse(parts[2])
		if err != nil || link.IsAbs() || strings.HasPrefix(parts[2], "/") || strings.HasPrefix(parts[2], "#") {
			return match
		}
		return parts[1] + filepath.ToSlash(filepath.Join(prefix, filepath.FromSlash(parts[2])))
	})
}

func saveLocalProgress(dir string, record *localPackage, reused bool) (*LocalResult, error) {
	result := localPackageResult(dir, record, reused)
	if len(result.Pages) == 0 {
		return nil, nil
	}
	var merged strings.Builder
	batches := slices.Clone(record.Batches)
	slices.SortFunc(batches, func(a, b localBatch) int { return a.Pages[0] - b.Pages[0] })
	for _, batch := range batches {
		if batch.Markdown == "" {
			continue
		}
		if !filepath.IsLocal(batch.Markdown) {
			return nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
		}
		data, err := os.ReadFile(filepath.Join(dir, batch.Markdown))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&merged, "%s\n\n%s\n\n", i18n.T("fetch.mineru.local_section", "Pages", LocalPageRanges(batch.Pages)),
			rewriteLocalLinks(string(data), filepath.Dir(batch.Markdown)))
	}
	if err := writeLocalFile(result.Markdown, []byte(merged.String())); err != nil {
		return nil, err
	}
	if result.Complete {
		if err := writeLocalJSON(filepath.Join(dir, "manifest.json"), record); err != nil {
			return nil, err
		}
		if err := os.Remove(filepath.Join(dir, "work.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		keep := make(map[string]bool)
		for _, batch := range record.Batches {
			for _, file := range batch.Files {
				keep[file] = true
			}
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		for _, file := range record.Obsolete {
			if !keep[file] && filepath.IsLocal(file) && strings.HasPrefix(filepath.ToSlash(file), "generated/") {
				if err := root.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
			}
		}
	}
	return result, nil
}

func localPackageResult(dir string, record *localPackage, reused bool) *LocalResult {
	result := &LocalResult{Directory: dir, Source: record.Source, TotalPages: record.TotalPages, Reused: reused}
	for _, batch := range record.Batches {
		if batch.Markdown != "" {
			result.Pages = unionLocalPages(result.Pages, batch.Pages)
		}
	}
	result.Remaining = missingLocalPages(record.Requested, record.Batches)
	result.Complete = len(result.Remaining) == 0
	name := "partial.md"
	if result.Complete {
		name = "full.md"
	}
	result.Markdown = filepath.Join(dir, name)
	return result
}

// LocalPageRanges formats original PDF page numbers, including disjoint selections.
func LocalPageRanges(pages []int) string {
	if len(pages) == 0 {
		return ""
	}
	ranges, _ := standardPageRanges(pages, 0, max(len(pages), pages[len(pages)-1]))
	return ranges
}
