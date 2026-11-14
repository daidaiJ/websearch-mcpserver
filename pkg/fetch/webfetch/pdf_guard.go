package webfetch

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"websearch/pkg/errcode"
	"websearch/pkg/i18n"

	webfetch "github.com/daidaiJ/go-webfetch"
)

// Local PDF text extraction is unbounded in the library: a malformed content
// stream can panic, hang, or grow the heap until the process is killed.
// The guard runs the parser with a deadline and a heap-growth budget, recovers
// panics, and lets the caller escalate to MinerU.
const (
	pdfParseHeapBudget = 64 << 20
	pdfParseTimeout    = 30 * time.Second
	pdfParseHeapTick   = 20 * time.Millisecond
)

// errPDFGuard marks a local-parser abort that is safe to retry with MinerU.
var errPDFGuard = errors.New("pdf parse guard")

func isPDFGuardFailure(err error) bool {
	return errors.Is(err, errPDFGuard)
}

func pdfGuardError(msg string) error {
	return errcode.Wrap(errcode.Parse, fmt.Errorf("%s: %w", msg, errPDFGuard))
}

func pdfGuardPanic(rec any) error {
	return errcode.Wrap(errcode.Parse, fmt.Errorf("%s: %v: %w", i18n.T("fetch.webfetch.pdf_parse_panic"), rec, errPDFGuard))
}

func (f *Fetcher) parsePDFFileGuarded(ctx context.Context, filePath string, opts []webfetch.PDFOption) (*Result, error) {
	if ctx.Err() != nil {
		return f.parsePDFFileRecovered(ctx, filePath, opts)
	}

	parseCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		parseCtx, cancel = context.WithTimeout(ctx, pdfParseTimeout)
		defer cancel()
	}

	type outcome struct {
		res *Result
		err error
	}
	out := make(chan outcome, 1)
	go func() {
		res, err := f.parsePDFFileRecovered(parseCtx, filePath, opts)
		out <- outcome{res: res, err: err}
	}()

	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	ticker := time.NewTicker(pdfParseHeapTick)
	defer ticker.Stop()
	for {
		select {
		case o := <-out:
			return classifyPDFOutcome(o.res, o.err)
		case <-parseCtx.Done():
			select {
			case o := <-out:
				return classifyPDFOutcome(o.res, o.err)
			default:
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ctx.Err()
			}
			return nil, pdfGuardError(i18n.T("fetch.webfetch.pdf_parse_timeout"))
		case <-ticker.C:
			var current runtime.MemStats
			runtime.ReadMemStats(&current)
			if current.HeapAlloc > initial.HeapAlloc+pdfParseHeapBudget {
				return nil, pdfGuardError(i18n.T("fetch.webfetch.pdf_parse_heap"))
			}
		}
	}
}

func classifyPDFOutcome(res *Result, err error) (*Result, error) {
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, pdfGuardError(i18n.T("fetch.webfetch.pdf_parse_timeout"))
	}
	return res, err
}

func (f *Fetcher) parsePDFFileRecovered(ctx context.Context, filePath string, opts []webfetch.PDFOption) (res *Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			res, err = nil, pdfGuardPanic(rec)
		}
	}()
	return f.parsePDFFileDirect(ctx, filePath, opts)
}
