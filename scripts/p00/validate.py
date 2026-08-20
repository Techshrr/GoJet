#!/usr/bin/env python3
"""GoJet V10 P00 bootstrap and G0 traceability validator.

This script uses only the Python standard library. It is intentionally suitable
for a fresh GitHub Actions checkout before application dependencies exist.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any, Callable

ROOT = Path(__file__).resolve().parents[2]
P00 = ROOT / "artifacts" / "v10" / "P00"
RESULTS = P00 / "results"
G0 = ROOT / "artifacts" / "v10" / "gates" / "G0" / "traceability"

SPECIFICATIONS = {
    "master": (
        ROOT / "specifications" / "GoJet_V10_MASTER_PLAN_OPTIMIZED.md",
        "GJ-V10-MP-GREENFIELD-2026-08-20",
    ),
    "design": (
        ROOT / "specifications" / "GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
    ),
    "ia": (
        ROOT / "specifications" / "GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ),
}

EXPECTED_IA_BLOB = "20609139a0265d3f3a40a1c7c07894dc69220290"
EXPECTED_REQUIRED_CAPABILITIES = 38
EXPECTED_ROUTE_ROWS = 131
EXPECTED_MODULE = "github.com/Techshrr/GoJet"
EXPECTED_GO = "1.26.5"
EXPECTED_NODE = "24.19.0"
EXPECTED_PNPM = "11.21.0"

COMMAND_LOG: list[str] = []


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def run(command: list[str], *, check: bool = False) -> subprocess.CompletedProcess[str]:
    COMMAND_LOG.append("$ " + " ".join(command))
    proc = subprocess.run(
        command,
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if proc.stdout.strip():
        COMMAND_LOG.append(proc.stdout.rstrip())
    if proc.stderr.strip():
        COMMAND_LOG.append(proc.stderr.rstrip())
    COMMAND_LOG.append(f"[exit={proc.returncode}]")
    if check and proc.returncode != 0:
        raise RuntimeError(f"command failed ({proc.returncode}): {' '.join(command)}")
    return proc


def git(*args: str, check: bool = True) -> str:
    proc = run(["git", *args], check=check)
    return proc.stdout.strip()


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def tracked_files() -> list[Path]:
    raw = git("ls-files", "-z")
    return [ROOT / part for part in raw.split("\0") if part]


def text_tracked_files() -> list[Path]:
    result: list[Path] = []
    for path in tracked_files():
        if not path.is_file():
            continue
        try:
            path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        result.append(path)
    return result


def add_error(errors: list[str], condition: bool, message: str) -> None:
    if condition:
        errors.append(message)


def tool_version(command: list[str]) -> str | None:
    if shutil.which(command[0]) is None:
        return None
    proc = run(command)
    if proc.returncode != 0:
        return None
    value = (proc.stdout or proc.stderr).strip().splitlines()
    return value[0].strip() if value else None


def case_t001() -> dict[str, Any]:
    errors: list[str] = []
    required = [
        ROOT / "README.md",
        ROOT / "go.mod",
        ROOT / "package.json",
        ROOT / "pnpm-workspace.yaml",
        ROOT / "toolchain.lock.json",
        ROOT / "services" / "README.md",
        ROOT / "frontend" / "apps" / "README.md",
        ROOT / "frontend" / "packages" / "README.md",
        ROOT / "installer" / "README.md",
        ROOT / "deploy" / "README.md",
        ROOT / "config" / "README.md",
        ROOT / "migrations" / "README.md",
        ROOT / "docs" / "architecture" / "P00_BASELINE.md",
        ROOT / "docs" / "architecture" / "adr" / "README.md",
        ROOT / "docs" / "security" / "SECURITY_INVARIANTS.md",
        ROOT / "contracts" / "traceability" / "capability-matrix.snapshot.md",
        ROOT / "contracts" / "traceability" / "route-registry.snapshot.md",
        P00 / "test-plan.json",
        P00 / "evidence-schema.json",
    ]
    missing = [str(p.relative_to(ROOT)) for p in required if not p.exists()]
    add_error(errors, bool(missing), "missing bootstrap files: " + ", ".join(missing))

    remote = git("remote", "get-url", "origin", check=False)
    normalized_remote = remote.removesuffix(".git")
    accepted = {
        "https://github.com/Techshrr/GoJet",
        "git@github.com:Techshrr/GoJet",
    }
    add_error(errors, normalized_remote not in accepted, f"unexpected origin: {remote!r}")

    go_mod = read_text(ROOT / "go.mod") if (ROOT / "go.mod").exists() else ""
    add_error(errors, f"module {EXPECTED_MODULE}" not in go_mod, "root Go module is not Techshrr/GoJet")
    add_error(errors, f"go {EXPECTED_GO}" not in go_mod, f"go.mod does not pin go {EXPECTED_GO}")

    package = json.loads(read_text(ROOT / "package.json")) if (ROOT / "package.json").exists() else {}
    add_error(errors, package.get("packageManager") != f"pnpm@{EXPECTED_PNPM}", "root packageManager pin is incorrect")
    engines = package.get("engines", {})
    add_error(errors, engines.get("node") != EXPECTED_NODE, "root Node engine pin is incorrect")
    add_error(errors, engines.get("pnpm") != EXPECTED_PNPM, "root pnpm engine pin is incorrect")

    versions = {
        "go": tool_version(["go", "version"]),
        "node": tool_version(["node", "--version"]),
        "pnpm": tool_version(["pnpm", "--version"]),
        "python": platform.python_version(),
    }
    if versions["go"] is not None:
        add_error(errors, f"go{EXPECTED_GO}" not in versions["go"], f"unexpected Go toolchain: {versions['go']}")
        proc = run(["go", "mod", "verify"])
        add_error(errors, proc.returncode != 0, "go mod verify failed")
    else:
        errors.append("Go toolchain is not available")
    if versions["node"] is not None:
        add_error(errors, versions["node"].lstrip("v") != EXPECTED_NODE, f"unexpected Node toolchain: {versions['node']}")
    else:
        errors.append("Node toolchain is not available")
    if versions["pnpm"] is not None:
        add_error(errors, versions["pnpm"] != EXPECTED_PNPM, f"unexpected pnpm toolchain: {versions['pnpm']}")
    else:
        errors.append("pnpm toolchain is not available")

    return {"errors": errors, "details": {"origin": remote, "versions": versions, "required_files": len(required)}}


def case_t002() -> dict[str, Any]:
    errors: list[str] = []
    tracked = [str(p.relative_to(ROOT)).replace(os.sep, "/") for p in tracked_files()]

    add_error(errors, ".gitmodules" in tracked, ".gitmodules is prohibited in the P00 Greenfield baseline")
    staged = git("ls-files", "-s")
    gitlinks = [line for line in staged.splitlines() if line.startswith("160000 ")]
    add_error(errors, bool(gitlinks), "gitlink/submodule entries are prohibited: " + "; ".join(gitlinks))

    prohibited_paths: list[str] = []
    for rel in tracked:
        low = rel.lower()
        base = Path(rel).name.lower()
        if base == "dockerfile" or base.startswith("dockerfile."):
            prohibited_paths.append(rel)
        if base in {"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}:
            prohibited_paths.append(rel)
        if low.startswith("deploy/docker/") or "/node_modules/" in f"/{low}/" or low.startswith("node_modules/"):
            prohibited_paths.append(rel)
    add_error(errors, bool(prohibited_paths), "prohibited production/runtime paths: " + ", ".join(sorted(set(prohibited_paths))))

    legacy_patterns = [
        re.compile(r"Techshrr/GoJet_Short_Link", re.I),
        re.compile(r"github\.com/Techshrr/GoJet_Short_Link", re.I),
        re.compile(r"rebuild/v(?:4|5)(?:[-/]|\b)", re.I),
    ]
    legacy_hits: list[str] = []
    for path in text_tracked_files():
        rel = str(path.relative_to(ROOT)).replace(os.sep, "/")
        text = read_text(path)
        if any(pattern.search(text) for pattern in legacy_patterns):
            legacy_hits.append(rel)
    add_error(errors, bool(legacy_hits), "prior GoJet implementation references detected: " + ", ".join(sorted(set(legacy_hits))))

    return {
        "errors": errors,
        "details": {
            "tracked_files": len(tracked),
            "gitlinks": len(gitlinks),
            "prohibited_paths": sorted(set(prohibited_paths)),
            "legacy_reference_files": sorted(set(legacy_hits)),
        },
    }


def case_t003() -> dict[str, Any]:
    errors: list[str] = []
    ids: dict[str, str] = {}
    for key, (path, expected_id) in SPECIFICATIONS.items():
        if not path.exists():
            errors.append(f"missing {key} specification: {path.relative_to(ROOT)}")
            continue
        text = read_text(path)
        ids[key] = expected_id
        add_error(errors, f"**Document ID:** `{expected_id}`" not in text, f"{key} document ID mismatch")
        add_error(errors, "**Implementation repository:** `Techshrr/GoJet`" not in text, f"{key} repository identity mismatch")
        add_error(errors, "**Implementation branch:** `main`" not in text, f"{key} normative branch identity mismatch")

    master = read_text(SPECIFICATIONS["master"][0]) if SPECIFICATIONS["master"][0].exists() else ""
    design = read_text(SPECIFICATIONS["design"][0]) if SPECIFICATIONS["design"][0].exists() else ""
    ia = read_text(SPECIFICATIONS["ia"][0]) if SPECIFICATIONS["ia"][0].exists() else ""
    add_error(errors, SPECIFICATIONS["design"][1] not in master, "Master Plan does not reference V10 Design System ID")
    add_error(errors, SPECIFICATIONS["ia"][1] not in master, "Master Plan does not reference V10 IA ID")
    add_error(errors, SPECIFICATIONS["master"][1] not in design, "Design System does not reference V10 Master ID")
    add_error(errors, SPECIFICATIONS["master"][1] not in ia, "IA does not reference V10 Master ID")
    add_error(errors, SPECIFICATIONS["design"][1] not in ia, "IA does not reference V10 Design ID")

    ia_blob = git("hash-object", str(SPECIFICATIONS["ia"][0].relative_to(ROOT)), check=False)
    add_error(errors, ia_blob != EXPECTED_IA_BLOB, f"IA source blob changed from frozen snapshot: {ia_blob}")
    route_snapshot = ROOT / "contracts" / "traceability" / "route-registry.snapshot.md"
    if route_snapshot.exists():
        snapshot = read_text(route_snapshot)
        add_error(errors, EXPECTED_IA_BLOB not in snapshot, "route snapshot does not record frozen IA blob")
    else:
        errors.append("route registry snapshot is missing")

    old_identity = re.compile(r"(?<![A-Za-z0-9])(?:V5|v5)(?![A-Za-z0-9])|GJ-V5-|GoJet_V5_")
    governed_roots = [ROOT / "README.md", ROOT / "specifications", ROOT / "contracts", ROOT / "docs"]
    residual: list[str] = []
    for target in governed_roots:
        candidates = [target] if target.is_file() else list(target.rglob("*.md"))
        for path in candidates:
            if path.is_file() and old_identity.search(read_text(path)):
                residual.append(str(path.relative_to(ROOT)))
    add_error(errors, bool(residual), "residual V5 project identity found: " + ", ".join(sorted(set(residual))))

    return {"errors": errors, "details": {"specification_ids": ids, "ia_blob": ia_blob, "residual_identity_files": residual}}


def extract_table_ids(path: Path, prefixes: tuple[str, ...]) -> list[str]:
    ids: list[str] = []
    pattern = re.compile(r"^\|\s*`([A-Z0-9-]+)`\s*\|")
    for line in read_text(path).splitlines():
        match = pattern.match(line)
        if match and match.group(1).startswith(prefixes):
            ids.append(match.group(1))
    return ids


def case_t004() -> dict[str, Any]:
    errors: list[str] = []
    required_boundaries = [
        "services/README.md",
        "frontend/apps/README.md",
        "frontend/packages/README.md",
        "installer/README.md",
        "deploy/README.md",
        "config/README.md",
        "migrations/README.md",
        "contracts/traceability/capability-matrix.snapshot.md",
        "contracts/traceability/route-registry.snapshot.md",
        "docs/architecture/adr/README.md",
        "docs/security/SECURITY_INVARIANTS.md",
    ]
    tracked = {str(p.relative_to(ROOT)).replace(os.sep, "/") for p in tracked_files()}
    missing = [path for path in required_boundaries if path not in tracked]
    add_error(errors, bool(missing), "missing tracked repository boundaries: " + ", ".join(missing))

    go_mod_files = sorted(path for path in tracked if path.endswith("go.mod"))
    add_error(errors, go_mod_files != ["go.mod"], f"expected exactly one root go.mod, found: {go_mod_files}")

    workspace = read_text(ROOT / "pnpm-workspace.yaml") if (ROOT / "pnpm-workspace.yaml").exists() else ""
    for expected in ["frontend/apps/*", "frontend/packages/*"]:
        add_error(errors, expected not in workspace, f"pnpm workspace missing {expected}")

    cap_path = ROOT / "contracts" / "traceability" / "capability-matrix.snapshot.md"
    route_path = ROOT / "contracts" / "traceability" / "route-registry.snapshot.md"
    cap_ids = extract_table_ids(cap_path, ("CAP-",)) if cap_path.exists() else []
    required_cap_ids = [identifier for identifier in cap_ids if identifier not in {"CAP-S3-STORAGE", "CAP-BIO-OPT-IN-INDEX"}]
    add_error(errors, len(required_cap_ids) != EXPECTED_REQUIRED_CAPABILITIES, f"expected {EXPECTED_REQUIRED_CAPABILITIES} REQUIRED capability rows, found {len(required_cap_ids)}")
    add_error(errors, len(required_cap_ids) != len(set(required_cap_ids)), "duplicate REQUIRED capability IDs in snapshot")

    route_prefixes = ("WEB-", "DOCS-", "PUB-", "API-", "AUTH-", "APP-", "ADMIN-", "INSTALL-", "ERR-")
    route_ids = extract_table_ids(route_path, route_prefixes) if route_path.exists() else []
    add_error(errors, len(route_ids) != EXPECTED_ROUTE_ROWS, f"expected {EXPECTED_ROUTE_ROWS} Route Registry rows, found {len(route_ids)}")
    add_error(errors, len(route_ids) != len(set(route_ids)), "duplicate Route IDs in snapshot")

    decision_text = read_text(ROOT / "docs" / "architecture" / "adr" / "README.md") if (ROOT / "docs" / "architecture" / "adr" / "README.md").exists() else ""
    add_error(errors, "`DECISION REQUIRED` count at the P00 baseline is **0**" not in decision_text, "P00 decision ledger is not explicitly zero")

    return {
        "errors": errors,
        "details": {
            "required_capability_rows": len(required_cap_ids),
            "route_registry_rows": len(route_ids),
            "go_mod_files": go_mod_files,
            "missing_boundaries": missing,
        },
    }


def case_t005() -> dict[str, Any]:
    errors: list[str] = []
    patterns = {
        "private-key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
        "github-token": re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})\b"),
        "aws-access-key": re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
        "openai-project-key": re.compile(r"\bsk-proj-[A-Za-z0-9_-]{24,}\b"),
        "slack-token": re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b"),
    }
    hits: list[dict[str, str]] = []
    for path in text_tracked_files():
        rel = str(path.relative_to(ROOT)).replace(os.sep, "/")
        text = read_text(path)
        for name, pattern in patterns.items():
            if pattern.search(text):
                hits.append({"path": rel, "pattern": name})
    add_error(errors, bool(hits), "high-confidence secret material detected: " + json.dumps(hits, ensure_ascii=False))
    return {"errors": errors, "details": {"scanned_text_files": len(text_tracked_files()), "hits": hits}}


def manifest_inventory() -> dict[str, Any]:
    inventory: dict[str, Any] = {"go": [], "npm": []}
    go_mod = ROOT / "go.mod"
    if go_mod.exists():
        text = read_text(go_mod)
        direct: list[str] = []
        in_block = False
        for raw in text.splitlines():
            line = raw.strip()
            if line == "require (":
                in_block = True
                continue
            if in_block and line == ")":
                in_block = False
                continue
            if in_block and line and not line.startswith("//"):
                direct.append(line)
            elif line.startswith("require ") and line != "require (":
                direct.append(line.removeprefix("require ").strip())
        inventory["go"] = sorted(direct)

    for package_path in sorted(ROOT.rglob("package.json")):
        if "node_modules" in package_path.parts:
            continue
        data = json.loads(read_text(package_path))
        rel = str(package_path.relative_to(ROOT)).replace(os.sep, "/")
        for section in ("dependencies", "devDependencies", "optionalDependencies", "peerDependencies"):
            for name, version in sorted((data.get(section) or {}).items()):
                inventory["npm"].append({"manifest": rel, "section": section, "name": name, "version": version})
    return inventory


def case_t006() -> dict[str, Any]:
    errors: list[str] = []
    inventory = manifest_inventory()
    lockfiles = [
        str(path.relative_to(ROOT)).replace(os.sep, "/")
        for path in tracked_files()
        if path.name in {"go.sum", "pnpm-lock.yaml"}
    ]
    # P00 intentionally has no third-party application dependencies yet. If a
    # dependency is added during P00, it must be explicitly reviewed rather
    # than silently passing an empty inventory assertion.
    dependency_count = len(inventory["go"]) + len(inventory["npm"])
    add_error(errors, dependency_count != 0, "P00 introduced third-party dependencies before P01: " + json.dumps(inventory, ensure_ascii=False))
    return {"errors": errors, "details": {"inventory": inventory, "tracked_lockfiles": sorted(lockfiles), "dependency_count": dependency_count}}


CASES: dict[str, Callable[[], dict[str, Any]]] = {
    "P00-T001": case_t001,
    "P00-T002": case_t002,
    "P00-T003": case_t003,
    "P00-T004": case_t004,
    "P00-T005": case_t005,
    "P00-T006": case_t006,
}


def write_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False, sort_keys=True) + "\n", encoding="utf-8")


def generate_common_evidence(results: dict[str, dict[str, Any]]) -> None:
    P00.mkdir(parents=True, exist_ok=True)
    RESULTS.mkdir(parents=True, exist_ok=True)
    G0.mkdir(parents=True, exist_ok=True)

    commit = git("rev-parse", "HEAD")
    branch = git("rev-parse", "--abbrev-ref", "HEAD")
    remote = git("remote", "get-url", "origin", check=False)
    toolchain = json.loads(read_text(ROOT / "toolchain.lock.json"))

    environment = {
        "generated_at": utc_now(),
        "platform": platform.platform(),
        "python": platform.python_version(),
        "go": tool_version(["go", "version"]),
        "node": tool_version(["node", "--version"]),
        "pnpm": tool_version(["pnpm", "--version"]),
    }
    write_json(P00 / "environment.json", environment)

    source = {
        "repository": "Techshrr/GoJet",
        "remote": remote,
        "branch": branch,
        "implementation_commit": commit,
        "specification_ids": [value[1] for value in SPECIFICATIONS.values()],
        "toolchain": toolchain["development"],
    }
    write_json(P00 / "source.json", source)

    spec_lines: list[str] = []
    for path, spec_id in SPECIFICATIONS.values():
        spec_lines.append(f"{sha256(path)}  {path.relative_to(ROOT)}  {spec_id}")
    (P00 / "spec-checksums.sha256").write_text("\n".join(spec_lines) + "\n", encoding="utf-8")

    tree = sorted(str(path.relative_to(ROOT)).replace(os.sep, "/") for path in tracked_files())
    (P00 / "repository-tree.txt").write_text("\n".join(tree) + "\n", encoding="utf-8")

    dependency_provenance = {
        "generated_at": utc_now(),
        "implementation_commit": commit,
        "go_module": EXPECTED_MODULE,
        "frontend_workspace": ["frontend/apps/*", "frontend/packages/*"],
        "production_dependency_contract": toolchain["production_contract"],
        "declared_dependencies": manifest_inventory(),
    }
    write_json(P00 / "dependency-provenance.json", dependency_provenance)
    write_json(P00 / "license-inventory.json", {
        "generated_at": utc_now(),
        "implementation_commit": commit,
        "inventory": manifest_inventory(),
        "note": "P00 introduces no third-party application dependency; dependency/license review expands in P01."
    })

    passed = sum(1 for result in results.values() if not result["errors"])
    failed = len(results) - passed
    summary = {
        "schema_version": 1,
        "node": "P00",
        "generated_at": utc_now(),
        "implementation_commit": commit,
        "results": {
            "passed": passed,
            "failed": failed,
            "total": len(results),
            "cases": {case_id: ("PASS" if not result["errors"] else "FAIL") for case_id, result in results.items()},
        },
    }
    write_json(RESULTS / "bootstrap-validation.json", summary)

    for case_id, result in results.items():
        write_json(RESULTS / f"{case_id}.json", {
            "case_id": case_id,
            "status": "PASS" if not result["errors"] else "FAIL",
            "generated_at": utc_now(),
            "implementation_commit": commit,
            **result,
        })

    capability_path = ROOT / "contracts" / "traceability" / "capability-matrix.snapshot.md"
    route_path = ROOT / "contracts" / "traceability" / "route-registry.snapshot.md"
    cap_ids = extract_table_ids(capability_path, ("CAP-",))
    required_caps = [identifier for identifier in cap_ids if identifier not in {"CAP-S3-STORAGE", "CAP-BIO-OPT-IN-INDEX"}]
    route_ids = extract_table_ids(route_path, ("WEB-", "DOCS-", "PUB-", "API-", "AUTH-", "APP-", "ADMIN-", "INSTALL-", "ERR-"))
    write_json(G0 / "status-owner-coverage.json", {
        "generated_at": utc_now(),
        "implementation_commit": commit,
        "required_capabilities": {"expected": EXPECTED_REQUIRED_CAPABILITIES, "observed": len(required_caps)},
        "route_registry_rows": {"expected": EXPECTED_ROUTE_ROWS, "observed": len(route_ids)},
        "decision_required": 0,
        "gate_status": "PENDING_REVIEW",
        "accountable_roles": ["Product Owner", "Backend Lead"],
    })

    (P00 / "commands.log").write_text("\n".join(COMMAND_LOG) + "\n", encoding="utf-8")

    candidate_files = [
        P00 / "environment.json",
        P00 / "source.json",
        P00 / "commands.log",
        P00 / "test-plan.json",
        RESULTS / "bootstrap-validation.json",
        P00 / "repository-tree.txt",
        P00 / "spec-checksums.sha256",
        P00 / "dependency-provenance.json",
        P00 / "license-inventory.json",
        G0 / "status-owner-coverage.json",
    ]
    files = [
        {"path": str(path.relative_to(ROOT)).replace(os.sep, "/"), "sha256": sha256(path)}
        for path in candidate_files
        if path.exists()
    ]
    index = {
        "schema_version": 1,
        "node": "P00",
        "implementation_commit": commit,
        "specification_ids": [value[1] for value in SPECIFICATIONS.values()],
        "generated_at": utc_now(),
        "results": {"passed": passed, "failed": failed, "total": len(results)},
        "files": files,
    }
    write_json(P00 / "evidence-index.json", index)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", choices=sorted(CASES), help="run only one stable P00 test case")
    args = parser.parse_args()

    selected = [args.case] if args.case else list(CASES)
    COMMAND_LOG.append("P00 validation invocation: " + " ".join(sys.argv))
    results: dict[str, dict[str, Any]] = {}

    for case_id in selected:
        try:
            result = CASES[case_id]()
        except Exception as exc:  # evidence must capture unexpected validator failures
            result = {"errors": [f"validator exception: {type(exc).__name__}: {exc}"], "details": {}}
        results[case_id] = result
        status = "PASS" if not result["errors"] else "FAIL"
        print(f"{case_id}: {status}")
        for error in result["errors"]:
            print(f"  - {error}")

    try:
        generate_common_evidence(results)
    except Exception as exc:
        print(f"evidence generation failed: {type(exc).__name__}: {exc}", file=sys.stderr)
        return 2

    return 0 if all(not result["errors"] for result in results.values()) else 1


if __name__ == "__main__":
    raise SystemExit(main())
