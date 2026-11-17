// Command searchbench replays the fixed tuning corpus through the scoring
// pipeline and prints parameter effects with paired-bootstrap intervals.
//
//	go run ./bench/tuning/cmd/searchbench
//	go run ./bench/tuning/cmd/searchbench -format json -out report.json
//	go run ./bench/tuning/cmd/searchbench -data ./my-capture -format md
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"websearch/bench/tuning"
)

func main() {
	var (
		dataDir    = flag.String("data", "", "directory with queries.json + fixtures/ (default: embedded reference corpus)")
		format     = flag.String("format", "md", "output format: md or json")
		out        = flag.String("out", "", "write the report to this file instead of stdout")
		iterations = flag.Int("iterations", 2000, "paired-bootstrap resamples")
		seed       = flag.Int64("seed", 42, "bootstrap seed")
		dump       = flag.String("dump", "", "write the normalized dataset JSON to this path")
	)
	flag.Parse()

	ds, err := loadDataset(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load dataset:", err)
		os.Exit(1)
	}
	if *dump != "" {
		if err := writeJSON(*dump, ds); err != nil {
			fmt.Fprintln(os.Stderr, "dump dataset:", err)
			os.Exit(1)
		}
	}
	report := tuning.Sweep(ds, tuning.ProductionOptions(), *iterations, *seed)

	var payload []byte
	switch *format {
	case "json":
		payload, err = json.MarshalIndent(report, "", "  ")
	case "md":
		payload = []byte(report.Markdown())
	default:
		fmt.Fprintf(os.Stderr, "unknown format %q (use md or json)\n", *format)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode report:", err)
		os.Exit(1)
	}
	if *out == "" {
		os.Stdout.Write(payload)
		return
	}
	if err := os.WriteFile(*out, payload, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write report:", err)
		os.Exit(1)
	}
}

func loadDataset(dir string) (tuning.Dataset, error) {
	if dir == "" {
		return tuning.ReferenceDataset()
	}
	return tuning.LoadReferenceDataset(dir)
}

func writeJSON(path string, v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
