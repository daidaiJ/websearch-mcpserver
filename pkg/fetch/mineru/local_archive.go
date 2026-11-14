package mineru

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"websearch/pkg/i18n"
)

const localPackageFormat = "websearch-mineru-v1"

// LocalResult is a persistent package; Complete describes the requested coverage.
type LocalResult struct {
	Directory, Markdown, Source string
	Pages, Remaining            []int
	TotalPages                  int
	Complete, Reused            bool
}

type localBatch struct {
	Pages    []int    `json:"pages"`
	TaskRef  string   `json:"task_ref"`
	Markdown string   `json:"markdown,omitempty"`
	Files    []string `json:"files,omitempty"`
}

type localPackage struct {
	Format     string       `json:"format"`
	Source     string       `json:"source"`
	SHA256     string       `json:"source_sha256"`
	Options    string       `json:"options"`
	TotalPages int          `json:"total_pages"`
	Requested  []int        `json:"requested_pages"`
	Batches    []localBatch `json:"batches"`
	Obsolete   []string     `json:"obsolete_files,omitempty"`
}

// SetLocalTaskDir stores only task-to-package pointers, not document content.
func (c *Client) SetLocalTaskDir(dir string) { c.localTaskDir = dir }

// ParseLocalArchive defaults to the whole PDF and submits at most budget new pages.
func (c *Client) ParseLocalArchive(ctx context.Context, source, output string, pages []int, batchSize, budget int) (*LocalResult, error) {
	c.localMu.Lock()
	defer c.localMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		if output != "" {
			return nil, errors.New(i18n.T("fetch.mineru.local_output_conflict"))
		}
		output = source
		record, err := readLocalPackage(filepath.Join(output, "manifest.json"))
		if err != nil {
			return nil, err
		}
		work, err := readLocalPackage(filepath.Join(output, "work.json"))
		if err != nil {
			return nil, err
		}
		if work != nil {
			record = work
		}
		if record == nil {
			return nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
		}
		source = record.Source
		nearby := filepath.Join(filepath.Dir(output), filepath.Base(source))
		if _, err := os.Stat(nearby); err == nil {
			source = nearby
		}
		info, err = os.Stat(source)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && work == nil && len(missingLocalPages(record.Requested, record.Batches)) == 0 {
				return localPackageResult(output, record, true), nil
			}
			return nil, err
		}
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(i18n.T("fetch.mineru.local_source_invalid"))
	}
	if output == "" {
		output = strings.TrimSuffix(source, filepath.Ext(source)) + ".mineru"
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return nil, err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	options := fmt.Sprintf("%s/%s/ocr=%t/formula=%t/table=%t", c.modelVersion, c.lang, c.ocr, c.formula, c.table)
	current, err := readLocalPackage(filepath.Join(output, "manifest.json"))
	if err != nil {
		return nil, err
	}
	work, err := readLocalPackage(filepath.Join(output, "work.json"))
	if err != nil {
		return nil, err
	}
	var record *localPackage
	for _, candidate := range []*localPackage{work, current} {
		if candidate != nil && candidate.SHA256 == sum && candidate.Options == options {
			record = candidate
			break
		}
	}
	if record == nil {
		total, err := PDFPageCount(ctx, source)
		if err != nil {
			return nil, err
		}
		record = &localPackage{Format: localPackageFormat, Source: source, SHA256: sum, Options: options, TotalPages: total}
		for _, old := range []*localPackage{current, work} {
			if old != nil {
				record.Obsolete = append(record.Obsolete, old.Obsolete...)
				for _, batch := range old.Batches {
					record.Obsolete = append(record.Obsolete, batch.Files...)
				}
			}
		}
	}
	requested, err := localPages(pages, record.TotalPages)
	if err != nil {
		return nil, err
	}
	for i := range record.Batches {
		for _, file := range record.Batches[i].Files {
			if !filepath.IsLocal(file) {
				return nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
			}
			if _, err := os.Stat(filepath.Join(output, file)); errors.Is(err, os.ErrNotExist) {
				record.Batches[i].Markdown = ""
				break
			} else if err != nil {
				return nil, err
			}
		}
	}
	if current == record && len(missingLocalPages(requested, record.Batches)) == 0 {
		if _, err := os.Stat(filepath.Join(output, "full.md")); err == nil {
			return localPackageResult(output, record, true), nil
		}
	}
	if !c.HasToken() {
		return nil, errors.New(i18n.T("fetch.mineru.token_required"))
	}
	if err := prepareLocalOutput(output, current != nil || work != nil); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("fetch.mineru.local_output_unwritable", "Path", output), err)
	}
	record.Source = source
	record.Requested = unionLocalPages(record.Requested, requested)
	if err := writeLocalJSON(filepath.Join(output, "work.json"), record); err != nil {
		return nil, err
	}
	for i := range record.Batches {
		if record.Batches[i].Markdown == "" {
			if err := c.finishLocalBatch(ctx, output, record, i); err != nil {
				result, saveErr := saveLocalProgress(output, record, true)
				return result, errors.Join(err, saveErr)
			}
		}
	}
	missing := missingLocalPages(requested, record.Batches)
	if batchSize <= 0 {
		batchSize = c.pageLimit
	}
	batchSize = min(batchSize, c.pageLimit)
	if budget <= 0 {
		budget = c.pageLimit
	}
	if len(missing) > budget {
		missing = missing[:budget]
	}
	for _, selected := range splitLocalBatches(missing, batchSize) {
		cropped, _, _, cleanup, err := CropPDF(ctx, source, selected, 0, c.pageLimit)
		if err != nil {
			return nil, err
		}
		now, statErr := os.Stat(source)
		if statErr != nil || now.Size() != info.Size() || !now.ModTime().Equal(info.ModTime()) {
			cleanup()
			return nil, errors.New(i18n.T("fetch.mineru.local_source_changed"))
		}
		index := len(record.Batches)
		record.Batches = append(record.Batches, localBatch{Pages: selected})
		_, err = c.submitStandardFile(ctx, cropped, func(ref string) error {
			record.Batches[index].TaskRef = ref
			if err := writeLocalJSON(filepath.Join(output, "work.json"), record); err != nil {
				return err
			}
			if c.localTaskDir != "" {
				return writeLocalJSON(c.localTaskIndex(ref), output)
			}
			return nil
		})
		cleanup()
		if err == nil {
			err = c.finishLocalBatch(ctx, output, record, index)
		}
		if err != nil {
			result, saveErr := saveLocalProgress(output, record, false)
			return result, errors.Join(err, saveErr)
		}
	}
	return saveLocalProgress(output, record, false)
}

// ResumeLocalArchive queries exactly the saved batch and never submits new work.
func (c *Client) ResumeLocalArchive(ctx context.Context, ref string) (*LocalResult, error) {
	c.localMu.Lock()
	defer c.localMu.Unlock()
	if c.localTaskDir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(c.localTaskIndex(ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dir string
	if err := json.Unmarshal(data, &dir); err != nil {
		return nil, err
	}
	if ctx.Value(taskWaitKey{}) == nil {
		ctx = WithTaskWait(ctx, 0)
	}
	for _, name := range []string{"work.json", "manifest.json"} {
		record, err := readLocalPackage(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if record == nil {
			continue
		}
		for i, batch := range record.Batches {
			if batch.TaskRef != ref {
				continue
			}
			if name == "manifest.json" {
				return localPackageResult(dir, record, true), nil
			}
			if batch.Markdown == "" {
				if err := c.finishLocalBatch(ctx, dir, record, i); err != nil {
					result, saveErr := saveLocalProgress(dir, record, true)
					return result, errors.Join(err, saveErr)
				}
			}
			return saveLocalProgress(dir, record, true)
		}
	}
	return nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
}

func (c *Client) localTaskIndex(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return filepath.Join(c.localTaskDir, hex.EncodeToString(sum[:])+".json")
}

func (c *Client) finishLocalBatch(ctx context.Context, dir string, record *localPackage, index int) error {
	batch := &record.Batches[index]
	if batch.TaskRef == "" {
		return errors.New(i18n.T("fetch.mineru.local_package_invalid"))
	}
	zipURL, err := c.pollTask(ctx, batch.TaskRef)
	if err != nil {
		return err
	}
	id := sha256.Sum256([]byte(batch.TaskRef))
	rel := filepath.Join("generated", hex.EncodeToString(id[:12]))
	markdown, files, err := c.saveLocalZIP(ctx, zipURL, filepath.Join(dir, rel))
	if err != nil {
		return fmt.Errorf("%s: %w (task_ref=%q)", i18n.T("fetch.mineru.result_download_failed"), err, batch.TaskRef)
	}
	batch.Markdown = filepath.Join(rel, markdown)
	batch.Files = nil
	for _, file := range files {
		batch.Files = append(batch.Files, filepath.Join(rel, file))
	}
	return writeLocalJSON(filepath.Join(dir, "work.json"), record)
}

func readLocalPackage(path string) (*localPackage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record localPackage
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.Format != localPackageFormat || record.TotalPages <= 0 {
		return nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
	}
	if _, err := localPages(record.Requested, record.TotalPages); err != nil {
		return nil, err
	}
	for _, batch := range record.Batches {
		if len(batch.Pages) == 0 {
			return nil, errors.New(i18n.T("fetch.mineru.local_package_invalid"))
		}
		if _, err := localPages(batch.Pages, record.TotalPages); err != nil {
			return nil, err
		}
	}
	return &record, nil
}

func writeLocalJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeLocalFile(path, data)
}

func writeLocalFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func prepareLocalOutput(dir string, managed bool) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !managed {
		return errors.New(i18n.T("fetch.mineru.local_package_invalid"))
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return err
	}
	closeErr := file.Close()
	return errors.Join(closeErr, os.Remove(file.Name()))
}

func localPages(pages []int, total int) ([]int, error) {
	if len(pages) == 0 {
		pages = make([]int, total)
		for i := range pages {
			pages[i] = i + 1
		}
	}
	for _, page := range pages {
		if page < 1 || page > total {
			return nil, errors.New(i18n.T("fetch.mineru.page_out_of_range", "Page", page, "Total", total))
		}
	}
	return pages, nil
}

func missingLocalPages(want []int, batches []localBatch) []int {
	have := make(map[int]bool)
	for _, batch := range batches {
		if batch.Markdown != "" {
			for _, page := range batch.Pages {
				have[page] = true
			}
		}
	}
	var missing []int
	for _, page := range want {
		if !have[page] {
			missing = append(missing, page)
		}
	}
	return missing
}

func unionLocalPages(a, b []int) []int {
	out := append(slices.Clone(a), b...)
	slices.Sort(out)
	return slices.Compact(out)
}

// Keep batches contiguous so later gap-filling preserves the document's reading order.
func splitLocalBatches(pages []int, size int) [][]int {
	var batches [][]int
	for start := 0; start < len(pages); {
		end := start + 1
		for end < len(pages) && end-start < size && pages[end] == pages[end-1]+1 {
			end++
		}
		batches = append(batches, pages[start:end])
		start = end
	}
	return batches
}
