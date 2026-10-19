package main

import (
	"path/filepath"
	"testing"

	"websearch/pkg/telemetry"
)

func TestReplayThresholdSensitivity(t *testing.T) {
	records := []guardRecord{
		{Mode: "shadow", Ref: 0.9, Buckets: []bucketSample{
			{Engine: "bing", Echo: 0.9, Count: 5},
			{Engine: "baidu", Echo: 0.2, Count: 6},
		}},
		{Mode: "shadow", Ref: 0.8, Buckets: []bucketSample{
			{Engine: "bing", Echo: 0.8, Count: 4},
			{Engine: "ddg", Echo: 0.3, Count: 5},
		}},
		{Mode: "shadow", Ref: 0.2, Buckets: []bucketSample{
			{Engine: "bing", Echo: 0.2, Count: 5},
			{Engine: "ddg", Echo: 0.3, Count: 5},
		}},
	}
	reports := replay(records, []float64{0.15, 0.25, 0.5})
	if len(reports) != 3 {
		t.Fatalf("阈值数量不符: %d", len(reports))
	}
	if reports[0].Dropped != 0 || reports[0].Affected != 0 {
		t.Errorf("0.15 阈值不应丢弃任何桶，实际 %+v", reports[0])
	}
	if reports[1].Dropped != 1 || reports[1].Affected != 1 {
		t.Errorf("0.25 阈值应只丢弃 baidu 桶，实际 %+v", reports[1])
	}
	if reports[2].Dropped != 2 {
		t.Errorf("0.5 阈值应丢弃 baidu 与 ddg 两桶，实际 %+v", reports[2])
	}
}

// TestParseDetail 校验线上 detail 格式可被回放解析。
func TestParseDetail(t *testing.T) {
	var rec guardRecord
	rec.parseDetail("mode=shadow ref=0.812 suspected=1 bing=0.042/5 ddg=0.833/6")
	if rec.Mode != "shadow" || rec.Ref != 0.812 {
		t.Fatalf("头部字段解析失败: %+v", rec)
	}
	if len(rec.Buckets) != 2 || rec.Buckets[0].Engine != "bing" || rec.Buckets[0].Count != 5 {
		t.Fatalf("桶明细解析失败: %+v", rec.Buckets)
	}
}

// TestLoadRecordsReadsGuardEvents 端到端读取遥测库中的 guard 事件。
func TestLoadRecordsReadsGuardEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.db")
	store, err := telemetry.Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(telemetry.Event{
		Kind:   "guard",
		Tool:   "smartsearch",
		Query:  "golang context",
		Detail: "mode=shadow ref=0.900 suspected=1 bing=0.100/5",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	records, err := loadRecords(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("应读到 1 条 guard 事件，实际 %d", len(records))
	}
	if records[0].Hash == "" || records[0].Keywords == "" {
		t.Errorf("查询应已脱敏为哈希与关键词: %+v", records[0])
	}
	if len(records[0].Buckets) != 1 || records[0].Buckets[0].Engine != "bing" {
		t.Errorf("桶明细缺失: %+v", records[0].Buckets)
	}
}
