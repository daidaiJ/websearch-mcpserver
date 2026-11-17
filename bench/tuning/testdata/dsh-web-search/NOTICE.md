# Reference benchmark notice

`queries.json` and `fixtures/*.json` come from the public retrieval benchmark of
[fangchenghao/dsh-web-search](https://github.com/fangchenghao/dsh-web-search)
at commit `0b7d1e42f5c965b0f46dc7f7cadb03302b041cdb` (package version 0.24.2,
2026-11-02). That project is MIT-licensed; the full license text is in
`LICENSE` beside this file.

We reuse its public fixtures with attribution, and its benchmark design inspired
the offline sweep in this directory. Source paths:

- `bench/queries.json`
- `bench/fixtures/*.json`

Changes in this repository:

- Files are stored unchanged under `bench/tuning/testdata/dsh-web-search/`.
- The Go loader normalizes engine names at read time (`cn.bing` → `bing`,
  `ddg-html` → `duckduckgo`) and maps `publishedAt`/`snippet` to
  `PublishDate`/`Content`; the stored JSON is not rewritten.
- Our benchmark replays these raw rankings through this project's scoring
  pipeline, which differs from the original ranker. Reported numbers are not
  the reference project's results.

The reference project's own caveat applies: 20 queries are enough to catch
regressions and large effects, not to establish universal parameter optima.
