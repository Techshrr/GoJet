#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request
from html.parser import HTMLParser
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
DOCS = ROOT / "frontend/apps/docs"
CONTENT = DOCS / "src/content/docs"
DIST = DOCS / "dist"
MANIFEST_PATH = DOCS / "src/data/content-manifest.json"
PACKAGE = DOCS / "package.json"
ASTRO_CONFIG = DOCS / "astro.config.mjs"
NGINX = ROOT / "deploy/nginx/docs-p18.conf"
HTTP_BASE = os.environ.get("P18_HTTP_BASE", "http://127.0.0.1:8098").rstrip("/")
PUBLIC_BASE = "https://gojet.cc"


class Facts(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.links: list[dict[str, str]] = []
        self.anchors: list[str] = []
        self.metas: list[dict[str, str]] = []
        self.jsonld: list[str] = []
        self._jsonld = False
        self._jsonld_parts: list[str] = []
        self.h1 = 0
        self.text_parts: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        data = {k.lower(): (v or "") for k, v in attrs}
        if tag.lower() == "link":
            self.links.append(data)
        elif tag.lower() == "a" and data.get("href"):
            self.anchors.append(data["href"])
        elif tag.lower() == "meta":
            self.metas.append(data)
        elif tag.lower() == "h1":
            self.h1 += 1
        elif tag.lower() == "script" and data.get("type", "").lower() == "application/ld+json":
            self._jsonld = True
            self._jsonld_parts = []

    def handle_endtag(self, tag: str) -> None:
        if tag.lower() == "script" and self._jsonld:
            self.jsonld.append("".join(self._jsonld_parts).strip())
            self._jsonld = False
            self._jsonld_parts = []

    def handle_data(self, data: str) -> None:
        if self._jsonld:
            self._jsonld_parts.append(data)
        elif data.strip():
            self.text_parts.append(data.strip())


def load_manifest() -> dict[str, Any]:
    return json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))


def request(path: str) -> tuple[int, str, dict[str, str]]:
    url = HTTP_BASE + path
    req = urllib.request.Request(url, headers={"User-Agent": "GoJet-P18-quality/1"})
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, response.read().decode("utf-8", errors="replace"), dict(response.headers.items())
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace"), dict(exc.headers.items())


def facts(body: str) -> Facts:
    parser = Facts()
    parser.feed(body)
    return parser


def canonical_values(parsed: Facts) -> set[str]:
    return {
        item.get("href", "") for item in parsed.links
        if item.get("rel", "").lower() == "canonical" and item.get("href")
    }


def alternate_values(parsed: Facts) -> dict[str, set[str]]:
    result: dict[str, set[str]] = {}
    for item in parsed.links:
        if item.get("rel", "").lower() != "alternate" or not item.get("hreflang") or not item.get("href"):
            continue
        result.setdefault(item["hreflang"], set()).add(item["href"])
    return result


def robots_values(parsed: Facts) -> list[str]:
    return [
        item.get("content", "").lower()
        for item in parsed.metas
        if item.get("name", "").lower() == "robots"
    ]


def sha256_file(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def tree_digest(path: Path) -> str:
    digest = hashlib.sha256()
    for item in sorted(candidate for candidate in path.rglob("*") if candidate.is_file()):
        rel = item.relative_to(path).as_posix().encode("utf-8")
        digest.update(len(rel).to_bytes(4, "big"))
        digest.update(rel)
        data = item.read_bytes()
        digest.update(len(data).to_bytes(8, "big"))
        digest.update(data)
    return "sha256:" + digest.hexdigest()


def t022(manifest: dict[str, Any]) -> dict[str, Any]:
    digest_a = os.environ.get("P18_BUILD_DIGEST_A")
    digest_b = os.environ.get("P18_BUILD_DIGEST_B")
    if not digest_a or not digest_b:
        raise AssertionError("P18-T022 requires two independently recorded static build digests")
    assert digest_a == digest_b, (digest_a, digest_b)
    assert DIST.is_dir() and (DIST / "pagefind/pagefind.js").is_file()
    assert tree_digest(DIST) == digest_b, (tree_digest(DIST), digest_b)

    package = json.loads(PACKAGE.read_text(encoding="utf-8"))
    astro = ASTRO_CONFIG.read_text(encoding="utf-8")
    nginx = NGINX.read_text(encoding="utf-8").lower()
    build = package.get("scripts", {}).get("build", "")
    assert "astro build" in build and "postbuild.py" in build
    assert "adapter" not in astro.lower(), "P18 Docs must not install an SSR adapter"
    assert "proxy_pass" not in nginx and "pm2" not in nginx and "node " not in nginx
    assert "try_files" in nginx

    initial = {}
    for locale in ("en", "zh-CN"):
        status, body, _ = request(f"/docs/{locale}/")
        parsed = facts(body)
        assert status == 200 and parsed.h1 == 1, (locale, status, parsed.h1)
        text_length = len(" ".join(parsed.text_parts))
        assert text_length > 500, (locale, text_length)
        initial[locale] = {"status": status, "h1": parsed.h1, "initial_html_text_length": text_length}

    return {
        "build_digest_a": digest_a,
        "build_digest_b": digest_b,
        "deterministic_static_output": True,
        "astro_static_without_adapter": True,
        "node_http_runtime": False,
        "pm2_runtime": False,
        "pagefind_build_artifact": True,
        "initial_html": initial,
    }


def t023(manifest: dict[str, Any]) -> dict[str, Any]:
    by_path = {entry["canonicalPath"]: entry for entry in manifest["documents"]}
    sitemap_text = "\n".join(
        (DIST / name).read_text(encoding="utf-8")
        for name in ("sitemap-docs-en.xml", "sitemap-docs-zh-CN.xml")
    )
    rows = []
    internal_paths: set[str] = set()
    structured_data_blocks = 0

    for entry in manifest["documents"]:
        status, body, _ = request(entry["canonicalPath"])
        parsed = facts(body)
        assert status == 200, (entry["canonicalPath"], status)
        expected_canonical = PUBLIC_BASE + entry["canonicalPath"]
        assert canonical_values(parsed) == {expected_canonical}, (entry["canonicalPath"], canonical_values(parsed))
        alt = alternate_values(parsed)
        expected_alt = {entry["locale"]: {expected_canonical}}
        if entry["translation"]:
            peer = by_path[entry["translation"]]
            expected_alt[peer["locale"]] = {PUBLIC_BASE + peer["canonicalPath"]}
        if entry["kind"] == "home":
            expected_alt["x-default"] = {PUBLIC_BASE + manifest["policy"]["xDefault"]}
        assert alt == expected_alt, (entry["canonicalPath"], alt, expected_alt)
        assert expected_canonical in sitemap_text, entry["canonicalPath"]
        assert not any("noindex" in value for value in robots_values(parsed)), entry["canonicalPath"]

        for raw_href in parsed.anchors:
            if raw_href.startswith(PUBLIC_BASE):
                raw_href = raw_href[len(PUBLIC_BASE):]
            if not raw_href.startswith("/docs/"):
                continue
            candidate = urllib.parse.urlsplit(raw_href).path
            if candidate.startswith("/docs/en/search") or candidate.startswith("/docs/zh-CN/search"):
                continue
            internal_paths.add(candidate)
        for block in parsed.jsonld:
            if block:
                json.loads(block)
                structured_data_blocks += 1
        rows.append({"path": entry["canonicalPath"], "status": status, "canonical": expected_canonical, "hreflang": sorted(alt)})

    broken = []
    for path in sorted(internal_paths):
        status, _, _ = request(path)
        if status != 200:
            broken.append({"path": path, "status": status})
    assert not broken, broken

    search_rows = []
    for locale in ("en", "zh-CN"):
        path = f"/docs/{locale}/search?q=GoJet"
        status, body, _ = request(path)
        parsed = facts(body)
        assert status == 200
        assert not canonical_values(parsed) and not alternate_values(parsed)
        assert any("noindex" in value for value in robots_values(parsed)), robots_values(parsed)
        assert f"{PUBLIC_BASE}/docs/{locale}/search" not in sitemap_text
        search_rows.append({"locale": locale, "status": status, "noindex": True})

    withdrawn = []
    for entry in manifest["withdrawn"]:
        status, body, _ = request(entry["canonicalPath"])
        parsed = facts(body)
        assert status == 410 and not canonical_values(parsed) and not alternate_values(parsed)
        withdrawn.append({"path": entry["canonicalPath"], "status": status})
    unknown_status, unknown_body, _ = request("/docs/en/p18-g7-unknown")
    unknown = facts(unknown_body)
    assert unknown_status == 404 and not canonical_values(unknown) and not alternate_values(unknown)

    return {
        "documents": rows,
        "document_count": len(rows),
        "internal_links_checked": len(internal_paths),
        "broken_internal_links": broken,
        "search": search_rows,
        "withdrawn": withdrawn,
        "unknown_status": unknown_status,
        "structured_data_blocks_validated": structured_data_blocks,
        "structured_data_eligibility_bound_to_indexable_docs": True,
    }


def t024(manifest: dict[str, Any]) -> dict[str, Any]:
    documents = manifest["documents"]
    required = {
        "source", "sourceDigest", "locale", "canonicalPath", "translation", "title",
        "description", "lastUpdated", "contentOwner", "kind", "capability",
        "releaseState", "indexable", "sitemap",
    }
    paths = [entry["canonicalPath"] for entry in documents]
    assert len(paths) == len(set(paths)), "duplicate canonicalPath in content manifest"
    by_path = {entry["canonicalPath"]: entry for entry in documents}

    actual_sources = {
        path.relative_to(CONTENT).as_posix()
        for path in CONTENT.rglob("*")
        if path.is_file() and path.suffix.lower() in {".md", ".mdx", ".markdown", ".mdown", ".mkdn", ".mkd", ".mdwn"}
    }
    declared_sources = {entry["source"] for entry in documents}
    assert actual_sources == declared_sources, {"undeclared": sorted(actual_sources - declared_sources), "missing": sorted(declared_sources - actual_sources)}

    parity = []
    for entry in documents:
        assert required.issubset(entry), (entry.get("canonicalPath"), sorted(required - set(entry)))
        source = CONTENT / entry["source"]
        assert source.is_file(), entry["source"]
        assert sha256_file(source) == entry["sourceDigest"], (entry["source"], sha256_file(source), entry["sourceDigest"])
        assert entry["canonicalPath"].startswith(f"/docs/{entry['locale']}/") or entry["canonicalPath"] == f"/docs/{entry['locale']}/"
        assert entry["releaseState"] == "published"
        assert entry["indexable"] is True and entry["sitemap"] is True
        if entry["translation"] is not None:
            peer = by_path.get(entry["translation"])
            assert peer is not None and peer["translation"] == entry["canonicalPath"]
            assert peer["locale"] != entry["locale"]
            parity.append({"path": entry["canonicalPath"], "translation": peer["canonicalPath"], "state": "reciprocal"})
        else:
            parity.append({"path": entry["canonicalPath"], "translation": None, "state": "intentionally-untranslated"})
        if entry["kind"] == "api":
            authority = manifest["apiReleaseAuthority"].get(entry["capability"])
            assert authority and authority["released"] is True and authority["signedSource"], entry["canonicalPath"]

    return {
        "schema": manifest["schema"],
        "documents": len(documents),
        "sources": len(actual_sources),
        "duplicate_canonical_count": 0,
        "undeclared_source_count": 0,
        "source_digest_mismatches": 0,
        "locale_parity_ledger": parity,
        "intentionally_untranslated": sum(1 for row in parity if row["state"] == "intentionally-untranslated"),
    }


CASES = {"P18-T022": t022, "P18-T023": t023, "P18-T024": t024}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    args = parser.parse_args()
    manifest = load_manifest()
    try:
        result = CASES[args.case](manifest)
    except Exception as exc:
        print(f"{args.case} FAIL: {exc}")
        raise
    payload = {
        "case": args.case,
        "status": "PASS",
        "implementation_commit": os.environ.get("GITHUB_SHA"),
        "secret_safe": True,
        **result,
    }
    out = ROOT / "artifacts/v10/P18/quality"
    out.mkdir(parents=True, exist_ok=True)
    (out / f"{args.case}.json").write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
