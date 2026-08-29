#!/usr/bin/env python3
from __future__ import annotations

import json
import re
from pathlib import Path

from common import HEAD, ROOT, ancestor, emit, fail_if_errors

INTEGRATIONS = {
    "P00": "accc9273c8b4c5cdf07d150e955055168cc9cc7a",
    "P01": "4f10d59c63b07b6b5f75f1be376edcd0fef4eb0",
    "P02": "874c9a81dd169712e0268d175e4ba3738b4c9fba",
    "P03": "186f4b8808ac78d8b460ca13c1afc29f99d49605",
    "P04": "16cddfa89279d698f30607f4dec79f3ed2f55b59",
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
    "P18": "43e693b10c0118e32d7f14c61156e0b06c155111",
    "P19": "6e628b9879eb4dddf335a324e4f4d7ae3a77cd5c",
}
SURFACE_OWNERS = {
    "WEB": ["P19"],
    "DOCS": ["P18"],
    "PUB": ["P05-P19"],
    "API": ["P05-P17"],
    "AUTH": ["P12", "P15"],
    "APP": ["P05-P17"],
    "ADMIN": ["P13-P17"],
    "INSTALL": ["P21", "P22"],
    "ERR": ["P04-P19"],
}


def expand_nodes(text: str) -> list[str]:
    result: list[str] = []
    for start, end in re.findall(r"P(\d{2})(?:-P?(\d{2}))?", text):
        a = int(start)
        b = int(end) if end else a
        result.extend(f"P{i:02d}" for i in range(a, b + 1))
    return sorted(set(result))


def capability_case() -> dict:
    errors: list[str] = []
    snap = (ROOT / "contracts/traceability/capability-matrix.snapshot.md").read_text(encoding="utf-8")
    master = (ROOT / "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md").read_text(encoding="utf-8")

    snapshot_rows = re.findall(
        r"^\| `(CAP-[^`]+)` \| ([^|]+?) \| ([^|]+?) \| REQUIRED \|$",
        snap,
        flags=re.MULTILINE,
    )
    master_rows = re.findall(
        r"^\| `(CAP-[^`]+)` \| .*? \| `REQUIRED` \| .*? \| ([^|]+?) \| ([^|]+?) \| .*? \|$",
        master,
        flags=re.MULTILINE,
    )
    snapshot_ids = [row[0] for row in snapshot_rows]
    master_ids = [row[0] for row in master_rows]
    if len(snapshot_ids) != 38 or len(set(snapshot_ids)) != 38:
        errors.append(f"frozen REQUIRED capability inventory expected 38 unique rows, got {len(snapshot_ids)}/{len(set(snapshot_ids))}")
    if snapshot_ids != master_ids:
        errors.append("Capability Matrix snapshot IDs/order do not match Master Plan REQUIRED capability rows")
    if "`DECISION REQUIRED` count at the specification freeze is **0**" not in snap:
        errors.append("frozen capability snapshot no longer records DECISION REQUIRED=0")

    integration_errors = [node for node, sha in INTEGRATIONS.items() if not ancestor(sha)]
    if integration_errors:
        errors.append(f"integrated node authority missing from HEAD ancestry: {integration_errors}")

    ledger = []
    for cap_id, owners, gates in snapshot_rows:
        nodes = expand_nodes(owners)
        integrated = [node for node in nodes if node in INTEGRATIONS]
        later = [node for node in nodes if node in {"P21", "P22"}]
        unknown = [node for node in nodes if node not in INTEGRATIONS and node not in {"P20", "P21", "P22"}]
        if unknown:
            errors.append(f"{cap_id} has unrecognized owner node(s): {unknown}")
        if not nodes:
            errors.append(f"{cap_id} has no owner nodes")
        if not gates.strip():
            errors.append(f"{cap_id} has no Gate disposition")
        if later and not integrated:
            disposition = "later-owned-carry-forward"
        elif later:
            disposition = "integrated-current-plus-later-owned-carry-forward"
        else:
            disposition = "integrated-current-authority"
        ledger.append({
            "capability": cap_id,
            "owner_text": owners.strip(),
            "owner_nodes": nodes,
            "integrated_owner_nodes": integrated,
            "later_owner_nodes": later,
            "gates": gates.strip(),
            "disposition": disposition,
        })

    native = {row["capability"]: row for row in ledger if row["capability"] in {"CAP-NATIVE-INSTALL", "CAP-NATIVE-ONLY-RELEASE"}}
    if set(native) != {"CAP-NATIVE-INSTALL", "CAP-NATIVE-ONLY-RELEASE"}:
        errors.append("native later-owned capability rows missing")
    for row in native.values():
        if row["disposition"] != "later-owned-carry-forward" or row["later_owner_nodes"] != ["P21", "P22"]:
            errors.append(f"native capability falsely completed or ownership drifted: {row}")

    return emit(
        "P20-T002",
        "traceability",
        "Capability matrix integrated-status closure",
        errors,
        {
            "required_capability_count": len(snapshot_ids),
            "decision_required": 0 if "**0**" in snap.split("## Decision state", 1)[-1].split("##", 1)[0] else None,
            "integrated_node_count": len(INTEGRATIONS),
            "integrated_nodes": INTEGRATIONS,
            "later_owned_native_capabilities": list(native),
            "capability_ledger": ledger,
            "p21_p22_completion_claim": "PROHIBITED",
        },
    )


def route_case() -> dict:
    errors: list[str] = []
    snap = (ROOT / "contracts/traceability/route-registry.snapshot.md").read_text(encoding="utf-8")
    route_ids = re.findall(r"^\| `([A-Z][A-Z0-9-]+)` \|", snap, flags=re.MULTILINE)
    if len(route_ids) != 131 or len(set(route_ids)) != 131:
        errors.append(f"Route Registry expected 131 unique rows, got {len(route_ids)}/{len(set(route_ids))}")
    if "| **Total registered rows** | **131** |" not in snap:
        errors.append("Route Registry summary total drift")

    website_ids = [rid for rid in route_ids if rid.startswith("WEB-")]
    site_content = json.loads((ROOT / "frontend/apps/site/src/website/content.json").read_text(encoding="utf-8"))
    implemented_website = [row.get("routeId") for row in site_content if isinstance(row, dict)]
    if len(website_ids) != 26 or set(website_ids) != set(implemented_website):
        errors.append("P19 Website content registry does not cover exactly the 26 frozen WEB route IDs")

    docs_manifest_path = ROOT / "frontend/apps/docs/src/data/content-manifest.json"
    if not docs_manifest_path.is_file():
        errors.append("P18 Docs content manifest missing")
    else:
        docs_manifest = json.loads(docs_manifest_path.read_text(encoding="utf-8"))
        raw = json.dumps(docs_manifest, ensure_ascii=False)
        if "/docs/en/" not in raw or "/docs/zh-CN/" not in raw:
            errors.append("P18 Docs manifest lacks both canonical locale roots")

    rows = []
    unknown_prefixes = set()
    for rid in route_ids:
        prefix = rid.split("-", 1)[0]
        owners = SURFACE_OWNERS.get(prefix)
        if owners is None:
            unknown_prefixes.add(prefix)
            owners = []
        disposition = "later-owned-installer" if prefix == "INSTALL" else "integrated-current-route-authority"
        rows.append({"route_id": rid, "surface_prefix": prefix, "owners": owners, "disposition": disposition})
    if unknown_prefixes:
        errors.append(f"Route Registry contains unmapped surface prefix(es): {sorted(unknown_prefixes)}")

    path_tokens = re.findall(r"^\| `([A-Z][A-Z0-9-]+)` \| `([^`]+)`", snap, flags=re.MULTILINE)
    if any("legacy" in path.lower() for _, path in path_tokens):
        errors.append("legacy route token unexpectedly entered frozen Route Registry")

    return emit(
        "P20-T003",
        "traceability",
        "Route registry cross-surface closure",
        errors,
        {
            "route_row_count": len(route_ids),
            "unique_route_row_count": len(set(route_ids)),
            "website_route_count": len(website_ids),
            "docs_route_ids": [rid for rid in route_ids if rid.startswith("DOCS-")],
            "installer_route_ids": [rid for rid in route_ids if rid.startswith("INSTALL-")],
            "route_ledger": rows,
            "invented_routes": 0,
            "missing_dispositions": len([row for row in rows if not row["owners"]]),
        },
    )


def main() -> int:
    payloads = [capability_case(), route_case()]
    fail_if_errors(payloads)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
