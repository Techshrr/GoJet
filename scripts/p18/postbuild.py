#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import xml.etree.ElementTree as ET
from pathlib import Path
from xml.sax.saxutils import escape

ROOT = Path(__file__).resolve().parents[2]
DOCS = ROOT / "frontend/apps/docs"
DIST = DOCS / "dist"
MANIFEST = json.loads((DOCS / "src/data/content-manifest.json").read_text(encoding="utf-8"))
SITE = "https://gojet.cc"


def document_output_path(canonical_path: str) -> Path:
    prefix = "/docs/"
    if not canonical_path.startswith(prefix):
        raise SystemExit(f"invalid Docs canonical path: {canonical_path}")
    relative = canonical_path[len(prefix):].strip("/")
    if not relative:
        raise SystemExit(f"invalid empty Docs canonical path: {canonical_path}")
    return DIST / relative / "index.html"


def normalize_document_canonicals() -> None:
    link_pattern = re.compile(r"<link\b[^>]*>", flags=re.I)
    canonical_rel = re.compile(r"\brel=[\"']canonical[\"']", flags=re.I)
    href_pattern = re.compile(r"(\bhref\s*=\s*[\"'])[^\"']*([\"'])", flags=re.I)

    for entry in MANIFEST["documents"]:
        path = document_output_path(entry["canonicalPath"])
        if not path.is_file():
            raise SystemExit(f"missing manifest Docs output: {path}")
        expected = SITE + entry["canonicalPath"]
        text = path.read_text(encoding="utf-8")
        replacements = 0

        def replace_link(match: re.Match[str]) -> str:
            nonlocal replacements
            tag = match.group(0)
            if not canonical_rel.search(tag):
                return tag
            replacements += 1
            if href_pattern.search(tag):
                return href_pattern.sub(lambda href: href.group(1) + expected + href.group(2), tag, count=1)
            return tag[:-1] + f' href="{expected}">'

        text = link_pattern.sub(replace_link, text)
        if replacements != 1:
            raise SystemExit(f"expected exactly one canonical link for {entry['canonicalPath']}, found {replacements}")
        path.write_text(text, encoding="utf-8")


def strip_search_metadata() -> None:
    for locale in ("en", "zh-CN"):
        path = DIST / locale / "search" / "index.html"
        if not path.is_file():
            raise SystemExit(f"missing static search route: {path}")
        text = path.read_text(encoding="utf-8")
        text = re.sub(r'<link\b(?=[^>]*\brel=["\']canonical["\'])[^>]*>\s*', '', text, flags=re.I)
        text = re.sub(r'<link\b(?=[^>]*\brel=["\']alternate["\'])(?=[^>]*\bhreflang=)[^>]*>\s*', '', text, flags=re.I)
        if not re.search(r'<meta\b(?=[^>]*\bname=["\']robots["\'])[^>]*\bnoindex\b[^>]*>', text, flags=re.I):
            text = text.replace('</head>', '<meta name="robots" content="noindex,nofollow"></head>', 1)
        path.write_text(text, encoding="utf-8")


def remove_search_from_generated_sitemaps() -> None:
    for path in DIST.glob("sitemap*.xml"):
        try:
            tree = ET.parse(path)
        except ET.ParseError:
            continue
        root = tree.getroot()
        if not root.tag.endswith("urlset"):
            continue
        removed = False
        for child in list(root):
            loc = next((node for node in child if node.tag.endswith("loc")), None)
            if loc is not None and loc.text and re.search(r'/docs/(?:en|zh-CN)/search(?:$|[/?#])', loc.text):
                root.remove(child)
                removed = True
        if removed:
            tree.write(path, encoding="utf-8", xml_declaration=True)


def write_locale_sitemap(locale: str) -> None:
    entries = [
        entry for entry in MANIFEST["documents"]
        if entry["locale"] == locale and entry["indexable"] is True and entry["sitemap"] is True and entry["releaseState"] == "published"
    ]
    entries.sort(key=lambda item: item["canonicalPath"])
    lines = ['<?xml version="1.0" encoding="UTF-8"?>', '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">']
    for entry in entries:
        lines.extend([
            '  <url>',
            f'    <loc>{escape(SITE + entry["canonicalPath"])}</loc>',
            f'    <lastmod>{escape(entry["lastUpdated"])}</lastmod>',
            '  </url>',
        ])
    lines.append('</urlset>')
    (DIST / f"sitemap-docs-{locale}.xml").write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> int:
    if not DIST.is_dir():
        raise SystemExit(f"Docs dist missing: {DIST}")
    normalize_document_canonicals()
    strip_search_metadata()
    remove_search_from_generated_sitemaps()
    write_locale_sitemap("en")
    write_locale_sitemap("zh-CN")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
