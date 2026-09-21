#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import urllib.request
from pathlib import Path

from common import HEAD, ROOT, ancestor, emit, fail_if_errors
from traceability_cases import INTEGRATIONS

REPO = os.environ.get("GITHUB_REPOSITORY", "Techshrr/GoJet")
TOKEN = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN") or ""


def github_commit(sha: str) -> dict:
    if not TOKEN:
        raise RuntimeError("GH_TOKEN/GITHUB_TOKEN is required for live authority binding")
    request = urllib.request.Request(
        f"https://api.github.com/repos/{REPO}/commits/{sha}",
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {TOKEN}",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "gojet-p20-authority",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.loads(response.read())


def main() -> int:
    errors: list[str] = []
    ledger = []
    for node, integration in INTEGRATIONS.items():
        in_ancestry = ancestor(integration)
        if not in_ancestry:
            errors.append(f"{node} integration is not in candidate ancestry: {integration}")
        live_exists = False
        live_url = None
        try:
            live = github_commit(integration)
            live_exists = live.get("sha") == integration
            live_url = live.get("html_url")
            if not live_exists:
                errors.append(f"{node} live GitHub integration identity mismatch")
        except Exception as exc:
            errors.append(f"{node} live GitHub integration lookup failed: {exc}")
        ledger.append({
            "node": node,
            "integration": integration,
            "in_head_ancestry": in_ancestry,
            "github_commit_exists": live_exists,
            "github_url": live_url,
        })

    review_rows = []
    for number in range(4, 20):
        node = f"P{number:02d}"
        path = ROOT / f"artifacts/v10/{node}/review.md"
        if not path.is_file():
            errors.append(f"{node} signed review file missing")
            continue
        text = path.read_text(encoding="utf-8")
        status_lines = re.findall(r"^Status: \*\*([^\n]+)\*\*$", text, flags=re.MULTILINE)
        signed = len(status_lines) == 1 and ("SIGNED" in status_lines[0] or "APPROVED" in status_lines[0]) and "PENDING" not in status_lines[0]
        if not signed:
            errors.append(f"{node} review is not in a signed/approved state: {status_lines}")
        review_rows.append({"node": node, "path": path.relative_to(ROOT).as_posix(), "status": status_lines[0] if status_lines else None, "signed": signed})

    p19_review = next((row for row in review_rows if row["node"] == "P19"), None)
    if not p19_review or p19_review.get("signed") is not True:
        errors.append("P19 immediate predecessor signed review is not live in candidate tree")

    authority = {
        "schema": "gojet.p20-integrated-authority-ledger.v1",
        "implementation_commit": HEAD,
        "integrations": ledger,
        "signed_reviews": review_rows,
        "live_bound_count": sum(1 for row in ledger if row["github_commit_exists"]),
        "ancestry_bound_count": sum(1 for row in ledger if row["in_head_ancestry"]),
        "revision_specific_predecessor_closure": "INHERITED_NOT_REINTERPRETED",
    }
    out = ROOT / "artifacts/v10/P20/authority/integrated-authority-ledger.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(authority, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    payload = emit(
        "P20-T006",
        "authority",
        "P00-P19 signed integration authority ledger",
        errors,
        {
            "integration_count": len(ledger),
            "live_bound_count": authority["live_bound_count"],
            "ancestry_bound_count": authority["ancestry_bound_count"],
            "all_in_ancestry": authority["ancestry_bound_count"] == 20,
            "all_live_bound": authority["live_bound_count"] == 20,
            "signed_review_count": sum(1 for row in review_rows if row["signed"]),
            "authority_ledger": out.relative_to(ROOT).as_posix(),
            "revision_specific_predecessor_closure": "INHERITED_NOT_REINTERPRETED",
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
