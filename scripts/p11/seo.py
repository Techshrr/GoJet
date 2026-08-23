#!/usr/bin/env python3
from __future__ import annotations

import argparse
import subprocess
from pathlib import Path

from integration_common import *


def assert_noindex_headers(headers, label: str) -> None:
    lower = headers_lower(headers)
    expect(lower.get("x-robots-tag", "").lower() == "noindex, nofollow", f"{label} X-Robots-Tag={lower.get('x-robots-tag')}")
    expect("no-store" in lower.get("cache-control", "").lower(), f"{label} Cache-Control={lower.get('cache-control')}")


def assert_html_noindex(raw: bytes, label: str) -> str:
    html = body_text(raw).lower()
    expect('<meta name="robots" content="noindex,nofollow">' in html, f"{label} robots meta missing")
    return html


def case_t013():
    published_ws = "ws-p11-013-published"
    published = create_page(published_ws, title="Published Bio", links=[])
    expect(transition_page(published_ws, published["id"], 1, "publish")[0] == 200, "published fixture transition failed")

    paused_ws = "ws-p11-013-paused"
    paused = create_page(paused_ws, title="Paused Bio", links=[])
    paused_published = transition_page(paused_ws, paused["id"], 1, "publish")[3]
    expect(transition_page(paused_ws, paused["id"], paused_published["version"], "pause")[0] == 200, "paused fixture transition failed")

    blocked_ws = "ws-p11-013-blocked"
    blocked = create_page(blocked_ws, title="Blocked child Bio", links=[child("Blocked", "https://example.com/blocked-seo", 0)])
    seed_risk(blocked["links"][0], "allow")
    expect(transition_page(blocked_ws, blocked["id"], 1, "publish")[0] == 200, "blocked fixture publish failed")
    seed_risk(blocked["links"][0], "block")

    removed_ws = "ws-p11-013-removed"
    removed = create_page(removed_ws, title="Removed Bio", links=[])
    expect(delete_page(removed_ws, removed["id"], 1)[0] == 204, "removed fixture delete failed")

    draft_ws = "ws-p11-013-draft"
    draft = create_page(draft_ws, title="Draft Bio", links=[])

    fixtures = [
        ("published", published["slug"], 200),
        ("paused", paused["slug"], 200),
        ("risk-blocked-child", blocked["slug"], 200),
        ("removed", removed["slug"], 410),
        ("draft", draft["slug"], 404),
        ("unknown", "p11-seo-unknown-013", 404),
    ]
    checks = []
    for label, slug, expected in fixtures:
        html_status, html_headers, html_raw = public_page(slug)
        expect(html_status == expected, f"{label} HTML status={html_status}, want={expected}")
        assert_noindex_headers(html_headers, f"{label} HTML")
        assert_html_noindex(html_raw, f"{label} HTML")

        api_status, api_headers, api_raw = public_api(slug)
        expect(api_status == expected, f"{label} API status={api_status}, want={expected}")
        assert_noindex_headers(api_headers, f"{label} API")
        checks.append({
            "label": label,
            "html_status": html_status,
            "api_status": api_status,
            "html_x_robots_tag": headers_lower(html_headers).get("x-robots-tag"),
            "api_x_robots_tag": headers_lower(api_headers).get("x-robots-tag"),
            "api_body_has_workspace_id": "workspace_id" in body_text(api_raw),
        })
    return {"checks": checks}


def sitemap_hits() -> list[str]:
    command = r'''files=$(find . -type f \( -iname '*sitemap*.xml' -o -iname '*sitemap*.txt' -o -iname '*sitemap*.json' \) -not -path './.git/*' -not -path './artifacts/v10/P11/sitemap/*' -print); if [ -n "$files" ]; then grep -nH -E '(^|["> ])/?p/' $files || true; fi'''
    proc = subprocess.run(["bash", "-lc", command], text=True, capture_output=True, check=True)
    return [line for line in proc.stdout.splitlines() if line.strip()]


def case_t014():
    workspace = "ws-p11-014"
    page = create_page(workspace, title="Sitemap probe", links=[])
    expect(transition_page(workspace, page["id"], 1, "publish")[0] == 200, "publish setup failed")
    status, headers, raw = public_page(page["slug"])
    expect(status == 200, f"published status={status}")
    assert_noindex_headers(headers, "published HTML")
    html = assert_html_noindex(raw, "published HTML")
    expect('rel="canonical"' not in html and "rel='canonical'" not in html, "PUB-BIO emitted canonical")
    expect("hreflang=" not in html, "PUB-BIO emitted hreflang")
    expect("application/ld+json" not in html, "PUB-BIO emitted structured data")

    draft = create_page("ws-p11-014-draft", title="Draft", links=[])
    draft_status = public_page(draft["slug"])[0]
    expect(draft_status == 404, f"draft soft-404 status={draft_status}")

    removed = create_page("ws-p11-014-removed", title="Removed", links=[])
    expect(delete_page("ws-p11-014-removed", removed["id"], 1)[0] == 204, "removed setup failed")
    removed_status = public_page(removed["slug"])[0]
    expect(removed_status == 410, f"removed soft-404 status={removed_status}")

    unknown_status = public_page("p11-t014-unknown-slug")[0]
    expect(unknown_status == 404, f"unknown soft-404 status={unknown_status}")

    hits = sitemap_hits()
    expect(hits == [], f"Bio UGC appeared in sitemap files: {hits}")
    return {
        "canonical_present": False,
        "hreflang_present": False,
        "structured_data_present": False,
        "sitemap_bio_hits": hits,
        "statuses": {"published": status, "draft": draft_status, "removed": removed_status, "unknown": unknown_status},
    }


def source_opt_in_hits() -> list[str]:
    forbidden = ("indexable", "allow_index", "index_opt_in", "enable_index", "canonical_url", "sitemap_membership")
    files = [
        Path("internal/bio/httpapi.go"),
        Path("internal/bio/store.go"),
        Path("migrations/000007_bio.sql"),
        Path("frontend/packages/api-client/src/bio.ts"),
        Path("frontend/apps/workspace/src/routes/BioListPage.tsx"),
        Path("frontend/apps/workspace/src/routes/BioDetailPage.tsx"),
    ]
    hits = []
    for path in files:
        text = path.read_text(encoding="utf-8").lower()
        for token in forbidden:
            if token in text:
                hits.append(f"{path}:{token}")
    return hits


def case_t015():
    workspace = "ws-p11-015"
    create_status, _, create_raw, _ = json_request(
        "POST",
        f"/api/workspaces/{workspace}/bio-pages",
        body={"title": "Index opt-in attempt", "bio": "x", "links": [], "indexable": True, "change_reason": "must reject"},
        workspace=workspace,
    )
    expect(create_status == 400, f"unknown indexable create field status={create_status} body={create_raw[:200]!r}")

    page = create_page(workspace, title="Permanent noindex", links=[])
    update_status, _, update_raw, _ = json_request(
        "PATCH",
        f"/api/workspaces/{workspace}/bio-pages/{page['id']}",
        body={"expected_version": page["version"], "indexable": True, "change_reason": "must reject"},
        workspace=workspace,
    )
    expect(update_status == 400, f"unknown indexable update field status={update_status} body={update_raw[:200]!r}")

    published = transition_page(workspace, page["id"], page["version"], "publish")
    expect(published[0] == 200, "valid publish failed after rejected opt-in input")

    query_status, query_headers, query_raw = http_request("GET", f"/p/{urllib.parse.quote(page['slug'], safe='')}?index=1&canonical=1")
    expect(query_status == 200, f"public opt-in query status={query_status}")
    assert_noindex_headers(query_headers, "query opt-in HTML")
    html = assert_html_noindex(query_raw, "query opt-in HTML")
    expect('rel="canonical"' not in html and "hreflang=" not in html and "application/ld+json" not in html, "query expanded index surface")

    hits = source_opt_in_hits()
    expect(hits == [], f"deferred index authority appeared in source/schema: {hits}")
    columns = mysql_scalar("SHOW COLUMNS FROM bio_pages")
    lowered_columns = columns.lower()
    expect("indexable" not in lowered_columns and "canonical" not in lowered_columns and "sitemap" not in lowered_columns, "Bio persistence gained index authority")
    return {
        "create_unknown_field_status": create_status,
        "update_unknown_field_status": update_status,
        "query_status": query_status,
        "query_x_robots_tag": headers_lower(query_headers).get("x-robots-tag"),
        "forbidden_source_hits": hits,
        "persisted_index_authority": False,
    }


CASES = {"P11-T013": case_t013, "P11-T014": case_t014, "P11-T015": case_t015}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    args = parser.parse_args()
    errors, observations = [], {}
    try:
        observations = CASES[args.case]()
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    directory = SITEMAP_DIR if args.case == "P11-T014" else HEADER_DIR
    path = record(args.case, observations, errors, directory)
    print(path.read_text())
    if errors:
        return 1
    print(f"{args.case} PASS on {HEAD}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
