#!/usr/bin/env python3
"""GoJet V10 P01 exact-head engineering validator and evidence generator."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P01 = ROOT / "artifacts/v10/P01"
RESULTS = P01 / "results"
LOGS = P01 / "build-logs"
G1 = ROOT / "artifacts/v10/gates/G1/native-architecture"
SPEC_IDS = [
    "GJ-V10-MP-GREENFIELD-2026-08-20",
    "GJ-V10-DS-GREENFIELD-2026-08-20",
    "GJ-V10-IA-GREENFIELD-2026-08-20",
]
APPS = {"@gojet/site", "@gojet/docs", "@gojet/workspace", "@gojet/admin"}
PACKAGES = {
    "@gojet/api-client", "@gojet/auth", "@gojet/charts", "@gojet/domain",
    "@gojet/icons", "@gojet/motion", "@gojet/tokens", "@gojet/ui", "@gojet/utils",
}
CASES = [
    ("P01-T001", "clean-frozen-install"),
    ("P01-T002", "strict-typecheck"),
    ("P01-T003", "independent-static-builds"),
    ("P01-T004", "package-boundaries-and-cycles"),
    ("P01-T005", "code-splitting-static-output"),
    ("P01-T006", "no-production-node-runtime"),
    ("P01-T007", "lockfile-and-dependency-inventory"),
]


def now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def load(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(*args: str) -> str:
    return subprocess.run(["git", *args], cwd=ROOT, text=True, capture_output=True, check=True).stdout.strip()


def version(command: list[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, check=False)
    text = result.stdout or result.stderr
    return text.strip().splitlines()[0] if text.strip() else "unknown"


def manifests() -> dict[str, tuple[Path, dict[str, Any]]]:
    found: dict[str, tuple[Path, dict[str, Any]]] = {}
    for pattern in ("frontend/apps/*/package.json", "frontend/packages/*/package.json"):
        for path in ROOT.glob(pattern):
            data = load(path)
            found[str(data["name"])] = (path, data)
    return found


def dependency_section(data: dict[str, Any], key: str) -> dict[str, str]:
    value = data.get(key)
    if not isinstance(value, dict):
        return {}
    return {str(name): str(spec) for name, spec in value.items()}


def record(case_id: str, passed: bool, errors: list[str] | None = None, details: dict[str, Any] | None = None) -> None:
    write_json(RESULTS / f"{case_id}.json", {
        "case_id": case_id,
        "name": dict(CASES)[case_id],
        "status": "PASS" if passed else "FAIL",
        "errors": errors or [],
        "details": details or {},
        "recorded_at": now(),
    })


def run(case_id: str, command: list[str], log_name: str) -> bool:
    started = time.monotonic()
    result = subprocess.run(command, cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
    LOGS.mkdir(parents=True, exist_ok=True)
    (LOGS / log_name).write_text(result.stdout or "", encoding="utf-8")
    errors = [] if result.returncode == 0 else [f"command exited {result.returncode}: {' '.join(command)}"]
    record(case_id, result.returncode == 0, errors, {
        "command": command,
        "exit_code": result.returncode,
        "duration_seconds": round(time.monotonic() - started, 3),
        "log": f"artifacts/v10/P01/build-logs/{log_name}",
    })
    return result.returncode == 0


def boundary_case() -> tuple[bool, list[str], dict[str, Any]]:
    workspace = manifests()
    errors: list[str] = []
    expected = APPS | PACKAGES
    missing = sorted(expected - set(workspace))
    extra = sorted(set(workspace) - expected)
    if missing:
        errors.append("missing workspace manifests: " + ", ".join(missing))
    if extra:
        errors.append("unexpected workspace manifests: " + ", ".join(extra))

    graph: dict[str, set[str]] = {name: set() for name in workspace}
    for name, (manifest_path, data) in workspace.items():
        declared: set[str] = set()
        for key in ("dependencies", "devDependencies", "peerDependencies"):
            for dep in dependency_section(data, key):
                if dep in workspace:
                    declared.add(dep)
                    graph[name].add(dep)
        if name in PACKAGES and declared & APPS:
            errors.append(f"shared package {name} depends on app(s): {sorted(declared & APPS)}")
        if name in APPS and (declared & APPS) - {name}:
            errors.append(f"app {name} depends on another app: {sorted((declared & APPS) - {name})}")

        source_root = manifest_path.parent / "src"
        if source_root.exists():
            for source in [*source_root.rglob("*.ts"), *source_root.rglob("*.tsx")]:
                text = source.read_text(encoding="utf-8")
                imports = re.findall(r"(?:from\s+|import\s*)['\"](@gojet/[^'\"]+)['\"]", text)
                for dep in imports:
                    if dep not in declared:
                        errors.append(f"{source.relative_to(ROOT)} imports undeclared workspace dependency {dep}")
                    if name in PACKAGES and dep in APPS:
                        errors.append(f"shared package {name} imports app {dep}")

    visiting: set[str] = set()
    visited: set[str] = set()
    stack: list[str] = []
    cycles: list[list[str]] = []

    def visit(node: str) -> None:
        if node in visiting:
            cycles.append(stack[stack.index(node):] + [node])
            return
        if node in visited:
            return
        visiting.add(node)
        stack.append(node)
        for dep in sorted(graph.get(node, set())):
            visit(dep)
        stack.pop()
        visiting.remove(node)
        visited.add(node)

    for node in sorted(graph):
        visit(node)
    errors.extend("workspace cycle: " + " -> ".join(cycle) for cycle in cycles)
    return not errors, errors, {
        "workspace_count": len(workspace),
        "graph": {key: sorted(value) for key, value in sorted(graph.items())},
        "cycles": cycles,
    }


def no_node_case() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    forbidden_paths: list[str] = []
    for path in git("ls-files").splitlines():
        lower = path.lower()
        if lower.endswith("dockerfile") or "/dockerfile" in lower or lower.endswith(("compose.yml", "compose.yaml")) or "/deploy/docker/" in f"/{lower}":
            forbidden_paths.append(path)
    if forbidden_paths:
        errors.append("production container path(s) present: " + ", ".join(forbidden_paths))

    bad_scripts: list[str] = []
    all_manifests: dict[str, tuple[Path, dict[str, Any]]] = {"root": (ROOT / "package.json", load(ROOT / "package.json")), **manifests()}
    for _, (path, data) in all_manifests.items():
        scripts = data.get("scripts") if isinstance(data.get("scripts"), dict) else {}
        for key, value in scripts.items():
            key_lower = str(key).lower()
            value_lower = str(value).lower()
            if key_lower in {"start", "serve", "serve:ssr", "preview:prod"} or "pm2" in value_lower or re.search(r"node\s+.*server", value_lower) or "vite preview" in value_lower or "astro preview" in value_lower:
                bad_scripts.append(f"{path.relative_to(ROOT)}:{key}={value}")
    if bad_scripts:
        errors.append("production Node/SSR script(s) present: " + "; ".join(bad_scripts))
    return not errors, errors, {"forbidden_paths": forbidden_paths, "forbidden_scripts": bad_scripts}


def dependency_case() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    lock = ROOT / "pnpm-lock.yaml"
    lock_sha: str | None = None
    if not lock.exists():
        errors.append("pnpm-lock.yaml missing")
    else:
        text = lock.read_text(encoding="utf-8")
        lock_sha = digest(lock)
        if "lockfileVersion: '9.0'" not in text:
            errors.append("unexpected pnpm lockfile version")
        pinned = ["19.2.8", "1.170.29", "5.101.4", "8.2.1", "7.2.2", "0.41.7", "6.0.3"]
        missing = [item for item in pinned if item not in text]
        if missing:
            errors.append("lockfile missing pinned versions: " + ", ".join(missing))

    inventory: dict[str, dict[str, str]] = {}
    for name, (_, data) in sorted(manifests().items()):
        merged: dict[str, str] = {}
        merged.update(dependency_section(data, "dependencies"))
        merged.update(dependency_section(data, "devDependencies"))
        inventory[name] = dict(sorted(merged.items()))
    return not errors, errors, {"lockfile_sha256": lock_sha, "workspace_dependencies": inventory}


def bundle_report() -> dict[str, Any]:
    report: dict[str, Any] = {"generated_at": now(), "apps": {}}
    for app in ("site", "workspace", "admin"):
        dist = ROOT / f"frontend/apps/{app}/dist"
        manifest_path = dist / ".vite/manifest.json"
        chunks: list[dict[str, Any]] = []
        if manifest_path.exists():
            for key, entry in sorted(load(manifest_path).items()):
                if isinstance(entry, dict):
                    file_path = dist / str(entry.get("file", ""))
                    chunks.append({
                        "key": key,
                        "file": entry.get("file"),
                        "bytes": file_path.stat().st_size if file_path.exists() else None,
                        "isEntry": bool(entry.get("isEntry")),
                        "isDynamicEntry": bool(entry.get("isDynamicEntry")),
                    })
        report["apps"][app] = {"manifest": str(manifest_path.relative_to(ROOT)), "chunks": chunks}
    docs_dist = ROOT / "frontend/apps/docs/dist"
    report["apps"]["docs"] = {"html_files": sorted(str(path.relative_to(docs_dist)) for path in docs_dist.rglob("*.html")) if docs_dist.exists() else []}
    return report


def summary() -> dict[str, Any]:
    items: list[dict[str, Any]] = []
    for case_id, _ in CASES:
        path = RESULTS / f"{case_id}.json"
        items.append(load(path) if path.exists() else {"case_id": case_id, "status": "NOT_RUN", "errors": ["result missing"]})
    passed = sum(item.get("status") == "PASS" for item in items)
    return {"passed": passed, "failed": len(items) - passed, "total": len(items), "cases": items}


def generate_evidence(commit: str, branch: str) -> None:
    environment = {"generated_at": now(), "os": platform.platform(), "python": platform.python_version(), "node": version(["node", "--version"]), "pnpm": version(["pnpm", "--version"])}
    write_json(P01 / "environment.json", environment)
    write_json(P01 / "source.json", {
        "repository": "Techshrr/GoJet",
        "remote": "https://github.com/Techshrr/GoJet.git",
        "branch": branch,
        "implementation_commit": commit,
        "specification_ids": SPEC_IDS,
        "toolchain": {"node": environment["node"], "pnpm": environment["pnpm"], "typescript": "6.0.3"},
    })
    dep_ok, dep_errors, dep_details = dependency_case()
    write_json(P01 / "dependency-report.json", {"status": "PASS" if dep_ok else "FAIL", "errors": dep_errors, **dep_details})
    write_json(P01 / "bundle-report.json", bundle_report())
    result_summary = summary()
    write_json(G1 / "p01-engineering.json", {
        "gate": "G1",
        "scope": "P01 engineering foundation subset",
        "implementation_commit": commit,
        "status": "PASS" if result_summary["failed"] == 0 else "FAIL",
        "results": {key: result_summary[key] for key in ("passed", "failed", "total")},
        "full_G1_release_gate_complete": False,
        "note": "G1 also has P21/P22 obligations; this closes only the P01 engineering subset.",
    })
    command_lines: list[str] = []
    for case_id, _ in CASES:
        result = load(RESULTS / f"{case_id}.json")
        command_lines.append(f"{case_id}\t{result['status']}\t{json.dumps(result.get('details', {}).get('command', '-'), ensure_ascii=False)}")
    (P01 / "commands.log").write_text("\n".join(command_lines) + "\n", encoding="utf-8")
    candidates = [
        P01 / "environment.json", P01 / "source.json", P01 / "commands.log", P01 / "test-plan.json", P01 / "review.md",
        P01 / "bundle-report.json", P01 / "dependency-report.json", G1 / "p01-engineering.json",
        *[RESULTS / f"{case_id}.json" for case_id, _ in CASES],
    ]
    write_json(P01 / "evidence-index.json", {
        "schema_version": 1,
        "node": "P01",
        "implementation_commit": commit,
        "specification_ids": SPEC_IDS,
        "generated_at": now(),
        "results": {key: result_summary[key] for key in ("passed", "failed", "total")},
        "files": [{"path": str(path.relative_to(ROOT)), "sha256": digest(path)} for path in candidates if path.exists()],
    })


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", choices=[case_id for case_id, _ in CASES])
    args = parser.parse_args()
    os.chdir(ROOT)
    for directory in (P01, RESULTS, LOGS, G1):
        directory.mkdir(parents=True, exist_ok=True)

    direct_cases = {"P01-T004": boundary_case, "P01-T006": no_node_case, "P01-T007": dependency_case}
    if args.case:
        if args.case not in direct_cases:
            print(f"{args.case} is command-driven; run the full validator")
            return 2
        passed, errors, details = direct_cases[args.case]()
        record(args.case, passed, errors, details)
        print(f"{args.case}: {'PASS' if passed else 'FAIL'}")
        for error in errors:
            print(f"  - {error}")
        return 0 if passed else 1

    install_ok = run("P01-T001", ["pnpm", "install", "--frozen-lockfile"], "install.log")
    if install_ok:
        run("P01-T002", ["pnpm", "run", "typecheck"], "typecheck.log")
        build_errors: list[str] = []
        build_details: dict[str, Any] = {"commands": []}
        started = time.monotonic()
        for app in ("@gojet/site", "@gojet/workspace", "@gojet/admin", "@gojet/docs"):
            command = ["pnpm", "--filter", app, "build"]
            result = subprocess.run(command, cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
            log_name = app.split("/")[-1] + "-build.log"
            (LOGS / log_name).write_text(result.stdout or "", encoding="utf-8")
            build_details["commands"].append({"app": app, "command": command, "exit_code": result.returncode, "log": f"artifacts/v10/P01/build-logs/{log_name}"})
            if result.returncode != 0:
                build_errors.append(f"{app} build exited {result.returncode}")
        build_details["duration_seconds"] = round(time.monotonic() - started, 3)
        record("P01-T003", not build_errors, build_errors, build_details)
    else:
        record("P01-T002", False, ["precondition failed: frozen install"])
        record("P01-T003", False, ["precondition failed: frozen install"])

    passed, errors, details = boundary_case()
    record("P01-T004", passed, errors, details)
    if load(RESULTS / "P01-T003.json")["status"] == "PASS":
        run("P01-T005", ["node", "scripts/p01/smoke.mjs"], "static-output-smoke.log")
    else:
        record("P01-T005", False, ["precondition failed: independent builds"])
    passed, errors, details = no_node_case()
    record("P01-T006", passed, errors, details)
    passed, errors, details = dependency_case()
    record("P01-T007", passed, errors, details)

    commit = git("rev-parse", "HEAD")
    branch = os.environ.get("GITHUB_HEAD_REF") or os.environ.get("GITHUB_REF_NAME") or git("branch", "--show-current") or "detached"
    generate_evidence(commit, branch)
    result_summary = summary()
    for item in result_summary["cases"]:
        print(f"{item['case_id']}: {item['status']}")
        for error in item.get("errors", []):
            print(f"  - {error}")
    print(f"P01 summary: {result_summary['passed']}/{result_summary['total']} PASS")
    return 0 if result_summary["failed"] == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
