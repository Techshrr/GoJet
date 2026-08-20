#!/usr/bin/env python3
"""Deterministic GoJet V10 P00 bootstrap/G0 validator.

Standard-library only so it can execute in a clean checkout before application
packages exist. Generated evidence is bound to the exact Git commit under test.
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

SPECS = {
    "master": ("specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md", "GJ-V10-MP-GREENFIELD-2026-08-20"),
    "design": ("specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md", "GJ-V10-DS-GREENFIELD-2026-08-20"),
    "ia": ("specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md", "GJ-V10-IA-GREENFIELD-2026-08-20"),
}
EXPECTED_IA_BLOB = "20609139a0265d3f3a40a1c7c07894dc69220290"
EXPECTED_CAPS = 38
EXPECTED_ROUTES = 131
EXPECTED_MODULE = "github.com/Techshrr/GoJet"
EXPECTED_GO = "1.26.5"
EXPECTED_NODE = "24.19.0"
EXPECTED_PNPM = "11.21.0"
LOG: list[str] = []


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def cmd(argv: list[str], check: bool = False) -> subprocess.CompletedProcess[str]:
    LOG.append("$ " + " ".join(argv))
    p = subprocess.run(argv, cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if p.stdout.strip():
        LOG.append(p.stdout.rstrip())
    if p.stderr.strip():
        LOG.append(p.stderr.rstrip())
    LOG.append(f"[exit={p.returncode}]")
    if check and p.returncode:
        raise RuntimeError(f"command failed ({p.returncode}): {' '.join(argv)}")
    return p


def git(*args: str, check: bool = True) -> str:
    return cmd(["git", *args], check=check).stdout.strip()


def rel(path: Path) -> str:
    return str(path.relative_to(ROOT)).replace(os.sep, "/")


def tracked() -> list[Path]:
    return [ROOT / item for item in git("ls-files", "-z").split("\0") if item]


def text_tracked() -> list[Path]:
    out: list[Path] = []
    for path in tracked():
        if not path.is_file():
            continue
        try:
            path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        out.append(path)
    return out


def text(path: str | Path) -> str:
    p = ROOT / path if isinstance(path, str) else path
    return p.read_text(encoding="utf-8")


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def version(argv: list[str]) -> str | None:
    if not shutil.which(argv[0]):
        return None
    p = cmd(argv)
    if p.returncode:
        return None
    lines = (p.stdout or p.stderr).strip().splitlines()
    return lines[0].strip() if lines else None


def result(errors: list[str], **details: Any) -> dict[str, Any]:
    return {"errors": errors, "details": details}


def t001() -> dict[str, Any]:
    errors: list[str] = []
    required = [
        "README.md", "go.mod", "package.json", "pnpm-workspace.yaml", "toolchain.lock.json",
        "services/README.md", "frontend/apps/README.md", "frontend/packages/README.md",
        "installer/README.md", "deploy/README.md", "config/README.md", "migrations/README.md",
        "docs/architecture/P00_BASELINE.md", "docs/architecture/adr/README.md",
        "docs/security/SECURITY_INVARIANTS.md",
        "contracts/traceability/capability-matrix.snapshot.md",
        "contracts/traceability/route-registry.snapshot.md",
        "artifacts/v10/P00/test-plan.json", "artifacts/v10/P00/evidence-schema.json",
        "artifacts/v10/P00/review.md", "artifacts/v10/gates/G0/traceability/README.md",
    ]
    missing = [p for p in required if not (ROOT / p).exists()]
    if missing:
        errors.append("missing bootstrap files: " + ", ".join(missing))

    remote = git("remote", "get-url", "origin", check=False).removesuffix(".git")
    if remote not in {"https://github.com/Techshrr/GoJet", "git@github.com:Techshrr/GoJet"}:
        errors.append(f"unexpected origin: {remote!r}")

    gomod = text("go.mod") if (ROOT / "go.mod").exists() else ""
    if f"module {EXPECTED_MODULE}" not in gomod:
        errors.append("root Go module mismatch")
    if f"go {EXPECTED_GO}" not in gomod:
        errors.append("Go version pin mismatch")

    package = json.loads(text("package.json")) if (ROOT / "package.json").exists() else {}
    if package.get("packageManager") != f"pnpm@{EXPECTED_PNPM}":
        errors.append("root pnpm packageManager pin mismatch")
    engines = package.get("engines") or {}
    if engines.get("node") != EXPECTED_NODE or engines.get("pnpm") != EXPECTED_PNPM:
        errors.append("root Node/pnpm engine pin mismatch")

    versions = {
        "go": version(["go", "version"]),
        "node": version(["node", "--version"]),
        "pnpm": version(["pnpm", "--version"]),
        "python": platform.python_version(),
    }
    if not versions["go"] or f"go{EXPECTED_GO}" not in versions["go"]:
        errors.append(f"unexpected/unavailable Go toolchain: {versions['go']}")
    elif cmd(["go", "mod", "verify"]).returncode:
        errors.append("go mod verify failed")
    if not versions["node"] or versions["node"].lstrip("v") != EXPECTED_NODE:
        errors.append(f"unexpected/unavailable Node toolchain: {versions['node']}")
    if versions["pnpm"] != EXPECTED_PNPM:
        errors.append(f"unexpected/unavailable pnpm toolchain: {versions['pnpm']}")
    return result(errors, origin=remote, versions=versions, required_files=len(required))


def t002() -> dict[str, Any]:
    errors: list[str] = []
    files = [rel(p) for p in tracked()]
    if ".gitmodules" in files:
        errors.append(".gitmodules is prohibited")
    gitlinks = [line for line in git("ls-files", "-s").splitlines() if line.startswith("160000 ")]
    if gitlinks:
        errors.append("gitlink/submodule entries are prohibited: " + "; ".join(gitlinks))

    bad_paths: list[str] = []
    for path in files:
        low = path.lower()
        base = Path(path).name.lower()
        if base == "dockerfile" or base.startswith("dockerfile."):
            bad_paths.append(path)
        if base in {"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}:
            bad_paths.append(path)
        if low.startswith("deploy/docker/") or low.startswith("node_modules/") or "/node_modules/" in f"/{low}/":
            bad_paths.append(path)
    if bad_paths:
        errors.append("prohibited production/runtime paths: " + ", ".join(sorted(set(bad_paths))))

    # Build forbidden legacy identifiers from fragments so the detector does not
    # match its own source text while still matching the real contiguous value.
    legacy_terms = [
        "Techshrr/GoJet_" + "Short_Link",
        "github.com/Techshrr/GoJet_" + "Short_Link",
    ]
    legacy_branch = re.compile(r"rebuild/v(?:4|5)(?:[-/]|\b)", re.I)
    hits: list[str] = []
    for path in text_tracked():
        body = text(path)
        low = body.lower()
        if any(term.lower() in low for term in legacy_terms) or legacy_branch.search(body):
            hits.append(rel(path))
    if hits:
        errors.append("prior GoJet implementation references detected: " + ", ".join(sorted(set(hits))))
    return result(errors, tracked_files=len(files), gitlinks=len(gitlinks), prohibited_paths=sorted(set(bad_paths)), legacy_reference_files=sorted(set(hits)))


def t003() -> dict[str, Any]:
    errors: list[str] = []
    bodies: dict[str, str] = {}
    ids: dict[str, str] = {}
    for key, (path, expected_id) in SPECS.items():
        p = ROOT / path
        if not p.exists():
            errors.append(f"missing {key} specification: {path}")
            continue
        body = text(p)
        bodies[key] = body
        ids[key] = expected_id
        if f"**Document ID:** `{expected_id}`" not in body:
            errors.append(f"{key} document ID mismatch")
        if "**Implementation repository:** `Techshrr/GoJet`" not in body:
            errors.append(f"{key} repository identity mismatch")
        if "**Implementation branch:** `main`" not in body:
            errors.append(f"{key} branch identity mismatch")

    if "master" in bodies and SPECS["design"][1] not in bodies["master"]:
        errors.append("Master does not reference Design ID")
    if "master" in bodies and SPECS["ia"][1] not in bodies["master"]:
        errors.append("Master does not reference IA ID")
    if "design" in bodies and SPECS["master"][1] not in bodies["design"]:
        errors.append("Design does not reference Master ID")
    if "ia" in bodies and SPECS["master"][1] not in bodies["ia"]:
        errors.append("IA does not reference Master ID")
    if "ia" in bodies and SPECS["design"][1] not in bodies["ia"]:
        errors.append("IA does not reference Design ID")

    ia_path = SPECS["ia"][0]
    ia_blob = git("hash-object", ia_path, check=False)
    if ia_blob != EXPECTED_IA_BLOB:
        errors.append(f"IA blob changed from frozen snapshot: {ia_blob}")
    snapshot = text("contracts/traceability/route-registry.snapshot.md")
    if EXPECTED_IA_BLOB not in snapshot:
        errors.append("Route snapshot does not record frozen IA blob")

    old_identity = re.compile(r"(?<![A-Za-z0-9])(?:V5|v5)(?![A-Za-z0-9])|GJ-V5-|GoJet_V5_")
    governed: list[Path] = [ROOT / "README.md"]
    for root in (ROOT / "specifications", ROOT / "contracts", ROOT / "docs"):
        governed.extend(root.rglob("*.md"))
    residual = [rel(p) for p in governed if p.is_file() and old_identity.search(text(p))]
    if residual:
        errors.append("residual V5 identity: " + ", ".join(sorted(set(residual))))
    return result(errors, specification_ids=ids, ia_blob=ia_blob, residual_identity_files=residual)


def table_ids(path: str, prefixes: tuple[str, ...]) -> list[str]:
    pattern = re.compile(r"^\|\s*`([A-Z0-9-]+)`\s*\|")
    out: list[str] = []
    for line in text(path).splitlines():
        m = pattern.match(line)
        if m and m.group(1).startswith(prefixes):
            out.append(m.group(1))
    return out


def t004() -> dict[str, Any]:
    errors: list[str] = []
    files = {rel(p) for p in tracked()}
    boundaries = {
        "services/README.md", "frontend/apps/README.md", "frontend/packages/README.md",
        "installer/README.md", "deploy/README.md", "config/README.md", "migrations/README.md",
        "contracts/traceability/capability-matrix.snapshot.md",
        "contracts/traceability/route-registry.snapshot.md",
        "docs/architecture/adr/README.md", "docs/security/SECURITY_INVARIANTS.md",
    }
    missing = sorted(boundaries - files)
    if missing:
        errors.append("missing repository boundaries: " + ", ".join(missing))

    gomods = sorted(p for p in files if p.endswith("go.mod"))
    if gomods != ["go.mod"]:
        errors.append(f"expected one root go.mod, found {gomods}")
    workspace = text("pnpm-workspace.yaml")
    for glob in ("frontend/apps/*", "frontend/packages/*"):
        if glob not in workspace:
            errors.append(f"pnpm workspace missing {glob}")

    cap_ids = table_ids("contracts/traceability/capability-matrix.snapshot.md", ("CAP-",))
    required_caps = [x for x in cap_ids if x not in {"CAP-S3-STORAGE", "CAP-BIO-OPT-IN-INDEX"}]
    if len(required_caps) != EXPECTED_CAPS:
        errors.append(f"expected {EXPECTED_CAPS} REQUIRED capability rows, found {len(required_caps)}")
    if len(required_caps) != len(set(required_caps)):
        errors.append("duplicate REQUIRED capability ID")

    prefixes = ("WEB-", "DOCS-", "PUB-", "API-", "AUTH-", "APP-", "ADMIN-", "INSTALL-", "ERR-")
    route_ids = table_ids("contracts/traceability/route-registry.snapshot.md", prefixes)
    if len(route_ids) != EXPECTED_ROUTES:
        errors.append(f"expected {EXPECTED_ROUTES} Route Registry rows, found {len(route_ids)}")
    if len(route_ids) != len(set(route_ids)):
        errors.append("duplicate Route ID")
    if "`DECISION REQUIRED` count at the P00 baseline is **0**" not in text("docs/architecture/adr/README.md"):
        errors.append("P00 decision ledger is not explicitly zero")
    return result(errors, required_capability_rows=len(required_caps), route_registry_rows=len(route_ids), go_mod_files=gomods, missing_boundaries=missing)


def t005() -> dict[str, Any]:
    errors: list[str] = []
    patterns = {
        "private-key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
        "github-token": re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})\b"),
        "aws-access-key": re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
        "openai-project-key": re.compile(r"\bsk-proj-[A-Za-z0-9_-]{24,}\b"),
        "slack-token": re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b"),
    }
    files = text_tracked()
    hits: list[dict[str, str]] = []
    for path in files:
        body = text(path)
        for name, pattern in patterns.items():
            if pattern.search(body):
                hits.append({"path": rel(path), "pattern": name})
    if hits:
        errors.append("high-confidence secret material detected: " + json.dumps(hits, ensure_ascii=False))
    return result(errors, scanned_text_files=len(files), hits=hits)


def dependency_inventory() -> dict[str, Any]:
    inventory: dict[str, Any] = {"go": [], "npm": []}
    gomod = text("go.mod")
    block = False
    for raw in gomod.splitlines():
        line = raw.strip()
        if line == "require (":
            block = True
            continue
        if block and line == ")":
            block = False
            continue
        if block and line and not line.startswith("//"):
            inventory["go"].append(line)
        elif line.startswith("require ") and line != "require (":
            inventory["go"].append(line.removeprefix("require ").strip())
    inventory["go"].sort()

    for package_path in sorted(ROOT.rglob("package.json")):
        if "node_modules" in package_path.parts:
            continue
        data = json.loads(text(package_path))
        for section in ("dependencies", "devDependencies", "optionalDependencies", "peerDependencies"):
            for name, value in sorted((data.get(section) or {}).items()):
                inventory["npm"].append({"manifest": rel(package_path), "section": section, "name": name, "version": value})
    return inventory


def t006() -> dict[str, Any]:
    errors: list[str] = []
    inventory = dependency_inventory()
    count = len(inventory["go"]) + len(inventory["npm"])
    if count:
        errors.append("P00 introduced third-party dependencies before P01: " + json.dumps(inventory, ensure_ascii=False))
    locks = sorted(rel(p) for p in tracked() if p.name in {"go.sum", "pnpm-lock.yaml"})
    return result(errors, inventory=inventory, dependency_count=count, tracked_lockfiles=locks)


CASES: dict[str, Callable[[], dict[str, Any]]] = {
    "P00-T001": t001,
    "P00-T002": t002,
    "P00-T003": t003,
    "P00-T004": t004,
    "P00-T005": t005,
    "P00-T006": t006,
}


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def emit_evidence(results: dict[str, dict[str, Any]]) -> None:
    P00.mkdir(parents=True, exist_ok=True)
    RESULTS.mkdir(parents=True, exist_ok=True)
    G0.mkdir(parents=True, exist_ok=True)
    commit = git("rev-parse", "HEAD")
    branch = git("rev-parse", "--abbrev-ref", "HEAD")
    remote = git("remote", "get-url", "origin", check=False)
    toolchain = json.loads(text("toolchain.lock.json"))

    write_json(P00 / "environment.json", {
        "generated_at": now(), "platform": platform.platform(), "python": platform.python_version(),
        "go": version(["go", "version"]), "node": version(["node", "--version"]), "pnpm": version(["pnpm", "--version"]),
    })
    write_json(P00 / "source.json", {
        "repository": "Techshrr/GoJet", "remote": remote, "branch": branch,
        "implementation_commit": commit, "specification_ids": [v[1] for v in SPECS.values()],
        "toolchain": toolchain["development"],
    })

    checksum_lines = []
    for path, spec_id in SPECS.values():
        p = ROOT / path
        checksum_lines.append(f"{digest(p)}  {path}  {spec_id}")
    (P00 / "spec-checksums.sha256").write_text("\n".join(checksum_lines) + "\n", encoding="utf-8")
    tree = sorted(rel(p) for p in tracked())
    (P00 / "repository-tree.txt").write_text("\n".join(tree) + "\n", encoding="utf-8")

    inventory = dependency_inventory()
    write_json(P00 / "dependency-provenance.json", {
        "generated_at": now(), "implementation_commit": commit, "go_module": EXPECTED_MODULE,
        "frontend_workspace": ["frontend/apps/*", "frontend/packages/*"],
        "production_dependency_contract": toolchain["production_contract"], "declared_dependencies": inventory,
    })
    write_json(P00 / "license-inventory.json", {
        "generated_at": now(), "implementation_commit": commit, "inventory": inventory,
        "note": "P00 introduces no third-party application dependency; dependency/license review expands in P01."
    })

    passed = sum(not r["errors"] for r in results.values())
    failed = len(results) - passed
    summary = {
        "schema_version": 1, "node": "P00", "generated_at": now(), "implementation_commit": commit,
        "results": {"passed": passed, "failed": failed, "total": len(results),
                    "cases": {k: "PASS" if not v["errors"] else "FAIL" for k, v in results.items()}},
    }
    write_json(RESULTS / "bootstrap-validation.json", summary)
    for case_id, case_result in results.items():
        write_json(RESULTS / f"{case_id}.json", {
            "case_id": case_id, "status": "PASS" if not case_result["errors"] else "FAIL",
            "generated_at": now(), "implementation_commit": commit, **case_result,
        })

    cap_ids = table_ids("contracts/traceability/capability-matrix.snapshot.md", ("CAP-",))
    required_caps = [x for x in cap_ids if x not in {"CAP-S3-STORAGE", "CAP-BIO-OPT-IN-INDEX"}]
    route_ids = table_ids("contracts/traceability/route-registry.snapshot.md",
                          ("WEB-", "DOCS-", "PUB-", "API-", "AUTH-", "APP-", "ADMIN-", "INSTALL-", "ERR-"))
    write_json(G0 / "status-owner-coverage.json", {
        "generated_at": now(), "implementation_commit": commit,
        "required_capabilities": {"expected": EXPECTED_CAPS, "observed": len(required_caps)},
        "route_registry_rows": {"expected": EXPECTED_ROUTES, "observed": len(route_ids)},
        "decision_required": 0, "gate_status": "PENDING_REVIEW",
        "accountable_roles": ["Product Owner", "Backend Lead"],
    })
    (P00 / "commands.log").write_text("\n".join(LOG) + "\n", encoding="utf-8")

    candidates = [
        P00 / "environment.json", P00 / "source.json", P00 / "commands.log", P00 / "test-plan.json",
        RESULTS / "bootstrap-validation.json", P00 / "repository-tree.txt", P00 / "spec-checksums.sha256",
        P00 / "dependency-provenance.json", P00 / "license-inventory.json", G0 / "status-owner-coverage.json",
    ]
    files = [{"path": rel(p), "sha256": digest(p)} for p in candidates if p.exists()]
    write_json(P00 / "evidence-index.json", {
        "schema_version": 1, "node": "P00", "implementation_commit": commit,
        "specification_ids": [v[1] for v in SPECS.values()], "generated_at": now(),
        "results": {"passed": passed, "failed": failed, "total": len(results)}, "files": files,
    })


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", choices=sorted(CASES))
    args = parser.parse_args()
    selected = [args.case] if args.case else list(CASES)
    LOG.append("P00 validation invocation: " + " ".join(sys.argv))
    results: dict[str, dict[str, Any]] = {}
    for case_id in selected:
        try:
            case_result = CASES[case_id]()
        except Exception as exc:
            case_result = result([f"validator exception: {type(exc).__name__}: {exc}"])
        results[case_id] = case_result
        status = "PASS" if not case_result["errors"] else "FAIL"
        print(f"{case_id}: {status}")
        for error in case_result["errors"]:
            print(f"  - {error}")
    try:
        emit_evidence(results)
    except Exception as exc:
        print(f"evidence generation failed: {type(exc).__name__}: {exc}", file=sys.stderr)
        return 2
    return 0 if all(not r["errors"] for r in results.values()) else 1


if __name__ == "__main__":
    raise SystemExit(main())
