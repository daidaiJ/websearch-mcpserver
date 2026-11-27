"""Render dashboard screenshots from the committed frontend build with mocked API data."""
from __future__ import annotations

import json
import re
import threading
from datetime import datetime, timedelta, timezone
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

from PIL import Image
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[3]
DIST = ROOT / "pkg" / "dashboard" / "web" / "dist"
LOCALES = ROOT / "pkg" / "i18n" / "locales"
OUT = ROOT / "docs" / "images"

DAY = "2026-11-27"
TZ = timezone(timedelta(hours=8))


def daily(day: str, requests: int, failures: int = 0, cache_hits: int = 0, duration_ms: int = 0) -> dict:
    return {
        "day": day,
        "requests": requests,
        "successes": requests - failures,
        "failures": failures,
        "cache_hits": cache_hits,
        "duration_ms": duration_ms,
        "result_count": requests * 8,
    }


def outcomes(successes: int, failures: int = 0) -> list[bool]:
    return [True] * successes + [False] * failures


def web_source(
    source_id: str,
    name: str,
    *,
    status: str = "healthy",
    requests: int = 20,
    failures: int = 1,
    p95: int = 900,
    category: str = "api",
    enabled: bool = True,
    configured: bool = True,
    participation: str = "参与 hybrid 检索",
    error_kinds: list[dict] | None = None,
) -> dict:
    return {
        "id": source_id,
        "name": name,
        "group": "web",
        "category": category,
        "control_path": f"{source_id}.enabled",
        "enabled": enabled,
        "configured": configured,
        "active": enabled and configured,
        "participation": participation,
        "condition": "需要有效配置",
        "status": status,
        "observation_status": status,
        "last_seen_at": f"{DAY}T12:{10 + requests % 40:02d}:00+08:00",
        "suspended": False,
        "recent_outcomes": outcomes(max(0, 20 - failures), failures),
        "today": daily(DAY, requests, failures, 4, requests * p95 // 2),
        "p95_ms": p95,
        "quota_queryable": category == "api",
        "score_key": source_id,
        "error_kinds": error_kinds or [],
        "actual_cooldown": None,
    }


def academic_source(source_id: str, name: str, requests: int, failures: int = 0, p95: int = 1400) -> dict:
    return {
        "id": source_id,
        "name": name,
        "group": "academic",
        "category": "academic",
        "control_path": f"academic.disable_{source_id}",
        "enabled": True,
        "configured": True,
        "active": True,
        "inverted": True,
        "participation": "参与学术检索",
        "condition": "需要网络可达",
        "status": "healthy" if failures == 0 else "degraded",
        "observation_status": "healthy" if failures == 0 else "degraded",
        "last_seen_at": f"{DAY}T11:{20 + requests:02d}:00+08:00",
        "suspended": False,
        "recent_outcomes": outcomes(max(0, 20 - failures), failures),
        "today": daily(DAY, requests, failures, 0, requests * p95 // 2),
        "p95_ms": p95,
        "quota_queryable": False,
        "error_kinds": [],
    }


WEB = [
    web_source("tavily", "Tavily", requests=26, failures=1, p95=760,
               error_kinds=[{"kind": "rate_limit", "count": 1}]),
    web_source("bing", "Bing", requests=31, failures=0, p95=640, category="engine"),
    web_source("baidu_web", "百度网页", requests=24, failures=2, p95=810, category="engine"),
    web_source("duckduckgo", "DuckDuckGo", requests=18, failures=1, p95=1120, category="engine"),
    web_source("exa", "Exa", status="degraded", requests=16, failures=3, p95=1450,
               error_kinds=[{"kind": "timeout", "count": 2}]),
    web_source("anysearch", "AnySearch", requests=14, failures=0, p95=680),
    web_source("doubao", "豆包搜索", requests=11, failures=0, p95=520),
    web_source("yuanbao", "腾讯元宝", requests=8, failures=1, p95=910),
    web_source("google", "Google", status="unknown", requests=0, failures=0, p95=0,
               category="engine", enabled=False, configured=False, participation="已关闭"),
]
ACADEMIC = [
    academic_source("arxiv", "arXiv", 6, 0, 2100),
    academic_source("crossref", "Crossref", 4, 0, 1300),
    academic_source("openalex", "OpenAlex", 4, 1, 1800),
    academic_source("pubmed", "PubMed", 3, 0, 1500),
    academic_source("semantic_scholar", "Semantic Scholar", 2, 0, 2400),
    academic_source("google_scholar", "Google Scholar", 0, 0, 0),
    academic_source("europe_pmc", "Europe PMC", 2, 0, 1200),
    academic_source("dblp", "DBLP", 1, 0, 900),
    academic_source("doaj", "DOAJ", 1, 0, 1100),
]

TREND = [
    daily("2026-11-14", 84, 3, 18, 31000),
    daily("2026-11-15", 92, 4, 20, 33400),
    daily("2026-11-16", 76, 5, 16, 29200),
    daily("2026-11-17", 101, 2, 24, 37600),
    daily("2026-11-18", 110, 4, 27, 40200),
    daily("2026-11-19", 96, 3, 21, 35100),
    daily("2026-11-20", 118, 5, 29, 42100),
    daily("2026-11-21", 88, 2, 19, 32800),
    daily("2026-11-22", 72, 1, 15, 26300),
    daily("2026-11-23", 104, 6, 25, 38900),
    daily("2026-11-24", 121, 3, 31, 43800),
    daily("2026-11-25", 116, 4, 30, 41700),
    daily("2026-11-26", 109, 2, 26, 39200),
    daily("2026-11-27", 128, 4, 34, 45200),
]

TOOLS = [
    {"name": "smartsearch", "label": "智能搜索", "enabled": True, "registered": True, "state": "active", "today": daily(DAY, 82, 2, 28, 30100)},
    {"name": "cleanfetch", "label": "网页抓取", "enabled": True, "registered": True, "state": "active", "today": daily(DAY, 31, 1, 5, 11800)},
    {"name": "pdf_parser", "label": "PDF 解析", "enabled": True, "registered": True, "state": "active", "today": daily(DAY, 9, 0, 1, 4200)},
    {"name": "academicsearch", "label": "学术搜索", "enabled": True, "registered": True, "state": "active", "today": daily(DAY, 18, 1, 0, 6800)},
    {"name": "file_search", "label": "本地文件搜索", "enabled": True, "registered": True, "state": "active", "today": daily(DAY, 6, 0, 0, 900)},
]

OVERVIEW = {
    "generated_at": f"{DAY}T12:00:00+08:00",
    "today": daily(DAY, 128, 4, 34, 45200),
    "trend": TREND,
    "sources": WEB,
    "academic_sources": ACADEMIC,
    "configured_tools": TOOLS,
    "tools": [
        {"name": "smartsearch", "status": "healthy", "today": TOOLS[0]["today"], "last_seen_at": f"{DAY}T12:00:00+08:00"},
        {"name": "cleanfetch", "status": "healthy", "today": TOOLS[1]["today"], "last_seen_at": f"{DAY}T11:58:00+08:00"},
        {"name": "academicsearch", "status": "healthy", "today": TOOLS[3]["today"], "last_seen_at": f"{DAY}T11:50:00+08:00"},
    ],
    "system": {"status": "healthy", "enabled": 14, "observed": 13, "degraded": 1, "down": 0, "suspended": 0},
    "runtime": {
        "version": "3.10.7",
        "uptime_seconds": 98400,
        "mcp_stateless": False,
        "mcp_resources": True,
        "proxy": {"mode": "auto", "resolved": True, "api_providers": False},
        "llm": {"enabled": True, "model_id": "deepseek-chat", "timeout_seconds": 30},
        "mcp_hub": {"enabled": False, "fetch_mode": "off", "servers": []},
    },
    "brand": {"title": "WebSearch 控制中心", "theme": "green", "accent": "", "logo": "", "footer": True},
    "language": "zh",
}

CLIENTS = {
    "window_days": 30,
    "clients": [
        {"client": "Cursor", "requests": 612, "successes": 601, "failures": 11, "avg_ms": 720, "last_seen": f"{DAY}T11:58:00+08:00",
         "tools": [{"tool": "smartsearch", "requests": 402, "successes": 395, "failures": 7, "avg_ms": 690}]},
        {"client": "Claude Code", "requests": 284, "successes": 278, "failures": 6, "avg_ms": 810, "last_seen": f"{DAY}T11:42:00+08:00",
         "tools": [{"tool": "smartsearch", "requests": 190, "successes": 186, "failures": 4, "avg_ms": 760}]},
        {"client": "Codex", "requests": 176, "successes": 173, "failures": 3, "avg_ms": 640, "last_seen": f"{DAY}T10:20:00+08:00",
         "tools": [{"tool": "cleanfetch", "requests": 98, "successes": 97, "failures": 1, "avg_ms": 610}]},
    ],
    "usage": [
        {"client": "Cursor", "application": "cursor", "project": "docs", "tool": "smartsearch", "requests": 302, "successes": 298, "failures": 4, "duration_ms": 205000},
        {"client": "Cursor", "application": "cursor", "project": "default", "tool": "cleanfetch", "requests": 140, "successes": 138, "failures": 2, "duration_ms": 88000},
        {"client": "Claude Code", "application": "claude", "project": "research", "tool": "academicsearch", "requests": 126, "successes": 123, "failures": 3, "duration_ms": 96000},
        {"client": "Codex", "application": "codex", "project": "websearch", "tool": "smartsearch", "requests": 98, "successes": 96, "failures": 2, "duration_ms": 61000},
        {"client": "Qwen Code", "application": "qwen", "project": "notes", "tool": "smartsearch", "requests": 74, "successes": 72, "failures": 2, "duration_ms": 52000},
    ],
}

QUOTAS = [
    {"provider": "tavily", "configured": True, "status": "available", "source": "tavily", "unit": "credits",
     "used": 720, "limit": 1000, "remaining": 280, "plan": "Growth", "reset_at": "2026-12-01T00:00:00Z",
     "auto_reset": "monthly", "details": {"account_plan_usage": 720, "account_plan_limit": 1000, "key_usage": 72, "key_limit": 100},
     "local_usage": {"status": "available", "unit": "calls", "used": 64, "limit": 1000, "enforced": True}},
    {"provider": "exa", "configured": True, "status": "available", "source": "exa", "unit": "credits",
     "used": 340, "limit": 1000, "remaining": 660, "plan": "Starter", "reset_at": "2026-12-01T00:00:00Z",
     "auto_reset": "monthly", "local_usage": {"status": "available", "unit": "calls", "used": 41, "limit": 1000, "enforced": True}},
    {"provider": "anysearch", "configured": True, "status": "available", "source": "official", "unit": "credits",
     "used": 48, "limit": 200, "remaining": 152, "plan": "Anonymous", "reset_at": "2026-11-28T00:00:00Z",
     "auto_reset": "daily", "local_usage": {"status": "available", "unit": "calls", "used": 33, "limit": 200, "enforced": True}},
    {"provider": "tinyfish", "configured": True, "status": "available", "source": "tinyfish", "unit": "credits",
     "used": 6200, "limit": 12000, "remaining": 5800, "plan": "Free", "reset_at": "2026-11-28T00:00:00Z",
     "auto_reset": "daily", "details": {"search_usage": 4200, "extract_usage": 2000, "fetch_usage": 620, "fetch_limit": 1000},
     "local_usage": {"status": "available", "unit": "calls", "used": 84, "limit": 12000, "enforced": True}},
    {"provider": "yuanbao", "configured": True, "status": "available", "source": "local", "unit": "calls",
     "used": 420, "limit": 2000, "remaining": 1580, "plan": "Free tier", "reset_at": "2026-12-01T00:00:00Z",
     "auto_reset": "monthly", "local_usage": {"status": "available", "unit": "calls", "used": 420, "limit": 2000, "enforced": True}},
]

SETTINGS_VALUES = {
    "mode": "hybrid",
    "apipool.strategy": "round-robin",
    "apipool.engines": ["tavily", "exa", "anysearch"],
    "apipool.weights": {"tavily": 50, "exa": 30, "anysearch": 20},
    "smartsearch.engine_timeout_sec": 12,
    "smartsearch.enhance": True,
    "smartsearch.off_topic_guard": "shadow",
    "smartsearch.relevance_threshold": 0.05,
    "smartsearch.mmr.enabled": True,
    "cache.enabled": True,
    "cache.cleanup_interval": 60,
    "upstream_timeout_sec": 30,
    "network": "china",
    "bing.enabled": True,
    "baidu_web.enabled": True,
    "duckduckgo.enabled": True,
    "tavily.enabled": True,
    "exa.enabled": True,
    "anysearch.enabled": True,
    "doubao.enabled": True,
    "yuanbao.enabled": True,
    "google.enabled": False,
    "academic.disable_arxiv": False,
    "academic.disable_crossref": False,
    "academic.disable_openalex": False,
    "academic.disable_pubmed": False,
    "academic.disable_semantic_scholar": False,
    "academic.disable_google_scholar": True,
    "academic.disable_europe_pmc": False,
    "academic.disable_dblp": False,
    "academic.disable_doaj": False,
    "smartsearch.engines.tavily.weight": 1,
    "smartsearch.engines.tavily.min_score": 0.3,
}
SETTINGS = {
    "revision": "cfg-20261127-1200",
    "values": SETTINGS_VALUES,
    "saved_values": SETTINGS_VALUES,
    "options": {
        "mode": ["hybrid", "engine", "apipool", "mcphub"],
        "apipool.strategy": ["round-robin", "priority", "weighted"],
        "apipool.engines": ["baidu", "baidu_web", "tavily", "exa", "anysearch", "doubao", "yuanbao", "tinyfish", "keenable", "mcphub"],
        "smartsearch.off_topic_guard": ["shadow", "enforce", "off"],
        "network": ["china", "international"],
    },
    "pending_changes": {},
    "locked_fields": {},
    "field_files": {key: "config.yaml" for key in SETTINGS_VALUES},
    "backups": [
        {"name": "settings-20261127T100000.000000000Z-a1b2.yaml", "created_at": f"{DAY}T10:00:00+08:00"},
        {"name": "settings-20261126T180000.000000000Z-c3d4.yaml", "created_at": "2026-11-26T18:00:00+08:00"},
    ],
    "credential_status": {
        "credentials": {
            "TAVILY_SK": {"count": 2, "configured": True, "multi": True, "read_only": False, "sources": ["config", "overlay"],
                          "slots": [{"id": "slot_1", "label": "primary", "source": "config"}, {"id": "slot_2", "label": "backup", "source": "overlay"}]},
            "EXA_API_KEY": {"count": 1, "configured": True, "multi": False, "read_only": False, "sources": ["config"], "slots": []},
            "ANYSEARCH_API_KEY": {"count": 1, "configured": True, "multi": False, "read_only": False, "sources": ["config"], "slots": []},
            "LLM_API_KEY": {"count": 1, "configured": True, "multi": False, "read_only": False, "sources": ["overlay"], "slots": []},
        },
        "pending": {},
        "pending_restart": False,
        "revision": "cred-20261127-1200",
    },
    "write_access": {"allowed": True, "reason": ""},
    "local_action_allowed": True,
    "admin_password_configured": True,
    "admin_username_configured": False,
}

TRACE_TOOLS = [
    ("smartsearch", "Cursor", "cursor", "docs", 842, 8, "tavily>bing"),
    ("cleanfetch", "Cursor", "cursor", "docs", 316, 1, "local>jina"),
    ("smartsearch", "Claude Code", "claude", "research", 1120, 12, "bing>duckduckgo"),
    ("academicsearch", "Claude Code", "claude", "research", 1860, 6, "arxiv>crossref>openalex"),
    ("smartsearch", "Codex", "codex", "websearch", 690, 9, "exa>tavily"),
    ("pdf_parser", "Cursor", "cursor", "docs", 2480, 1, "mineru"),
    ("smartsearch", "Qwen Code", "qwen", "notes", 760, 7, "anysearch>bing"),
    ("cleanfetch", "Codex", "codex", "websearch", 420, 1, "local"),
]
TRACES = []
start = datetime(2026, 11, 27, 11, 58, tzinfo=TZ)
for index in range(32):
    tool, client, application, project, duration, results, chain = TRACE_TOOLS[index % len(TRACE_TOOLS)]
    failed = index in (4, 11, 19)
    TRACES.append({
        "id": 4800 - index,
        "occurred_at": (start - timedelta(minutes=index * 3)).isoformat(),
        "kind": "tool",
        "tool": tool,
        "provider": "",
        "success": not failed,
        "duration_ms": duration + (index % 5) * 37,
        "cache_hit": index % 9 == 0,
        "result_count": results if not failed else 0,
        "request_id": f"req_{4800 - index}",
        "attempt_chain": chain,
        "query_hash": f"sha256:{index:02d}a4c",
        "query_chars": 18 + index % 12,
        "query_topic": ["technology", "academic", "product"][index % 3],
        "query_language": "zh",
        "query_keywords": ["MCP,搜索", "RAG,评测", "浏览器,自动化"][index % 3],
        "client": client,
        "application": application,
        "project": project,
        "error_kind": "timeout" if failed else "",
        "error_summary": "上游在 12s 内未返回" if failed else "",
    })

DASHBOARD = {
    "overview": OVERVIEW,
    "clients": CLIENTS,
    "calls": TRACES,
    "settings": SETTINGS,
    "quotas": QUOTAS,
    "providers": {"generated_at": OVERVIEW["generated_at"], "summary": OVERVIEW["system"],
                  "providers": WEB + ACADEMIC},
    "storage": {"cache": {"entries": 1842, "bytes": 148_000_000}, "history": {"entries": 3620, "bytes": 24_000_000},
                "fetchdata": {"files": 118, "bytes": 64_000_000}},
    "secrets": SETTINGS["credential_status"],
    "request-events": [],
}

SCREENS = {
    "dashboard-overview.jpg": ("#overview", "多日调用趋势"),
    "dashboard-sources.jpg": ("#sources", "搜索源"),
    "dashboard-usage.jpg": ("#traces", "调用追踪"),
    "dashboard-settings.jpg": ("#settings/routing", "搜索路由"),
}


def mock_api(path: str) -> dict:
    key = path.removeprefix("/__admin/api/").split("?")[0]
    return DASHBOARD.get(key, {})


class QuietHandler(SimpleHTTPRequestHandler):
    def log_message(self, *_args) -> None:
        pass


def main() -> None:
    handler = partial(QuietHandler, directory=str(DIST))
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{server.server_address[1]}/"

    catalogs = {
        lang: json.loads((LOCALES / f"web.{lang}.json").read_text(encoding="utf-8"))
        for lang in ("zh", "en")
    }
    boot = {
        "locale": "zh",
        "brand": {"title": "WebSearch 控制中心", "theme": "green", "accent": "", "logo": "", "footer": True},
        "catalogs": catalogs,
    }

    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1400, "height": 875}, device_scale_factor=2,
                                      color_scheme="light", locale="zh-CN")
        context.add_init_script("""
            try {
              localStorage.setItem('websearch.theme', 'light');
              localStorage.setItem('websearch.language', 'zh');
              localStorage.setItem('websearch.density', 'normal');
            } catch {}
            window.__DEFAULT_LOCALE__ = 'zh';
            window.__I18N_CATALOGS__ = %%CATALOGS%%;
            window.__I18N__ = %%CATALOGS%%.zh;
            window.__BRAND__ = %%BRAND%%;
            document.documentElement.dataset.theme = 'light';
            document.documentElement.dataset.density = 'normal';
            document.documentElement.dataset.brand = 'green';
        """.replace("%%CATALOGS%%", json.dumps(boot["catalogs"], ensure_ascii=False))
           .replace("%%BRAND%%", json.dumps(boot["brand"], ensure_ascii=False)))

        def handle(route) -> None:
            url = urlparse(route.request.url)
            route.fulfill(
                status=200,
                content_type="application/json",
                body=json.dumps(mock_api(url.path), ensure_ascii=False),
                headers={"X-WebSearch-Dashboard-API": "1", "Cache-Control": "no-store"},
            )

        context.route("**/__admin/api/**", handle)
        page = context.new_page()
        page.emulate_media(reduced_motion="reduce")

        for name, (route, marker) in SCREENS.items():
            page.goto(base + route, wait_until="networkidle")
            page.add_style_tag(content="*{animation:none!important;transition:none!important}html{scrollbar-width:none}")
            page.wait_for_timeout(1200)
            try:
                page.get_by_text(marker, exact=False).first.wait_for(state="visible", timeout=15000)
            except Exception:
                page.screenshot(path=str(OUT / "debug-dashboard.png"))
                print(page.locator("body").inner_text()[:3000])
                raise
            page.evaluate("window.scrollTo(0, 0)")
            page.wait_for_timeout(300)
            body_text = page.locator("body").inner_text()
            unresolved = sorted(set(re.findall(r"\bweb\.[a-z0-9_.]+", body_text)))
            if unresolved:
                raise RuntimeError(f"{name}: unresolved i18n keys: {unresolved[:8]}")
            print(name, "theme=", page.evaluate("document.documentElement.dataset.theme"),
                  "body=", body_text[:60].replace("\n", " | "))
            png = OUT / name.replace(".jpg", ".png")
            page.screenshot(path=str(png), full_page=False)
            with Image.open(png) as image:
                image.convert("RGB").save(OUT / name, quality=95, optimize=True)
            png.unlink()
            print(f"wrote {OUT / name}")

        browser.close()
    server.shutdown()


if __name__ == "__main__":
    main()
