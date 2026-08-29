#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
from html.parser import HTMLParser
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
DOCS = ROOT / "frontend/apps/docs"
CONTENT = DOCS / "src/content/docs"
MANIFEST_PATH = DOCS / "src/data/content-manifest.json"
EVIDENCE_ROOT = ROOT / "artifacts/v10/P18"
API_SOURCE = ROOT / "services/platformapi/cmd/server/admin_access.go"
NGINX_FRAGMENT = ROOT / "deploy/nginx/docs-p18.conf"
PUBLIC_BASE = "https://gojet.cc"
HTTP_BASE = os.environ.get("P18_HTTP_BASE", "").rstrip("/")

REQUIRED_FRONTMATTER = ("title", "description", "locale", "lastUpdated", "canonicalPath", "translation", "contentOwner")

API_ROUTES = {
    "CAP-API-KEYS": [
        "GET /api/workspaces/{workspaceId}/api-keys",
        "POST /api/workspaces/{workspaceId}/api-keys",
        "POST /api/workspaces/{workspaceId}/api-keys/{keyId}/rotate",
        "POST /api/workspaces/{workspaceId}/api-keys/{keyId}/revoke",
    ],
    "CAP-USER-WEBHOOKS": [
        "GET /api/workspaces/{workspaceId}/webhooks",
        "POST /api/workspaces/{workspaceId}/webhooks",
        "GET /api/workspaces/{workspaceId}/webhooks/{webhookId}",
        "POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/rotate-secret",
        "POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/enable",
        "POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/disable",
        "GET /api/workspaces/{workspaceId}/webhooks/{webhookId}/deliveries",
        "POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/deliveries/{deliveryId}/retry",
    ],
}

class HTMLFacts(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.title_parts: list[str] = []
        self.in_title = False
        self.h1 = 0
        self.meta: list[dict[str, str]] = []
        self.links: list[dict[str, str]] = []
        self.text_parts: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = {k.lower(): (v or "") for k, v in attrs}
        if tag.lower() == "title":
            self.in_title = True
        elif tag.lower() == "h1":
            self.h1 += 1
        elif tag.lower() == "meta":
            self.meta.append(values)
        elif tag.lower() == "link":
            self.links.append(values)

    def handle_endtag(self, tag: str) -> None:
        if tag.lower() == "title":
            self.in_title = False

    def handle_data(self, data: str) -> None:
        value = data.strip()
        if not value:
            return
        self.text_parts.append(value)
        if self.in_title:
            self.title_parts.append(value)

    @property
    def title(self) -> str:
        return " ".join(self.title_parts).strip()

    @property
    def text(self) -> str:
        return " ".join(self.text_parts)


def git_head() -> str:
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def load_manifest() -> dict[str, Any]:
    data = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    assert data["schema"] == "gojet.docs-manifest.v1", data
    assert data["node"] == "P18", data
    return data


def parse_frontmatter(path: Path) -> dict[str, Any]:
    text = path.read_text(encoding="utf-8")
    match = re.match(r"\A---\n(.*?)\n---\n", text, flags=re.S)
    if not match:
        raise AssertionError(f"missing frontmatter: {path}")
    result: dict[str, Any] = {}
    for raw in match.group(1).splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        if ":" not in raw:
            raise AssertionError(f"unsupported frontmatter line in {path}: {raw!r}")
        key, value = raw.split(":", 1)
        value = value.strip()
        if value == "null":
            parsed: Any = None
        elif len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            parsed = value[1:-1]
        else:
            parsed = value
        result[key.strip()] = parsed
    return result


def source_path(entry: dict[str, Any]) -> Path:
    return CONTENT / entry["source"]


def source_digest(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def normalize_canonical(path: str) -> str:
    if path in ("/docs/en/", "/docs/zh-CN/"):
        return path
    return path[:-1] if path.endswith("/") else path


def request(path: str) -> tuple[int, str, dict[str, str]]:
    if not HTTP_BASE:
        raise AssertionError("P18_HTTP_BASE is required for raw-HTTP cases")
    req = urllib.request.Request(HTTP_BASE + path, headers={"User-Agent": "GoJet-P18-evidence/1"})
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, response.read().decode("utf-8", "replace"), dict(response.headers.items())
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", "replace"), dict(exc.headers.items())


def html_facts(body: str) -> HTMLFacts:
    parser = HTMLFacts()
    parser.feed(body)
    return parser


def canonical_links(facts: HTMLFacts) -> list[str]:
    return [item.get("href", "") for item in facts.links if "canonical" in item.get("rel", "").lower().split()]


def alternates(facts: HTMLFacts) -> dict[str, set[str]]:
    result: dict[str, set[str]] = {}
    for item in facts.links:
        if "alternate" not in item.get("rel", "").lower().split():
            continue
        language = item.get("hreflang", "")
        href = item.get("href", "")
        if language and href:
            result.setdefault(language, set()).add(href)
    return result


def description(facts: HTMLFacts) -> str:
    for item in facts.meta:
        if item.get("name", "").lower() == "description":
            return item.get("content", "").strip()
    return ""


def robots_noindex(facts: HTMLFacts) -> bool:
    return any(item.get("name", "").lower() == "robots" and "noindex" in item.get("content", "").lower() for item in facts.meta)


def evidence_path(case_id: str) -> Path:
    plan = json.loads((EVIDENCE_ROOT / "test-plan.json").read_text(encoding="utf-8"))
    row = next(item for item in plan["cases"] if item["id"] == case_id)
    return ROOT / row["evidence"]


def write_evidence(case_id: str, facts: dict[str, Any]) -> None:
    path = evidence_path(case_id)
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = {"case": case_id, "status": "PASS", "implementation_commit": git_head(), "secret_safe": True, **facts}
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True))


def t001(manifest: dict[str, Any]) -> dict[str, Any]:
    homes = [entry for entry in manifest["documents"] if entry["kind"] == "home"]
    assert {entry["locale"] for entry in homes} == {"en", "zh-CN"}
    observed: dict[str, Any] = {}
    for entry in homes:
        status, body, _ = request(entry["canonicalPath"])
        facts = html_facts(body)
        assert status == 200, (entry["canonicalPath"], status)
        assert facts.h1 == 1, (entry["canonicalPath"], facts.h1)
        assert facts.title.strip(), entry["canonicalPath"]
        assert description(facts) == entry["description"], (entry["canonicalPath"], description(facts))
        assert len(facts.text) > 120, entry["canonicalPath"]
        observed[entry["locale"]] = {"path": entry["canonicalPath"], "status": status, "h1": facts.h1, "title": facts.title, "description": description(facts), "initial_html_text_length": len(facts.text)}
    assert observed["en"]["title"] != observed["zh-CN"]["title"]
    return {"homes": observed, "raw_html_primary_content": True}


def t002(manifest: dict[str, Any]) -> dict[str, Any]:
    checked = []
    for entry in manifest["documents"]:
        path = source_path(entry)
        assert path.is_file(), path
        fm = parse_frontmatter(path)
        for key in REQUIRED_FRONTMATTER:
            assert key in fm, (entry["source"], key)
        assert fm["locale"] == entry["locale"]
        assert fm["canonicalPath"] == entry["canonicalPath"]
        assert fm["translation"] == entry["translation"]
        assert fm["contentOwner"] == entry["contentOwner"]
        assert fm["lastUpdated"] == entry["lastUpdated"]
        assert fm["title"] == entry["title"]
        assert fm["description"] == entry["description"]
        assert source_digest(path) == entry["sourceDigest"], entry["source"]
        checked.append({"source": entry["source"], "owner": entry["contentOwner"], "lastUpdated": entry["lastUpdated"], "digest": entry["sourceDigest"]})
    return {"required_fields": list(REQUIRED_FRONTMATTER), "documents": checked, "build_time_lastmod_used": False}


def t003(manifest: dict[str, Any]) -> dict[str, Any]:
    checked = []
    for entry in manifest["documents"]:
        expected_path = normalize_canonical(entry["canonicalPath"])
        if entry["kind"] == "home":
            expected_path = entry["canonicalPath"]
        assert entry["canonicalPath"] == expected_path
        probe = entry["canonicalPath"] + ("&utm_source=p18" if "?" in entry["canonicalPath"] else "?utm_source=p18")
        status, body, _ = request(probe)
        facts = html_facts(body)
        assert status == 200, (entry["canonicalPath"], status)
        links = canonical_links(facts)
        expected = PUBLIC_BASE + entry["canonicalPath"]
        assert expected in links, (entry["canonicalPath"], links, expected)
        assert all("?" not in link for link in links), (entry["canonicalPath"], links)
        for segment in entry["canonicalPath"].split("/"):
            if segment and segment != "zh-CN":
                assert segment == segment.lower(), entry["canonicalPath"]
        checked.append({"path": entry["canonicalPath"], "canonical": expected, "canonical_links": sorted(set(links))})
    return {"documents": checked, "query_parameters_excluded": True}


def t004(manifest: dict[str, Any]) -> dict[str, Any]:
    by_path = {entry["canonicalPath"]: entry for entry in manifest["documents"]}
    checked = []
    for entry in manifest["documents"]:
        if not entry["translation"]:
            continue
        peer = by_path.get(entry["translation"])
        assert peer is not None
        assert peer["translation"] == entry["canonicalPath"], (entry["canonicalPath"], peer)
        status, body, _ = request(entry["canonicalPath"])
        assert status == 200
        alt = alternates(html_facts(body))
        assert PUBLIC_BASE + entry["canonicalPath"] in alt.get(entry["locale"], set()), (entry["canonicalPath"], alt)
        assert PUBLIC_BASE + peer["canonicalPath"] in alt.get(peer["locale"], set()), (entry["canonicalPath"], alt)
        if entry["kind"] == "home":
            assert PUBLIC_BASE + manifest["policy"]["xDefault"] in alt.get("x-default", set()), alt
        checked.append({"path": entry["canonicalPath"], "alternates": {k: sorted(v) for k, v in alt.items()}})
    return {"reciprocal_pairs_checked": len(checked), "documents": checked}


def t005(manifest: dict[str, Any]) -> dict[str, Any]:
    candidates = [entry for entry in manifest["documents"] if entry["translation"] is None]
    assert candidates, "frozen P18 content set must include an untranslated published document"
    checked = []
    all_paths = {entry["canonicalPath"] for entry in manifest["documents"]}
    for entry in candidates:
        assert entry["releaseState"] == "published"
        status, body, _ = request(entry["canonicalPath"])
        assert status == 200
        alt = alternates(html_facts(body))
        other_locale = "zh-CN" if entry["locale"] == "en" else "en"
        assert not alt.get(other_locale), (entry["canonicalPath"], alt)
        expected_fake = re.sub(r"/docs/(?:en|zh-CN)/", f"/docs/{other_locale}/", entry["canonicalPath"], count=1)
        assert expected_fake not in all_paths
        assert expected_fake not in body
        checked.append({"path": entry["canonicalPath"], "absent_translation": expected_fake, "alternates": {k: sorted(v) for k, v in alt.items()}})
    return {"documents": checked, "fabricated_translation_count": 0}


def t006(manifest: dict[str, Any]) -> dict[str, Any]:
    config = NGINX_FRAGMENT.read_text(encoding="utf-8")
    checked = []
    for entry in manifest["withdrawn"]:
        assert entry["status"] == 410
        assert f"location = {entry['canonicalPath']}" in config
        status, body, _ = request(entry["canonicalPath"])
        assert status == 410, (entry["canonicalPath"], status)
        facts = html_facts(body)
        assert not canonical_links(facts)
        assert not alternates(facts)
        checked.append({"path": entry["canonicalPath"], "status": status})
    unknown = "/docs/en/this-document-does-not-exist"
    status, body, _ = request(unknown)
    assert status == manifest["policy"]["unknownStatus"] == 404, status
    facts = html_facts(body)
    assert not canonical_links(facts)
    assert not alternates(facts)
    return {"withdrawn": checked, "unknown": {"path": unknown, "status": status}, "soft_404_count": 0}


def t007(manifest: dict[str, Any]) -> dict[str, Any]:
    source = API_SOURCE.read_text(encoding="utf-8")
    api_docs = [entry for entry in manifest["documents"] if entry["kind"] == "api"]
    assert api_docs
    observed = []
    for entry in api_docs:
        authority = manifest["apiReleaseAuthority"].get(entry["capability"])
        assert authority and authority["released"] is True, entry
        assert authority["node"] == "P17"
        assert authority["signedSource"] == "5818406072a131db1c7d8aa7bc5ef8a7adc8d51f"
        page = source_path(entry).read_text(encoding="utf-8")
        for route in API_ROUTES[entry["capability"]]:
            assert route in source, (entry["capability"], route)
            assert route in page, (entry["source"], route)
        status, body, _ = request(entry["canonicalPath"])
        facts = html_facts(body)
        assert status == 200
        assert not robots_noindex(facts)
        assert PUBLIC_BASE + entry["canonicalPath"] in canonical_links(facts)
        observed.append({"path": entry["canonicalPath"], "capability": entry["capability"], "signed_source": authority["signedSource"], "routes_checked": API_ROUTES[entry["capability"]]})
    api_index_entries = [entry for entry in manifest["documents"] if entry["source"].endswith("/api/index.mdx")]
    assert len(api_index_entries) == 2
    for entry in api_index_entries:
        status, body, _ = request(entry["canonicalPath"])
        assert status == 200
        text = html_facts(body).text
        if entry["locale"] == "en":
            assert "not released" in text.lower()
        else:
            assert "尚未发布" in text
    return {"published_api_documents": observed, "publication_policy": manifest["policy"]["apiPublication"], "unreleased_route_fabrication_count": 0}


CASES = {"P18-T001": t001, "P18-T002": t002, "P18-T003": t003, "P18-T004": t004, "P18-T005": t005, "P18-T006": t006, "P18-T007": t007}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    args = parser.parse_args()
    manifest = load_manifest()
    try:
        result = CASES[args.case](manifest)
    except Exception as exc:
        print(f"{args.case} FAIL: {exc}", file=sys.stderr)
        raise
    write_evidence(args.case, result)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
