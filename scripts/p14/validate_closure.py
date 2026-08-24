#!/usr/bin/env python3
"""P14-T025 exact-head pre-sign/final signed accountable closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P14 = ROOT / "artifacts" / "v10" / "P14"
RESULTS = P14 / "results"
PLAN = P14 / "test-plan.json"
REG = P14 / "regression-manifest.json"
COH = P14 / "evidence-index.json"
REVIEW = P14 / "review.md"
CLOSURE = P14 / "closure.json"
INDEX = P14 / "closure-evidence-index.json"
T025 = RESULTS / "P14-T025.json"

P13A = P14 / "inherited" / "p13-authority.json"
P13 = P14 / "inherited" / "P13"
P13C = P13 / "closure.json"
P13T = P13 / "results" / "P13-T027.json"
P13R = P13 / "review.md"
P13I = P13 / "closure-evidence-index.json"

P12A = P14 / "inherited" / "p12-authority.json"
P06A = P14 / "inherited" / "p06-authority.json"
P09A = P14 / "inherited" / "p09-authority.json"

CASE_DIRS = {
    1: "api", 2: "rbac", 3: "api", 4: "security",
    5: "entitlement", 6: "entitlement", 7: "entitlement",
    8: "security", 9: "security", 10: "security", 11: "security", 12: "security",
    13: "api",
    14: "mail", 15: "mail", 16: "mail", 17: "mail",
    18: "notification", 19: "rbac", 20: "api", 21: "audit",
    22: "browser", 23: "browser", 24: "results",
}
CASES = tuple(f"P14-T{i:03d}" for i in range(1, 26))
INPUT = CASES[:-1]

WF = (
    "P00 Bootstrap and G0 Traceability",
    "P01 Engineering Foundation",
    "P02 Brand Foundation",
    "P03 Design System",
    "P04 Product Shells",
    "P05 Links Domain Contract",
    "P05 Real Integration",
    "P05 Workspace Browser",
    "P06 Custom Domains",
    "P06 Real Integration",
    "P06 Workspace Domains Browser",
    "P07 Analytics Contract",
    "P07 Real Integration",
    "P07 Workspace Analytics Browser",
    "P08 QR Contract",
    "P08 Real QR Integration",
    "P08 Workspace QR Browser",
    "P08 Evidence Coherence",
    "P09 Files Contract",
    "P09 Real Files and ClamAV Integration",
    "P09 Files Health and Installer Preflight",
    "P09 Workspace Files Browser",
    "P09 Evidence Coherence",
    "P10 Text Contract",
    "P10 Real Text Integration",
    "P10 Workspace Text Browser",
    "P10 Evidence Coherence",
    "P11 Bio Contract",
    "P11 Real Bio Integration",
    "P11 Workspace Bio Browser",
    "P11 Evidence Coherence",
    "P12 Workspace Organization Contract",
    "P12 Real Workspace Organization Integration",
    "P12 Workspace Organization Browser",
    "P12 Evidence Coherence",
    "P13 Billing Payments Entitlements Contract",
    "P13 Real Billing Payments Entitlements Integration",
    "P13 Billing Commerce Browser",
    "P13 Evidence Coherence",
    "P14 Support Tickets and Mail Contract",
    "P14 Real Support Tickets and Mail Integration",
    "P14 Workspace Support Browser",
    "P14 Admin Tickets Mail Contact Browser",
)
EXCLUDED = tuple(f"P{i:02d} Closure" for i in range(5, 14))
ZERO_DEFECTS = {"p0": 0, "p1": 0, "decision_required": 0}
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

P13_SOURCE = "24cdbdf848bf722e53e38ed15dce12e1d42eb9d2"
P13_INTEGRATION = "a94f1d9894916b995a2379571f6ab3de520fc4ba"
P13_RUN = 32711262325
P13_ART = 9514396804
P13_DIG = "sha256:494a7942272afac7588eab153c07daf5a1f557c10b58b0dbd915eeda8709e998"

P12_SOURCE = "9d49d5ebf0e697ae9cd6537c432c27a15edc60bd"
P12_RUN = 32663159008
P12_ART = 9499336765
P12_DIG = "sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a"

P06_SOURCE = "4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23"
P06_RUN = 32519298309
P06_ART = 9460016077
P06_DIG = "sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8"

P09_SOURCE = "eafa369a9c150c22c2c14c9f21848a9544f4f96a"
P09_RUN = 32618657967
P09_ART = 9487743843
P09_DIG = "sha256:f12aeeb5503bf375314f1d13a2d9833180d6617322765cef2aae0d728cc278d7"


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def load(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(131072), b""):
            h.update(chunk)
    return h.hexdigest()


def req(ok: bool, message: str, errors: list[str]) -> None:
    if not ok:
        errors.append(message)


def case_path(cid: str) -> Path:
    n = int(cid[-3:])
    return P14 / CASE_DIRS[n] / f"{cid}.json"


def validate_plan(errors: list[str]) -> dict[str, Any]:
    req(PLAN.is_file(), "missing P14 test-plan.json", errors)
    if not PLAN.is_file():
        return {}
    try:
        plan = load(PLAN)
    except Exception as exc:
        errors.append(f"invalid P14 test plan: {exc}")
        return {}
    ids = tuple(item.get("id") for item in plan.get("cases", []) if isinstance(item, dict))
    req(ids == CASES, "P14 test-plan case range/order drift", errors)
    closure = plan.get("closure", {})
    req(isinstance(closure, dict), "P14 closure contract missing", errors)
    if isinstance(closure, dict):
        req(closure.get("same_exact_head_required") is True, "P14 same-exact-head closure rule drift", errors)
        req(closure.get("required_case_range") == "P14-T001..P14-T025", "P14 closure case range drift", errors)
        req(closure.get("review_required") is True, "P14 accountable review requirement drift", errors)
        req(closure.get("defect_limits") == ZERO_DEFECTS, "P14 defect limits drift", errors)
        req(closure.get("phases") == {
            "pre-sign": {"review_status": "PENDING", "merge_authoritative": False},
            "signed": {"review_status": "SIGNED", "merge_authoritative": True},
        }, "P14 closure phases drift", errors)
    pred = plan.get("predecessor_signed_authority", {})
    req(
        isinstance(pred, dict)
        and pred.get("node") == "P13"
        and pred.get("integration_commit") == P13_INTEGRATION
        and pred.get("signed_source_commit") == P13_SOURCE
        and pred.get("closure_run_id") == P13_RUN
        and pred.get("artifact_id") == P13_ART
        and pred.get("artifact_digest") == P13_DIG
        and pred.get("phase") == "signed"
        and pred.get("merge_authoritative") is True,
        "P13 frozen predecessor authority drift", errors,
    )
    return plan


def validate_regression(head: str, errors: list[str]) -> dict[str, Any]:
    req(REG.is_file(), "missing P14 regression-manifest.json", errors)
    if not REG.is_file():
        return {}
    try:
        manifest = load(REG)
    except Exception as exc:
        errors.append(f"invalid P14 regression manifest: {exc}")
        return {}
    req(manifest.get("implementation_commit") == head, "P14 regression manifest head mismatch", errors)
    workflows = manifest.get("required_workflows", {})
    req(isinstance(workflows, dict) and set(workflows) == set(WF), "P14 regression workflow set mismatch", errors)
    req(manifest.get("missing") == [] and manifest.get("pending") == [] and manifest.get("failed") == [], "P14 regression matrix not fully green", errors)
    if isinstance(workflows, dict):
        for name in WF:
            item = workflows.get(name, {})
            req(
                isinstance(item, dict)
                and item.get("head_sha") == head
                and item.get("status") == "completed"
                and item.get("conclusion") == "success"
                and isinstance(item.get("run_id"), int)
                and item.get("run_id", 0) > 0,
                f"{name} exact-head success record invalid", errors,
            )
            if name == "P14 Support Tickets and Mail Contract":
                req(item.get("source") == "current-run-contract-job", "P14 local contract authority marker invalid", errors)
    excluded = manifest.get("excluded_revision_specific_workflows", {})
    req(isinstance(excluded, dict) and set(excluded) == set(EXCLUDED), "revision-specific closure exclusion set drift", errors)
    if isinstance(excluded, dict):
        for name in EXCLUDED:
            rationale = str(excluded.get(name, ""))
            req("revision-specific" in rationale.lower() and "inherited" in rationale.lower(), f"{name} exclusion rationale invalid", errors)
        req(P13_SOURCE in str(excluded.get("P13 Closure", "")), "P13 closure exclusion does not bind signed source", errors)
    return manifest


def validate_cases(head: str, errors: list[str]) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    for cid in INPUT:
        path = case_path(cid)
        req(path.is_file(), f"missing evidence {cid}", errors)
        if not path.is_file():
            continue
        try:
            data = load(path)
        except Exception as exc:
            errors.append(f"invalid {cid}: {exc}")
            continue
        req(data.get("case_id", data.get("case")) == cid, f"{cid} identity invalid", errors)
        req(data.get("status") == "PASS" and data.get("errors") == [], f"{cid} evidence not PASS", errors)
        req(data.get("implementation_commit") == head, f"{cid} exact-head mismatch", errors)
        if cid == "P14-T024":
            obs = data.get("observations", {})
            req(obs.get("input_evidence_count") == 23, "T024 input evidence count drift", errors)
            req(obs.get("same_exact_head") is True, "T024 exact-head coherence false", errors)
            req(obs.get("secret_safe") is True, "T024 secret-safe evidence false", errors)
            req(obs.get("mixed_head_rejected") is True, "T024 mixed-head rejection false", errors)
            req(obs.get("inspectable_runtime_browser_mail_clamav_evidence") is True, "T024 inspectable evidence false", errors)
            req(isinstance(obs.get("t022_capture_count"), int) and obs.get("t022_capture_count", 0) >= 20, "T024 T022 capture count insufficient", errors)
            req(isinstance(obs.get("t023_capture_count"), int) and obs.get("t023_capture_count", 0) >= 36, "T024 T023 capture count insufficient", errors)
        entries.append({
            "case_id": cid,
            "path": str(path.relative_to(ROOT)),
            "sha256": digest(path),
            "status": data.get("status"),
            "implementation_commit": data.get("implementation_commit"),
        })
    req(tuple(item["case_id"] for item in entries) == INPUT, "P14 closure evidence set/order mismatch", errors)
    return entries


def validate_authority(path: Path, *, source: str, run: int, artifact: int, digest_value: str, label: str, errors: list[str]) -> dict[str, Any]:
    req(path.is_file(), f"missing {label} authority metadata", errors)
    if not path.is_file():
        return {}
    try:
        data = load(path)
    except Exception as exc:
        errors.append(f"invalid {label} authority metadata: {exc}")
        return {}
    req(
        data.get("source_commit") == source
        and data.get("closure_run_id") == run
        and data.get("artifact_id") == artifact
        and data.get("artifact_digest") == digest_value
        and data.get("workflow_head_sha") == source
        and data.get("workflow_status") == "completed"
        and data.get("workflow_conclusion") == "success"
        and data.get("artifact_expired") is False,
        f"{label} live authority metadata invalid", errors,
    )
    return data


def validate_p13(errors: list[str]) -> dict[str, Any]:
    authority = validate_authority(
        P13A, source=P13_SOURCE, run=P13_RUN, artifact=P13_ART, digest_value=P13_DIG,
        label="P13", errors=errors,
    )
    for path in (P13C, P13T, P13R, P13I):
        req(path.is_file(), f"missing inherited P13 authority: {path}", errors)
    if not all(path.is_file() for path in (P13C, P13T, P13R, P13I)):
        return authority
    try:
        closure, t027, index = load(P13C), load(P13T), load(P13I)
    except Exception as exc:
        errors.append(f"invalid inherited P13 JSON: {exc}")
        return authority
    review_text = P13R.read_text(encoding="utf-8")
    req(authority.get("archive_sha256") == P13_DIG.removeprefix("sha256:"), "P13 signed artifact archive digest mismatch", errors)
    req(closure.get("implementation_commit") == P13_SOURCE and closure.get("status") == "PASS", "P13 signed closure identity/status invalid", errors)
    req(closure.get("phase") == "signed" and closure.get("merge_authoritative") is True, "P13 predecessor is not signed merge authority", errors)
    req(closure.get("defects") == ZERO_DEFECTS, "P13 predecessor defect ledger is not zero", errors)
    req(t027.get("implementation_commit") == P13_SOURCE and t027.get("status") == "PASS", "P13-T027 signed evidence invalid", errors)
    t027_details = t027.get("details", {})
    req(
        isinstance(t027_details, dict)
        and t027_details.get("closure_phase") == "signed"
        and t027_details.get("merge_authoritative") is True
        and t027_details.get("defects") == ZERO_DEFECTS,
        "P13-T027 is not signed authority", errors,
    )
    req("Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**" in review_text, "P13 signed review marker invalid", errors)
    req("Accountable reviewer identity: **GPT-5.6 Sol — P13 Technical Review**" in review_text, "P13 reviewer identity missing", errors)
    req(isinstance(index, dict) and index.get("implementation_commit") == P13_SOURCE, "P13 closure index head mismatch", errors)
    return authority


def validate_review(head: str, errors: list[str]) -> tuple[str, bool, dict[str, Any]]:
    req(REVIEW.is_file(), "missing P14 review.md", errors)
    if not REVIEW.is_file():
        return "pre-sign", False, {}
    text = REVIEW.read_text(encoding="utf-8")
    pending = PENDING in text
    signed = SIGNED in text
    req(pending ^ signed, "P14 review must be exactly pending or signed", errors)
    if pending:
        return "pre-sign", False, {"status": "PENDING", "review_sha256": digest(REVIEW)}
    phase = "signed"
    merge_authoritative = True
    sha_match = re.search(r"Pre-sign exact implementation SHA: `([0-9a-f]{40})`", text)
    reviewer_ok = "Accountable reviewer identity: **GPT-5.6 Sol — P14 Technical Review**" in text
    date_match = re.search(r"Review date: \*\*(\d{4}-\d{2}-\d{2})\*\*", text)
    run_match = re.search(r"Pre-sign closure run: `(\d+)`", text)
    art_match = re.search(r"Pre-sign closure artifact: `(\d+)`", text)
    dig_match = re.search(r"Pre-sign closure artifact digest: `(sha256:[0-9a-f]{64})`", text)
    req(sha_match is not None, "signed review missing pre-sign exact implementation SHA", errors)
    req(reviewer_ok, "signed review reviewer identity invalid", errors)
    req(date_match is not None, "signed review date missing/invalid", errors)
    req(run_match is not None and art_match is not None and dig_match is not None, "signed review missing pre-sign closure authority", errors)
    for required in (
        "P14-T001..P14-T021: PASS",
        "P14-T022: PASS",
        "P14-T023: PASS",
        "P14-T024: PASS",
        "P14-T025: PASS",
        "P0 defects: 0",
        "P1 defects: 0",
        "`DECISION REQUIRED`: 0",
        "P15 identity",
        "P17",
        "P19",
    ):
        req(required in text, f"signed review missing required disposition: {required}", errors)
    pre_sign_sha = sha_match.group(1) if sha_match else ""
    parent = git("rev-parse", "HEAD^")
    req(parent == pre_sign_sha, "signed revision parent is not recorded pre-sign implementation SHA", errors)
    changed = [line for line in git("diff", "--name-only", "HEAD^", "HEAD").splitlines() if line]
    req(changed == ["artifacts/v10/P14/review.md"], f"signed child is not review-only: {changed}", errors)
    req(head != pre_sign_sha, "signed revision did not change HEAD", errors)
    return phase, merge_authoritative, {
        "status": "SIGNED",
        "review_sha256": digest(REVIEW),
        "pre_sign_implementation_commit": pre_sign_sha,
        "pre_sign_closure_run": int(run_match.group(1)) if run_match else None,
        "pre_sign_closure_artifact": int(art_match.group(1)) if art_match else None,
        "pre_sign_closure_artifact_digest": dig_match.group(1) if dig_match else None,
        "review_date": date_match.group(1) if date_match else None,
        "reviewer": "GPT-5.6 Sol — P14 Technical Review",
        "review_only_signed_child": changed == ["artifacts/v10/P14/review.md"] and parent == pre_sign_sha,
    }


def run_closure(_: bool = True) -> int:
    head = git("rev-parse", "HEAD")
    errors: list[str] = []
    validate_plan(errors)
    validate_regression(head, errors)
    inputs = validate_cases(head, errors)
    validate_p13(errors)
    validate_authority(P12A, source=P12_SOURCE, run=P12_RUN, artifact=P12_ART, digest_value=P12_DIG, label="P12", errors=errors)
    validate_authority(P06A, source=P06_SOURCE, run=P06_RUN, artifact=P06_ART, digest_value=P06_DIG, label="P06", errors=errors)
    validate_authority(P09A, source=P09_SOURCE, run=P09_RUN, artifact=P09_ART, digest_value=P09_DIG, label="P09", errors=errors)
    phase, merge_authoritative, review = validate_review(head, errors)

    defects = ZERO_DEFECTS.copy() if not errors else {"p0": 0, "p1": 1, "decision_required": len(errors)}
    status = "PASS" if not errors else "FAIL"
    closure = {
        "node": "P14",
        "case_id": "P14-T025",
        "status": status,
        "phase": phase,
        "merge_authoritative": merge_authoritative if not errors else False,
        "implementation_commit": head,
        "generated_at": now(),
        "input_evidence_count": len(inputs),
        "required_regression_workflow_count": len(WF),
        "defects": defects,
        "review": review,
        "predecessor": {
            "node": "P13",
            "signed_source_commit": P13_SOURCE,
            "integration_commit": P13_INTEGRATION,
            "closure_run_id": P13_RUN,
            "artifact_id": P13_ART,
            "artifact_digest": P13_DIG,
        },
        "functional_inherited_authorities": {
            "P12": {"source_commit": P12_SOURCE, "closure_run_id": P12_RUN, "artifact_id": P12_ART, "artifact_digest": P12_DIG},
            "P06": {"source_commit": P06_SOURCE, "closure_run_id": P06_RUN, "artifact_id": P06_ART, "artifact_digest": P06_DIG},
            "P09": {"source_commit": P09_SOURCE, "closure_run_id": P09_RUN, "artifact_id": P09_ART, "artifact_digest": P09_DIG},
        },
        "errors": errors,
    }
    result = {
        "case_id": "P14-T025",
        "status": status,
        "implementation_commit": head,
        "phase": phase,
        "merge_authoritative": closure["merge_authoritative"],
        "defects": defects,
        "input_evidence_count": len(inputs),
        "required_regression_workflow_count": len(WF),
        "errors": errors,
    }
    index = {
        "node": "P14",
        "case_id": "P14-T025",
        "implementation_commit": head,
        "generated_at": now(),
        "phase": phase,
        "merge_authoritative": closure["merge_authoritative"],
        "input_evidence": inputs,
        "regression_manifest_sha256": digest(REG) if REG.is_file() else None,
        "evidence_index_sha256": digest(COH) if COH.is_file() else None,
        "review_sha256": digest(REVIEW) if REVIEW.is_file() else None,
        "closure_sha256": None,
    }

    RESULTS.mkdir(parents=True, exist_ok=True)
    T025.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    index["closure_sha256"] = digest(CLOSURE)
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(run_closure(True))
