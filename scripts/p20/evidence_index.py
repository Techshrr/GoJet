#!/usr/bin/env python3
from __future__ import annotations

import copy
import json
import re
from pathlib import Path

from common import HEAD, ROOT, emit, fail_if_errors, sha256_file

EXPECTED = [f"P20-T{i:03d}" for i in (1, 2, 3, 4, 5, 6, 8)]
SAFE_SENTINELS = {
    "PROHIBITED", "REDACTED", "NOT_MADE", "REQUIRED_WHERE_APPLICABLE",
    "INHERITED_NOT_REINTERPRETED", "BUILD_TEST_ONLY",
}


def case_path(case_id: str) -> Path | None:
    matches = list((ROOT / "artifacts/v10/P20").glob(f"*/{case_id}.json"))
    return matches[0] if len(matches) == 1 else None


def identity(data: dict) -> str | None:
    return data.get("case") or data.get("case_id")


def valid_case(data: dict, case_id: str, head: str) -> bool:
    return (
        identity(data) == case_id
        and data.get("status") == "PASS"
        and data.get("errors") == []
        and data.get("implementation_commit") == head
    )


def unsafe_secret(data: object) -> list[str]:
    hits: list[str] = []
    pattern = re.compile(r"(?i)(password|secret|token|authorization|cookie|private[_-]?key|client[_-]?secret|api[_-]?key)")

    def walk(value: object, path: str) -> None:
        if isinstance(value, dict):
            for key, child in value.items():
                child_path = f"{path}.{key}" if path else str(key)
                if pattern.search(str(key)) and isinstance(child, str) and child and child not in SAFE_SENTINELS:
                    # Names/contract labels are allowed; concrete credential-like values are not.
                    if re.search(r"(?i)(bearer\s+\S+|sk[-_][A-Za-z0-9_-]{8,}|[A-Za-z0-9+/]{24,}={0,2}|[0-9a-f]{32,})", child):
                        hits.append(child_path)
                walk(child, child_path)
        elif isinstance(value, list):
            for index, child in enumerate(value):
                walk(child, f"{path}[{index}]")
    walk(data, "")
    return hits


def main() -> int:
    errors: list[str] = []
    rows = []
    loaded: dict[str, dict] = {}
    for case_id in EXPECTED:
        path = case_path(case_id)
        if path is None:
            errors.append(f"expected exactly one evidence file for {case_id}")
            continue
        data = json.loads(path.read_text(encoding="utf-8"))
        loaded[case_id] = data
        if not valid_case(data, case_id, HEAD):
            errors.append(f"{case_id} is not exact-head PASS evidence")
        secret_hits = unsafe_secret(data)
        if secret_hits:
            errors.append(f"{case_id} contains secret-bearing evidence paths: {secret_hits}")
        rows.append({
            "case": case_id,
            "path": path.relative_to(ROOT).as_posix(),
            "sha256": sha256_file(path),
            "status": data.get("status"),
            "implementation_commit": data.get("implementation_commit"),
            "secret_safe": not secret_hits,
        })

    authority_path = ROOT / "artifacts/v10/P20/authority/integrated-authority-ledger.json"
    if not authority_path.is_file():
        errors.append("integrated authority ledger missing")
        authority = {}
    else:
        authority = json.loads(authority_path.read_text(encoding="utf-8"))
        if authority.get("implementation_commit") != HEAD:
            errors.append("integrated authority ledger is not exact-head")
        if authority.get("live_bound_count") != 20 or authority.get("ancestry_bound_count") != 20:
            errors.append("integrated authority ledger does not live/ancestry bind P00-P19")

    mixed_head_rejected = malformed_rejected = unsafe_rejected = False
    if loaded:
        sample_id = sorted(loaded)[0]
        sample = loaded[sample_id]
        mixed = copy.deepcopy(sample)
        mixed["implementation_commit"] = "0" * 40
        mixed_head_rejected = not valid_case(mixed, sample_id, HEAD)
        malformed = copy.deepcopy(sample)
        malformed.pop("errors", None)
        malformed_rejected = not valid_case(malformed, sample_id, HEAD)
        unsafe = {"case": sample_id, "status": "PASS", "errors": [], "implementation_commit": HEAD, "secret": "sk-test-unsafe-credential-material"}
        unsafe_rejected = bool(unsafe_secret(unsafe))
    if not mixed_head_rejected:
        errors.append("mixed-head negative probe did not fail closed")
    if not malformed_rejected:
        errors.append("malformed-evidence negative probe did not fail closed")
    if not unsafe_rejected:
        errors.append("secret-bearing negative probe did not fail closed")

    index = {
        "schema": "gojet.p20-candidate-evidence-index.v1",
        "implementation_commit": HEAD,
        "indexed_cases": rows,
        "indexed_case_count": len(rows),
        "integrated_authority_ledger": {
            "path": authority_path.relative_to(ROOT).as_posix(),
            "sha256": sha256_file(authority_path) if authority_path.is_file() else None,
            "live_bound_count": authority.get("live_bound_count"),
            "ancestry_bound_count": authority.get("ancestry_bound_count"),
        },
        "secret_safe": not any(unsafe_secret(data) for data in loaded.values()),
        "mixed_head_rejected": mixed_head_rejected,
        "malformed_evidence_rejected": malformed_rejected,
        "unsafe_evidence_rejected": unsafe_rejected,
    }
    out = ROOT / "artifacts/v10/P20/evidence/candidate-evidence-index.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    payload = emit(
        "P20-T007",
        "evidence",
        "Release-candidate evidence index integrity",
        errors,
        {
            "indexed_case_count": len(rows),
            "expected_pre_index_cases": EXPECTED,
            "same_exact_head": all(row["implementation_commit"] == HEAD for row in rows),
            "secret_safe": index["secret_safe"],
            "integrated_authority_live_bound": authority.get("live_bound_count") == 20 and authority.get("ancestry_bound_count") == 20,
            "mixed_head_rejected": mixed_head_rejected,
            "malformed_evidence_rejected": malformed_rejected,
            "unsafe_evidence_rejected": unsafe_rejected,
            "index_path": out.relative_to(ROOT).as_posix(),
            "index_sha256": sha256_file(out),
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
