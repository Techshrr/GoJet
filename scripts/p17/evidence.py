#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path("artifacts/v10/P17")
CASES = ROOT / "cases"
CAPTURES = ROOT / "captures"
PRODUCERS = ROOT / "evidence-producer-manifest.json"
INDEX = ROOT / "evidence-index.json"
RESULT = ROOT / "results" / "P17-T034.json"
CONTRACT_AUTHORITY = "30174f40df28678360f644b8fed79736906b0ea0"
CASE_RANGE = "P17-T001..P17-T035"

REQUIRED_PRODUCERS = (
    "P17 Admin Permissions Audit Contract",
    "P17 Real Admin Access Integration",
    "P17 Domain Entitlement Governance Integration",
    "P17 User Workspace Governance Integration",
    "P17 Resource and Operations Governance Integration",
    "P17 Platform Governance Integration",
    "P17 API-key Governance Integration",
    "P17 Webhook Governance Integration",
    "P17 Browser Governance Authority",
)

EXPECTED_ARTIFACT_COUNTS = {
    "P17 Admin Permissions Audit Contract": 1,
    "P17 Real Admin Access Integration": 1,
    "P17 Domain Entitlement Governance Integration": 1,
    "P17 User Workspace Governance Integration": 1,
    "P17 Resource and Operations Governance Integration": 5,
    "P17 Platform Governance Integration": 6,
    "P17 API-key Governance Integration": 3,
    "P17 Webhook Governance Integration": 5,
    "P17 Browser Governance Authority": 4,
}

FORBIDDEN_MARKERS = (
    b"p17-browser-root-password",
    b"p17-browser-limited-password",
    b"p17-browser-seed-password",
    b"gak_",
    b"gwhsec_",
    b"gojet_admin_session=",
    b"authorization: bearer",
    b"root:root@tcp",
    b"gojet_mysql_dsn",
    b"private_key",
    b"client_secret",
)


def exact_head() -> str:
    supplied = os.environ.get("EXACT_HEAD", "").strip()
    return supplied or subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def load_json(path: Path, errors: list[str]) -> dict:
    if not path.is_file():
        errors.append(f"missing {path}")
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        errors.append(f"invalid JSON {path}: {exc}")
        return {}
    if not isinstance(value, dict):
        errors.append(f"JSON object required {path}")
        return {}
    return value


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def contains_forbidden(raw: bytes) -> bytes | None:
    lowered = raw.lower()
    for marker in FORBIDDEN_MARKERS:
        if marker in lowered:
            return marker
    return None


def secret_safe(paths: list[Path], errors: list[str]) -> bool:
    clean = True
    for path in paths:
        if path.suffix.lower() == ".png":
            continue
        try:
            raw = path.read_bytes()
        except OSError as exc:
            errors.append(f"cannot read evidence {path}: {exc}")
            clean = False
            continue
        marker = contains_forbidden(raw)
        if marker is not None:
            errors.append(f"forbidden evidence marker {marker!r} in {path}")
            clean = False
    return clean


def case_is_bound(data: dict, case_id: str, head: str) -> bool:
    return (
        data.get("node") == "P17"
        and data.get("case") == case_id
        and data.get("status") == "PASS"
        and data.get("exact_head") == head
        and data.get("contract_authority") == CONTRACT_AUTHORITY
    )


def run() -> int:
    head = exact_head()
    errors: list[str] = []
    manifest = load_json(PRODUCERS, errors)

    need(manifest.get("implementation_commit") == head, "producer manifest exact-head mismatch", errors)
    need(manifest.get("missing") == [], f"producer manifest missing={manifest.get('missing')}", errors)
    need(manifest.get("pending") == [], f"producer manifest pending={manifest.get('pending')}", errors)
    need(manifest.get("failed") == [], f"producer manifest failed={manifest.get('failed')}", errors)

    producer_rows = manifest.get("required_workflows", {})
    need(isinstance(producer_rows, dict), "producer required_workflows must be object", errors)
    need(isinstance(producer_rows, dict) and set(producer_rows) == set(REQUIRED_PRODUCERS), "producer workflow set mismatch", errors)

    producer_run_ids: dict[str, int] = {}
    producer_artifacts: dict[str, list[dict]] = {}
    artifact_total = 0
    if isinstance(producer_rows, dict):
        for name in REQUIRED_PRODUCERS:
            row = producer_rows.get(name, {}) if isinstance(producer_rows.get(name, {}), dict) else {}
            artifacts = row.get("artifacts", []) if isinstance(row.get("artifacts", []), list) else []
            need(row.get("head_sha") == head, f"{name} producer head mismatch", errors)
            need(row.get("status") == "completed", f"{name} producer status={row.get('status')}", errors)
            need(row.get("conclusion") == "success", f"{name} producer conclusion={row.get('conclusion')}", errors)
            need(isinstance(row.get("run_id"), int) and row.get("run_id", 0) > 0, f"{name} run id missing", errors)
            need(len(artifacts) == EXPECTED_ARTIFACT_COUNTS[name], f"{name} artifact count {len(artifacts)} != {EXPECTED_ARTIFACT_COUNTS[name]}", errors)
            normalized: list[dict] = []
            for artifact in artifacts:
                artifact = artifact if isinstance(artifact, dict) else {}
                need(isinstance(artifact.get("id"), int) and artifact.get("id", 0) > 0, f"{name} artifact id missing", errors)
                need(isinstance(artifact.get("name"), str) and artifact.get("name", "").endswith(head), f"{name} artifact not exact-head named: {artifact.get('name')}", errors)
                need(isinstance(artifact.get("digest"), str) and artifact.get("digest", "").startswith("sha256:"), f"{name} artifact digest missing", errors)
                need(isinstance(artifact.get("size_in_bytes"), int) and artifact.get("size_in_bytes", 0) > 0, f"{name} artifact size missing", errors)
                normalized.append({key: artifact.get(key) for key in ("id", "name", "digest", "size_in_bytes")})
            if isinstance(row.get("run_id"), int):
                producer_run_ids[name] = row["run_id"]
            producer_artifacts[name] = normalized
            artifact_total += len(artifacts)

    need(artifact_total == 27, f"expected 27 producer artifacts, got {artifact_total}", errors)

    contract = load_json(ROOT / "contract-guard" / "contract.json", errors)
    need(contract.get("node") == "P17", "contract node mismatch", errors)
    need(contract.get("status") == "PASS", f"contract status={contract.get('status')}", errors)
    need(contract.get("implementation_commit") == head, "contract implementation commit mismatch", errors)
    need(contract.get("contract_authority") == CONTRACT_AUTHORITY, "contract authority mismatch", errors)
    need(contract.get("case_range") == CASE_RANGE, "contract case range mismatch", errors)
    need(contract.get("frozen_contract_preserved") is True, "frozen contract not preserved", errors)
    review_phase = contract.get("review_phase")
    need(review_phase in ("pending", "signed"), f"invalid review phase {review_phase}", errors)
    need(contract.get("merge_authoritative") is False, "T034 must not create merge authority", errors)

    evidence_entries: list[dict] = []
    same_exact_head = True
    for number in range(1, 34):
        case_id = f"P17-T{number:03d}"
        path = CASES / f"{case_id}.json"
        data = load_json(path, errors)
        bound = case_is_bound(data, case_id, head)
        need(bound, f"{case_id} exact-head/status/authority mismatch", errors)
        if data.get("exact_head") != head:
            same_exact_head = False
        checks = data.get("checks") or {}
        need(isinstance(checks, dict) and bool(checks) and all(value is True for value in checks.values()), f"{case_id} checks not all true", errors)
        evidence_policy = data.get("evidence_policy") or {}
        need(isinstance(evidence_policy, dict) and bool(evidence_policy) and not any(evidence_policy.values()), f"{case_id} unsafe evidence policy flags", errors)
        if number >= 30:
            need(data.get("errors") == [], f"{case_id} browser errors={data.get('errors')}", errors)
            details = data.get("details") or {}
            need(details.get("frozen_contract_completion") is True, f"{case_id} browser frozen completion missing", errors)
            need(details.get("closure_claim") is False, f"{case_id} premature closure claim", errors)
            security = details.get("security_checks") or {}
            need(isinstance(security, dict) and bool(security) and all(value is True for value in security.values()), f"{case_id} browser security checks not all true", errors)
        if path.is_file():
            evidence_entries.append({
                "case_id": case_id,
                "path": path.as_posix(),
                "sha256": sha256(path),
                "size_in_bytes": path.stat().st_size,
                "exact_head": data.get("exact_head"),
            })

    need(len(evidence_entries) == 33, f"expected 33 case files, got {len(evidence_entries)}", errors)
    need(same_exact_head, "mixed-head P17 evidence detected", errors)

    capture_entries: list[dict] = []
    capture_counts: dict[str, int] = {}
    minimums = {"P17-T030": 5, "P17-T031": 5, "P17-T032": 8, "P17-T033": 12}
    for case_id, minimum in minimums.items():
        paths = sorted(CAPTURES.glob(f"{case_id}-*.png")) if CAPTURES.is_dir() else []
        capture_counts[case_id] = len(paths)
        need(len(paths) >= minimum, f"{case_id} capture count {len(paths)} < {minimum}", errors)
        for path in paths:
            need(path.stat().st_size > 0, f"empty browser capture {path}", errors)
            capture_entries.append({"path": path.as_posix(), "sha256": sha256(path), "size_in_bytes": path.stat().st_size})

    inspectable = []
    for path in (CASES, ROOT / "contract-guard"):
        if path.is_dir():
            inspectable.extend(sorted(item for item in path.rglob("*") if item.is_file()))
    is_secret_safe = secret_safe(inspectable, errors)

    mixed_probe = {
        "node": "P17", "case": "P17-T001", "status": "PASS",
        "exact_head": "0" * 40, "contract_authority": CONTRACT_AUTHORITY,
    }
    mixed_head_rejected = not case_is_bound(mixed_probe, "P17-T001", head)
    unsafe_evidence_rejected = contains_forbidden(b"prefix gak_synthetic_negative_probe") is not None
    need(mixed_head_rejected, "mixed-head negative oracle did not fail closed", errors)
    need(unsafe_evidence_rejected, "unsafe-evidence negative oracle did not fail closed", errors)

    index = {
        "node": "P17",
        "case": "P17-T034",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "review_phase": review_phase,
        "producer_manifest": manifest,
        "case_evidence": evidence_entries,
        "browser_captures": capture_entries,
    }
    INDEX.parent.mkdir(parents=True, exist_ok=True)
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    result = {
        "node": "P17",
        "case": "P17-T034",
        "status": "PASS" if not errors else "FAIL",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "observations": {
            "input_evidence_count": len(evidence_entries),
            "same_exact_head": same_exact_head,
            "secret_safe": is_secret_safe,
            "producer_count": len(producer_run_ids),
            "producer_artifact_count": artifact_total,
            "producer_run_ids": producer_run_ids,
            "producer_artifacts": producer_artifacts,
            "browser_capture_counts": capture_counts,
            "mixed_head_rejected": mixed_head_rejected,
            "unsafe_evidence_rejected": unsafe_evidence_rejected,
            "review_phase": review_phase,
            "merge_authoritative": False,
        },
        "errors": errors,
    }
    RESULT.parent.mkdir(parents=True, exist_ok=True)
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(run())
