package mineru

// SplitPageBatches 将升序页码列表按每批至多 batch 页切分（保持原顺序）。
// batch <= 0 或页数不超标时整体为一批。
func SplitPageBatches(pages []int, batch int) [][]int {
	if batch <= 0 || len(pages) <= batch {
		if len(pages) == 0 {
			return nil
		}
		return [][]int{pages}
	}
	var batches [][]int
	for start := 0; start < len(pages); start += batch {
		end := start + batch
		if end > len(pages) {
			end = len(pages)
		}
		batches = append(batches, pages[start:end])
	}
	return batches
}

// PlanPageBatches 在分批基础上叠加单次调用的页数预算（budget <= 0 不设预算）：
// 预算耗尽后放不下的批次被丢弃，返回保留的批次与是否发生截断。
// 截断时调用方应在结果中说明已解析的原页码范围与续读方式。
func PlanPageBatches(pages []int, batch, budget int) (batches [][]int, truncated bool) {
	all := SplitPageBatches(pages, batch)
	if budget <= 0 {
		return all, false
	}
	used := 0
	for _, b := range all {
		if used+len(b) > budget {
			return batches, true
		}
		used += len(b)
		batches = append(batches, b)
	}
	return batches, false
}
