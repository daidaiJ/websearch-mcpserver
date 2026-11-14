package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	runTimeout  = 5 * time.Second
	initTimeout = 180 * time.Second
)

func init() {
	if v := os.Getenv("PDFCORPUS_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			runTimeout = time.Duration(ms) * time.Millisecond
		}
	}
}

type engine struct {
	Name string
	Kind string // go, python, node, cli
	Cmd  []string
}

type row struct {
	Engine, Case, Status string
	Ms                   int64
	Retained, Form       bool
	Chars                int
	Err                  string
}

func main() {
	if w := os.Getenv("PDFCORPUS_WORKER"); w != "" {
		runWorker(w)
		return
	}
	if w := os.Getenv("PDFCORPUS_BENCH"); w != "" {
		runBenchWorker(w, os.Getenv("PDFCORPUS_BENCH_DIR"))
		return
	}
	if s := os.Getenv("PDFCORPUS_STRESS"); s != "" {
		runStress(s, os.Getenv("PDFCORPUS_FILE"))
		return
	}

	root, err := os.MkdirTemp("", "pdfcorpus-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixDir := filepath.Join(root, "fixtures")
	if err := os.MkdirAll(fixDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	here, _ := os.Getwd()
	py := filepath.Join(here, "extract.py")
	jsRoot := os.Getenv("PDFCORPUS_JSROOT")
	pdfcpuBin := os.Getenv("PDFCORPUS_PDFCPU")
	popplerBin := os.Getenv("PDFCORPUS_POPPLER")

	lithoBin := os.Getenv("PDFCORPUS_LITHO")

	engines := []engine{
		{Name: "ledongthuc", Kind: "go"},
		{Name: "pdfcpu", Kind: "go"},
		{Name: "aspose", Kind: "go"},
		{Name: "razvan", Kind: "go"},
		{Name: "gofastpdf", Kind: "go"},
		{Name: "pdfium-wasm", Kind: "go"},
		{Name: "pymupdf", Kind: "python", Cmd: []string{"python", py, "pymupdf"}},
		{Name: "pypdfium2", Kind: "python", Cmd: []string{"python", py, "pypdfium2"}},
		{Name: "pypdf", Kind: "python", Cmd: []string{"python", py, "pypdf"}},
		{Name: "pdfminer", Kind: "python", Cmd: []string{"python", py, "pdfminer"}},
		{Name: "xpdf", Kind: "cli", Cmd: []string{"pdftotext", "-enc", "UTF-8"}},
	}
	if cgoEnabled {
		engines = append(engines, engine{Name: "go-fitz", Kind: "go"})
	}
	if _, err := os.Stat(pdfiumLibraryPath()); err == nil {
		engines = append(engines, engine{Name: "pdfium-dll", Kind: "go"})
	}
	if jsRoot != "" {
		engines = append(engines, engine{Name: "pdfjs", Kind: "node", Cmd: []string{"node", filepath.Join(jsRoot, "extract.mjs")}})
	}
	if pdfcpuBin != "" {
		engines = append(engines, engine{Name: "pdfcpu-cli", Kind: "pdfcpu-cli", Cmd: []string{pdfcpuBin}})
	}
	if popplerBin != "" {
		engines = append(engines, engine{Name: "poppler", Kind: "cli", Cmd: []string{popplerBin, "-enc", "UTF-8"}})
	}
	if lithoBin != "" {
		engines = append(engines, engine{Name: "litho", Kind: "args", Cmd: []string{lithoBin, "text"}})
	}
	filter := os.Getenv("PDFCORPUS_ENGINES")
	if filter == "" {
		filter = os.Getenv("PDFCORPUS_ONLY")
	}
	if filter != "" {
		engines = filterEngines(engines, filter)
	}

	var rows []row
	for _, fx := range fixtures() {
		path := filepath.Join(fixDir, fx.Name+".pdf")
		if err := os.WriteFile(path, fx.Data, 0600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, eng := range engines {
			rows = append(rows, runOne(self, eng, fx, path, jsRoot))
		}
	}

	printTable(rows)
	printErrors(rows)
	if err := writeBenchFixtures(fixDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runBenchInProcess(self, engines, fixDir)
	runBenchSpawn(self, engines, fixDir, jsRoot)
}

func runOne(self string, eng engine, fx fixture, path, jsRoot string) row {
	if eng.Kind == "go" {
		return runGoWorker(self, eng.Name, fx, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch eng.Kind {
	case "python":
		args := append(append([]string{}, eng.Cmd[1:]...), path)
		cmd = exec.CommandContext(ctx, eng.Cmd[0], args...)
	case "node":
		cmd = exec.CommandContext(ctx, eng.Cmd[0], append(append([]string{}, eng.Cmd[1:]...), path)...)
		if jsRoot != "" {
			cmd.Dir = jsRoot
		}
	case "cli":
		cmd = exec.CommandContext(ctx, eng.Cmd[0], append(eng.Cmd[1:], path, "-")...)
	case "args":
		cmd = exec.CommandContext(ctx, eng.Cmd[0], append(append([]string{}, eng.Cmd[1:]...), path)...)
	case "pdfcpu-cli":
		outDir := filepath.Join(filepath.Dir(path), "pdfcpu-"+fx.Name+"-"+eng.Name)
		_ = os.MkdirAll(outDir, 0700)
		cmd = exec.CommandContext(ctx, eng.Cmd[0], "extract", "-m", "content", path, outDir)
		cmd.Dir = filepath.Dir(eng.Cmd[0])
	default:
		return row{Engine: eng.Name, Case: fx.Name, Status: "skip", Err: "unknown kind"}
	}

	start := time.Now()
	out, err := cmd.CombinedOutput()
	ms := time.Since(start).Milliseconds()
	r := row{Engine: eng.Name, Case: fx.Name, Ms: ms, Chars: len(out)}
	if ctx.Err() != nil {
		r.Status = "timeout"
		r.Err = fmt.Sprintf("killed after %s", runTimeout)
		return r
	}
	if err != nil {
		r.Status = "error"
		r.Err = clip(err.Error() + " " + string(out))
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() < 0 {
			r.Status = "crash"
		}
		return r
	}
	text := string(out)
	if eng.Kind == "pdfcpu-cli" {
		text = readDir(filepath.Join(filepath.Dir(path), "pdfcpu-"+fx.Name+"-"+eng.Name))
		r.Chars = len(text)
	}
	r.Status = "ok"
	r.Retained = fx.Want != "" && strings.Contains(strings.ToLower(text), strings.ToLower(fx.Want))
	r.Form = fx.WantForm != "" && strings.Contains(strings.ToLower(text), strings.ToLower(fx.WantForm))
	if fx.Want != "" && !r.Retained {
		r.Status = "ok-miss"
	}
	return r
}

func runWorker(engineName string) {
	path := os.Getenv("PDFCORPUS_FILE")
	var (
		ext pdfiumExtractor
		err error
	)
	switch engineName {
	case "pdfium-dll":
		ext, err = openPDFiumDLL()
	case "pdfium-wasm":
		ext, err = newPDFiumWASM()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// The parent starts its 5s pathological timer only after this marker, so
	// library load / wasm compilation is not mistaken for a hang.
	fmt.Fprintln(os.Stderr, "PDFCORPUS_READY")

	var text string
	if ext != nil {
		defer ext.Close()
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, rerr)
			os.Exit(2)
		}
		text, err = ext.Extract(data)
	} else {
		text, err = extractGo(engineName, path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Stdout.WriteString(text)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func runGoWorker(self, engineName string, fx fixture, path string) row {
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), "PDFCORPUS_WORKER="+engineName, "PDFCORPUS_FILE="+path)
	var stdout, stderr syncBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return row{Engine: engineName, Case: fx.Name, Status: "error", Err: clip(err.Error())}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(initTimeout)
	for !strings.Contains(stderr.String(), "PDFCORPUS_READY") {
		select {
		case err := <-done:
			return row{
				Engine: engineName, Case: fx.Name, Status: "error",
				Ms: time.Since(start).Milliseconds(), Chars: len(stdout.String()),
				Err: clip(fmt.Sprintf("before ready: %v %s", err, stderr.String())),
			}
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			<-done
			return row{Engine: engineName, Case: fx.Name, Status: "error", Err: "init timeout"}
		}
		time.Sleep(5 * time.Millisecond)
	}

	extractStart := time.Now()
	select {
	case err := <-done:
		ms := time.Since(extractStart).Milliseconds()
		text := stdout.String()
		r := row{Engine: engineName, Case: fx.Name, Ms: ms, Chars: len(text)}
		if err != nil {
			r.Status = "error"
			r.Err = clip(err.Error() + " " + stderr.String())
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() < 0 {
				r.Status = "crash"
			}
			return r
		}
		r.Status = "ok"
		r.Retained = fx.Want != "" && strings.Contains(strings.ToLower(text), strings.ToLower(fx.Want))
		r.Form = fx.WantForm != "" && strings.Contains(strings.ToLower(text), strings.ToLower(fx.WantForm))
		if fx.Want != "" && !r.Retained {
			r.Status = "ok-miss"
		}
		return r
	case <-time.After(runTimeout):
		_ = cmd.Process.Kill()
		<-done
		return row{
			Engine: engineName, Case: fx.Name, Status: "timeout",
			Ms: time.Since(extractStart).Milliseconds(), Err: fmt.Sprintf("killed after %s of extraction", runTimeout),
		}
	}
}

func readDir(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil {
			b.Write(data)
		}
	}
	return b.String()
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " / ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func printTable(rows []row) {
	engines := unique(rows, func(r row) string { return r.Engine })
	cases := unique(rows, func(r row) string { return r.Case })
	index := map[string]row{}
	for _, r := range rows {
		index[r.Engine+"\t"+r.Case] = r
	}
	fmt.Print("| case |")
	for _, e := range engines {
		fmt.Printf(" %s |", e)
	}
	fmt.Print("\n|---|")
	for range engines {
		fmt.Print("---|")
	}
	fmt.Println()
	for _, c := range cases {
		fmt.Printf("| %s |", c)
		for _, e := range engines {
			r := index[e+"\t"+c]
			fmt.Printf(" %s |", cell(r))
		}
		fmt.Println()
	}
	fmt.Println()
	fmt.Println("legend: OK = opened and kept expected text; miss = opened, expected text absent; err = returned error; timeout = killed at 5s; crash = nonzero abort")
}

func cell(r row) string {
	switch r.Status {
	case "ok":
		if r.Form {
			return fmt.Sprintf("OK+form %dms", r.Ms)
		}
		return fmt.Sprintf("OK %dms", r.Ms)
	case "ok-miss":
		return fmt.Sprintf("miss %dms", r.Ms)
	case "timeout":
		return "TIMEOUT"
	case "crash":
		return "CRASH"
	case "error":
		return "err"
	default:
		return r.Status
	}
}

func printErrors(rows []row) {
	fmt.Println("errors:")
	for _, r := range rows {
		if r.Status == "ok" || r.Status == "ok-miss" || r.Err == "" {
			continue
		}
		fmt.Printf("  %s / %s: %s %s\n", r.Engine, r.Case, r.Status, r.Err)
	}
}

type benchCell struct {
	Status string
	Chars  int
	Times  []float64
}

func writeBenchFixtures(fixDir string) error {
	for _, fx := range benchFixtures() {
		if err := os.WriteFile(filepath.Join(fixDir, fx.Name+".pdf"), fx.Data, 0600); err != nil {
			return err
		}
	}
	return nil
}

// runBenchWorker is the child side of the warm-process benchmark: load the
// engine once, warm it up, then measure seven rounds of repeated extractions.
// Process startup, file reads and wasm compilation are outside every timing.
func runBenchWorker(engineName, dir string) {
	bench, cleanup, err := newBenchExtractor(engineName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer cleanup()

	for _, fx := range benchFixtures() {
		path := filepath.Join(dir, fx.Name+".pdf")
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("BENCH|%s|read-error|0|\n", fx.Name)
			continue
		}
		iters := benchIterations(fx.Name)
		for i := 0; i < iters; i++ {
			_, _ = bench(data, path)
		}

		status := "ok"
		chars := 0
		var times []string
		for round := 0; round < benchRounds; round++ {
			text, us, rerr := measureRound(bench, data, path, iters)
			times = append(times, strconv.FormatFloat(us, 'f', 2, 64))
			if rerr != nil {
				status = "err"
				break
			}
			chars = len(text)
			if fx.Want != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(fx.Want)) {
				status = "miss"
			}
		}
		fmt.Printf("BENCH|%s|%s|%d|%s\n", fx.Name, status, chars, strings.Join(times, ","))
	}
}

const benchRounds = 7

func benchIterations(name string) int {
	if strings.HasSuffix(name, "_1p") {
		return 500
	}
	return 20
}

// measureRound returns the per-extraction average in microseconds. A round
// that reports zero elapsed time is retried; Windows QPC occasionally hands
// back a zero-length interval under load.
func measureRound(bench benchExtract, data []byte, path string, iters int) (string, float64, error) {
	var (
		text string
		err  error
		us   float64
	)
	for attempt := 0; attempt < 3; attempt++ {
		start := time.Now()
		for i := 0; i < iters; i++ {
			text, err = bench(data, path)
			if err != nil {
				break
			}
		}
		us = float64(time.Since(start).Microseconds()) / float64(iters)
		if us > 0 || err != nil {
			return text, us, err
		}
	}
	return text, us, err
}

// runStress repeats one fixture in one process to check throughput and
// long-run stability. It prints progress every 500 extractions so an external
// sampler can pair RSS with iteration counts.
func runStress(engineName, path string) {
	iters := 3000
	if v := os.Getenv("PDFCORPUS_STRESS_ITERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			iters = n
		}
	}
	bench, cleanup, err := newBenchExtractor(engineName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for i := 0; i < 10; i++ {
		_, _ = bench(data, path)
	}

	start := time.Now()
	chars := 0
	for i := 0; i < iters; i++ {
		text, err := bench(data, path)
		if err != nil {
			fmt.Printf("STRESS|error|%d|%v\n", i+1, err)
			os.Exit(2)
		}
		chars = len(text)
		if (i+1)%500 == 0 {
			fmt.Printf("STRESS|%d|%d|%.0f\n", i+1, chars, time.Since(start).Seconds()*1000)
		}
	}
	fmt.Printf("STRESS_DONE|%d|%d|%.0f\n", iters, chars, time.Since(start).Seconds()*1000)
}

type benchExtract func(data []byte, path string) (string, error)

func newBenchExtractor(name string) (benchExtract, func(), error) {
	switch name {
	case "pdfium-dll":
		ext, err := openPDFiumDLL()
		if err != nil {
			return nil, nil, err
		}
		return func(data []byte, _ string) (string, error) { return ext.Extract(data) }, func() {}, nil
	case "pdfium-wasm":
		ext, err := newPDFiumWASM()
		if err != nil {
			return nil, nil, err
		}
		return func(data []byte, _ string) (string, error) { return ext.Extract(data) }, ext.Close, nil
	case "ledongthuc":
		return func(data []byte, _ string) (string, error) { return extractLedongBytes(data) }, func() {}, nil
	default:
		return func(_ []byte, path string) (string, error) { return extractGo(name, path) }, func() {}, nil
	}
}

func runBenchInProcess(self string, engines []engine, fixDir string) {
	fmt.Println()
	fmt.Println("## extraction-only (warm process; median of 7 rounds × N iterations, µs/extraction)")
	benches := benchFixtures()
	results := map[string]map[string]benchCell{}
	var names []string
	for _, eng := range engines {
		if eng.Kind != "go" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		cmd := exec.CommandContext(ctx, self)
		cmd.Env = append(os.Environ(), "PDFCORPUS_BENCH="+eng.Name, "PDFCORPUS_BENCH_DIR="+fixDir)
		out, err := cmd.Output()
		cancel()
		if err != nil {
			msg := err.Error()
			if exitErr, ok := err.(*exec.ExitError); ok {
				msg = clip(string(exitErr.Stderr))
			}
			fmt.Printf("(skip warm bench %s: %s)\n", eng.Name, msg)
			continue
		}
		cells := map[string]benchCell{}
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Split(line, "|")
			if len(parts) != 5 || parts[0] != "BENCH" {
				continue
			}
			chars, _ := strconv.Atoi(parts[3])
			cell := benchCell{Status: parts[2], Chars: chars}
			if parts[4] != "" {
				for _, raw := range strings.Split(parts[4], ",") {
					v, perr := strconv.ParseFloat(raw, 64)
					if perr == nil {
						cell.Times = append(cell.Times, v)
					}
				}
			}
			cells[parts[1]] = cell
		}
		results[eng.Name] = cells
		names = append(names, eng.Name)
	}
	if len(names) == 0 {
		return
	}

	fmt.Print("| engine |")
	for _, fx := range benches {
		fmt.Printf(" %s |", fx.Name)
	}
	fmt.Print("\n|---|")
	for range benches {
		fmt.Print("---:|")
	}
	fmt.Println()
	for _, name := range names {
		fmt.Printf("| %s |", name)
		for _, fx := range benches {
			fmt.Printf(" %s |", benchCellText(results[name][fx.Name]))
		}
		fmt.Println()
	}

	fmt.Println()
	fmt.Println("raw rounds (µs per extraction):")
	fmt.Print("| engine |")
	for _, fx := range benches {
		fmt.Printf(" %s |", fx.Name)
	}
	fmt.Print("\n|---|")
	for range benches {
		fmt.Print("---|")
	}
	fmt.Println()
	for _, name := range names {
		fmt.Printf("| %s |", name)
		for _, fx := range benches {
			cell := results[name][fx.Name]
			if len(cell.Times) == 0 {
				fmt.Printf(" %s |", cell.Status)
				continue
			}
			raw := make([]string, 0, len(cell.Times))
			for _, v := range cell.Times {
				raw = append(raw, strconv.FormatFloat(v, 'f', 1, 64))
			}
			fmt.Printf(" %s |", strings.Join(raw, " / "))
		}
		fmt.Println()
	}
}

func benchCellText(c benchCell) string {
	if c.Status != "ok" && c.Status != "miss" {
		return c.Status
	}
	if len(c.Times) == 0 {
		return c.Status
	}
	med := medianFloat(c.Times)
	lo, hi := minMaxFloat(c.Times)
	return fmt.Sprintf("%s (%s–%s) · %d chars", benchDuration(med), benchDuration(lo), benchDuration(hi), c.Chars)
}

func benchDuration(us float64) string {
	if us >= 1000 {
		return fmt.Sprintf("%.2f ms", us/1000)
	}
	return fmt.Sprintf("%.1f µs", us)
}

func medianFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	cp := append([]float64(nil), v...)
	sort.Float64s(cp)
	return cp[len(cp)/2]
}

func minMaxFloat(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	lo, hi := v[0], v[0]
	for _, x := range v[1:] {
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	return lo, hi
}

// runBenchSpawn measures engines that are naturally out-of-process (Python,
// Node, CLI). Numbers include interpreter startup, so they are not comparable
// with the warm-process table above.
func runBenchSpawn(self string, engines []engine, fixDir, jsRoot string) {
	const warmup = 1
	const rounds = 3
	fmt.Println()
	fmt.Println("## process-inclusive (median of 3 after 1 warmup; Python/Node/CLI only)")
	fmt.Print("| engine |")
	benches := benchFixtures()
	for _, fx := range benches {
		fmt.Printf(" %s |", fx.Name)
	}
	fmt.Print("\n|---|")
	for range benches {
		fmt.Print("---|")
	}
	fmt.Println()
	for _, eng := range engines {
		if eng.Kind == "go" {
			continue
		}
		fmt.Printf("| %s |", eng.Name)
		for _, fx := range benches {
			path := filepath.Join(fixDir, fx.Name+".pdf")
			var times []int64
			var last row
			ok := 0
			for i := 0; i < warmup+rounds; i++ {
				r := runOne(self, eng, fx, path, jsRoot)
				last = r
				if i >= warmup && (r.Status == "ok" || r.Status == "ok-miss") {
					times = append(times, r.Ms)
					ok++
				}
			}
			if len(times) == 0 {
				fmt.Printf(" %s |", cell(last))
				continue
			}
			fmt.Printf(" %dms n=%d chars=%d |", median(times), ok, last.Chars)
		}
		fmt.Println()
	}
}

func median(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	cp := append([]int64(nil), ms...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

func filterEngines(engines []engine, filter string) []engine {
	wanted := map[string]bool{}
	for _, name := range strings.Split(filter, ",") {
		wanted[strings.TrimSpace(name)] = true
	}
	kept := engines[:0]
	for _, eng := range engines {
		if wanted[eng.Name] {
			kept = append(kept, eng)
		}
	}
	return kept
}

func unique(rows []row, key func(row) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		k := key(r)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}
