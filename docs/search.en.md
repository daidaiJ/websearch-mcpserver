# Search Modes, Engines & MCP Tools

[English](search.en.md) | [中文](search.md)

## Contents

- [Search Modes](#search-modes)
- [Engine Reference](#engine-reference)
- [Relevance Scoring](#relevance-scoring)
  - [General Search Scoring (Wigolo)](#general-search-scoring-wigolo)
  - [MMR Diversity Re-ranking](#mmr-diversity-re-ranking)
  - [Academic Search Scoring](#academic-search-scoring)
- [SmartSearch Advanced Config](#smartsearch-advanced-config)
- [Apipool Config](#apipool-config)
- [MCP Tools](#mcp-tools)
  - [`smartsearch` — General Web Search](#smartsearch--general-web-search)
  - [`academicsearch` — Academic Paper Search](#academicsearch--academic-paper-search)
  - [`cleanfetch` — Web Content Fetch](#cleanfetch--web-content-fetch)
  - [`pdf_parser` — PDF Parsing](#pdf_parser--pdf-parsing)
  - [`file_search` — Local File Quick Search](#file_search--local-file-quick-search)
- [Academic Search Tips](#academic-search-tips)

---

## Search Modes

| Mode | Description | Key Required |
|------|-------------|--------------|
| `engine` | Baidu web search + Bing concurrently (DuckDuckGo joins when a proxy is available, Google when enabled) | **None** |
| `baidu` | Baidu Qianfan search (`enable_ai_search` controls endpoint), falls back to Baidu web search; uses Baidu web search directly when no SK | `BAIDU_SK` (optional) |
| `apipool` | API key pool rotation: one provider per request, auto-switch on failure; supports `round-robin` / `priority` / `weighted` strategies; Baidu web search as final fallback | All optional |
| `tavily` | Tavily Search API ([get key](https://app.tavily.com/home)) | `TAVILY_SK` |
| `exa` | Exa Web Search API ([get key](https://dashboard.exa.ai/api-keys)) | `EXA_API_KEY` |
| `anysearch` | AnySearch API ([get key](https://www.anysearch.com/console/api-keys)) | `ANYSEARCH_API_KEY` |
| `doubao` | Doubao Search Global / Custom ([get key](https://console.volcengine.com/search-infinity/api-key)) | `DOUBAO_SEARCH_API_KEY` |
| `hybrid` | Full mix (Anysearch + Baidu AI + Baidu web + Tavily + Exa + Doubao if keyed + Bing + DuckDuckGo + Google) | All optional |

> All modes auto-fallback on primary engine failure. Auto-degrades to `engine` mode when keys are missing. `baidu`/`tavily`/`exa`/`anysearch`/`doubao` all support `sk_list` multi-key rotation (duplicate keys within one provider are deduplicated automatically); `sk_list` falls back to `api_key` as a single-element list when empty.

**Mode → engine mapping** (from `pkg/search/factory.go`):

| Mode | Engines |
|------|---------|
| `engine` | Baidu web + Bing + Google (if enabled) + DuckDuckGo (if proxy available), concurrent |
| `baidu` | Baidu Qianfan (`enable_ai_search` controls endpoint) → falls back to Baidu web search |
| `tavily` | Tavily; falls back to Bing when no key |
| `exa` | Exa; falls back to Bing when no key |
| `anysearch` | AnySearch; falls back to Bing when no key |
| `doubao` | Doubao Search Global/Custom (`doubao.version`); falls back to Bing when no key |
| `apipool` | Rotates anysearch / baidu / tavily / exa in configured order (`doubao` must be listed in `apipool.engines`), Baidu web search always last |
| `hybrid` | Anysearch + Baidu AI + Baidu web + Tavily + Exa + Doubao (if keyed) + Bing + Google + DuckDuckGo, concurrent |

---

## Engine Reference

**General-purpose engines**:

| Config Name | Engine | Returns Score | Needs Proxy |
|-------------|--------|---------------|-------------|
| `baidu` | Baidu web search (built-in, `tn=json`) | ❌ | No |
| `bing` | Bing (built-in) | ❌ | No |
| `duckduckgo` | DuckDuckGo | ❌ | Yes (auto-detected) |
| `google` | Google (disabled by default, anti-bot blocked) | ❌ | Yes |
| `tavily_api` | Tavily Search API | ✅ | No |
| `exa` | Exa Web Search API | ❌ | No |
| `anysearch` | AnySearch API (built-in local blacklist filtering) | ❌ | No |
| `doubao` | Doubao Search Global/Custom API (local blacklist; Custom returns score) | ✅ | No |
| `baidu_api` | Baidu Qianfan search (`enable_ai_search` controls endpoint) | ❌ | No |

**Academic engines** (no keys required):

| Engine | Description | Needs Proxy |
|--------|-------------|-------------|
| `arxiv` | Preprints (CS/AI/physics) | No |
| `crossref` | All-discipline DOI metadata | No |
| `openalex` | All-discipline open scholarly graph | No |
| `pubmed` | Biomedical literature | No |
| `europepmc` | Europe PMC (biomedical / PubMed supplement, full text) | No |
| `dblp` | DBLP (CS conference/journal index) | No |
| `doaj` | DOAJ (open-access journals directory) | No |
| `semantic_scholar` | Semantic scholar (disabled by default) | Yes (auto-detected) |
| `google_scholar` | All-discipline academic search (disabled by default) | Yes (auto-detected) |

> **Network availability**: Google / DuckDuckGo / Crossref / Google Scholar are unstable under `network: china` without a proxy (may time out or be blocked by anti-bot measures); Bing web scraping may also time out. Failed engines are auto-skipped/fallback and do not affect other engines' results; partial academic-engine failures append a "some engines failed this run" note to results.

---

## Relevance Scoring

### General Search Scoring (Wigolo)

Multi-engine results go through the following pipeline (fully heuristic, no AI model):

1. **RRF fusion ranking** (Reciprocal Rank Fusion, K=60) — fuses multi-engine results by rank
2. **Lexical alignment** — word-level matching between query and result title/content
3. **Rare-term / phrase contiguity** — rare terms and contiguous phrase hits weighted higher
4. **Domain-quality penalty** — down-weights brand / e-commerce / dictionary mismatches
5. **Consensus / authority / recency boosts** — multi-engine consensus, authority sites, recency
6. **Global low-score threshold** — results with `final_score < relevance_threshold` (default 0.05) are dropped, keeping only Top-1 (and at least 2 results)

Config: `smartsearch.enhance` (default true), `smartsearch.relevance_threshold` (default 0.05).

### MMR Diversity Re-ranking

Runs after score filtering and before `max_size` truncation — greedy MMR re-ranking (Token Jaccard similarity) breaks up highly similar same-topic results (mirror / repost / same-source blogs), with Top-1 protection.

```yaml
smartsearch:
  mmr:
    enabled: true      # Master switch (default true)
    lambda: 0.7        # Relevance weight [0,1]; higher = more relevance, lower = more diversity (default 0.7)
    target_count: 0    # Target count after MMR; 0 = no extra truncation (max_size applies)
```

### Academic Search Scoring

Nine academic engines are fused via RRF ranking with academic-specific signals:

- **Citation count** (log-compressed, clamped [1.0, 1.7])
- **High-impact journal / conference** boost
- **PDF full-text availability**
- **Recency factor** (×1.15 for the last year on time-sensitive queries)

Low-score papers are auto-filtered (Top-1 + per-engine floor). Config: `academic.enhance` (default true), `academic.threshold` (default 0.02), independent of smartsearch.

---

## SmartSearch Advanced Config

The `smartsearch` section controls result filtering, truncation, and output format:

```yaml
smartsearch:
  max_size: 10           # Global max results (truncated by score), 0 = unlimited
  fetch_top_n: 0         # Server-side default body-fetch count (applies when the agent omits fetch_top_n), default 0 = no fetch (same as before);
                         # set 1-5 to fetch full text by default too (API engines use the fast path, web engines fetch internally)
  show_meta: true        # Show engine source and relevance score in output (default true)
  enhance: true          # Local scoring enhancement (RRF fusion + lexical alignment + domain quality + boosts + threshold), default true
  relevance_threshold: 0.05  # Relevance threshold after enhancement; below this is filtered (Top-1 protected), default 0.05
  mmr:                       # MMR diversity re-ranking (breaks up same-topic similar results)
    enabled: true            # Master switch (default true)
    lambda: 0.7              # Relevance weight [0,1]; higher = more relevance, lower = more diversity (default 0.7)
    target_count: 0          # Target count after MMR; 0 = no extra truncation (max_size applies)
  engines:
    tavily_api:        # Tavily API (returns score, supports min_score)
      min_score: 0.5   # Minimum relevance score threshold, 0 = no filter
      max_size: 6      # Per-engine max results (default 4)
      weight: 1.0      # Engine weight, affects RRF fusion score (when enhance=true), 0 = default 1.0
    bing:              # Bing (no score, min_score ignored)
      max_size: 4
    baidu_api:         # Baidu Qianfan Search (no score, enable_ai_search controls endpoint)
      max_size: 5
    baidu:             # Baidu web search (no score)
      max_size: 5
    google:            # Google (disabled by default, anti-bot blocked)
      max_size: 4
    duckduckgo:        # DuckDuckGo (no score, needs proxy)
      max_size: 4
    anysearch:         # AnySearch (no score)
      max_size: 4
    doubao:            # Doubao Search (Custom returns score; Global does not)
      min_score: 0
      max_size: 4
```

**Score filtering logic**:
- Engine returns score: filter by `min_score`, keep `max_size` results
- Engine returns no score: ignore `min_score`, take `min(max_size, ⌈global_max_size / engine_count⌉)`
- Global `max_size`: with scores → sort by score and truncate; without scores → round-robin distribution across engines

---

## Apipool Config

The `apipool` section controls provider selection strategy, priority order and weights for `mode: apipool`:

```yaml
apipool:
  strategy: weighted      # round-robin (default) / priority / weighted
  engines:                # Provider priority order (default [anysearch, baidu, tavily, exa])
    - anysearch
    - baidu
    - tavily
    - exa
    # - doubao            # not in the default list; add explicitly when you have a key
  weights:                # weighted strategy weights (per-key; defaults below)
    anysearch: 30000
    baidu: 1500
    tavily: 1200
    exa: 1200
    doubao: 500           # free-tier 500 credits/month
```

**Strategy details**:
- **`round-robin`** (default): rotates the starting provider across requests; within a single request, exhausts all available SKs in the current provider before falling back to the next
- **`priority`**: always starts from the first provider; exhausts all SKs → switches to next provider → Baidu web search as final fallback
- **`weighted`**: weighted-random selection of the starting provider, which naturally spreads request bursts across providers. A provider's effective weight = **configured weight × currently available SK count** (auto-shrinks when SKs cool down, self-healing); providers absent from the weight table count as 1; an explicit `0` excludes a provider from weighted starting selection (it stays in the failure-switch chain); when all weights are 0 it degrades to round-robin. The Baidu web search fallback engine has no key pool and a fixed weight of 1

**Default weights** (overridable via `apipool.weights`): `anysearch=30000`, `baidu=1500`, `tavily=1200`, `exa=1200`, `doubao=500` (free-tier monthly credits)

**Workflow**: select provider → `pool.Next()` → call API → success / mark key cooldown 30 min → retry next SK in same provider → all exhausted → next provider → all failed → Baidu web search fallback

---

## MCP Tools

> Tool registration conditions: `smartsearch` needs `bing.enabled=true`; `academicsearch` needs `academic.enabled=true`; `cleanfetch` needs `cleanfetch.enabled=true`; `pdf_parser` needs `pdf_parser.enabled=true`; `file_search` has no switch — it is registered when the lazy probe of the Everything HTTP Server at first MCP client connection succeeds, and stays hidden otherwise.

### `smartsearch` — General Web Search

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | ✅ | Search keyword |
| `intent` | string | ❌ | Search intent (only effective when LLM is enabled; auto-removed to save context when disabled) |
| `time_range` | int | ❌ | Search time range in months, default 3. `1`=last month, `6`=last 6 months, `12`=last year, `0`=unlimited. Doubao Custom maps this to `OneDay`/`OneWeek`/`OneMonth`/`OneYear`; Global has no time-filter API and ignores it |
| `fetch_top_n` | int | ❌ | Body-fetch mode: when omitted, behaves as before — only engine/provider-provided content is returned (server config `smartsearch.fetch_top_n` can change the default, default `0` = no fetch); `0` means title+snippet+URL only; `1-5` obtains page-original text for the top N results — API engines that support full-text params (Tavily/Exa/Doubao) use the fast path directly, web engines fetch internally (no `cleanfetch.enabled` needed, webfetch lazily initializes); results already carrying sufficient body text are skipped; anti-bot blocks (JS challenge/WAF) are explicitly annotated on the result |

Results include engine source and relevance score by default (for engines that support scores like Tavily / Doubao Custom). Disable via `smartsearch.show_meta: false`.

**LLM summarization**: with the `llm` section configured, `smartsearch` accepts `intent` and generates a structured summary; the summary stage pushes tokens in real time via MCP progress notifications, auto-cancels on client disconnect, and falls back to non-streaming summary on failure.

### `academicsearch` — Academic Paper Search

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | ✅ | Search keyword; a DOI or arXiv id can also be passed directly for single-paper lookup (see below) |
| `engines` | []string | ❌ | Engine subset: `arxiv` `crossref` `openalex` `pubmed` `europepmc` `dblp` `doaj` `semantic_scholar` `google_scholar` |
| `time_range` | string | ❌ | `year` / `month` / `week` / `day` |
| `page` | int | ❌ | Page number, default 1 |

With `time_range`, each academic engine uses its official syntax (aligned in v3.4.0 so Crossref / DOAJ / arXiv no longer fail upstream):

| Engine | Syntax |
|--------|--------|
| Crossref | `filter=from-pub-date:YYYY-MM-DD` |
| DOAJ | `bibjson.year:[startYear TO current UTC year]` (no `*`; `day`/`week`/`month` collapse to year granularity) |
| arXiv | `submittedDate:[YYYYMMDDHHMM TO YYYYMMDDHHMM]` (UTC/GMT) |

**Single-paper lookup (DOI / arXiv id short-circuit)**: when you already have a DOI or arXiv id, pass it directly as `query`, e.g. `10.1038/s41586-020-2649-2`, `doi:10.1038/s41586-020-2649-2`, `https://doi.org/10.1038/s41586-020-2649-2`, or `2401.04085`, `arXiv:2401.04085`, `https://arxiv.org/abs/2401.04085`. This triggers a single-paper lookup that ignores `engines` / `time_range` / `page`; once you have the `pdf_url` from the result, pass that URL as the `path` parameter of the `pdf_parser` tool to parse the full text instead of searching by title.

Results are ranked by the academic scoring enhancement (enabled by default): RRF fusion ranking + citation / journal authority / PDF availability / recency signals, with low-score papers auto-filtered (Top-1 + per-engine floor). Config: `academic.enhance` (default true), `academic.threshold` (default 0.02).

### `cleanfetch` — Web Content Fetch

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | one of `url`/`urls` | Web page URL |
| `urls` | string[] | one of `url`/`urls` | Batch fetch; merged with `url`, deduplicated, up to 5 |

Requires `cleanfetch.enabled: true`. Based on go-webfetch, no proxy needed; built-in DNS rebinding protection and HEAD pre-check for large files (`max_fetch_size_mb` controls threshold, default 10MB; every redirect hop is re-checked against private-network/metadata rules, up to 5 hops); falls back to Jina Reader on failure (requires `jina.api_key`, proxy auto-detected).

In batch mode (`urls`), each URL is pre-checked and fetched independently; one failure does not affect the others, and results are returned grouped by URL. With only `url`, output is identical to previous versions.

### `pdf_parser` — PDF Parsing

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | Local PDF file path or remote http(s) URL (academic `pdf_url` can be passed directly) |
| `pages` | string | ❌ | Page range (1-based), e.g. `1-10`, `1,3,5-7`; invalid formats raise a parameter error, and explicit page counts above `max_pages` or a single range wider than 1000 pages are rejected with a split suggestion |

Requires `pdf_parser.enabled: true`. Large documents auto-stored to temp files. Remote URLs use the same SSRF and HEAD pre-checks as `cleanfetch` (including per-hop redirect re-checks) and are not prefixed with `file://`.

When `pages` is omitted, the first `pdf_parser.max_pages` (default 20) pages are requested. Local PDF text extraction reports the known total and truncation; MinerU may not return the original page count. Remote MinerU Standard API first uses `page_ranges` (per-task page limit defaults to 600 per MinerU official docs, tunable via `pdf_parser.mineru_page_limit`); MinerU rejects the task outright when the source file exceeds its page limit or the source URL is not fetchable; in that case the service downloads it, uses qpdf to select the requested pages, and uploads the smaller PDF. Explicitly requesting pages beyond the per-task limit (requires raising `pdf_parser.max_pages`; the default 20 rejects wide requests first) takes the same path (original download cap 200 MB; the uploaded file must still meet MinerU's 200 MB / 600-page limits). With `pdf_parser.mineru_page_batch_size` (10-200, 0 = no batching) set, pages beyond the per-task limit are split into batches submitted serially, bounded by the `pdf_parser.mineru_page_budget` page budget per call; on exhaustion the parsed part is returned with a continuation hint. Per-batch progress (source download, each batch start/completion) is streamed via MCP progress notifications, so agents do not block blind through the whole run. The result states the original page count and renumbering and includes a complete ZIP URL. Relative `images/` references in Markdown require the images in that ZIP. Scanned OCR also crops before upload, with at most 20 pages per request and a 10 MB cropped-file limit. Agent returns Markdown only and does not guarantee downloadable relative image assets. Standalone binaries require qpdf on the host; project Docker images include it.

**Parsing strategy**: local PDFs prefer the PDF library (ledongthuc/pdf) for text extraction; if there is no text layer and `mineru_ocr` is enabled, fall back to MinerU OCR.
- `mineru_ocr: true`: OCR fallback for scanned / image-based PDFs (Agent Lightweight API, ≤10 MB and at most 20 pages per request)
- `mineru_token`: Standard API for remote URLs (≤200MB/600 pages); can also be used with OCR fallback
- Get Token: https://mineru.net/apiManage
- Environment variable: `MINERU_TOKEN`

---

### `file_search` — Local File Quick Search

Filename/path search over the index of [Everything (voidtools)](https://www.voidtools.com/) HTTP Server on Windows (millisecond results, read-only, never reads file contents). Windows only; **not recommended on any Linux distribution unless WSL** (set `everything.url` explicitly to the Windows host — on non-Windows platforms without an explicit url there is no probe and the tool stays hidden). For version-specific enable/hardening guidance (1.4 built-in vs 1.5a plugin, ini pitfalls, curl verification, hardening checklist) see [skills/everything-http-server](../skills/everything-http-server/SKILL.md).

| Parameter | Type | Required | Description |
|------|------|------|------|
| `query` | string | ✅ | Search text with Everything syntax: `factory`, `*.go`, `ext:pdf report`, `dm:lastweek`, `size:>1mb`; use `match_regex` for regular expressions |
| `folder` | string | ❌ | Scope the search to a directory; accepts Windows (`D:\CODEi`) or Git Bash (`/d/code/ai`) style paths. When a whitelist is configured the folder must fall inside it; when omitted, all whitelisted directories are searched |
| `match_case` | bool | ❌ | Case-sensitive match (Everything `i` flag) |
| `whole_word` | bool | ❌ | Whole-word match (`w` flag) |
| `match_regex` | bool | ❌ | Regex search (spliced as a `regex:` function so directory scoping is unaffected) |
| `match_diacritics` | bool | ❌ | Match diacritics (`m` flag) |
| `exclude` | []string | ❌ | Exclusion terms, each appended as an Everything NOT term (space-containing terms are auto-quoted): `["\obj\", "
ode_modules\"]`; wrap path fragments with leading/trailing backslashes to avoid false hits on filenames |
| `min_alignment` | number | ❌ | Lexical alignment threshold (0-1) overriding `everything.min_alignment`; start at 0.3 when results overflow, 0 = re-rank only |
| `max_results` | int | ❌ | Max results returned (default 50, hard cap 200); keep it small to save context |
| `sort` | string | ❌ | `name` (default) / `date_modified` / `size` / `path` |
| `descending` | bool | ❌ | Descending order (with `sort`) |
| `time_format` | string | ❌ | `datetime` (default, `2026-01-02 15:04:05`) / `iso` (ISO 8601 UTC) / `filetime` (raw FILETIME) |

**Registration (automatic probe, no enabled switch)**: when the first MCP client connects, the server lazily probes `everything.url` (this avoids the autostart race between websearch and Everything hiding the tool for the whole process lifetime); the tool is registered only when the server is reachable and authentication passes. If Everything is not running, its HTTP Server is disabled, or authentication fails, the tool stays hidden without affecting anything else.

**Restraint constraints**: with `everything.roots` (directory whitelist) configured, every search is forcibly scoped to the whitelist and out-of-scope folders fail with an error. **Second-stage filtering (agent cost optimization)**: the server over-fetches 3x candidates (hard cap 600), when `sort` is not explicitly set, re-ranks them locally by lexical alignment of the query against filename/path (reusing the smartsearch scoring pipeline tokenizer and stop words; an explicit `sort` is respected and only the threshold filter applies), demotes results under noise directories such as `node_modules`/`.git`/`target` (`everything.noise_dirs` overrides), and `everything.min_alignment` drops weak matches — one line per result, weak hits never reach the context.

---

## Academic Search Tips

- Medicine/Biology → `pubmed` + `europepmc`; CS/AI → `arxiv` + `semantic_scholar` + `dblp`; Open access → `doaj`; All fields → `crossref` + `openalex`
- Keep `network: china` for domestic access; overseas engines auto-skipped (Europe PMC / DBLP / DOAJ are directly reachable)
- Semantic Scholar / Google Scholar disabled by default; set `disable_semantic_scholar: false` / `disable_google_scholar: false` to enable; proxy auto-detected
