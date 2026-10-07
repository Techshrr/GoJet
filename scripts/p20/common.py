#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
HEAD = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def ancestor(older: str, newer: str = HEAD) -> bool:
    return subprocess.run(
        ["git", "merge-base", "--is-ancestor", older, newer],
        cwd=ROOT,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    ).returncode == 0


def blob(revision: str, path: str) -> str:
    return git("rev-parse", f"{revision}:{path}")


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            h.update(chunk)
    return "sha256:" + h.hexdigest()


def generated_at() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def emit(case_id: str, folder: str, name: str, errors: list[str], details: dict[str, Any]) -> dict[str, Any]:
    payload = {
        "node": "P20",
        "case": case_id,
        "name": name,
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "implementation_commit": HEAD,
        "generated_at": generated_at(),
        "details": details,
    }
    out = ROOT / "artifacts" / "v10" / "P20" / folder / f"{case_id}.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False))
    return payload


def file_tree_inventory(roots: list[Path]) -> dict[str, str]:
    rows: dict[str, str] = {}
    for root in roots:
        if not root.exists():
            continue
        for path in sorted(p for p in root.rglob("*") if p.is_file()):
            rows[path.relative_to(ROOT).as_posix()] = sha256_file(path)
    return rows


def fail_if_errors(payloads: list[dict[str, Any]]) -> None:
    if any(item.get("status") != "PASS" for item in payloads):
        raise SystemExit(1)
