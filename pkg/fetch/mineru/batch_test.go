package mineru

import (
	"reflect"
	"testing"
)

func TestSplitPageBatches(t *testing.T) {
	tests := []struct {
		name  string
		pages []int
		batch int
		want  [][]int
	}{
		{"disabled", []int{1, 2, 3}, 0, [][]int{{1, 2, 3}}},
		{"fits", []int{1, 2, 3}, 10, [][]int{{1, 2, 3}}},
		{"even", []int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{"uneven", []int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{"negative batch", []int{1, 2}, -1, [][]int{{1, 2}}},
		{"empty", nil, 10, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitPageBatches(tt.pages, tt.batch)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SplitPageBatches(%v, %d) = %v; want %v", tt.pages, tt.batch, got, tt.want)
			}
		})
	}
}

func TestPlanPageBatchesBudget(t *testing.T) {
	pages := []int{601, 602, 603, 604, 605}
	// 预算充足：不截断
	batches, truncated := PlanPageBatches(pages, 2, 0)
	if truncated || !reflect.DeepEqual(batches, [][]int{{601, 602}, {603, 604}, {605}}) {
		t.Fatalf("no budget: batches=%v truncated=%v", batches, truncated)
	}
	// 预算 4 页：第 3 批（1 页）被丢弃
	batches, truncated = PlanPageBatches(pages, 2, 4)
	if !truncated || !reflect.DeepEqual(batches, [][]int{{601, 602}, {603, 604}}) {
		t.Fatalf("budget=4: batches=%v truncated=%v", batches, truncated)
	}
	// 预算小于批大小：放不下任何完整批次 → 全部丢弃
	batches, truncated = PlanPageBatches(pages, 2, 1)
	if !truncated || len(batches) != 0 {
		t.Fatalf("budget=1: batches=%v truncated=%v", batches, truncated)
	}
}
