package i18n

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// localesDir 是文案目录，文件名形如 <域>.<语言>.json（log.zh.json / mcp.en.json /
// dashboard.zh.json / web.en.json）。文件名决定语言——go-i18n 取扩展名前的最后一段，
// 所以不能按 zh/ en/ 分子目录。按域拆文件：一个域一个文件，多人/多批次改动互不冲突。
//
//go:embed locales/*.json
var localeFS embed.FS

var (
	bundleOnce sync.Once
	shared     *goi18n.Bundle
)

// bundle 加载全部域文件。bundle 默认语言取 zh：en 缺条目时自动回退 zh 文案，
// 不会把 key 露给用户；zh 也缺才会返回 key 本身。
func bundle() *goi18n.Bundle {
	bundleOnce.Do(func() {
		b := goi18n.NewBundle(language.Chinese)
		b.RegisterUnmarshalFunc("json", json.Unmarshal)
		err := fs.WalkDir(localeFS, "locales", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
				return err
			}
			_, lerr := b.LoadMessageFileFS(localeFS, p)
			return lerr
		})
		if err != nil {
			// 内嵌文案目录损坏属于构建期问题，启动即失败比静默降级更容易定位。
			panic("i18n: 加载内嵌文案目录失败: " + err.Error())
		}
		shared = b
	})
	return shared
}

// RawMessages 返回某个域在指定语言下的原始 JSON 文案。
// 供 dashboard 把前端域（web）内联注入 index.html，让前端与后端共用同一份文案数据。
// 语言或域不存在时返回 nil。
func RawMessages(lang, domain string) []byte {
	b, err := localeFS.ReadFile("locales/" + domain + "." + lang + ".json")
	if err != nil {
		return nil
	}
	return b
}
