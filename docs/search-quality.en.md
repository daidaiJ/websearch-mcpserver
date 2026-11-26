# Search Quality Benchmark and Parameter Tuning

> 2026-11-17. This benchmark was inspired by the public evaluation in the open-source [`fangchenghao/dsh-web-search`](https://github.com/fangchenghao/dsh-web-search) project. It reuses that project's MIT-licensed 20-query corpus and real engine snapshots, replayed through this project's scoring pipeline. This document records the methodology, measured effects of the default configuration, per-parameter direction and confidence intervals, and a design for tuning on your own history.

## 1. Dataset

- Source: [`fangchenghao/dsh-web-search`](https://github.com/fangchenghao/dsh-web-search), commit `0b7d1e42f5c965b0f46dc7f7cadb03302b041cdb` (npm `0.24.2`, MIT).
- Contents: 20 queries (10 Chinese, 10 English; 5 each for docs / release / package / news), each annotated with 1–3 expected domains. Each query stores the raw result lists of five engines (anysearch / exa / firecrawl / ddg / bing, up to 10 results each).
- Location: `bench/tuning/testdata/dsh-web-search/`; see `LICENSE` and `NOTICE.md` beside it.
- Replay: no network, no cache. The raw rankings are fed directly into `pkg/search/enhance`, so any weight or threshold change produces a reproducible metric delta.

## 2. Metrics and statistics

| Metric | Definition |
|---|---|
| `P@8` | matching rows in the top 8 / 8 (fixed denominator; short lists are not excused) |
| `R@8` | distinct expected domains found in the top 8 / total expected domains |
| `MRR@8` | reciprocal rank of the first matching result; 0 when absent |
| `ΔMRR [95% CI]` | paired bootstrap against the default configuration (4,000 resamples, seed 42); the same query indices are used on both sides |
| `W/L/T` | per-query MRR wins / losses / ties |

Domain matching is one-way: expected `python.org` covers `docs.python.org`; expected `docs.python.org` does not cover `python.org`.

**Reading the table**: 20 queries have low statistical power. A confidence interval spanning zero is not evidence of an improvement. This document calls an effect “significant” only when the 95% CI excludes zero.

## 3. Running

```bash
# Embedded reference corpus, Markdown table
go run ./bench/tuning/cmd/searchbench

# More resamples and a saved report
go run ./bench/tuning/cmd/searchbench -iterations 4000 -format md -out report.md

# Your own replay directory (queries.json + fixtures/)
go run ./bench/tuning/cmd/searchbench -data ./my-capture -dump dataset.json
```

Use `-format json` for machine-readable output. `-dump` writes the normalized dataset for inspection or editing.

## 4. Baseline

Production defaults: RRF K=60, lexical weight 0.5, consensus/authority/recency/domain-quality/rare-term scales 1, threshold 0.05, MMR enabled with λ=0.7, top 8. Engine weights follow the snapshots (anysearch 2, all others 1).

| Configuration | P@8 | R@8 | MRR@8 |
|---|---:|---:|---:|
| This project's scoring pipeline (snapshot weights) | **0.6625** | **0.7917** | **0.8750** |
| `dsh-web-search` 0.24.2 self-eval (`node scripts/bench.mjs`, same fixtures) | 0.4375 | 0.7917 | 0.8071 |

The `dsh-web-search` numbers come from running its offline `node scripts/bench.mjs` at 0.24.2 (tier-aware replay; all five engines in this capture are marked primary, so it matches our replay of every list). These are not an apples-to-apples comparison of rankers. This project adds domain-quality, authority, lexical-alignment and rare-term signals on top of RRF, and the corpus is dominated by official documentation and release notes — exactly where authority signals apply. Treat it as two pipelines on the same data, not a universal quality claim.

The authority boost gained a data/statistics intent gate in 2026-11: when a query carries GDP / population / census / statistics / indicators / dataset signals, official primary sources such as `.gov`, `.gov.cn`, the World Bank, the IMF and the OECD receive an extra boost; other queries are unaffected. Disable this branch with `smartsearch.primary_source_boost: false`. The 20-query corpus here is dominated by technical documentation and does not cover that branch.

## 5. Parameter effects

The full table is produced by `searchbench`; the rows below are the interpretable ones.

| Knob | Value | P@8 | R@8 | MRR@8 | ΔMRR [95% CI] | W/L/T | Reading |
|---|---:|---:|---:|---:|---|---:|---|
| `authority_scale` | 0 | 0.2562 | 0.6917 | 0.6975 | **-0.1775 [-0.3417, -0.0258]** | 2/7/11 | **The only significant effect**: disabling authority boost clearly hurts; keep it |
| | 0.5 | 0.5750 | 0.7917 | 0.8725 | -0.0025 [-0.0750, +0.0725] | 1/2/17 | Halving it buys nothing measurable |
| | 1 (default) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | — |
| | 1.5 | 0.6813 | 0.7917 | 0.8625 | -0.0125 [-0.0750, +0.0375] | 1/1/18 | P@8 up, MRR down; not significant |
| `rrf_k` | 40 | 0.6500 | 0.7917 | 0.8750 | 0 | 0/0/20 | No measurable difference from 60 |
| | 60 (default) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | — |
| | 80 | 0.6687 | 0.7917 | 0.9000 | +0.0250 [0, +0.0750] | 1/0/19 | Directionally better, CI touches zero |
| | 120 | 0.6687 | 0.7917 | 0.9042 | +0.0292 [0, +0.0833] | 2/0/18 | Same; only 2 queries changed |
| `lexical_weight` | 0 | 0.6625 | 0.7917 | 0.9042 | +0.0292 [0, +0.0833] | 2/0/18 | Disabling lexical alignment does not hurt on this corpus, but CI touches zero |
| | 0.5 (default) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | — |
| | 1 | 0.6687 | 0.7917 | 0.8725 | -0.0025 [-0.0075, 0] | 0/1/19 | No gain |
| `consensus_scale` | 0 | 0.6687 | 0.7750 | 0.8225 | -0.0525 [-0.1467, +0.0167] | 1/4/15 | Directionally worse, CI spans zero |
| | 1 (default) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | — |
| | 1.5 | 0.6500 | 0.7917 | 0.8975 | +0.0225 [-0.0075, +0.0750] | 1/1/18 | Directionally better, CI spans zero |
| `mmr_lambda` | 0.3 | 0.5813 | 0.7917 | 0.9000 | +0.0250 [0, +0.0625] | 2/0/18 | Diversity for MRR: P@8 drops sharply |
| | 0.5 | 0.6312 | 0.7917 | 0.9000 | +0.0250 [0, +0.0625] | 2/0/18 | Same trade-off |
| | 0.7 (default) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | Precision/diversity balance |
| | 0.9 | 0.6750 | 0.7917 | 0.8725 | -0.0025 [-0.0075, 0] | 0/1/19 | Slightly more precision |
| `mmr_enabled` | 0 | 0.6813 | 0.7917 | 0.8725 | -0.0025 [-0.0075, 0] | 0/1/19 | Disabling changes little; keep it on |
| `threshold` | 0.02 | 0.6438 | 0.7917 | 0.8750 | 0 | 0/0/20 | No difference |
| | 0.05 (default) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | — |
| | 0.1 | 0.6500 | 0.7750 | 0.8750 | 0 | 0/0/20 | Recall starts to drop |
| | 0.2 | 0.5750 | 0.7000 | 0.8500 | -0.0250 [-0.0625, 0] | 0/2/18 | Too strict |
| `recency_scale` | 0 / 0.5 / 1 / 1.5 | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | No strongly temporal query in this corpus |
| `domain_quality_scale` | 0 / 0.5 / 1 / 1.5 | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | Expected domains already rank well |
| `rare_terms_scale` | 0 | 0.6625 | 0.7917 | 0.9000 | +0.0250 [0, +0.0750] | 1/0/19 | Directional, CI touches zero |
| `anysearch_weight` | 1 | 0.6750 | 0.7917 | 0.9000 | +0.0250 [0, +0.0750] | 1/0/19 | Directional, CI touches zero |
| | 2 (snapshot weight) | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | — |
| `bing_weight` | 0.25 / 0.5 / 1 / 2 | 0.6625 | 0.7917 | 0.8750 | 0 | 0/0/20 | No change on this corpus |
| `duckduckgo_weight` | 0.5 / 1 / 2 | 0.6625 / 0.6625 / 0.6562 | 0.7917 | 0.8750 / 0.8750 / 0.8725 | 0 / -0.0025 | 0/0/20, 0/1/19 | No significant difference |

## 6. Conclusions and recommendations

1. **Keep the defaults.** The only significant effect is that disabling authority boost hurts. Every other knob is either flat or inside the noise band. The reference project reached the same conclusion with its own sweep: the 20-query corpus cannot separate configurations, so the defaults stay.
2. **The two knobs worth adjusting for a deliberate trade-off**:
   - `smartsearch.mmr.lambda`: lower it to ~0.5 for more diverse results and accept lower P@8; raise it to 0.9 for more precision.
   - `smartsearch.relevance_threshold`: a small increase to 0.1 is reasonable for stricter output; 0.2 already loses recall.
3. **Do not chase RRF K or engine weights on small samples.** K=80/120 looks directionally better here but the CI touches zero and only 1–2 queries change; the reference project's K=40 sweep also spans zero. A different query mix can reverse the direction.
4. **Keep authority, consensus and lexical alignment.** This corpus does not prove “more is better,” but disabling authority clearly hurts, disabling consensus is directionally worse, and lexical alignment has no evidence against it outside docs-heavy queries.
5. **This corpus is docs/release-heavy.** It naturally favors authority and domain-quality signals. News, forum and Chinese community queries need a replay on your own data before applying these conclusions.

## 7. Tuning on your own history (design, pending approval)

Goal: let an agent sweep the same knobs on the user's own query/engine mix instead of guessing from 20 public queries.

**Capture (off by default)**

- A new `search_eval.capture_raw: true` (zero value = off) records the per-engine buckets immediately before `enhance.EnhanceResultsMMR` in `hybrid.mergeResults`: engine name, rank, title, URL, snippet, publish date, query, timestamp.
- Local-only SQLite at `history/search-eval.db`; no network, no keys; 30-day retention by default, cleaned with the existing history janitor; turning the switch off stops writes, deleting the DB erases the data.
- This is more sensitive than the existing `search_history` snapshot (URLs and snippets), so it needs its own switch and database and explicit docs.

**Export and replay**

- `websearch-mcpserver tune export --since 30d --out ./my-capture` writes the same `queries.json + fixtures/*.json` shape as the reference corpus (or the normalized JSON format).
- The agent runs `go run ./bench/tuning/cmd/searchbench -data ./my-capture` and gets P@8/R@8/MRR@8, ΔMRR confidence intervals and W/L/T per knob; it proposes a config diff, and the user approves before editing `config.yaml`.
- No new MCP tool: tuning is a low-frequency operator action, and a shell-capable agent can run the CLI. This keeps the resident tool surface at five. If Docker/stdio users cannot run a shell, a disabled-by-default `search_eval` tool can be evaluated later.

**Acceptance criteria**

- Capture adds < 1 ms per query and never affects the search path;
- Offline replay works without network; exported files are inspectable;
- Deleting `search-eval.db` leaves search behavior unchanged;
- Docs cover the switch, retention, export command and a “back up config before editing” reminder.

## 8. Limitations

- Twenty queries can only detect large effects; this document claims “the defaults do not visibly regress on this corpus,” not “the optimal configuration.”
- The snapshots were captured by the reference project in October 2026; engine rankings drift. Use them for regression and direction, not as a permanent absolute score.
- This project's authority/domain-quality signals structurally favor official documentation; cross-language, news and forum queries need local data.
- `recency`, `domain_quality` and `rare_terms` show no measurable effect here; that does not mean they are useless on other distributions.
