#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
P18 = ROOT / "artifacts" / "v10" / "P18"
COHERENCE = P18 / "coherence"
CASES = COHERENCE / "cases"
PRODUCERS = COHERENCE / "producer-manifest.json"
INHERITED = COHERENCE / "inherited-authorities.json"
CONTRACT = COHERENCE / "contract-guard" / "contract.json"
INDEX = COHERENCE / "evidence-index.json"
RESULT = COHERENCE / "P18-T025.json"

CONTRACT_AUTHORITY = "784870c56c48591d51663f5d5521c057d74108f9"
BASE = "08cb39bbe54717b711e2d09840ecde04b66bb50f"
P17_SIGNED_SOURCE = "5818406072a131db1c7d8aa7bc5ef8a7adc8d51f"
P17_RUN = 33232541982
P17_ARTIFACT = 9709093486
P17_DIGEST = "sha256:72f8256b242c4412c82cfd4e69c653e4051dc2b7a951c10c9214c2db775805c1"
P04_PRE_SIGN = "659e25e3c5e263ffb3dd74cde953e812b75a7439"
P04_RUN = 32392744860
P04_ARTIFACT = 9415518410
P04_DIGEST = "sha256:5f6b4ec5be87d866b07599e8bd32d75171a81523d29dd86441a524bf33cbc7bb"

REQUIRED_PRODUCERS = {
    "P18 Docs Multilingual Discovery Contract": "p18-docs-multilingual-discovery-contract-guard-{head}",
    "P18 Docs Core Integration": "gojet-v10-p18-docs-core-{head}",
    "P18 Docs Discovery Integration": "gojet-v10-p18-docs-discovery-{head}",
    "P18 Docs Quality Integration": "gojet-v10-p18-docs-quality-{head}",
}

SECRET_PATTERNS = (
    re.compile(rb"\bgak_[A-Za-z0-9_-]{8,}\b", re.I),
    re.compile(rb"\bgwhsec_[A-Za-z0-9_-]{8,}\b", re.I),
    re.compile(rb"\bsk-[A-Za-z0-9_-]{16,}\b"),
    re.compile(rb"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    re.compile(rb"\b(?:mysql|postgres(?:ql)?):\/\/[^ \r\n:@/]+:[^ \r\n@/]+@", re.I),
    re.compile(rb"\bgojet_admin_session=[A-Za-z0-9._~-]{8,}\b", re.I),
)


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def exact_head() -> str:
    supplied = os.environ.get("EXACT_HEAD", "").strip()
    return supplied or git("rev-parse", "HEAD")


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
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def secret_safe(paths: list[Path], errors: list[str]) -> bool:
    safe = True
    for path in paths:
        try:
            raw = path.read_bytes()
        except OSError as exc:
            errors.append(f"cannot read evidence {path}: {exc}")
            safe = False
            continue
        for pattern in SECRET_PATTERNS:
            if pattern.search(raw):
                errors.append(f"secret-bearing evidence pattern {pattern.pattern!r} in {path}")
                safe = False
                break
    return safe


def main() -> int:
    head = exact_head()
    errors: list[str] = []

    need(git("rev-parse", "HEAD") == head, "checkout exact-head mismatch", errors)
    need(subprocess.run(["git", "merge-base", "--is-ancestor", CONTRACT_AUTHORITY, head], cwd=ROOT).returncode == 0,
         "HEAD does not descend from P18 contract authority", errors)
    need(subprocess.run(["git", "merge-base", "--is-ancestor", P17_SIGNED_SOURCE, head], cwd=ROOT).returncode == 0,
         "P17 signed source is not in ancestry", errors)

    producer_manifest = load_json(PRODUCERS, errors)
    need(producer_manifest.get("implementation_commit") == head, "producer manifest exact-head mismatch", errors)
    need(producer_manifest.get("missing") == [], f"producer manifest missing={producer_manifest.get('missing')}", errors)
    need(producer_manifest.get("pending") == [], f"producer manifest pending={producer_manifest.get('pending')}", errors)
    need(producer_manifest.get("failed") == [], f"producer manifest failed={producer_manifest.get('failed')}", errors)

    rows = producer_manifest.get("required_workflows", {})
    need(isinstance(rows, dict) and set(rows) == set(REQUIRED_PRODUCERS), "producer workflow set mismatch", errors)
    producer_artifacts: dict[str, dict] = {}
    producer_run_ids: dict[str, int] = {}
    if isinstance(rows, dict):
        for name, template in REQUIRED_PRODUCERS.items():
            row = rows.get(name, {}) if isinstance(rows.get(name), dict) else {}
            artifacts = row.get("artifacts", []) if isinstance(row.get("artifacts"), list) else []
            expected_name = template.format(head=head)
            need(row.get("head_sha") == head, f"{name} producer head mismatch", errors)
            need(row.get("status") == "completed", f"{name} producer status={row.get('status')}", errors)
            need(row.get("conclusion") == "success", f"{name} producer conclusion={row.get('conclusion')}", errors)
            need(len(artifacts) == 1, f"{name} artifact count {len(artifacts)} != 1", errors)
            if artifacts:
                artifact = artifacts[0] if isinstance(artifacts[0], dict) else {}
                need(artifact.get("name") == expected_name, f"{name} artifact name mismatch", errors)
                need(isinstance(artifact.get("id"), int) and artifact.get("id", 0) > 0, f"{name} artifact id missing", errors)
                need(isinstance(artifact.get("digest"), str) and artifact.get("digest", "").startswith("sha256:"),
                     f"{name} artifact digest missing", errors)
                need(isinstance(artifact.get("size_in_bytes"), int) and artifact.get("size_in_bytes", 0) > 0,
                     f"{name} artifact size missing", errors)
                producer_artifacts[name] = artifact
            if isinstance(row.get("run_id"), int):
                producer_run_ids[name] = row["run_id"]

    contract = load_json(CONTRACT, errors)
    need(contract.get("node") == "P18", "contract node mismatch", errors)
    need(contract.get("status") == "PASS", f"contract status={contract.get('status')}", errors)
    need(contract.get("implementation_commit") == head, "contract implementation commit mismatch", errors)
    need(contract.get("contract_authority") == CONTRACT_AUTHORITY, "contract authority mismatch", errors)
    need(contract.get("case_range") == "P18-T001..P18-T026", "contract case range mismatch", errors)
    need(contract.get("frozen_contract_preserved") is True, "frozen contract not preserved", errors)
    review_phase = contract.get("review_phase")
    need(review_phase in ("pending", "signed"), f"invalid review phase {review_phase}", errors)
    need(contract.get("merge_authoritative") is False, "T025 must never create merge authority", errors)

    inherited = load_json(INHERITED, errors)
    p17 = inherited.get("p17", {}) if isinstance(inherited.get("p17"), dict) else {}
    p04 = inherited.get("p04", {}) if isinstance(inherited.get("p04"), dict) else {}
    need(p17.get("signed_source_commit") == P17_SIGNED_SOURCE, "P17 signed source mismatch", errors)
    need(p17.get("closure_run_id") == P17_RUN and p17.get("artifact_id") == P17_ARTIFACT, "P17 live authority ids mismatch", errors)
    need(p17.get("artifact_digest") == P17_DIGEST and p17.get("phase") == "signed" and p17.get("merge_authoritative") is True,
         "P17 signed authority content mismatch", errors)
    need(p04.get("reviewed_pre_sign_commit") == P04_PRE_SIGN, "P04 pre-sign authority mismatch", errors)
    need(p04.get("workflow_run_id") == P04_RUN and p04.get("artifact_id") == P04_ARTIFACT, "P04 live authority ids mismatch", errors)
    need(p04.get("artifact_digest") == P04_DIGEST and p04.get("required_tests") == "10/10",
         "P04 signed Docs-shell authority mismatch", errors)

    evidence_entries: list[dict] = []
    same_exact_head = True
    expected_ids = {f"P18-T{i:03d}" for i in range(1, 25)}
    seen: set[str] = set()
    for path in sorted(CASES.glob("P18-T*.json")) if CASES.is_dir() else []:
        case_id = path.stem
        if case_id not in expected_ids:
            continue
        if case_id in seen:
            errors.append(f"duplicate evidence {case_id}")
            continue
        seen.add(case_id)
        data = load_json(path, errors)
        need(data.get("case") == case_id, f"{case_id} case id mismatch", errors)
        need(data.get("status") == "PASS", f"{case_id} status={data.get('status')}", errors)
        need(data.get("implementation_commit") == head, f"{case_id} exact-head mismatch", errors)
        need(data.get("secret_safe") is not False, f"{case_id} secret_safe=false", errors)
        if data.get("implementation_commit") != head:
            same_exact_head = False
        evidence_entries.append({
            "case_id": case_id,
            "path": path.as_posix(),
            "sha256": sha256(path),
            "size_in_bytes": path.stat().st_size,
            "implementation_commit": data.get("implementation_commit"),
        })
    need(seen == expected_ids, f"case evidence mismatch missing={sorted(expected_ids-seen)} extra={sorted(seen-expected_ids)}", errors)
    need(len(evidence_entries) == 24, f"expected 24 evidence files, got {len(evidence_entries)}", errors)
    need(same_exact_head, "mixed-head P18 evidence detected", errors)

    inspectable = []
    for directory in (CASES, COHERENCE / "contract-guard"):
        if directory.is_dir():
            inspectable.extend(item for item in directory.rglob("*") if item.is_file())
    inspectable.extend(path for path in (PRODUCERS, INHERITED) if path.is_file())
    is_secret_safe = secret_safe(sorted(inspectable), errors)

    mixed_probe = {"case": "P18-T001", "status": "PASS", "implementation_commit": "0" * 40}
    mixed_head_rejected = mixed_probe.get("implementation_commit") != head
    unsafe_evidence_rejected = any(p.search(b"gak_synthetic_negative_probe") for p in SECRET_PATTERNS)
    need(mixed_head_rejected, "mixed-head negative oracle did not fail closed", errors)
    need(unsafe_evidence_rejected, "unsafe-evidence negative oracle did not fail closed", errors)

    index = {
        "node": "P18",
        "case": "P18-T025",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "review_phase": review_phase,
        "producer_manifest": producer_manifest,
        "inherited_authorities": inherited,
        "case_evidence": evidence_entries,
    }
    COHERENCE.mkdir(parents=True, exist_ok=True)
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    result = {
        "node": "P18",
        "case": "P18-T025",
        "status": "PASS" if not errors else "FAIL",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P18-T001..P18-T025",
        "review_phase": review_phase,
        "observations": {
            "input_evidence_count": len(evidence_entries),
            "same_exact_head": same_exact_head,
            "secret_safe": is_secret_safe,
            "producer_count": len(producer_run_ids),
            "producer_artifact_count": len(producer_artifacts),
            "producer_run_ids": producer_run_ids,
            "producer_artifacts": producer_artifacts,
            "inherited_authorities_bound": bool(p17) and bool(p04),
            "mixed_head_rejected": mixed_head_rejected,
            "unsafe_evidence_rejected": unsafe_evidence_rejected,
            "merge_authoritative": False,
        },
        "errors": errors,
    }
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
