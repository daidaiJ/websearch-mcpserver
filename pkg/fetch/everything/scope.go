package everything

import (
	"fmt"
	"strings"
)

// ScopeQuery 把目录限定项拼进查询串，返回可直接交给 Search 的最终 query。
//
// 约束规则（克制优先）：
//   - roots 为白名单（绝对路径）；folder 与 roots 均归一化后做大小写不敏感
//     的组件边界前缀比较，folder 必须落在某个 root 内（或恰为 root）。
//   - folder 非空：以 folder 为范围（roots 非空时先校验，越界即报错）。
//   - folder 为空且 roots 非空：以全部 roots 的 OR 项为范围，不给模型
//     全盘检索的口子。
//   - folder 为空且 roots 为空：不限定范围。
//
// 路径项保留原始大小写拼入查询：Everything 的路径匹配默认大小写不敏感，
// 但 match_case（i=1）时路径项也参与大小写敏感匹配，小写化会让限定静默失效。
func ScopeQuery(roots []string, folder, query string) (string, error) {
	query = strings.TrimSpace(query)

	if folder != "" {
		folder = normalizeDir(folder)
		if err := checkRoots(roots, folder); err != nil {
			return "", err
		}
		return joinQuery(pathTerm(folder), query), nil
	}

	if len(roots) == 0 {
		return query, nil
	}
	terms := make([]string, 0, len(roots))
	for _, root := range roots {
		if root = normalizeDir(root); root != "" {
			terms = append(terms, pathTerm(root))
		}
	}
	if len(terms) == 0 {
		return query, nil
	}
	return joinQuery(strings.Join(terms, "|"), query), nil
}

// checkRoots 校验 dir 是否落在白名单内；roots 为空 = 不限定。
func checkRoots(roots []string, dir string) error {
	if len(roots) == 0 {
		return nil
	}
	lower := strings.ToLower(dir)
	for _, root := range roots {
		if root = strings.ToLower(normalizeDir(root)); root != "" && withinDir(lower, root) {
			return nil
		}
	}
	return fmt.Errorf("目录 %s 不在 everything.roots 白名单内", dir)
}

// withinDir 判断 dir 是否等于 root 或位于 root 之下（组件边界比较，
// 避免 D:\CODE\ai 误匹配 D:\CODE\ai2）。入参需已小写归一化。
func withinDir(dir, root string) bool {
	if dir == root {
		return true
	}
	return strings.HasPrefix(dir, root+`\`)
}

// AppendExcludes 把排除项拼成 Everything NOT 项追加到查询串（每项独立生效，
// 互为 OR 关系即「排除任一命中」）。含空格的词加引号保证作为单一词项。
func AppendExcludes(query string, excludes []string) string {
	terms := make([]string, 0, len(excludes))
	for _, ex := range excludes {
		ex = strings.TrimSpace(ex)
		if ex == "" {
			continue
		}
		if strings.ContainsAny(ex, " \t") {
			ex = `"` + ex + `"`
		}
		terms = append(terms, "!"+ex)
	}
	if len(terms) == 0 {
		return query
	}
	return joinQuery(query, strings.Join(terms, " "))
}

// pathTerm 生成 Everything 的路径前缀限定项：带尾部分隔符的路径
// 会被 Everything 按完整路径前缀匹配。
func pathTerm(dir string) string {
	return dir + `\`
}

// joinQuery 拼接限定项与用户查询词。
func joinQuery(term, query string) string {
	if query == "" {
		return term
	}
	return term + " " + query
}

// normalizeDir 归一化目录路径，返回反斜杠分隔、无尾分隔符的原始大小写形式：
//   - Git Bash / MSYS 风格（/d/code/ai）转为盘符风格（D:\code\ai）——
//     agent 在 shell 环境里最常给出这种写法；
//   - 正斜杠统一转反斜杠（D:/code/ai → D:\code\ai）。
//
// 白名单比较的大小写不敏感由调用方另行 lower 处理。
func normalizeDir(dir string) string {
	dir = strings.TrimSpace(dir)
	// Git Bash 风格：/d/code/ai 或 /d/ → 盘符路径
	if strings.HasPrefix(dir, "/") && len(dir) >= 2 && dir[1] >= 'a' && dir[1] <= 'z' &&
		(len(dir) == 2 || dir[2] == '/') {
		dir = string(dir[1]-'a'+'A') + ":" + dir[2:]
	}
	dir = strings.ReplaceAll(dir, "/", `\`)
	return strings.TrimRight(dir, `\`)
}
