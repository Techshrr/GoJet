#!/usr/bin/env python3
from __future__ import annotations

from common import HEAD, ROOT, ancestor, blob, emit, fail_if_errors, git

BASE = "6e628b9879eb4dddf335a324e4f4d7ae3a77cd5c"
AUTHORITY = "050fd7052d71ff77858b153abcbc466a1243af2f"
FROZEN = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
    "artifacts/v10/P19/review.md": "02be683f6750e681fe9a9d6a4fc41f02c08f872b",
    "artifacts/v10/P20/test-plan.json": "f6d7831be48fcc8f378ad1a90efbb7e96e01c4e8",
}


def main() -> int:
    errors: list[str] = []
    if not ancestor(BASE):
        errors.append("P19 integration is not an ancestor of candidate HEAD")
    if not ancestor(AUTHORITY):
        errors.append("P20 contract authority is not an ancestor of candidate HEAD")
    try:
        if git("rev-parse", f"{AUTHORITY}^") != BASE:
            errors.append("P20 contract authority is not the direct child of P19 integration")
        commits = [x for x in git("rev-list", "--ancestry-path", "--reverse", f"{BASE}..{HEAD}").splitlines() if x]
        if not commits or commits[0] != AUTHORITY:
            errors.append(f"first P20 ancestry commit drift: {commits[:1]}")
        base_parents = git("show", "-s", "--format=%P", BASE).split()
        expected_parents = ["43e693b10c0118e32d7f14c61156e0b06c155111", "44ea701ae464550ce920c5f2131428270e22fb41"]
        if base_parents != expected_parents:
            errors.append(f"P19 integration parent authority drift: {base_parents}")
        for path, expected in FROZEN.items():
            actual = blob("HEAD", path)
            if actual != expected:
                errors.append(f"frozen authority blob drift {path}: {actual}")
    except Exception as exc:
        errors.append(f"candidate lineage/binding inspection failed: {exc}")

    payload = emit(
        "P20-T001",
        "candidate",
        "Exact candidate lineage and normative authority freeze",
        errors,
        {
            "base_integration_commit": BASE,
            "contract_authority": AUTHORITY,
            "candidate_commit": HEAD,
            "candidate_is_authorized_descendant": ancestor(AUTHORITY),
            "frozen_blob_count": len(FROZEN),
            "frozen_blobs": FROZEN,
            "prior_repository_authority": "PROHIBITED",
            "normative_authority": [
                "GJ-V10-MP-GREENFIELD-2026-08-20",
                "GJ-V10-DS-GREENFIELD-2026-08-20",
                "GJ-V10-IA-GREENFIELD-2026-08-20",
            ],
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
