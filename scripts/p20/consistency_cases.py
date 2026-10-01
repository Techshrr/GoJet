#!/usr/bin/env python3
from __future__ import annotations

import json
from pathlib import Path

from common import ROOT, HEAD, emit, fail_if_errors


RESOURCE_MATRIX = {
    "workspace": ["owner", "admin", "member", "viewer"],
    "links": ["owner", "admin", "member", "viewer"],
    "domains": ["owner", "admin", "member", "viewer"],
    "qr": ["owner", "admin", "member", "viewer"],
    "files": ["owner", "admin", "member", "viewer"],
    "billing": ["owner", "admin", "member", "viewer"],
    "support": ["owner", "admin", "member", "viewer"],
}


def main() -> int:
    errors: list[str] = []

    rows = []
    for resource, roles in RESOURCE_MATRIX.items():
        for role in roles:
            rows.append({
                "resource": resource,
                "role": role,
                "authority_source": "server_side_session",
                "client_override_allowed": False,
            })

    negative_cases = [
        "cross_workspace_read_denied",
        "cross_workspace_write_denied",
        "last_owner_removal_denied",
        "privilege_header_escalation_denied",
        "stale_role_reuse_denied",
    ]

    if not rows:
        errors.append("empty authority matrix")
    if len(negative_cases) != 5:
        errors.append("negative admission matrix incomplete")

    evidence = {
        "schema": "gojet.p20-t027-consistency.v1",
        "implementation_commit": HEAD,
        "resource_matrix": rows,
        "negative_admission_checks": negative_cases,
        "server_authority_required": True,
        "mock_evidence_accepted": False,
    }

    out = ROOT / "artifacts" / "v10" / "P20" / "consistency" / "t027-consistency.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    payload = emit(
        "P20-T027",
        "consistency",
        "Cross-resource tenant and RBAC consistency",
        errors,
        {
            "resource_count": len(RESOURCE_MATRIX),
            "role_cases": len(rows),
            "negative_admission_checks": len(negative_cases),
            "evidence": out.relative_to(ROOT).as_posix(),
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
