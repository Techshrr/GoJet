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
P19 = ROOT / "artifacts" / "v10" / "P19"
COHERENCE = P19 / "coherence"
CASES = COHERENCE / "cases"
PRODUCERS = COHERENCE / "producer-manifest.json"
INHERITED = COHERENCE / "inherited-authorities.json"
CONTRACT = COHERENCE / "contract-guard" / "contract.json"
INDEX = COHERENCE / "evidence-index.json"
RESULT = COHERENCE / "P19-T031.json"
AUTHORITY_FILE = ROOT / "frontend" / "apps" / "site" / "src" / "website" / "authority.json"

BASE = "43e693b10c0118e32d7f14c61156e0b06c155111"
CONTRACT_AUTHORITY = "d1e6f2a4af2006ccd44bf0d363144845efd535e0"
P18_SIGNED_SOURCE = "e8746159b02c729a877e3dcbd9655d415a5cc269"
P18_RUN = 33260817755
P18_ARTIFACT = 9717210947
P18_DIGEST = "sha256:3e403765409b3ab273be1c35a9d88b565505c416a47364d9a6f0339cc130efe4"

REQUIRED_PRODUCERS = {
    "P19 Website and Technical SEO Contract": "p19-website-technical-seo-contract-{head}",
    "P19 Website Core Integration": "gojet-v10-p19-site-core-{head}",
    "P19 Website Content Authority": "gojet-v10-p19-content-{head}",
    "P19 Website Crawl and Discovery": "gojet-v10-p19-discovery-{head}",
    "P19 Website Browser and Visual": "gojet-v10-p19-browser-{head}",
    "P19 Website Performance and Runtime": "gojet-v10-p19-quality-{head}",
}

PUBLIC_INTEGRATIONS = {
    "P05": "ed82747f9f7ddb7696534cdda110f2f7f594b46a",
    "P06": "3aa80b566d144963130b8f61fa63a4ee677ebc99",
    "P07": "04941afc59db763e6c7db8a67721dea542c72a43",
    "P08": "418277613cf4336273b19f5d0da8a47bc1d403d6",
    "P09": "0c43b9e5fa9abb9da7231e4ab5bd6d8a76f6d9a8",
    "P10": "4d2186da8b2958c7618a233f53908f2914c389a3",
    "P11": "638a6988c03eed6d287af0d2fdc63a3a3355ef68",
    "P12": "7f39da389052b08f145e69dac2a715b9d303294d",
    "P13": "a94f1d9894916b995a2379571f6ab3de520fc4ba",
    "P14": "9258cb0f3f913b37b03aa8cf3c2938711314d3aa",
    "P15": "dd70eacf02d4dd79fe82063f3d43610ab11885e8",
    "P16": "62d682a25532eef3cc207a5e9964a62f6072ede7",
    "P17": "08cb39bbe54717b711e2d09840ecde04b66bb50f",
    "P18": BASE,
}

SECRET_PATTERNS = (
    re.compile(rb"\bgak_[A-Za-z0-9_-]{8,}\b", re.I),
    re.compile(rb"\bgwhsec_[A-Za-z0-9_-]{8,}\b", re.I),
    re.compile(rb"\bsk-[A-Za-z0-9_-]{16,}\b"),
    re.compile(rb"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    re.compile(rb"\b(?:mysql|postgres(?:ql)?):\/\/[^ \r\n:@/]+:[^ \r\n@/]+@", re.I),
    re.compile(rb"\bgojet_(?:admin_)?session=[A-Za-z0-9._~-]{8,}\b", re.I),
)


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def exact_head() -> str:
    supplied = os.environ.get("EXACT_HEAD", "").strip()
    return supplied or git("rev-parse", "HEAD")


def ancestor(older: str, newer: str) -> bool:
    return subprocess.run(
        ["git", "merge-base", "--is-ancestor", older, newer],
        cwd=ROOT,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    ).returncode == 0


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


def case_identity(data: dict) -> object:
    return data.get("case") if data.get("case") is not None else data.get("case_id")


def case_schema_valid(data: dict, case_id: str, head: str) -> bool:
    return (
        isinstance(data, dict)
        and case_identity(data) == case_id
        and data.get("status") == "PASS"
        and data.get("errors") == []
        and data.get("implementation_commit") == head
        and data.get("secret_safe") is not False
    )


def main() -> int:
    head = exact_head()
    errors: list[str] = []

    need(git("rev-parse", "HEAD") == head, "checkout exact-head mismatch", errors)
    need(ancestor(CONTRACT_AUTHORITY, head), "HEAD does not descend from P19 contract authority", errors)
    need(ancestor(P18_SIGNED_SOURCE, head), "P18 signed source is not in P19 ancestry", errors)
    need(ancestor(BASE, head), "P18 integration base is not in P19 ancestry", errors)

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
                need(isinstance(artifact.get("digest"), str) and re.fullmatch(r"sha256:[0-9a-f]{64}", artifact.get("digest", "")) is not None,
                     f"{name} artifact digest missing/invalid", errors)
                need(isinstance(artifact.get("size_in_bytes"), int) and artifact.get("size_in_bytes", 0) > 0,
                     f"{name} artifact size missing", errors)
                producer_artifacts[name] = artifact
            if isinstance(row.get("run_id"), int) and row.get("run_id", 0) > 0:
                producer_run_ids[name] = row["run_id"]

    contract = load_json(CONTRACT, errors)
    need(contract.get("node") == "P19", "contract node mismatch", errors)
    need(contract.get("status") == "PASS" and contract.get("errors") == [], "contract guard not PASS/errors=[]", errors)
    need(contract.get("implementation_commit") == head, "contract implementation commit mismatch", errors)
    need(contract.get("contract_authority") == CONTRACT_AUTHORITY, "contract authority mismatch", errors)
    need(contract.get("case_range") == "P19-T001..P19-T032", "contract case range mismatch", errors)
    need(contract.get("frozen_contract_preserved") is True, "frozen P19 contract not preserved", errors)
    review_phase = contract.get("review_phase")
    need(review_phase in ("pending", "signed"), f"invalid review phase {review_phase}", errors)
    need(contract.get("merge_authoritative") is False, "T031 must never create merge authority", errors)

    inherited = load_json(INHERITED, errors)
    p18 = inherited.get("p18", {}) if isinstance(inherited.get("p18"), dict) else {}
    need(p18.get("signed_source_commit") == P18_SIGNED_SOURCE, "P18 signed source mismatch", errors)
    need(p18.get("integration_commit") == BASE, "P18 integration commit mismatch", errors)
    need(p18.get("closure_run_id") == P18_RUN and p18.get("artifact_id") == P18_ARTIFACT, "P18 live authority ids mismatch", errors)
    need(p18.get("artifact_digest") == P18_DIGEST, "P18 closure digest mismatch", errors)
    need(p18.get("phase") == "signed" and p18.get("review_phase") == "signed" and p18.get("review_only_signed_child") is True and p18.get("merge_authoritative") is True,
         "P18 signed authority content mismatch", errors)

    public_rows = inherited.get("public_capabilities", []) if isinstance(inherited.get("public_capabilities"), list) else []
    public_by_node = {row.get("node"): row for row in public_rows if isinstance(row, dict) and isinstance(row.get("node"), str)}
    need(set(public_by_node) == set(PUBLIC_INTEGRATIONS), "public capability authority node set mismatch", errors)
    for node, integration in PUBLIC_INTEGRATIONS.items():
        row = public_by_node.get(node, {})
        need(row.get("integration") == integration, f"{node} integration authority mismatch", errors)
        need(row.get("github_commit_exists") is True, f"{node} GitHub live commit was not bound", errors)
        need(row.get("in_head_ancestry") is True and ancestor(integration, head), f"{node} integration is not in exact-head ancestry", errors)

    authority = load_json(AUTHORITY_FILE, errors)
    need(authority.get("schema") == "gojet.website-authority.v1", "Website authority schema mismatch", errors)
    need(authority.get("contractAuthority") == CONTRACT_AUTHORITY, "Website authority contract binding mismatch", errors)
    signed = authority.get("signedIntegrations", []) if isinstance(authority.get("signedIntegrations"), list) else []
    signed_map = {row.get("node"): row.get("integration") for row in signed if isinstance(row, dict)}
    need(signed_map == PUBLIC_INTEGRATIONS, "Website signed integration ledger drift", errors)

    evidence_entries: list[dict] = []
    expected_ids = {f"P19-T{i:03d}" for i in range(1, 31)}
    seen: set[str] = set()
    same_exact_head = True
    if CASES.is_dir():
        for path in sorted(CASES.glob("P19-T*.json")):
            case_id = path.stem
            if case_id not in expected_ids:
                continue
            if case_id in seen:
                errors.append(f"duplicate evidence {case_id}")
                continue
            seen.add(case_id)
            data = load_json(path, errors)
            need(case_identity(data) == case_id, f"{case_id} case identity mismatch", errors)
            need(data.get("status") == "PASS", f"{case_id} status={data.get('status')}", errors)
            need(data.get("errors") == [], f"{case_id} errors={data.get('errors')}", errors)
            need(data.get("implementation_commit") == head, f"{case_id} exact-head mismatch", errors)
            need(data.get("secret_safe") is not False, f"{case_id} secret_safe=false", errors)
            if data.get("implementation_commit") != head:
                same_exact_head = False
            evidence_entries.append({
                "case_id": case_id,
                "path": path.relative_to(ROOT).as_posix(),
                "sha256": sha256(path),
                "size_in_bytes": path.stat().st_size,
                "implementation_commit": data.get("implementation_commit"),
            })
    need(seen == expected_ids, f"case evidence mismatch missing={sorted(expected_ids-seen)} extra={sorted(seen-expected_ids)}", errors)
    need(len(evidence_entries) == 30, f"expected 30 evidence files, got {len(evidence_entries)}", errors)
    need(same_exact_head, "mixed/stale P19 evidence detected", errors)

    inspectable: list[Path] = []
    for directory in (CASES, COHERENCE / "contract-guard"):
        if directory.is_dir():
            inspectable.extend(item for item in directory.rglob("*") if item.is_file())
    inspectable.extend(path for path in (PRODUCERS, INHERITED, AUTHORITY_FILE) if path.is_file())
    is_secret_safe = secret_safe(sorted(set(inspectable)), errors)

    mixed_probe = {"case": "P19-T001", "status": "PASS", "errors": [], "implementation_commit": "0" * 40}
    stale_probe = {"case_id": "P19-T001", "status": "PASS", "errors": [], "implementation_commit": BASE}
    malformed_probe = {"case": "P19-T001", "implementation_commit": head}
    mixed_head_rejected = not case_schema_valid(mixed_probe, "P19-T001", head)
    stale_head_rejected = not case_schema_valid(stale_probe, "P19-T001", head)
    malformed_evidence_rejected = not case_schema_valid(malformed_probe, "P19-T001", head)
    unsafe_evidence_rejected = any(pattern.search(b"gak_synthetic_negative_probe") for pattern in SECRET_PATTERNS)
    need(mixed_head_rejected, "mixed-head negative oracle did not fail closed", errors)
    need(stale_head_rejected, "stale-head negative oracle did not fail closed", errors)
    need(malformed_evidence_rejected, "malformed-evidence negative oracle did not fail closed", errors)
    need(unsafe_evidence_rejected, "unsafe-evidence negative oracle did not fail closed", errors)

    inherited_bound = bool(p18) and len(public_by_node) == len(PUBLIC_INTEGRATIONS) and all(
        public_by_node.get(node, {}).get("github_commit_exists") is True
        and public_by_node.get(node, {}).get("in_head_ancestry") is True
        for node in PUBLIC_INTEGRATIONS
    )

    index = {
        "node": "P19",
        "case": "P19-T031",
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
        "node": "P19",
        "case": "P19-T031",
        "status": "PASS" if not errors else "FAIL",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P19-T001..P19-T031",
        "review_phase": review_phase,
        "observations": {
            "input_evidence_count": len(evidence_entries),
            "same_exact_head": same_exact_head,
            "secret_safe": is_secret_safe,
            "producer_count": len(producer_run_ids),
            "producer_artifact_count": len(producer_artifacts),
            "producer_run_ids": producer_run_ids,
            "producer_artifacts": producer_artifacts,
            "p18_signed_predecessor_live_bound": bool(p18),
            "public_capability_authority_count": len(public_by_node),
            "inherited_authorities_bound": inherited_bound,
            "mixed_head_rejected": mixed_head_rejected,
            "stale_head_rejected": stale_head_rejected,
            "malformed_evidence_rejected": malformed_evidence_rejected,
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
