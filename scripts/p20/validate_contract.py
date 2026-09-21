#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "6e628b9879eb4dddf335a324e4f4d7ae3a77cd5c"
P19_SIGNED_SOURCE = "44ea701ae464550ce920c5f2131428270e22fb41"
P19_SIGNED_REVIEW_BLOB = "02be683f6750e681fe9a9d6a4fc41f02c08f872b"
FROZEN_TEST_PLAN_BLOB = "f6d7831be48fcc8f378ad1a90efbb7e96e01c4e8"
PENDING_REVIEW_BLOB = "9c1410dc02f62bfead5a12b2d323291152bf1ea6"

AUTHORITY_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
    "artifacts/v10/P19/review.md": P19_SIGNED_REVIEW_BLOB,
}
EXPECTED_CONTRACT_FILES = {
    "artifacts/v10/P20/test-plan.json",
    "artifacts/v10/P20/review.md",
    "scripts/p20/validate_contract.py",
    ".github/workflows/p20-whole-product-verification.yml",
}
EXPECTED_P0 = [
    "register", "verify", "login", "link", "redirect", "analytics", "QR", "file",
    "text", "bio", "domain", "ticket", "billing", "notification", "admin",
]
EXPECTED_RC_GATES = [f"G{i}" for i in range(0, 11)]
EXPECTED_P20_PASS_GATES = ["G0", "G3", "G4", "G5", "G6", "G7", "G8", "G9", "G10"]
EXPECTED_APPS = ["admin", "docs", "site", "workspace"]
EXPECTED_SERVICES = [
    "redirectengine", "analyticsworker", "analyticsreconciler", "platformapi",
    "mailworker", "fileworker", "operationsmonitor", "logreceiver",
]
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def ancestor(older: str, newer: str) -> bool:
    return subprocess.run(
        ["git", "merge-base", "--is-ancestor", older, newer],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    ).returncode == 0


def blob(revision: str, path: str) -> str:
    return git("rev-parse", f"{revision}:{path}")


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def derive_contract_authority(head: str) -> str:
    commits = [
        line for line in git("rev-list", "--ancestry-path", "--reverse", f"{BASE}..{head}").splitlines()
        if line
    ]
    return commits[0] if commits else ""


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P20/test-plan.json")
    review_path = Path("artifacts/v10/P20/review.md")
    need(plan_path.is_file(), "missing P20 test-plan.json", errors)
    need(review_path.is_file(), "missing P20 review.md", errors)
    if errors:
        print(json.dumps({"node": "P20", "status": "FAIL", "errors": errors}, indent=2))
        return 1

    head = git("rev-parse", "HEAD")
    authority = derive_contract_authority(head)
    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    review = review_path.read_text(encoding="utf-8")

    need(bool(authority) and re.fullmatch(r"[0-9a-f]{40}", authority) is not None,
         "cannot derive P20 contract authority", errors)
    if authority:
        need(git("rev-parse", f"{authority}^") == BASE,
             "P20 contract authority must be direct child of P19 integration", errors)
        need(ancestor(authority, head), "P20 HEAD must descend from contract authority", errors)
        changed = {x for x in git("diff", "--name-only", f"{BASE}..{authority}").splitlines() if x}
        need(changed == EXPECTED_CONTRACT_FILES,
             f"P20 contract-freeze path set drift: {sorted(changed)}", errors)
        for path in ("scripts/p20/validate_contract.py", ".github/workflows/p20-whole-product-verification.yml"):
            try:
                need(blob("HEAD", path) == blob(authority, path),
                     f"frozen P20 contract tooling drift: {path}", errors)
            except Exception as exc:
                errors.append(f"cannot bind frozen P20 tooling {path}: {exc}")

    need(ancestor(P19_SIGNED_SOURCE, BASE),
         "P19 signed source must remain in P20 base ancestry", errors)
    try:
        base_parents = git("show", "-s", "--format=%P", BASE).split()
        need(base_parents == [
            "43e693b10c0118e32d7f14c61156e0b06c155111",
            P19_SIGNED_SOURCE,
        ], f"P19 integration parent authority drift: {base_parents}", errors)
        need(blob("HEAD", "artifacts/v10/P20/test-plan.json") == FROZEN_TEST_PLAN_BLOB,
             "frozen P20 test-plan blob drift", errors)
        if authority:
            need(blob(authority, "artifacts/v10/P20/test-plan.json") == FROZEN_TEST_PLAN_BLOB,
                 "authority P20 test-plan blob mismatch", errors)
            need(blob(authority, "artifacts/v10/P20/review.md") == PENDING_REVIEW_BLOB,
                 "authority pending P20 review blob mismatch", errors)
        for path, expected in AUTHORITY_BLOBS.items():
            need(blob("HEAD", path) == expected, f"normative authority blob drift: {path}", errors)
    except Exception as exc:
        errors.append(f"cannot bind P20 frozen authority blobs: {exc}")

    need(plan.get("schema") == "gojet.p20-test-plan.v1", "P20 test-plan schema drift", errors)
    need(plan.get("node") == "P20", "node must remain P20", errors)
    need(plan.get("title") == "Whole Product Verification", "P20 title drift", errors)
    need(plan.get("issue") == 53, "P20 issue drift", errors)
    need(plan.get("base_integration_commit") == BASE, "P20 base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "P20 specification IDs/order drift", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node": "P19",
        "integration_commit": BASE,
        "signed_source_commit": P19_SIGNED_SOURCE,
        "closure_run_id": 33268403700,
        "artifact_id": 9719405957,
        "artifact_digest": "sha256:de62ca1484b7eeedc7249303fa525885584f49d5983ca9378000eb7bb82e7bd2",
        "phase": "signed",
        "review_phase": "signed",
        "review_only_signed_child": True,
        "merge_authoritative": True,
        "case_range": "P19-T001..P19-T032",
        "applicable_matrix": "19/19",
        "gate_contribution": "G4/G5/G7/G8/G9 5/5",
        "defects": "0/0/0",
    }, "P19 predecessor signed authority drift", errors)

    scope = plan.get("scope_contract", {})
    need(scope.get("goal") == "Unified regression across every implemented Surface and capability before native release packaging.",
         "P20 goal drift", errors)
    required_scope = {
        "capability matrix closure for integrated P00-P19 authority",
        "candidate implementation commit/schema catalog/frontend build/evidence index freeze",
        "real correlated P0 workflows",
        "cross-surface consistency",
        "failure and recovery behavior",
        "release candidate evidence index",
        "defect closure ledger",
        "P20 release-wide verification contribution through G0-G10",
    }
    need(set(scope.get("in_scope", [])) == required_scope, "P20 in-scope boundary drift", errors)
    need(scope.get("p21_p22_native_capabilities") == ["CAP-NATIVE-INSTALL", "CAP-NATIVE-ONLY-RELEASE"],
         "P21/P22 native capability boundary drift", errors)
    need(scope.get("p21_p22_completion_claim") == "PROHIBITED",
         "P20 must not claim P21/P22 completion", errors)
    excluded = set(scope.get("excluded_work", []))
    for required in (
        "new unplanned product features",
        "invented routes or acquisition surfaces",
        "reinterpretation of signed P00-P19 authority",
        "mock substitutes for required real P0 flows",
        "weakening security or fail-closed behavior",
        "P21 native package implementation",
        "P22 fresh-install or owner-controlled production validation claims",
    ):
        need(required in excluded, f"P20 excluded-work boundary missing: {required}", errors)

    freeze = plan.get("candidate_freeze_contract", {})
    need(freeze.get("candidate_commit") == "EXACT_HEAD", "P20 exact candidate freeze drift", errors)
    need(freeze.get("schema_catalog") == "DETERMINISTIC_CURRENT_REPOSITORY_INVENTORY",
         "P20 schema catalog freeze drift", errors)
    need(freeze.get("frontend_apps") == EXPECTED_APPS, "P20 frontend app inventory drift", errors)
    need(freeze.get("frontend_build") == "DETERMINISTIC_AND_EXACT_HEAD",
         "P20 frontend build freeze drift", errors)
    need(freeze.get("evidence_index") == "DIGEST_BOUND_EXACT_HEAD",
         "P20 evidence-index freeze drift", errors)
    need(freeze.get("decision_required") == 0, "P20 decision-required freeze must be zero", errors)

    need(plan.get("p0_workflow_sequence") == EXPECTED_P0,
         "P20 required P0 sequence/order drift", errors)
    corr = plan.get("correlation_contract", {})
    need(corr.get("required") is True, "P20 correlated timeline must remain required", errors)
    need(corr.get("surfaces") == ["browser", "HTTP/API", "MySQL", "Redis", "workers", "mail", "audit"],
         "P20 correlation surface set/order drift", errors)
    need(corr.get("stable_ids") == "REQUIRED_WHERE_APPLICABLE", "P20 stable-ID contract drift", errors)
    need(corr.get("secret_safe") is True, "P20 evidence must remain secret-safe", errors)
    need(corr.get("mock_p0_authority") == "PROHIBITED", "P20 mock P0 authority prohibition drift", errors)

    gates = plan.get("gate_contract", {})
    need(gates.get("release_candidate_ledger") == EXPECTED_RC_GATES, "P20 G0-G10 ledger drift", errors)
    need(gates.get("p20_execution_pass_required") == EXPECTED_P20_PASS_GATES,
         "P20 execution PASS gate set/order drift", errors)
    need(gates.get("inherited_complete") == {
        "G2": "P03 signed Design System authority, revalidated by applicable UI evidence"
    }, "P20 G2 inherited authority drift", errors)
    expected_g1 = (
        "Execution Stage is P01/P21/P22; P20 must live-bind current P01/native architecture authority "
        "and preserve explicit P21/P22 CAP-NATIVE-INSTALL/CAP-NATIVE-ONLY-RELEASE obligations without claiming them complete"
    )
    need(gates.get("later_owned_carry_forward") == {"G1": expected_g1},
         "P20 G1 carry-forward boundary drift", errors)
    need(gates.get("outside_p20") == ["G11", "G12", "G13"], "P20 later-gate boundary drift", errors)
    need(gates.get("conditional_pass") == "PROHIBITED", "P20 conditional PASS prohibition drift", errors)
    need(gates.get("missing_disposition") == "PROHIBITED", "P20 missing gate disposition prohibition drift", errors)

    env = plan.get("environment_contract", {})
    need(env.get("go_services") == EXPECTED_SERVICES, "P20 eight-service inventory drift", errors)
    need(env.get("dependencies") == ["MySQL 8.x", "Redis", "ClamAV", "local filesystem"],
         "P20 dependency inventory drift", errors)
    need(env.get("nginx_unique_entry") is True, "P20 Nginx entry boundary drift", errors)
    need(env.get("php_business_api") == "PROHIBITED", "P20 PHP business API prohibition drift", errors)
    need(env.get("production_node_http_ssr_pm2") == "PROHIBITED", "P20 Node production prohibition drift", errors)
    need(env.get("production_docker_compose") == "PROHIBITED", "P20 Docker production prohibition drift", errors)
    need(env.get("node_vite_build_test_only") is True, "P20 Node/Vite boundary drift", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True and closure.get("review_only_signed_child_required") is True,
         "P20 closure discipline drift", errors)
    need(closure.get("required_case_range") == "P20-T001..P20-T049", "P20 case range drift", errors)
    need(closure.get("pre_sign_evidence_range") == "P20-T001..P20-T048", "P20 pre-sign evidence range drift", errors)
    need(closure.get("defect_limits") == {"p0": 0, "p1": 0, "decision_required": 0},
         "P20 defect limits drift", errors)
    need(closure.get("signed_merge_authority_fields") == [
        "phase=signed", "review_phase=signed", "review_only_signed_child=true", "merge_authoritative=true"
    ], "P20 signed closure fields drift", errors)

    cases = plan.get("cases", [])
    expected_ids = [f"P20-T{i:03d}" for i in range(1, 50)]
    actual_ids = [c.get("id") for c in cases if isinstance(c, dict)]
    need(actual_ids == expected_ids, f"P20 frozen case IDs/order drift: {actual_ids}", errors)
    for case in cases:
        for field in ("id", "name", "driver", "oracle", "evidence", "owner"):
            need(bool(str(case.get(field, "")).strip()), f"{case.get('id')} missing {field}", errors)
    if len(cases) == 49:
        need(cases[46].get("id") == "P20-T047" and cases[46].get("driver") == "scripts/p20/gate_ledger.py",
             "P20 T047 gate-ledger authority drift", errors)
        need(cases[47].get("id") == "P20-T048" and cases[47].get("driver") == "scripts/p20/evidence_coherence.py",
             "P20 T048 coherence authority drift", errors)
        need(cases[48].get("id") == "P20-T049" and cases[48].get("driver") == "scripts/p20/closure_ci.py",
             "P20 T049 closure authority drift", errors)

    status_lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", review, flags=re.MULTILINE)
    review_phase = "invalid"
    if status_lines == [PENDING]:
        review_phase = "pending"
        try:
            need(blob("HEAD", "artifacts/v10/P20/review.md") == PENDING_REVIEW_BLOB,
                 "pending P20 review blob drift", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P20 review: {exc}")
    elif status_lines == [SIGNED]:
        review_phase = "signed"
        need(bool(re.search(r"Reviewed pre-sign implementation SHA: `[0-9a-f]{40}`", review)),
             "signed P20 review missing reviewed pre-sign SHA", errors)
        need(bool(re.search(r"Pre-sign T049 closure run: `[0-9]+`", review)),
             "signed P20 review missing pre-sign T049 run", errors)
        need(bool(re.search(r"Pre-sign T049 closure artifact: `[0-9]+`", review)),
             "signed P20 review missing pre-sign T049 artifact", errors)
        need(bool(re.search(r"Pre-sign T049 closure digest: `sha256:[0-9a-f]{64}`", review)),
             "signed P20 review missing pre-sign T049 digest", errors)
        need("Evidence disposition: `P20-T001..P20-T048 PASS`" in review,
             "signed P20 review missing pre-sign evidence disposition", errors)
        need("P0/P1/DECISION REQUIRED: `0/0/0`" in review,
             "signed P20 review missing zero defect/decision ledger", errors)
    else:
        need(False, f"invalid P20 review status lines: {status_lines}", errors)

    if authority and head == authority:
        mode, implementation_authorized = "contract-freeze", False
    elif review_phase == "pending":
        mode, implementation_authorized = "verification-guard", True
    elif review_phase == "signed":
        mode, implementation_authorized = "signed-review-guard", False
    else:
        mode, implementation_authorized = "invalid", False

    result = {
        "node": "P20",
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "contract_authority": authority,
        "case_range": "P20-T001..P20-T049",
        "review_phase": review_phase,
        "mode": mode,
        "implementation_authorized": implementation_authorized,
        "frozen_contract_preserved": not errors,
        "merge_authoritative": False,
        "predecessor_signed_source": P19_SIGNED_SOURCE,
        "predecessor_closure_run": 33268403700,
        "predecessor_artifact": 9719405957,
        "p0_workflow_steps": len(EXPECTED_P0),
        "release_candidate_gate_rows": len(EXPECTED_RC_GATES),
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
