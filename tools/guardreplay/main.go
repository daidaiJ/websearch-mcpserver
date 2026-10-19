// guardreplay 离线校准 off-topic 守卫：读取遥测库中 guard 事件的判定明细，
// 按不同回声率阈值回放，输出「若 enforce 会误杀哪些桶」的报表与阈值敏感度。
//
// 用法：
//
//	go run ./tools/guardreplay -db ./data/dashboard.db
//	go run ./tools/guardreplay -db ./data/dashboard.db -thresholds 0.15,0.25,0.35,0.5 -json
//
// 明细来自 hybrid 守卫落库的 detail 字段（mode=… ref=… suspected=… engine=回声率/条数），
// 不含 URL 或页面原文；查询仅以遥测层的哈希与关键词形式存在。
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type bucketSample struct {
	Engine string
	Echo   float64
	Count  int
}

type guardRecord struct {
	ID       int64
	Day      string
	Mode     string
	Ref      float64
	Hash     string
	Keywords string
	Topic    string
	Buckets  []bucketSample
}

type thresholdReport struct {
	Threshold   float64  `json:"threshold"`
	Dropped     int      `json:"dropped_buckets"`
	Affected    int      `json:"affected_queries"`
	Samples     []string `json:"samples,omitempty"`
	BucketTotal int      `json:"bucket_total"`
}

func main() {
	dbPath := flag.String("db", "./data/dashboard.db", "遥测 SQLite 路径（dashboard.storage_path）")
	rawThresholds := flag.String("thresholds", "0.15,0.25,0.35,0.5", "回声率阈值候选，逗号分隔")
	limit := flag.Int("limit", 5000, "最多回放多少条 guard 事件")
	asJSON := flag.Bool("json", false, "以 JSON 输出")
	flag.Parse()

	thresholds, err := parseThresholds(*rawThresholds)
	if err != nil {
		fmt.Fprintln(os.Stderr, "guardreplay:", err)
		os.Exit(2)
	}
	records, err := loadRecords(*dbPath, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "guardreplay:", err)
		os.Exit(1)
	}
	reports := replay(records, thresholds)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"records":   len(records),
			"threshold": reports,
		})
		return
	}
	printReport(os.Stdout, records, reports)
}

func parseThresholds(raw string) ([]float64, error) {
	var out []float64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseFloat(part, 64)
		if err != nil || v < 0 || v > 1 {
			return nil, fmt.Errorf("非法阈值 %q（须在 0~1 之间）", part)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("阈值列表为空")
	}
	sort.Float64s(out)
	return out, nil
}

func loadRecords(path string, limit int) ([]guardRecord, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("打开遥测库失败: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("打开遥测库失败: %w", err)
	}
	defer db.Close()

	query := `SELECT id,day,query_hash,query_keywords,query_topic,detail
FROM usage_events WHERE kind='guard' ORDER BY occurred_at DESC,id DESC LIMIT ?`
	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("查询 guard 事件失败（遥测库是否启用？）: %w", err)
	}
	defer rows.Close()

	var out []guardRecord
	for rows.Next() {
		var rec guardRecord
		var detail string
		if err := rows.Scan(&rec.ID, &rec.Day, &rec.Hash, &rec.Keywords, &rec.Topic, &detail); err != nil {
			return nil, err
		}
		rec.parseDetail(detail)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// parseDetail 解析 "mode=shadow ref=0.812 suspected=1 bing=0.042/5 ddg=0.833/6"。
func (r *guardRecord) parseDetail(detail string) {
	for _, field := range strings.Fields(detail) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "mode":
			r.Mode = value
		case "ref":
			r.Ref, _ = strconv.ParseFloat(value, 64)
		case "suspected":
			// 只用于落库时观察，回放时按候选阈值重新判定
		default:
			echo, count, ok := strings.Cut(value, "/")
			if !ok {
				continue
			}
			e, err := strconv.ParseFloat(echo, 64)
			if err != nil {
				continue
			}
			c, err := strconv.Atoi(count)
			if err != nil {
				continue
			}
			r.Buckets = append(r.Buckets, bucketSample{Engine: key, Echo: e, Count: c})
		}
	}
}

// replay 按每个候选阈值重新判定：桶条数 ≥3 且回声率 ≤ 阈值即会被 enforce 丢弃。
// 参照桶（回声率 ≥ offTopicMinReference 0.75）永不判为 off-topic，与线上守卫一致。
func replay(records []guardRecord, thresholds []float64) []thresholdReport {
	const (
		minBucket    = 3
		minReference = 0.75
	)
	reports := make([]thresholdReport, 0, len(thresholds))
	for _, threshold := range thresholds {
		report := thresholdReport{Threshold: threshold}
		for _, rec := range records {
			hasRef := rec.Ref >= minReference
			if !hasRef {
				for _, b := range rec.Buckets {
					if b.Echo >= minReference {
						hasRef = true
						break
					}
				}
			}
			if !hasRef {
				continue
			}
			affected := false
			for _, b := range rec.Buckets {
				report.BucketTotal++
				if b.Count >= minBucket && b.Echo <= threshold {
					report.Dropped++
					affected = true
					if len(report.Samples) < 20 {
						report.Samples = append(report.Samples, fmt.Sprintf("%s %s [%s] 关键词=%s 引擎=%s 回声率=%.3f 条数=%d",
							rec.Day, rec.Hash, rec.Topic, rec.Keywords, b.Engine, b.Echo, b.Count))
					}
				}
			}
			if affected {
				report.Affected++
			}
		}
		reports = append(reports, report)
	}
	return reports
}

func printReport(w io.Writer, records []guardRecord, reports []thresholdReport) {
	_, _ = fmt.Fprintf(w, "guard 事件: %d 条（回放窗口由 -limit 控制）\n\n", len(records))
	if len(records) == 0 {
		_, _ = fmt.Fprintln(w, "没有 guard 事件：确认 smartsearch.off_topic_guard 未设为 off，且已产生过低回声率桶。")
		return
	}
	_, _ = fmt.Fprintln(w, "阈值敏感度（若把该阈值用作 enforce 上限）：")
	_, _ = fmt.Fprintln(w, "| 阈值 | 会被丢弃的桶 | 受影响查询 |")
	_, _ = fmt.Fprintln(w, "|---|---|---|")
	for _, report := range reports {
		_, _ = fmt.Fprintf(w, "| %.2f | %d | %d |\n", report.Threshold, report.Dropped, report.Affected)
	}
	_, _ = fmt.Fprintln(w)
	for _, report := range reports {
		if len(report.Samples) == 0 {
			continue
		}
		_, _ = fmt.Fprintf(w, "阈值 %.2f 的丢弃样例：\n", report.Threshold)
		for _, sample := range report.Samples {
			_, _ = fmt.Fprintf(w, "  - %s\n", sample)
		}
		_, _ = fmt.Fprintln(w)
	}
	_, _ = fmt.Fprintln(w, "说明：回声率 = 桶内标题/摘要命中「查询其余词」的比例；阈值越低越保守。")
	_, _ = fmt.Fprintln(w, "线上阈值见 pkg/search/hybrid/hybrid.go 的 offTopicMaxEchoRatio（当前 0.25）。")
}
