"""Render README diagrams from readme-diagrams.html via Playwright."""
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent
HTML = ROOT / "readme-diagrams.html"
OUT = ROOT.parent

TARGETS = {
    "hero": "hero-banner.png",
    "architecture": "architecture.png",
    "toolchain": "toolchain.png",
    "pipeline": "pipeline.png",
}


def main() -> None:
    uri = HTML.as_uri()
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(
            viewport={"width": 1280, "height": 3000},
            device_scale_factor=2,
        )
        page.goto(uri, wait_until="networkidle")
        for sid, name in TARGETS.items():
            dest = OUT / name
            page.locator(f"#{sid}").screenshot(path=str(dest), type="png")
            print(f"wrote {dest}")
        browser.close()


if __name__ == "__main__":
    main()
