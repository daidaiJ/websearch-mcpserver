package mcpserver

// 长期运行稳定性 soak：反复执行摘要流式（含 progress 通知）、续读分页、
// 遥测写入，验证无 goroutine 泄漏、无持续堆增长。默认测试集跑完整轮次，
// -short 跳过。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"websearch/pkg/llm"
	"websearch/pkg/search"
	"websearch/pkg/telemetry"
)

func TestLongRunStabilitySoak(t *testing.T) {
	if testing.Short() {
		t.Skip("soak test, skipped in -short")
	}
	ctx := context.Background()

	// mock LLM：每次请求 128 个 token
	var counter int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter++
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		token, _ := json.Marshal(fmt.Sprintf("soak token %d ", counter))
		for i := 0; i < 128; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\n", token)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	oldSum := summarizerInst
	oldInterval := summaryProgressInterval
	summarizerInst = llm.NewSummarizer(srv.URL, "soak-key", "soak-model")
	summaryProgressInterval = time.Millisecond // 强制高频通知路径
	defer func() {
		summarizerInst = oldSum
		summaryProgressInterval = oldInterval
	}()

	// 遥测独立库，避免污染测试环境默认库
	ts, err := telemetry.Open(filepath.Join(t.TempDir(), "soak-telemetry.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()

	// 续读用落盘文件
	dir := t.TempDir()
	content := strings.Repeat(" soak soak soak", 1<<15) // ~1MB
	resultPath := filepath.Join(dir, "soak.md")
	if err := os.WriteFile(resultPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	ref := "result:soak"
	resultFiles.Lock()
	resultFiles.files[ref] = resultFile{path: resultPath, info: info, expires: time.Now().Add(time.Hour)}
	resultFiles.Unlock()
	defer func() {
		resultFiles.Lock()
		delete(resultFiles.files, ref)
		resultFiles.Unlock()
	}()

	results := []search.SearchResult{{Title: "Soak", Url: "https://example.com", Content: "soak body"}}

	oneIteration := func() error {
		req := &mcp.CallToolRequest{
			Session: nil, // 无 session 时 notifyProgress 短路；progress 路径由 bench 覆盖
			Params:  &mcp.CallToolParamsRaw{},
		}
		if _, err := streamSummarize(ctx, req, "soak query", "summarize", results); err != nil {
			return err
		}
		for offset := int64(0); ; {
			_, out, err := readSavedResult(ctx, nil, &ReadResultParams{Ref: ref, Offset: offset, Limit: 8192})
			if err != nil {
				return err
			}
			offset = out.NextOffset
			if out.EOF {
				break
			}
		}
		_ = ts.Record(telemetry.Event{Kind: "tool", Tool: "smartsearch", Query: "soak query", Success: true, Duration: 10 * time.Millisecond, ResultCount: 3})
		return nil
	}

	// 预热 50 轮后取基线
	for i := 0; i < 50; i++ {
		if err := oneIteration(); err != nil {
			t.Fatalf("warmup iteration %d: %v", i, err)
		}
	}
	runtime.GC()
	runtime.GC()
	baseGoroutines := runtime.NumGoroutine()
	var baseMem runtime.MemStats
	runtime.ReadMemStats(&baseMem)

	const rounds = 300
	for i := 0; i < rounds; i++ {
		if err := oneIteration(); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}

	runtime.GC()
	runtime.GC()
	gotGoroutines := runtime.NumGoroutine()
	var gotMem runtime.MemStats
	runtime.ReadMemStats(&gotMem)

	if leaked := gotGoroutines - baseGoroutines; leaked > 2 {
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
		t.Errorf("goroutine leak: baseline %d, now %d (+%d)", baseGoroutines, gotGoroutines, leaked)
	}
	heapGrowth := float64(gotMem.HeapAlloc) / float64(baseMem.HeapAlloc)
	if gotMem.HeapAlloc > baseMem.HeapAlloc+32<<20 {
		t.Errorf("heap growth: baseline %.1fMB, now %.1fMB (%.1fx)",
			float64(baseMem.HeapAlloc)/(1<<20), float64(gotMem.HeapAlloc)/(1<<20), heapGrowth)
	}
	t.Logf("goroutines %d→%d, heap %.1fMB→%.1fMB (%.2fx)",
		baseGoroutines, gotGoroutines, float64(baseMem.HeapAlloc)/(1<<20), float64(gotMem.HeapAlloc)/(1<<20), heapGrowth)
}
