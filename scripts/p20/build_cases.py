#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
from pathlib import Path

from common import HEAD, ROOT, emit, fail_if_errors, file_tree_inventory, sha256_file

APPS = ["admin", "docs", "site", "workspace"]


def load(path: str) -> dict:
    return json.loads(Path(path).read_text(encoding="utf-8"))


def active_deploy_text() -> str:
    lines: list[str] = []
    for path in sorted((ROOT / "deploy").rglob("*")):
        if not path.is_file() or path.name.lower() == "readme.md":
            continue
        for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
            if line.lstrip().startswith("#"):
                continue
            lines.append(line)
    return "\n".join(lines)


def main() -> int:
    errors: list[str] = []
    one_path = os.environ.get("P20_BUILD_ONE", "")
    two_path = os.environ.get("P20_BUILD_TWO", "")
    if not one_path or not two_path:
        errors.append("P20_BUILD_ONE/P20_BUILD_TWO snapshots are required")
        one = two = {}
    else:
        one = load(one_path)
        two = load(two_path)

    if one.get("implementation_commit") != HEAD or two.get("implementation_commit") != HEAD:
        errors.append("build snapshots are not bound to exact candidate HEAD")
    if one.get("files") != two.get("files"):
        first = one.get("files", {})
        second = two.get("files", {})
        changed = sorted(set(first) ^ set(second) | {k for k in set(first) & set(second) if first[k] != second[k]})
        errors.append(f"repeated clean frontend build is not byte-equivalent; changed={changed[:25]}")

    current = file_tree_inventory([ROOT / "frontend/apps" / app / "dist" for app in APPS])
    if current != two.get("files", {}):
        errors.append("current dist inventory does not match second exact-head build snapshot")

    missing = [app for app in APPS if not (ROOT / "frontend/apps" / app / "dist").is_dir()]
    if missing:
        errors.append(f"frontend app build output missing: {missing}")

    deploy_text = active_deploy_text()
    prohibited = []
    patterns = {
        "production_node_proxy": r"(?i)proxy_pass\s+http://[^;]*(?:node|vite|next|astro)",
        "pm2": r"(?i)\bpm2\b",
        "docker_compose": r"(?i)docker[- ]?compose|compose\.ya?ml",
    }
    for label, pattern in patterns.items():
        if re.search(pattern, deploy_text):
            prohibited.append(label)
    if prohibited:
        errors.append(f"production runtime boundary violation in deploy assets: {prohibited}")

    inventory = {
        "schema": "gojet.p20-frontend-build-inventory.v1",
        "implementation_commit": HEAD,
        "apps": APPS,
        "file_count": len(current),
        "files": current,
        "repeat_equivalent": one.get("files") == two.get("files"),
        "production_node_http_ssr_pm2": "PROHIBITED",
        "production_docker_compose": "PROHIBITED",
        "node_vite_role": "BUILD_TEST_ONLY",
    }
    out = ROOT / "artifacts/v10/P20/build/frontend-build-inventory.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(inventory, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    payload = emit(
        "P20-T005",
        "build",
        "Whole-frontend build inventory and determinism",
        errors,
        {
            "apps": APPS,
            "first_build_file_count": len(one.get("files", {})),
            "second_build_file_count": len(two.get("files", {})),
            "repeat_equivalent": one.get("files") == two.get("files"),
            "inventory_path": out.relative_to(ROOT).as_posix(),
            "inventory_sha256": sha256_file(out),
            "production_runtime_violations": prohibited,
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
