#!/usr/bin/env python3
"""P15-T028 exact-head evidence coherence validator."""
from __future__ import annotations

import hashlib
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT = Path("artifacts/v10/P15")
PRODUCERS = ROOT / "evidence-producer-manifest.json"
INDEX = ROOT / "evidence-index.json"
RESULT = ROOT / "results" / "P15-T028.json"
CONTRACT = ROOT / "contract-guard"

REQUIRED_PRODUCERS = (
    "P15 Authentication OAuth Account Contract",
    "P15 Real Authentication Integration",
    "P15 Authentication Security Integration",
    "P15 Account OAuth Integration",
    "P15 Handoff Mail Audit Integration",
    "P15 Auth Route Browser Authority",
    "P15 Workspace Account Settings Browser Authority",
    "P15 Admin OAuth Browser Authority",
)

CASE_DIRS = {
    1: "api", 2: "api", 3: "security", 4: "api", 5: "security", 6: "security", 7: "security",
    8: "security", 9: "security", 10: "security", 11: "security", 12: "security",
    13: "api", 14: "api",
    15: "oauth", 16: "oauth", 17: "oauth", 18: "oauth", 19: "oauth", 20: "oauth", 21: "oauth",
    22: "mail", 23: "security",
    24: "browser", 25: "browser", 26: "browser",
    27: "audit",
}

BROWSER_CAPTURE_MINIMUMS = {24: 12, 25: 12, 26: 9}
FORBIDDEN_EVIDENCE_MARKERS = (
    b"@example.test",
    b"@gojet.local",
    b"authorization:",
    b"gvc_",
    b"grp_",
    b"glc_",
    b"ghd_",
    b"gos_",
    b"p15-t024-password",
    b"p15-t025-initial-password",
    b"p15-t025-new-password",
    b"p15-t026-secret",
)


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def exact_head() -> str:
    supplied = os.environ.get("EXACT_HEAD", "").strip()
    if supplied:
        return supplied
    return subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def load_json(path: Path, errors: list[str]) -> dict[str, Any]:
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


def case_path(number: int) -> Path:
    return ROOT / CASE_DIRS[number] / f"P15-T{number:03d}.json"


def case_identity(payload: dict[str, Any]) -> str | None:
    value = payload.get("case_id", payload.get("case"))
    return value if isinstance(value, str) else None


def case_head(payload: dict[str, Any]) -> str | None:
    value = payload.get("implementation_commit", payload.get("exact_head"))
    return value if isinstance(value, str) else None


def collect_files(directory: Path) -> list[Path]:
    if not directory.is_dir():
        return []
    return sorted(path for path in directory.rglob("*") if path.is_file())


def forbidden_marker(path: Path) -> bytes | None:
    if path.suffix.lower() == ".png":
        return None
    data = path.read_bytes().lower()
    for marker in FORBIDDEN_EVIDENCE_MARKERS:
        if marker.lower() in data:
            return marker
    return None


def forbidden_marker_bytes_for_self_test() -> bool:
    probe = b"synthetic evidence gvc_should-never-be-reviewable"
    return any(marker.lower() in probe.lower() for marker in FORBIDDEN_EVIDENCE_MARKERS)


def run() -> int:
    head = exact_head()
    errors: list[str] = []
    producer_manifest = load_json(PRODUCERS, errors)

    need(producer_manifest.get("implementation_commit") == head, "producer manifest exact-head mismatch", errors)
    need(producer_manifest.get("missing") == [], f"producer manifest missing={producer_manifest.get('missing')}", errors)
    need(producer_manifest.get("pending") == [], f"producer manifest pending={producer_manifest.get('pending')}", errors)
    need(producer_manifest.get("failed") == [], f"producer manifest failed={producer_manifest.get('failed')}", errors)

    rows = producer_manifest.get("required_workflows", {})
    need(isinstance(rows, dict), "producer required_workflows must be object", errors)
    need(
        isinstance(rows, dict) and set(rows) == set(REQUIRED_PRODUCERS),
        f"producer workflow set mismatch: {sorted(rows) if isinstance(rows, dict) else rows}",
        errors,
    )

    producer_run_ids: dict[str, int] = {}
    producer_artifacts: dict[str, dict[str, Any]] = {}
    if isinstance(rows, dict):
        for name in REQUIRED_PRODUCERS:
            row = rows.get(name, {})
            artifact = row.get("artifact", {}) if isinstance(row, dict) else {}
            need(row.get("head_sha") == head, f"{name} producer head mismatch", errors)
            need(row.get("status") == "completed", f"{name} producer status={row.get('status')}", errors)
            need(row.get("conclusion") == "success", f"{name} producer conclusion={row.get('conclusion')}", errors)
            need(isinstance(row.get("run_id"), int) and row.get("run_id", 0) > 0, f"{name} run id missing", errors)
            need(isinstance(artifact.get("id"), int) and artifact.get("id", 0) > 0, f"{name} artifact id missing", errors)
            need(isinstance(artifact.get("name"), str) and bool(artifact.get("name", "").strip()), f"{name} artifact name missing", errors)
            need(isinstance(artifact.get("digest"), str) and artifact.get("digest", "").startswith("sha256:"), f"{name} artifact digest missing", errors)
            need(isinstance(artifact.get("size_in_bytes"), int) and artifact.get("size_in_bytes", 0) > 0, f"{name} artifact size missing", errors)
            if isinstance(row.get("run_id"), int):
                producer_run_ids[name] = row["run_id"]
            if isinstance(artifact, dict):
                producer_artifacts[name] = {
                    "id": artifact.get("id"),
                    "name": artifact.get("name"),
                    "digest": artifact.get("digest"),
                    "size_in_bytes": artifact.get("size_in_bytes"),
                }

    implementation_marker = CONTRACT / "implementation_commit.txt"
    need(implementation_marker.is_file(), f"missing {implementation_marker}", errors)
    if implementation_marker.is_file():
        need(implementation_marker.read_text(encoding="utf-8").strip() == head, "contract guard implementation commit mismatch", errors)

    contract = load_json(CONTRACT / "contract.json", errors)
    need(contract.get("node") == "P15", "contract node mismatch", errors)
    need(contract.get("status") == "PASS", f"contract status={contract.get('status')}", errors)
    need(contract.get("errors") == [], f"contract errors={contract.get('errors')}", errors)
    need(contract.get("case_range") == "P15-T001..P15-T029", f"contract case range={contract.get('case_range')}", errors)
    need(contract.get("contract_authority") == "9ba89a42281709087b40cdcf0cb2eebd54952a99", "contract authority mismatch", errors)
    need(contract.get("frozen_contract_preserved") is True, "frozen contract preservation false", errors)
    need(contract.get("merge_authoritative") is False, "T028 contract guard must not claim merge authority", errors)

    case_entries: list[dict[str, Any]] = []
    case_payloads: dict[str, dict[str, Any]] = {}
    for number in range(1, 28):
        cid = f"P15-T{number:03d}"
        path = case_path(number)
        data = load_json(path, errors)
        case_payloads[cid] = data
        need(case_identity(data) == cid, f"{cid} identity mismatch", errors)
        need(data.get("status") == "PASS", f"{cid} status={data.get('status')}", errors)
        need(case_head(data) == head, f"{cid} exact-head mismatch", errors)
        if "errors" in data:
            need(data.get("errors") == [], f"{cid} errors={data.get('errors')}", errors)
        if number <= 23 or number == 27:
            policy = data.get("evidence_policy", {})
            need(isinstance(policy, dict), f"{cid} evidence_policy missing", errors)
            if isinstance(policy, dict):
                need(all(value is False for value in policy.values()), f"{cid} unsafe evidence policy={policy}", errors)
            checks = data.get("checks", {})
            need(isinstance(checks, dict) and bool(checks) and all(value is True for value in checks.values()), f"{cid} integration checks not all PASS", errors)
        else:
            details = data.get("details", {})
            need(isinstance(details, dict), f"{cid} browser details missing", errors)
            if isinstance(details, dict):
                need(details.get("frozen_contract_completion") is True, f"{cid} frozen browser contract incomplete", errors)
                need(details.get("closure_claim") is False, f"{cid} must not claim closure", errors)
        if path.is_file():
            case_entries.append({
                "case_id": cid,
                "path": path.as_posix(),
                "sha256": sha256(path),
                "size_in_bytes": path.stat().st_size,
                "implementation_commit": case_head(data),
            })

    same_exact_head = len(case_entries) == 27 and all(entry["implementation_commit"] == head for entry in case_entries)
    need(same_exact_head, "mixed-head P15 case evidence detected", errors)

    mixed_probe = dict(case_payloads.get("P15-T001", {}))
    if "exact_head" in mixed_probe:
        mixed_probe["exact_head"] = "0" * 40
    else:
        mixed_probe["implementation_commit"] = "0" * 40
    mixed_head_rejected = case_head(mixed_probe) != head
    need(mixed_head_rejected, "mixed-head rejection self-check failed", errors)

    capture_entries: list[dict[str, Any]] = []
    browser_capture_counts: dict[str, int] = {}
    for number, minimum in BROWSER_CAPTURE_MINIMUMS.items():
        cid = f"P15-T{number:03d}"
        captures = sorted((ROOT / "captures").glob(f"{cid}-*.png")) if (ROOT / "captures").is_dir() else []
        browser_capture_counts[cid] = len(captures)
        need(len(captures) >= minimum, f"{cid} browser capture count {len(captures)} < {minimum}", errors)
        for path in captures:
            need(path.stat().st_size > 0, f"empty browser capture {path}", errors)
            capture_entries.append({"path": path.as_posix(), "size_in_bytes": path.stat().st_size, "sha256": sha256(path)})

    inspectable_paths: list[Path] = []
    for dirname in ("api", "security", "oauth", "mail", "audit", "browser", "runtime", "contract-guard"):
        inspectable_paths.extend(collect_files(ROOT / dirname))
    secret_safe = True
    for path in inspectable_paths:
        try:
            marker = forbidden_marker(path)
        except OSError as exc:
            errors.append(f"cannot inspect evidence {path}: {exc}")
            secret_safe = False
            continue
        if marker is not None:
            errors.append(f"forbidden evidence marker {marker!r} in {path}")
            secret_safe = False
    unsafe_marker_rejected = forbidden_marker_bytes_for_self_test()
    need(unsafe_marker_rejected, "unsafe-evidence rejection self-check failed", errors)

    index = {
        "node": "P15",
        "case": "P15-T028",
        "generated_at": now(),
        "implementation_commit": head,
        "same_exact_head": same_exact_head,
        "producer_manifest": producer_manifest,
        "case_evidence": case_entries,
        "browser_captures": capture_entries,
    }
    INDEX.parent.mkdir(parents=True, exist_ok=True)
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    result = {
        "case_id": "P15-T028",
        "status": "PASS" if not errors else "FAIL",
        "generated_at": now(),
        "implementation_commit": head,
        "observations": {
            "input_evidence_count": len(case_entries),
            "same_exact_head": same_exact_head,
            "producer_coherent": len(producer_run_ids) == len(REQUIRED_PRODUCERS),
            "producer_run_ids": producer_run_ids,
            "producer_artifacts": producer_artifacts,
            "browser_capture_counts": browser_capture_counts,
            "secret_safe": secret_safe,
            "mixed_head_rejected": mixed_head_rejected,
            "unsafe_evidence_rejected": unsafe_marker_rejected,
            "reviewable_hashed_case_evidence": len(case_entries) == 27 and all(entry["size_in_bytes"] > 0 and len(entry["sha256"]) == 64 for entry in case_entries),
        },
        "errors": errors,
    }
    RESULT.parent.mkdir(parents=True, exist_ok=True)
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(run())
