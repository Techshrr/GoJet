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


def normalize_document_metadata() -> None:
    link_pattern = re.compile(r"<link\b[^>]*>", flags=re.I)
    canonical_rel = re.compile(r"\brel=[\"']canonical[\"']", flags=re.I)
    alternate_rel = re.compile(r"\brel=[\"']alternate[\"']", flags=re.I)
    hreflang_attr = re.compile(r"\bhreflang\s*=", flags=re.I)
    href_pattern = re.compile(r"(\bhref\s*=\s*[\"'])[^\"']*([\"'])", flags=re.I)
    by_path = {entry["canonicalPath"]: entry for entry in MANIFEST["documents"]}
    x_default = MANIFEST["policy"]["xDefault"]

    for entry in MANIFEST["documents"]:
        path = document_output_path(entry["canonicalPath"])
        if not path.is_file():
            raise SystemExit(f"missing manifest Docs output: {path}")
        expected = SITE + entry["canonicalPath"]
        text = path.read_text(encoding="utf-8")
        canonical_replacements = 0

        def replace_link(match: re.Match[str]) -> str:
            nonlocal canonical_replacements
            tag = match.group(0)
            if alternate_rel.search(tag) and hreflang_attr.search(tag):
                return ""
            if not canonical_rel.search(tag):
                return tag
            canonical_replacements += 1
            if href_pattern.search(tag):
                return href_pattern.sub(lambda href: href.group(1) + expected + href.group(2), tag, count=1)
            return tag[:-1] + f' href="{expected}">'

        text = link_pattern.sub(replace_link, text)
        if canonical_replacements != 1:
            raise SystemExit(
                f"expected exactly one canonical link for {entry['canonicalPath']}, found {canonical_replacements}"
            )

        alternates = [(entry["locale"], entry["canonicalPath"])]
        translation = entry["translation"]
        if translation is not None:
            peer = by_path.get(translation)
            if peer is None or peer.get("translation") != entry["canonicalPath"]:
                raise SystemExit(f"invalid reciprocal translation for {entry['canonicalPath']}")
            alternates.append((peer["locale"], peer["canonicalPath"]))
        else:
            # Starlight derives a same-slug locale target even when no translated
            # document exists. P18 ALT-DOCS forbids that fabricated route anywhere
            # in the published HTML, not only in hreflang metadata. Keep the
            # language-switch escape hatch useful by routing the absent locale to
            # that locale's published Docs home instead of a fake article URL.
            other_locale = "zh-CN" if entry["locale"] == "en" else "en"
            fake_path = re.sub(
                r"^/docs/(?:en|zh-CN)/",
                f"/docs/{other_locale}/",
                entry["canonicalPath"],
                count=1,
            )
            locale_home = f"/docs/{other_locale}/"
            if fake_path != locale_home and fake_path not in by_path:
                text = text.replace(fake_path, locale_home)

        if entry["kind"] == "home":
            alternates.append(("x-default", x_default))

        tags = "".join(
            f'<link rel="alternate" hreflang="{escape(locale)}" href="{escape(SITE + canonical_path)}">'
            for locale, canonical_path in alternates
        )
        if "</head>" not in text:
            raise SystemExit(f"missing head close for {entry['canonicalPath']}")
        text = text.replace("</head>", tags + "</head>", 1)
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


def normalize_pagefind_entry() -> None:
    """Canonicalize Pagefind's JSON manifest without changing its semantics.

    With a multilingual index, Pagefind may serialize the language map in a
    different object-key order between otherwise byte-identical builds. The
    browser parses this file as JSON, so object order has no runtime meaning.
    P18 nevertheless requires the deployed static tree itself to be
    deterministic, so normalize the complete object recursively by key before
    the final dist digest is taken.
    """
    path = DIST / "pagefind" / "pagefind-entry.json"
    if not path.is_file():
        raise SystemExit(f"missing Pagefind entry manifest: {path}")
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SystemExit(f"invalid Pagefind entry manifest {path}: {exc}") from exc
    if not isinstance(payload, dict) or not isinstance(payload.get("version"), str):
        raise SystemExit("Pagefind entry manifest must contain a string version")
    languages = payload.get("languages")
    if not isinstance(languages, dict) or not languages:
        raise SystemExit("Pagefind entry manifest must contain at least one language")
    path.write_text(
        json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")),
        encoding="utf-8",
    )


def main() -> int:
    if not DIST.is_dir():
        raise SystemExit(f"Docs dist missing: {DIST}")
    normalize_document_metadata()
    strip_search_metadata()
    remove_search_from_generated_sitemaps()
    write_locale_sitemap("en")
    write_locale_sitemap("zh-CN")
    normalize_pagefind_entry()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
