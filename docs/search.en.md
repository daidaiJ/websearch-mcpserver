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
- [Academic Search Tips](#academic-search-tips)

---

## Search Modes

| Mode | Description | Key Required |
|------|-------------|--------------|
| `engine` | Baidu web search + Bing concurrently (DuckDuckGo joins when a proxy is available, Google when enabled) | **None** |
| `baidu` | Baidu Qianfan search (`enable_ai_search` controls endpoint), falls back to Baidu web search; uses Baidu web search directly when no SK | `BAIDU_SK` (optional) |
| `apipool` | API key pool rotation: one provider per request, auto-switch on failure; supports `round-robin` / `priority` / `weighted` strategies; Baidu web search as final fallback | All optional |
| `tavily` | Tavily Search API | `TAVILY_SK` |
| `exa` | Exa Web Search API | `EXA_API_KEY` |
| `anysearch` | AnySearch API ([anysearch.com](https://www.anysearch.com/docs)) | `ANYSEARCH_API_KEY` |
| `hybrid` | Full mix (Anysearch + Baidu AI + Baidu web + Tavily + Exa + Bing + DuckDuckGo + Google) | All optional |

> All modes auto-fallback on primary engine failure. Auto-degrades to `engine` mode when keys are missing. `baidu`/`tavily`/`exa`/`anysearch` all support `sk_list` multi-key rotation (duplicate keys within one provider are deduplicated automatically); `sk_list` falls back to `api_key` as a single-element list when empty.

**Mode → engine mapping** (from `pkg/search/factory.go`):

| Mode | Engines |
|------|---------|
| `engine` | Baidu web + Bing + Google (if enabled) + DuckDuckGo (if proxy available), concurrent |
| `baidu` | Baidu Qianfan (`enable_ai_search` controls endpoint) → falls back to Baidu web search |
| `tavily` | Tavily; falls back to Bing when no key |
| `exa` | Exa; falls back to Bing when no key |
| `anysearch` | AnySearch; falls back to Bing when no key |
| `apipool` | Rotates anysearch / baidu / tavily / exa in configured order, Baidu web search always last |
| `hybrid` | Anysearch + Baidu AI + Baidu web + Tavily + Exa + Bing + Google + DuckDuckGo, concurrent |

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
| `baidu_api` | Baidu Qianfan search (`enable_ai_search` controls endpoint) | ❌ | No |

**Academic engines** (no keys required):

| Engine | Description | Needs Proxy |
|--------|-------------|-------------|
| `arxiv` | Preprints (CS/AI/physics) | No |
| `crossref` | All-discipline DOI metadata | No |
| `openalex` | All-discipline open scholarly graph | No |
| `pubmed` | Biomedical literature | No |
| `semantic_scholar` | Semantic scholar (disabled by default) | Yes (auto-detected) |
| `google_scholar` | All-discipline academic search (disabled by default) | Yes (auto-detected) |

> **Network availability**: Google / DuckDuckGo / Crossref / Google Scholar are unstable under `network: china` without a proxy (may time out or be blocked by anti-bot measures); Bing web scraping may also time out. Failed engines are auto-skipped/fallback and do not affect other engines' results.

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

Six academic engines are fused via RRF ranking with academic-specific signals:

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
  weights:                # weighted strategy weights (per-key; defaults below)
    anysearch: 30000
    baidu: 1500
    tavily: 1200
    exa: 1200
```

**Strategy details**:
- **`round-robin`** (default): rotates the starting provider across requests; within a single request, exhausts all available SKs in the current provider before falling back to the next
- **`priority`**: always starts from the first provider; exhausts all SKs → switches to next provider → Baidu web search as final fallback
- **`weighted`**: weighted-random selection of the starting provider, which naturally spreads request bursts across providers. A provider's effective weight = **configured weight × currently available SK count** (auto-shrinks when SKs cool down, self-healing); providers absent from the weight table count as 1; an explicit `0` excludes a provider from weighted starting selection (it stays in the failure-switch chain); when all weights are 0 it degrades to round-robin. The Baidu web search fallback engine has no key pool and a fixed weight of 1

**Default weights** (overridable via `apipool.weights`): `anysearch=30000`, `baidu=1500`, `tavily=1200`, `exa=1200`

**Workflow**: select provider → `pool.Next()` → call API → success / mark key cooldown 30 min → retry next SK in same provider → all exhausted → next provider → all failed → Baidu web search fallback

---

## MCP Tools

> Tool registration conditions: `smartsearch` needs `bing.enabled=true`; `academicsearch` needs `academic.enabled=true`; `cleanfetch` needs `cleanfetch.enabled=true`; `pdf_parser` needs `pdf_parser.enabled=true`.

### `smartsearch` — General Web Search

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | ✅ | Search keyword |
| `intent` | string | ❌ | Search intent (only effective when LLM is enabled; auto-removed to save context when disabled) |
| `time_range` | int | ❌ | Search time range in months, default 3. `1`=last month, `6`=last 6 months, `12`=last year, `0`=unlimited |

Results include engine source and relevance score by default (for engines that support scores like Tavily). Disable via `smartsearch.show_meta: false`.

**LLM summarization**: with the `llm` section configured, `smartsearch` accepts `intent` and generates a structured summary; the summary stage pushes tokens in real time via MCP progress notifications, auto-cancels on client disconnect, and falls back to non-streaming summary on failure.

### `academicsearch` — Academic Paper Search

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | ✅ | Search keyword |
| `engines` | []string | ❌ | Engine subset: `arxiv` `crossref` `openalex` `pubmed` `semantic_scholar` `google_scholar` |
| `time_range` | string | ❌ | `year` / `month` / `week` / `day` |
| `page` | int | ❌ | Page number, default 1 |

Results are ranked by the academic scoring enhancement (enabled by default): RRF fusion ranking + citation / journal authority / PDF availability / recency signals, with low-score papers auto-filtered (Top-1 + per-engine floor). Config: `academic.enhance` (default true), `academic.threshold` (default 0.02).

### `cleanfetch` — Web Content Fetch

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | ✅ | Web page URL |

Requires `cleanfetch.enabled: true`. Based on go-webfetch, no proxy needed; built-in DNS rebinding protection and HEAD pre-check for large files (`max_fetch_size_mb` controls threshold, default 10MB); falls back to Jina Reader on failure (requires `jina.api_key`, proxy auto-detected).

### `pdf_parser` — PDF Parsing

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | Local PDF file path or remote URL |

Requires `pdf_parser.enabled: true`. Large documents auto-stored to temp files.

**Parsing strategy**: local PDFs prefer the PDF library (ledongthuc/pdf) for text extraction; if there is no text layer and `mineru_ocr` is enabled, fall back to MinerU OCR.
- `mineru_ocr: true`: OCR fallback for scanned / image-based PDFs (without Token uses Agent Lightweight API, ≤10MB/20 pages)
- `mineru_token`: Standard API for remote URLs (≤200MB/200 pages); can also be used with OCR fallback
- Get Token: https://mineru.net/apiManage
- Environment variable: `MINERU_TOKEN`

---

## Academic Search Tips

- Medicine/Biology → `pubmed`; CS/AI → `arxiv` + `semantic_scholar`; All fields → `crossref` + `openalex`
- Keep `network: china` for domestic access; overseas engines auto-skipped
- Semantic Scholar / Google Scholar disabled by default; set `disable_semantic_scholar: false` / `disable_google_scholar: false` to enable; proxy auto-detected
