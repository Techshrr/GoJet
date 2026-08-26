#!/usr/bin/env python3
"""P15-T029 exact-head pre-sign/final signed accountable closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P15 = ROOT / "artifacts" / "v10" / "P15"
RESULTS = P15 / "results"
PLAN = P15 / "test-plan.json"
REG = P15 / "regression-manifest.json"
REVIEW = P15 / "review.md"
CLOSURE = P15 / "closure.json"
INDEX = P15 / "closure-evidence-index.json"
T029 = RESULTS / "P15-T029.json"
P14A = P15 / "inherited" / "p14-authority.json"
P14 = P15 / "inherited" / "P14"
P14C = P14 / "closure.json"
P14T = P14 / "results" / "P14-T025.json"
P14R = P14 / "review.md"
P14I = P14 / "closure-evidence-index.json"
PRESIGN = P15 / "inherited" / "pre-sign-authority.json"

CASE_DIRS = {
    1: "api", 2: "api", 3: "security", 4: "api", 5: "security", 6: "security", 7: "security",
    8: "security", 9: "security", 10: "security", 11: "security", 12: "security",
    13: "api", 14: "api",
    15: "oauth", 16: "oauth", 17: "oauth", 18: "oauth", 19: "oauth", 20: "oauth", 21: "oauth",
    22: "mail", 23: "security", 24: "browser", 25: "browser", 26: "browser", 27: "audit", 28: "results",
}
CASES = tuple(f"P15-T{i:03d}" for i in range(1, 30))
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
    "P14 Real Support Tickets and Mail Integration",
    "P14 Workspace Support Browser",
    "P14 Admin Tickets Mail Contact Browser",
    "P15 Real Authentication Integration",
    "P15 Authentication Security Integration",
    "P15 Account OAuth Integration",
    "P15 Handoff Mail Audit Integration",
    "P15 Auth Route Browser Authority",
    "P15 Workspace Account Settings Browser Authority",
    "P15 Admin OAuth Browser Authority",
    "P15 Authentication OAuth Account Contract",
)
EXCLUDED = tuple([f"P{i:02d} Closure" for i in range(5, 14)] + ["P14 Support Tickets and Mail Contract"])
ZERO_DEFECTS = {"p0": 0, "p1": 0, "decision_required": 0}
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

P14_SOURCE = "f079c938dbe49d0f55b8b09995e72201cd0aab6e"
P14_INTEGRATION = "9258cb0f3f913b37b03aa8cf3c2938711314d3aa"
P14_RUN = 32763705854
P14_ART = 9533837642
P14_DIG = "sha256:3f334718539e8fdd9cf5896fffdca9c00b8d0fc9a57b03d39795e97e6af853a8"


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
    return P15 / CASE_DIRS[n] / f"{cid}.json"


def validate_plan(errors: list[str]) -> dict[str, Any]:
    req(PLAN.is_file(), "missing P15 test-plan.json", errors)
    if not PLAN.is_file():
        return {}
    try:
        plan = load(PLAN)
    except Exception as exc:
        errors.append(f"invalid P15 test plan: {exc}")
        return {}
    ids = tuple(item.get("id") for item in plan.get("cases", []) if isinstance(item, dict))
    req(ids == CASES, "P15 test-plan case range/order drift", errors)
    closure = plan.get("closure", {})
    req(isinstance(closure, dict), "P15 closure contract missing", errors)
    if isinstance(closure, dict):
        req(closure.get("same_exact_head_required") is True, "P15 same-exact-head closure rule drift", errors)
        req(closure.get("required_case_range") == "P15-T001..P15-T029", "P15 closure case range drift", errors)
        req(closure.get("review_required") is True, "P15 accountable review requirement drift", errors)
        req(closure.get("defect_limits") == ZERO_DEFECTS, "P15 defect limits drift", errors)
    pred = plan.get("predecessor_signed_authority", {})
    req(
        isinstance(pred, dict)
        and pred.get("node") == "P14"
        and pred.get("integration_commit") == P14_INTEGRATION
        and pred.get("signed_source_commit") == P14_SOURCE
        and pred.get("closure_run_id") == P14_RUN
        and pred.get("artifact_id") == P14_ART
        and pred.get("artifact_digest") == P14_DIG
        and pred.get("phase") == "signed"
        and pred.get("merge_authoritative") is True,
        "P14 frozen predecessor authority drift", errors,
    )
    return plan


def validate_regression(head: str, errors: list[str]) -> dict[str, Any]:
    req(REG.is_file(), "missing P15 regression-manifest.json", errors)
    if not REG.is_file():
        return {}
    try:
        manifest = load(REG)
    except Exception as exc:
        errors.append(f"invalid P15 regression manifest: {exc}")
        return {}
    req(manifest.get("implementation_commit") == head, "P15 regression manifest head mismatch", errors)
    workflows = manifest.get("required_workflows", {})
    req(isinstance(workflows, dict) and set(workflows) == set(WF), "P15 regression workflow set mismatch", errors)
    req(manifest.get("missing") == [] and manifest.get("pending") == [] and manifest.get("failed") == [], "P15 regression matrix not fully green", errors)
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
            if name == "P15 Authentication OAuth Account Contract":
                req(item.get("source") == "current-run-contract-job", "P15 local contract authority marker invalid", errors)
    excluded = manifest.get("excluded_revision_specific_workflows", {})
    req(isinstance(excluded, dict) and set(excluded) == set(EXCLUDED), "revision-specific exclusion set drift", errors)
    if isinstance(excluded, dict):
        for name in EXCLUDED:
            rationale = str(excluded.get(name, ""))
            req("revision-specific" in rationale.lower() and "inherited" in rationale.lower(), f"{name} exclusion rationale invalid", errors)
        req(P14_SOURCE in str(excluded.get("P14 Support Tickets and Mail Contract", "")), "P14 contract exclusion does not bind signed source", errors)
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
        req(data.get("status") == "PASS", f"{cid} evidence not PASS", errors)
        if "errors" in data:
            req(data.get("errors") == [], f"{cid} evidence has errors", errors)
        req(data.get("implementation_commit", data.get("exact_head")) == head, f"{cid} exact-head mismatch", errors)
        if cid == "P15-T028":
            obs = data.get("observations", {})
            req(obs.get("input_evidence_count") == 27, "T028 input evidence count drift", errors)
            req(obs.get("same_exact_head") is True, "T028 exact-head coherence false", errors)
            req(obs.get("producer_coherent") is True, "T028 producer coherence false", errors)
            req(obs.get("secret_safe") is True, "T028 secret-safe evidence false", errors)
            req(obs.get("mixed_head_rejected") is True, "T028 mixed-head rejection false", errors)
            req(obs.get("unsafe_evidence_rejected") is True, "T028 unsafe-evidence rejection false", errors)
            req(obs.get("reviewable_hashed_case_evidence") is True, "T028 hashed evidence false", errors)
            req(len(obs.get("producer_run_ids", {})) == 8, "T028 producer count drift", errors)
            counts = obs.get("browser_capture_counts", {})
            req(int(counts.get("P15-T024", 0)) >= 12, "T028 T024 capture count insufficient", errors)
            req(int(counts.get("P15-T025", 0)) >= 12, "T028 T025 capture count insufficient", errors)
            req(int(counts.get("P15-T026", 0)) >= 9, "T028 T026 capture count insufficient", errors)
        entries.append({
            "case_id": cid,
            "path": str(path.relative_to(ROOT)),
            "sha256": digest(path),
            "status": data.get("status"),
            "implementation_commit": data.get("implementation_commit", data.get("exact_head")),
        })
    req(tuple(item["case_id"] for item in entries) == INPUT, "P15 closure evidence set/order mismatch", errors)
    return entries


def validate_p14(errors: list[str]) -> dict[str, Any]:
    req(P14A.is_file(), "missing P14 live authority metadata", errors)
    for path in (P14C, P14T, P14R, P14I):
        req(path.is_file(), f"missing inherited P14 authority: {path}", errors)
    if not P14A.is_file() or not all(path.is_file() for path in (P14C, P14T, P14R, P14I)):
        return {}
    try:
        authority = load(P14A)
        closure = load(P14C)
        t025 = load(P14T)
    except Exception as exc:
        errors.append(f"invalid inherited P14 authority: {exc}")
        return {}
    req(
        authority.get("source_commit") == P14_SOURCE
        and authority.get("closure_run_id") == P14_RUN
        and authority.get("artifact_id") == P14_ART
        and authority.get("artifact_digest") == P14_DIG
        and authority.get("workflow_head_sha") == P14_SOURCE
        and authority.get("workflow_status") == "completed"
        and authority.get("workflow_conclusion") == "success"
        and authority.get("artifact_expired") is False
        and authority.get("archive_sha256") == P14_DIG.removeprefix("sha256:"),
        "P14 live authority metadata invalid", errors,
    )
    req(closure.get("node") == "P14" and closure.get("implementation_commit") == P14_SOURCE, "P14 signed closure identity invalid", errors)
    req(closure.get("status") == "PASS" and closure.get("phase") == "signed" and closure.get("merge_authoritative") is True, "P14 predecessor is not signed merge authority", errors)
    req(closure.get("defects") == ZERO_DEFECTS and closure.get("required_regression_workflow_count") == 43, "P14 predecessor closure ledger/matrix invalid", errors)
    req(t025.get("case_id") == "P14-T025" and t025.get("implementation_commit") == P14_SOURCE and t025.get("status") == "PASS", "P14-T025 signed evidence invalid", errors)
    req(t025.get("phase") == "signed" and t025.get("merge_authoritative") is True and t025.get("defects") == ZERO_DEFECTS, "P14-T025 is not signed authority", errors)
    review_text = P14R.read_text(encoding="utf-8")
    req("Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**" in review_text, "P14 signed review marker missing", errors)
    return authority


def primary_review_status(text: str, errors: list[str]) -> str | None:
    lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", text, flags=re.MULTILINE)
    req(len(lines) == 1, f"P15 review must contain exactly one primary Status line, got {len(lines)}", errors)
    if len(lines) != 1:
        return None
    status = lines[0]
    req(status in (PENDING, SIGNED), f"unsupported P15 primary review status: {status}", errors)
    return status


def validate_review_phase_parser(errors: list[str]) -> None:
    sample = PENDING + "\n\n`" + SIGNED + "`\n"
    req(
        primary_review_status(sample, errors) == PENDING,
        "P15 review phase parser accepted a quoted future signed marker as authority",
        errors,
    )


def parse_review(head: str, errors: list[str]) -> dict[str, Any]:
    req(REVIEW.is_file(), "missing P15 review.md", errors)
    if not REVIEW.is_file():
        return {"phase": "invalid", "merge_authoritative": False}
    text = REVIEW.read_text(encoding="utf-8")
    status = primary_review_status(text, errors)
    if status == PENDING:
        return {
            "phase": "pre-sign",
            "status": "PENDING",
            "merge_authoritative": False,
            "review_sha256": digest(REVIEW),
        }
    if status != SIGNED:
        return {"phase": "invalid", "status": "INVALID", "merge_authoritative": False, "review_sha256": digest(REVIEW)}

    def grab(label: str, pattern: str) -> str | None:
        match = re.search(pattern, text)
        req(match is not None, f"signed review missing {label}", errors)
        return match.group(1) if match else None

    pre_sha = grab("pre-sign SHA", r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`")
    run = grab("pre-sign closure run", r"Pre-sign T029 closure run: `([0-9]+)`")
    artifact = grab("pre-sign closure artifact", r"Pre-sign T029 closure artifact: `([0-9]+)`")
    artifact_digest = grab("pre-sign closure digest", r"Pre-sign T029 closure digest: `(sha256:[0-9a-f]{64})`")
    reviewer = grab("reviewer", r"Accountable reviewer: `([^`]+)`")
    date = grab("review date", r"Review date: `(\d{4}-\d{2}-\d{2})`")
    req("Evidence disposition: `P15-T001..P15-T029 PASS`" in text, "signed review evidence disposition invalid", errors)
    req("P0/P1/DECISION REQUIRED: `0/0/0`" in text, "signed review zero-defect ledger missing", errors)
    req("Review-only signed child: `true`" in text, "signed review child marker missing", errors)
    req("Signed revision requires complete same-revision affected matrix before merge: `true`" in text, "signed review same-revision rule missing", errors)

    try:
        parent = git("rev-parse", "HEAD^")
        changed = tuple(line for line in git("diff", "--name-only", "HEAD^..HEAD").splitlines() if line)
        req(changed == ("artifacts/v10/P15/review.md",), f"signed child is not review-only: {changed}", errors)
        req(pre_sha == parent, "signed review pre-sign SHA is not direct parent", errors)
    except Exception as exc:
        errors.append(f"cannot validate review-only signed child: {exc}")

    req(PRESIGN.is_file(), "missing live pre-sign closure authority metadata", errors)
    if PRESIGN.is_file():
        try:
            authority = load(PRESIGN)
            req(
                authority.get("source_commit") == pre_sha
                and authority.get("closure_run_id") == int(run or 0)
                and authority.get("artifact_id") == int(artifact or 0)
                and authority.get("artifact_digest") == artifact_digest
                and authority.get("workflow_head_sha") == pre_sha
                and authority.get("workflow_status") == "completed"
                and authority.get("workflow_conclusion") == "success"
                and authority.get("artifact_expired") is False,
                "pre-sign closure live authority metadata invalid", errors,
            )
        except Exception as exc:
            errors.append(f"invalid pre-sign authority metadata: {exc}")

    return {
        "phase": "signed",
        "status": "SIGNED",
        "merge_authoritative": True,
        "pre_sign_implementation_commit": pre_sha,
        "pre_sign_closure_run": int(run or 0),
        "pre_sign_closure_artifact": int(artifact or 0),
        "pre_sign_closure_artifact_digest": artifact_digest,
        "reviewer": reviewer,
        "review_date": date,
        "review_only_signed_child": True,
        "review_sha256": digest(REVIEW),
    }


def main() -> int:
    errors: list[str] = []
    validate_review_phase_parser(errors)
    head = git("rev-parse", "HEAD")
    plan = validate_plan(errors)
    regression = validate_regression(head, errors)
    cases = validate_cases(head, errors)
    p14 = validate_p14(errors)
    review = parse_review(head, errors)
    phase = review.get("phase", "invalid")
    merge_authoritative = phase == "signed" and not errors

    closure = {
        "node": "P15",
        "case_id": "P15-T029",
        "status": "PASS" if not errors else "FAIL",
        "generated_at": now(),
        "implementation_commit": head,
        "phase": phase,
        "merge_authoritative": merge_authoritative,
        "input_evidence_count": len(cases),
        "required_regression_workflow_count": len(WF),
        "defects": ZERO_DEFECTS if not errors else {"p0": 0, "p1": 0, "decision_required": len(errors)},
        "predecessor": {
            "node": "P14",
            "integration_commit": P14_INTEGRATION,
            "signed_source_commit": P14_SOURCE,
            "closure_run_id": P14_RUN,
            "artifact_id": P14_ART,
            "artifact_digest": P14_DIG,
        },
        "review": review,
        "errors": errors,
    }
    P15.mkdir(parents=True, exist_ok=True)
    RESULTS.mkdir(parents=True, exist_ok=True)
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    t029 = {
        "case_id": "P15-T029",
        "status": closure["status"],
        "generated_at": closure["generated_at"],
        "implementation_commit": head,
        "phase": phase,
        "merge_authoritative": merge_authoritative,
        "input_evidence_count": len(cases),
        "required_regression_workflow_count": len(WF),
        "defects": closure["defects"],
        "errors": errors,
    }
    T029.write_text(json.dumps(t029, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    index = {
        "node": "P15",
        "case": "P15-T029",
        "generated_at": now(),
        "implementation_commit": head,
        "phase": phase,
        "merge_authoritative": merge_authoritative,
        "case_evidence": cases + [{
            "case_id": "P15-T029",
            "path": str(T029.relative_to(ROOT)),
            "sha256": digest(T029),
            "status": t029["status"],
            "implementation_commit": head,
        }],
        "regression_manifest": {
            "path": str(REG.relative_to(ROOT)),
            "sha256": digest(REG) if REG.is_file() else None,
            "required_workflow_count": len(regression.get("required_workflows", {})) if isinstance(regression, dict) else 0,
        },
        "predecessor_authority": {
            "metadata_path": str(P14A.relative_to(ROOT)),
            "metadata_sha256": digest(P14A) if P14A.is_file() else None,
            "source_commit": p14.get("source_commit") if isinstance(p14, dict) else None,
        },
        "review": {"path": str(REVIEW.relative_to(ROOT)), "sha256": digest(REVIEW) if REVIEW.is_file() else None},
        "closure": {"path": str(CLOSURE.relative_to(ROOT)), "sha256": digest(CLOSURE)},
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(closure, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())