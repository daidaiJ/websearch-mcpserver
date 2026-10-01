package everything

import (
	"sort"
	"strings"

	"websearch/pkg/search/enhance"
)

// DefaultNoiseDirs 内置噪声目录：命中这些路径片段的结果只做降权（不剔除）。
// Everything 不感知 gitignore，仓库噪声会淹没检索结果，这是文件检索
// 最实用的二次过滤。nil = 使用内置默认；配置显式空数组 = 关闭降权。
var DefaultNoiseDirs = []string{
	`node_modules`, `.git`, `target`, `dist`, `build`, `obj`, `bin`,
	`__pycache__`, `.idea`, `.vscode`, `venv`, `.venv`, `coverage`,
}

// ScoreWeights 二次过滤的打分权重：文件名对齐远比路径对齐重要——
// 文件名直接命中查询词的结果才是用户想要的。
const (
	nameAlignWeight = 1.0
	pathAlignWeight = 0.3
	noisePenalty    = 0.5
)

// EnhanceOptions 二次过滤选项。
type EnhanceOptions struct {
	// ReRank 是否按对齐分重排。sort 参数未显式指定（服务端默认按 name）
	// 时为 true；用户显式指定排序时应传 false，尊重服务端排序，
	// 仅保留阈值过滤。
	ReRank    bool
	NoiseDirs map[string]struct{}
	// MinAlign 词汇对齐阈值（0~1，0 = 只重排不过滤）：查询词与
	// 文件名+路径合并后的对齐率（enhance.LexicalAlignment 口径）低于
	// 该值的结果被丢弃。
	MinAlign float64
}

// EnhanceItems 对检索结果做二次过滤与重排（借鉴 smartsearch 评分管线：
// 词汇对齐 + 阈值过滤，文件检索场景的裁剪版）：
//
//  1. 词汇对齐打分：文件名对齐（权重 1.0）+ 路径对齐（权重 0.3），
//     复用 enhance.LexicalAlignment 的分词与停用词处理（中英文通用）；
//  2. 噪声降权：命中 noiseDirs 的结果分数减半，只影响排序不剔除；
//  3. 阈值过滤：opt.MinAlign > 0 时丢弃对齐率低于阈值的弱结果；
//  4. 排序：opt.ReRank 时分数降序（同分按修改时间新者在前），
//     否则保留服务端返回顺序。
//
// 返回新切片，不修改入参顺序。
func EnhanceItems(items []Item, query string, opt EnhanceOptions) []Item {
	type scored struct {
		item  Item
		score float64
		mod   int64
	}
	out := make([]scored, 0, len(items))
	for _, it := range items {
		nameAlign := enhance.LexicalAlignment(query, it.Name, "")
		pathAlign := enhance.LexicalAlignment(query, it.Path, "")
		combined := enhance.LexicalAlignment(query, it.Name, it.Path)
		score := nameAlignWeight*nameAlign + pathAlignWeight*pathAlign
		if len(opt.NoiseDirs) > 0 && isNoise(it.Path, opt.NoiseDirs) {
			score *= noisePenalty
		}
		if opt.MinAlign > 0 && combined < opt.MinAlign {
			continue
		}
		mod, _ := filetimeToTime(it.DateModified)
		out = append(out, scored{item: it, score: score, mod: mod.UnixNano()})
	}
	if opt.ReRank {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].score != out[j].score {
				return out[i].score > out[j].score
			}
			return out[i].mod > out[j].mod
		})
	}
	result := make([]Item, 0, len(out))
	for _, s := range out {
		result = append(result, s.item)
	}
	return result
}

// NoiseDirSet 把配置的噪声目录列表转为集合；nil 表示使用内置默认。
func NoiseDirSet(dirs []string) map[string]struct{} {
	if dirs == nil {
		dirs = DefaultNoiseDirs
	}
	set := make(map[string]struct{}, len(dirs))
	for _, d := range dirs {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			set[d] = struct{}{}
		}
	}
	return set
}

// isNoise 判断路径是否包含噪声目录片段（按路径组件匹配，避免误伤名字里
// 恰好含 "bin" 的文件，如 robin.go）。
func isNoise(path string, noiseDirs map[string]struct{}) bool {
	for _, seg := range strings.FieldsFunc(strings.ToLower(path), func(r rune) bool {
		return r == '\\' || r == '/'
	}) {
		if _, ok := noiseDirs[seg]; ok {
			return true
		}
	}
	return false
}
