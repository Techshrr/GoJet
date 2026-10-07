#!/usr/bin/env python3
from __future__ import annotations

import json
import re
from pathlib import Path

from common import HEAD, ROOT, emit, fail_if_errors, sha256_file


def main() -> int:
    errors: list[str] = []
    migrations = sorted((ROOT / "migrations").glob("*.sql"))
    if not migrations:
        errors.append("no current-repository SQL migrations found")

    rows = []
    numbers = []
    names = set()
    for path in migrations:
        match = re.fullmatch(r"(\d{6})_([a-z0-9_]+)\.sql", path.name)
        if not match:
            errors.append(f"migration filename violates deterministic catalog format: {path.name}")
            continue
        number = int(match.group(1))
        logical_name = match.group(2)
        if logical_name in names:
            errors.append(f"duplicate migration logical name: {logical_name}")
        names.add(logical_name)
        numbers.append(number)
        text = path.read_text(encoding="utf-8")
        if not text.strip():
            errors.append(f"empty migration: {path.name}")
        if re.search(r"(?i)legacy[ _-]?gojet|old[ _-]?gojet", text):
            errors.append(f"legacy implementation dependency marker in migration: {path.name}")
        rows.append({
            "number": number,
            "filename": path.name,
            "logical_name": logical_name,
            "sha256": sha256_file(path),
            "bytes": path.stat().st_size,
        })

    if len(numbers) != len(set(numbers)):
        errors.append("duplicate migration numbers")
    if numbers and numbers != list(range(1, max(numbers) + 1)):
        errors.append(f"migration numbers are not contiguous from 000001: {numbers}")

    catalog = {
        "schema": "gojet.p20-schema-catalog.v1",
        "implementation_commit": HEAD,
        "migration_count": len(rows),
        "migrations": rows,
        "source": "current repository migrations/ only",
        "legacy_import": "PROHIBITED",
        "p21_package_completion_claim": False,
    }
    out = ROOT / "artifacts/v10/P20/schema/schema-catalog.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(catalog, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    payload = emit(
        "P20-T004",
        "schema",
        "Schema and migration catalog freeze",
        errors,
        {
            "migration_count": len(rows),
            "migration_numbers": numbers,
            "catalog_path": out.relative_to(ROOT).as_posix(),
            "catalog_sha256": sha256_file(out),
            "catalog": catalog,
            "p21_native_package_claim": "NOT_MADE",
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
