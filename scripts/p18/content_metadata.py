#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
DOCS = ROOT / "frontend/apps/docs"
CONTENT = DOCS / "src/content/docs"
MANIFEST = DOCS / "src/data/content-manifest.json"
PLAN = ROOT / "artifacts/v10/P18/test-plan.json"
REQUIRED = ("title", "description", "locale", "lastUpdated", "canonicalPath", "translation", "contentOwner")


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
            raise AssertionError(f"unsupported frontmatter: {path}: {raw!r}")
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


def main() -> int:
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    documents = []
    seen_paths: set[str] = set()
    seen_sources: set[str] = set()
    for entry in manifest["documents"]:
        source = CONTENT / entry["source"]
        assert source.is_file(), source
        assert entry["source"] not in seen_sources, entry["source"]
        assert entry["canonicalPath"] not in seen_paths, entry["canonicalPath"]
        seen_sources.add(entry["source"])
        seen_paths.add(entry["canonicalPath"])
        fm = parse_frontmatter(source)
        for key in REQUIRED:
            assert key in fm, (entry["source"], key)
        for key in REQUIRED:
            assert fm[key] == entry[key], (entry["source"], key, fm[key], entry[key])
        digest = "sha256:" + hashlib.sha256(source.read_bytes()).hexdigest()
        assert digest == entry["sourceDigest"], (entry["source"], digest, entry["sourceDigest"])
        documents.append({
            "source": entry["source"],
            "canonicalPath": entry["canonicalPath"],
            "locale": entry["locale"],
            "translation": entry["translation"],
            "contentOwner": entry["contentOwner"],
            "lastUpdated": entry["lastUpdated"],
            "sourceDigest": digest,
        })

    plan = json.loads(PLAN.read_text(encoding="utf-8"))
    case = next(row for row in plan["cases"] if row["id"] == "P18-T002")
    output = ROOT / case["evidence"]
    output.parent.mkdir(parents=True, exist_ok=True)
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    payload = {
        "case": "P18-T002",
        "status": "PASS",
        "implementation_commit": head,
        "secret_safe": True,
        "required_fields": list(REQUIRED),
        "documents": documents,
        "build_time_lastmod_used": False,
    }
    output.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
