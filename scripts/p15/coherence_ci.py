#!/usr/bin/env python3
"""Bind exact-head P15 producer workflows, assemble T001..T027 evidence, and run T028."""
from __future__ import annotations

import json
import os
import shutil
import subprocess
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable

ROOT = Path("artifacts/v10/P15")
MANIFEST = ROOT / "evidence-producer-manifest.json"
CONTRACT_NAME = "P15 Authentication OAuth Account Contract"

INDEPENDENT: dict[str, tuple[str, Callable[[str], str]]] = {
    "P15 Real Authentication Integration": ("exact", lambda sha: f"p15-t001-t007-real-auth-{sha}"),
    "P15 Authentication Security Integration": ("exact", lambda sha: f"p15-t008-t012-auth-security-{sha}"),
    "P15 Account OAuth Integration": ("exact", lambda sha: f"p15-t013-t018-account-oauth-{sha}"),
    "P15 Handoff Mail Audit Integration": ("exact", lambda sha: f"p15-t019-t023-t027-handoff-mail-audit-{sha}"),
    "P15 Auth Route Browser Authority": ("prefix", lambda sha: "p15-t024-auth-browser-"),
    "P15 Workspace Account Settings Browser Authority": ("exact", lambda sha: f"p15-t025-account-browser-{sha}"),
    "P15 Admin OAuth Browser Authority": ("exact", lambda sha: f"p15-t026-admin-oauth-{sha}"),
}

CASE_DIRS = {
    1: "api", 2: "api", 3: "security", 4: "api", 5: "security", 6: "security", 7: "security",
    8: "security", 9: "security", 10: "security", 11: "security", 12: "security",
    13: "api", 14: "api",
    15: "oauth", 16: "oauth", 17: "oauth", 18: "oauth", 19: "oauth", 20: "oauth", 21: "oauth",
    22: "mail", 23: "security", 24: "browser", 25: "browser", 26: "browser", 27: "audit",
}

PRODUCER_CASES = {
    "P15 Real Authentication Integration": tuple(range(1, 8)),
    "P15 Authentication Security Integration": tuple(range(8, 13)),
    "P15 Account OAuth Integration": tuple(range(13, 19)),
    "P15 Handoff Mail Audit Integration": (19, 20, 21, 22, 23, 27),
    "P15 Auth Route Browser Authority": (24,),
    "P15 Workspace Account Settings Browser Authority": (25,),
    "P15 Admin OAuth Browser Authority": (26,),
}


def need_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"{name} is required")
    return value


HEAD = need_env("EXACT_HEAD")
REPOSITORY = need_env("REPOSITORY")
TOKEN = need_env("GH_TOKEN")
CURRENT_RUN_ID = int(need_env("GITHUB_RUN_ID"))
CURRENT_RUN_NUMBER = int(os.environ.get("GITHUB_RUN_NUMBER", "0"))

HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": f"Bearer {TOKEN}",
    "X-GitHub-Api-Version": "2022-11-28",
}


def api_get(url: str) -> dict:
    request = urllib.request.Request(url, headers=HEADERS)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def artifact_for(run_id: int, mode: str, locator: str) -> dict | None:
    data = api_get(f"https://api.github.com/repos/{REPOSITORY}/actions/runs/{run_id}/artifacts?per_page=100")
    artifacts = [item for item in data.get("artifacts", []) if not item.get("expired")]
    if mode == "exact":
        matches = [item for item in artifacts if item.get("name") == locator]
    elif mode == "prefix":
        matches = [item for item in artifacts if isinstance(item.get("name"), str) and item["name"].startswith(locator)]
    else:
        raise SystemExit(f"unsupported artifact locator mode {mode}")
    if len(matches) != 1:
        return None
    item = matches[0]
    return {
        "id": int(item["id"]),
        "name": item["name"],
        "digest": item.get("digest"),
        "size_in_bytes": int(item.get("size_in_bytes", 0)),
    }


def bind_producers() -> dict:
    ROOT.mkdir(parents=True, exist_ok=True)
    contract_name = f"p15-authentication-oauth-account-contract-guard-{HEAD}"
    deadline = time.time() + 60 * 60

    while time.time() < deadline:
        contract_artifact = artifact_for(CURRENT_RUN_ID, "exact", contract_name)
        query = urllib.parse.urlencode({"head_sha": HEAD, "event": "pull_request", "per_page": 100})
        runs = api_get(f"https://api.github.com/repos/{REPOSITORY}/actions/runs?{query}").get("workflow_runs", [])
        latest: dict[str, dict] = {}
        for run in runs:
            name = run.get("name")
            if name in INDEPENDENT and (name not in latest or int(run.get("id", 0)) > int(latest[name].get("id", 0))):
                latest[name] = run

        missing = [name for name in INDEPENDENT if name not in latest]
        pending = [name for name in INDEPENDENT if name in latest and latest[name].get("status") != "completed"]
        failed = [
            name for name in INDEPENDENT
            if name in latest and latest[name].get("status") == "completed" and latest[name].get("conclusion") != "success"
        ]
        if contract_artifact is None:
            pending.append(f"{CONTRACT_NAME}:artifact")

        entries: dict[str, dict] = {}
        if contract_artifact is not None:
            current = api_get(f"https://api.github.com/repos/{REPOSITORY}/actions/runs/{CURRENT_RUN_ID}")
            entries[CONTRACT_NAME] = {
                "run_id": CURRENT_RUN_ID,
                "run_number": CURRENT_RUN_NUMBER,
                "head_sha": HEAD,
                "status": "completed",
                "conclusion": "success",
                "authority_scope": "contract-artifact-after-successful-contract-steps",
                "workflow_status_at_bind": current.get("status"),
                "artifact": contract_artifact,
            }

        if not missing and not pending and not failed:
            for name, run in latest.items():
                mode, locator_fn = INDEPENDENT[name]
                artifact = artifact_for(int(run["id"]), mode, locator_fn(HEAD))
                if artifact is None:
                    failed.append(f"{name}:artifact:{locator_fn(HEAD)}")
                    continue
                entries[name] = {
                    "run_id": int(run["id"]),
                    "run_number": int(run.get("run_number", 0)),
                    "head_sha": run.get("head_sha"),
                    "status": run.get("status"),
                    "conclusion": run.get("conclusion"),
                    "authority_scope": "completed-workflow",
                    "artifact": artifact,
                }

        manifest = {
            "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
            "implementation_commit": HEAD,
            "required_workflows": entries,
            "missing": missing,
            "pending": pending,
            "failed": failed,
        }
        MANIFEST.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")

        if failed:
            raise SystemExit("required P15 producer/artifact failed: " + ", ".join(failed))
        if not missing and not pending and len(entries) == 8:
            print(f"P15 T028 producer authority green for {HEAD}")
            return manifest
        print(f"Waiting P15 T028 producers missing={missing} pending={pending}", flush=True)
        time.sleep(10)

    raise SystemExit(f"timed out waiting for P15 T028 producers on {HEAD}")


def download(run_id: int, artifact_name: str, destination: Path) -> None:
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    subprocess.run(
        ["gh", "run", "download", str(run_id), "--repo", REPOSITORY, "--name", artifact_name, "--dir", str(destination)],
        check=True,
        env={**os.environ, "GH_TOKEN": TOKEN},
    )


def first_file(root: Path, name: str) -> Path:
    matches = sorted(root.rglob(name))
    if not matches:
        raise SystemExit(f"missing {name} in {root}")
    return matches[0]


def copy_file(source: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination)


def assemble_evidence(manifest: dict) -> None:
    temp = Path("/tmp/p15-t028")
    if temp.exists():
        shutil.rmtree(temp)
    temp.mkdir(parents=True)

    rows = manifest["required_workflows"]
    destinations: dict[str, Path] = {CONTRACT_NAME: temp / "contract"}
    for index, name in enumerate(INDEPENDENT, start=1):
        destinations[name] = temp / f"producer-{index:02d}"

    for name, destination in destinations.items():
        row = rows[name]
        download(int(row["run_id"]), row["artifact"]["name"], destination)

    for dirname in ("api", "security", "oauth", "mail", "audit", "browser", "captures", "runtime", "results", "contract-guard"):
        path = ROOT / dirname
        if path.exists():
            shutil.rmtree(path)
        path.mkdir(parents=True, exist_ok=True)

    contract_root = destinations[CONTRACT_NAME]
    for filename in ("contract.json", "implementation_commit.txt", "base_integration_commit.txt", "contract_authority.txt", "case_range.txt"):
        copy_file(first_file(contract_root, filename), ROOT / "contract-guard" / filename)
    if (ROOT / "contract-guard" / "implementation_commit.txt").read_text(encoding="utf-8").strip() != HEAD:
        raise SystemExit("downloaded P15 contract guard exact-head mismatch")

    for name, cases in PRODUCER_CASES.items():
        producer_root = destinations[name]
        for number in cases:
            cid = f"P15-T{number:03d}"
            copy_file(first_file(producer_root, f"{cid}.json"), ROOT / CASE_DIRS[number] / f"{cid}.json")
        if name in (
            "P15 Auth Route Browser Authority",
            "P15 Workspace Account Settings Browser Authority",
            "P15 Admin OAuth Browser Authority",
        ):
            number = cases[0]
            cid = f"P15-T{number:03d}"
            captures = sorted(producer_root.rglob(f"{cid}-*.png"))
            for source in captures:
                copy_file(source, ROOT / "captures" / source.name)
            native_hash = first_file(producer_root, "native-platformapi.sha256")
            copy_file(native_hash, ROOT / "runtime" / f"browser-{number:03d}" / "native-platformapi.sha256")

    expected_counts = {"api": 5, "security": 10, "oauth": 7, "mail": 1, "audit": 1, "browser": 3}
    for dirname, expected in expected_counts.items():
        actual = len(list((ROOT / dirname).glob("P15-T*.json")))
        if actual != expected:
            raise SystemExit(f"{dirname} case evidence count {actual} != {expected}")

    capture_minimums = {24: 12, 25: 12, 26: 9}
    for number, minimum in capture_minimums.items():
        actual = len(list((ROOT / "captures").glob(f"P15-T{number:03d}-*.png")))
        if actual < minimum:
            raise SystemExit(f"P15-T{number:03d} capture count {actual} < {minimum}")


def validate() -> None:
    env = {**os.environ, "EXACT_HEAD": HEAD}
    subprocess.run(["python3", "scripts/p15/validate_coherence.py"], check=True, env=env)
    result_path = ROOT / "results" / "P15-T028.json"
    result = json.loads(result_path.read_text(encoding="utf-8"))
    if result.get("status") != "PASS" or result.get("errors") != []:
        raise SystemExit(f"P15-T028 did not PASS: {result}")
    if result.get("implementation_commit") != HEAD:
        raise SystemExit("P15-T028 exact-head mismatch")
    observations = result.get("observations", {})
    counts = observations.get("browser_capture_counts", {})
    checks = {
        "input_evidence_count": observations.get("input_evidence_count") == 27,
        "same_exact_head": observations.get("same_exact_head") is True,
        "producer_coherent": observations.get("producer_coherent") is True,
        "eight_producers": len(observations.get("producer_run_ids", {})) == 8,
        "eight_artifacts": len(observations.get("producer_artifacts", {})) == 8,
        "t024_captures": int(counts.get("P15-T024", 0)) >= 12,
        "t025_captures": int(counts.get("P15-T025", 0)) >= 12,
        "t026_captures": int(counts.get("P15-T026", 0)) >= 9,
        "secret_safe": observations.get("secret_safe") is True,
        "mixed_head_rejected": observations.get("mixed_head_rejected") is True,
        "unsafe_evidence_rejected": observations.get("unsafe_evidence_rejected") is True,
        "reviewable_hashed_case_evidence": observations.get("reviewable_hashed_case_evidence") is True,
    }
    failed = [name for name, passed in checks.items() if not passed]
    if failed:
        raise SystemExit(f"P15-T028 observation checks failed: {failed}: {observations}")
    print(f"P15-T028 exact-head evidence coherence PASS on {HEAD}")


def main() -> int:
    if subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip() != HEAD:
        raise SystemExit("checkout exact-head mismatch")
    manifest = bind_producers()
    assemble_evidence(manifest)
    validate()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
