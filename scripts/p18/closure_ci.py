#!/usr/bin/env python3
"""P18-T026 accountable exact-head closure orchestration."""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
import time
import urllib.parse
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
P18 = ROOT / "artifacts" / "v10" / "P18"
HEAD = os.environ["EXACT_HEAD"].strip()
HEAD_REF = os.environ["HEAD_REF"].strip()
REPO = os.environ["REPOSITORY"].strip()
TOKEN = os.environ["GH_TOKEN"].strip()

CONTRACT_AUTHORITY = "784870c56c48591d51663f5d5521c057d74108f9"
P17_SOURCE = "5818406072a131db1c7d8aa7bc5ef8a7adc8d51f"
P17_INTEGRATION = "08cb39bbe54717b711e2d09840ecde04b66bb50f"
P17_RUN = 33232541982
P17_ARTIFACT = 9709093486
P17_DIGEST = "sha256:72f8256b242c4412c82cfd4e69c653e4051dc2b7a951c10c9214c2db775805c1"
P04_SOURCE = "659e25e3c5e263ffb3dd74cde953e812b75a7439"
P04_RUN = 32392744860
P04_ARTIFACT = 9415518410
P04_DIGEST = "sha256:5f6b4ec5be87d866b07599e8bd32d75171a81523d29dd86441a524bf33cbc7bb"

PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

REQUIRED = {
    "P00 Bootstrap and G0 Traceability": "p00-bootstrap.yml",
    "P01 Engineering Foundation": "p01-engineering.yml",
    "P02 Brand Foundation": "p02-brand-foundation.yml",
    "P03 Design System": "p03-design-system.yml",
    "P04 Product Shells": "p04-product-shells.yml",
    "P18 Docs Multilingual Discovery Contract": "p18-docs-multilingual-discovery.yml",
    "P18 Docs Core Integration": "p18-docs-core.yml",
    "P18 Docs Discovery Integration": "p18-docs-discovery.yml",
    "P18 Docs Quality Integration": "p18-docs-quality.yml",
    "P18 Evidence Coherence": "p18-docs-evidence.yml",
}

EXCLUDED_REVISION_SPECIFIC = {
    **{f"P{i:02d} Closure": "signed predecessor closure is revision-specific; inherited authority remains bound while P18 reruns its affected Docs/foundation surface" for i in range(5, 14)},
    "P14 Support Tickets and Mail Contract": "P14 revision-specific closure is inherited and outside the P18 Docs change surface",
    "P15 Authentication OAuth Account Contract": "P15 revision-specific closure is inherited and outside the P18 Docs change surface",
    "P16 Closure": "P16 revision-specific closure is inherited and outside the P18 Docs change surface",
    "P17 Closure": f"immediate predecessor signed closure is live-bound from {P17_SOURCE}; P18 does not reinterpret it",
}
MATRIX_SCOPE = (
    "P18 changes only Docs/static-discovery, P18 governance tooling and the inherited P04 Docs fixture. "
    "The matrix reruns P00-P04 foundations plus all P18 producer/coherence workflows; P17 signed authority is live-bound."
)

HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": f"Bearer {TOKEN}",
    "X-GitHub-Api-Version": "2022-11-28",
}


class CrossHostSafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, response_headers, newurl):
        redirected = super().redirect_request(req, fp, code, msg, response_headers, newurl)
        if redirected is not None and urllib.parse.urlsplit(req.full_url).netloc != urllib.parse.urlsplit(newurl).netloc:
            for header in ("Authorization", "Accept", "X-GitHub-Api-Version"):
                redirected.remove_header(header)
        return redirected


ARTIFACT_OPENER = urllib.request.build_opener(CrossHostSafeRedirect())


def api(url: str, *, method: str = "GET", body=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(url, data=data, method=method, headers=HEADERS)
    with urllib.request.urlopen(request, timeout=30) as response:
        raw = response.read()
        return json.loads(raw) if raw else None


def exact_runs() -> list[dict]:
    query = urllib.parse.urlencode({"head_sha": HEAD, "per_page": 100})
    return api(f"https://api.github.com/repos/{REPO}/actions/runs?{query}").get("workflow_runs", [])


def dispatch(workflow: str) -> None:
    quoted = urllib.parse.quote(workflow, safe="")
    api(f"https://api.github.com/repos/{REPO}/actions/workflows/{quoted}/dispatches",
        method="POST", body={"ref": HEAD_REF})


def wait_matrix() -> dict[str, dict]:
    P18.mkdir(parents=True, exist_ok=True)
    path = P18 / "regression-manifest.json"
    dispatched: set[str] = set()
    deadline = time.time() + 210 * 60
    while time.time() < deadline:
        latest: dict[str, dict] = {}
        for run in exact_runs():
            name = run.get("name")
            if name in REQUIRED and (name not in latest or int(run.get("id", 0)) > int(latest[name].get("id", 0))):
                latest[name] = run
        missing = [name for name in REQUIRED if name not in latest]
        pending = [name for name in REQUIRED if name in latest and latest[name].get("status") != "completed"]
        failed = [name for name in REQUIRED if name in latest and latest[name].get("status") == "completed" and latest[name].get("conclusion") != "success"]
        rows = {
            name: {
                "run_id": int(latest[name]["id"]),
                "run_number": int(latest[name].get("run_number", 0)),
                "head_sha": latest[name].get("head_sha"),
                "event": latest[name].get("event"),
                "status": latest[name].get("status"),
                "conclusion": latest[name].get("conclusion"),
                "html_url": latest[name].get("html_url"),
            }
            for name in REQUIRED if name in latest
        }
        manifest = {
            "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
            "implementation_commit": HEAD,
            "scope": MATRIX_SCOPE,
            "required_workflows": rows,
            "excluded_revision_specific_workflows": EXCLUDED_REVISION_SPECIFIC,
            "dispatched_workflows": sorted(dispatched),
            "missing": missing,
            "pending": pending,
            "failed": failed,
        }
        path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        if failed:
            raise SystemExit("required P18 exact-head workflows failed: " + ", ".join(failed))
        if not missing and not pending and len(rows) == len(REQUIRED):
            print(f"P18 affected exact-head matrix green ({len(REQUIRED)}/{len(REQUIRED)}) for {HEAD}")
            return rows
        for name in missing:
            if name not in dispatched:
                print(f"Dispatching missing exact-head workflow {name} via {REQUIRED[name]} on {HEAD_REF}", flush=True)
                dispatch(REQUIRED[name])
                dispatched.add(name)
        print(f"Waiting P18 matrix missing={missing} pending={pending}", flush=True)
        time.sleep(10)
    raise SystemExit(f"timed out waiting for P18 affected workflows on {HEAD}")


def archive_artifact(artifact_id: int, expected_digest: str, destination: Path) -> Path:
    destination.parent.mkdir(parents=True, exist_ok=True)
    request = urllib.request.Request(
        f"https://api.github.com/repos/{REPO}/actions/artifacts/{artifact_id}/zip",
        headers=HEADERS,
    )
    with ARTIFACT_OPENER.open(request, timeout=60) as response, destination.open("wb") as handle:
        shutil.copyfileobj(response, handle)
    digest = "sha256:" + hashlib.sha256(destination.read_bytes()).hexdigest()
    if digest != expected_digest:
        raise SystemExit(f"artifact archive digest mismatch {artifact_id}: {digest} != {expected_digest}")
    return destination


def artifact_for_run(run_id: int, name: str) -> dict:
    artifacts = api(f"https://api.github.com/repos/{REPO}/actions/runs/{run_id}/artifacts?per_page=100").get("artifacts", [])
    matches = [item for item in artifacts if item.get("name") == name and not item.get("expired")]
    if len(matches) != 1:
        raise SystemExit(f"expected one exact-head artifact {name} on run {run_id}, got {len(matches)}")
    artifact = matches[0]
    if not str(artifact.get("digest", "")).startswith("sha256:") or int(artifact.get("size_in_bytes", 0)) <= 0:
        raise SystemExit(f"invalid artifact metadata for {name}")
    return artifact


def download_t025(rows: dict[str, dict]) -> dict:
    run_id = int(rows["P18 Evidence Coherence"]["run_id"])
    name = f"gojet-v10-p18-evidence-{HEAD}"
    artifact = artifact_for_run(run_id, name)
    archive = archive_artifact(int(artifact["id"]), artifact["digest"], Path("/tmp/p18-t025.zip"))
    temp = Path("/tmp/p18-t025")
    shutil.rmtree(temp, ignore_errors=True)
    temp.mkdir(parents=True)
    with zipfile.ZipFile(archive) as z:
        z.extractall(temp)
    result_path = next(iter(temp.rglob("P18-T025.json")), None)
    if result_path is None:
        raise SystemExit("T025 artifact missing P18-T025.json")
    t025 = json.loads(result_path.read_text(encoding="utf-8"))
    if t025.get("node") != "P18" or t025.get("case") != "P18-T025" or t025.get("status") != "PASS" or t025.get("implementation_commit") != HEAD:
        raise SystemExit(f"downloaded T025 is not exact-head PASS: {t025}")
    obs = t025.get("observations") or {}
    if not (
        obs.get("input_evidence_count") == 24
        and obs.get("same_exact_head") is True
        and obs.get("secret_safe") is True
        and obs.get("producer_count") == 4
        and obs.get("producer_artifact_count") == 4
        and obs.get("inherited_authorities_bound") is True
        and obs.get("mixed_head_rejected") is True
        and obs.get("unsafe_evidence_rejected") is True
        and obs.get("merge_authoritative") is False
    ):
        raise SystemExit(f"T025 coherence observations invalid: {obs}")
    (P18 / "coherence").mkdir(parents=True, exist_ok=True)
    shutil.copy2(result_path, P18 / "coherence" / "P18-T025.json")
    for filename in ("evidence-index.json", "producer-manifest.json", "inherited-authorities.json"):
        source = next(iter(temp.rglob(filename)), None)
        if source is None:
            raise SystemExit(f"T025 artifact missing {filename}")
        shutil.copy2(source, P18 / "coherence" / filename)
    return {
        "run_id": run_id,
        "artifact_id": int(artifact["id"]),
        "artifact_name": artifact["name"],
        "artifact_digest": artifact["digest"],
        "artifact_size_in_bytes": int(artifact["size_in_bytes"]),
    }


def bind_p17() -> dict:
    run = api(f"https://api.github.com/repos/{REPO}/actions/runs/{P17_RUN}")
    artifact = api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{P17_ARTIFACT}")
    if not (
        run.get("head_sha") == P17_SOURCE
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and artifact.get("digest") == P17_DIGEST
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == P17_RUN
        and artifact.get("workflow_run", {}).get("head_sha") == P17_SOURCE
    ):
        raise SystemExit("P17 inherited signed authority live metadata mismatch")
    archive = archive_artifact(P17_ARTIFACT, P17_DIGEST, Path("/tmp/p17-signed-authority-p18.zip"))
    temp = Path("/tmp/p17-signed-authority-p18")
    shutil.rmtree(temp, ignore_errors=True)
    temp.mkdir(parents=True)
    with zipfile.ZipFile(archive) as z:
        z.extractall(temp)
    closure_path = next(iter(temp.rglob("closure.json")), None)
    if closure_path is None:
        raise SystemExit("P17 signed authority missing closure.json")
    closure = json.loads(closure_path.read_text(encoding="utf-8"))
    if closure.get("node") != "P17" or closure.get("status") != "PASS" or closure.get("phase") != "signed" or closure.get("merge_authoritative") is not True:
        raise SystemExit("P17 signed authority content invalid")
    inherited = P18 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    metadata = {
        "node": "P17",
        "signed_source_commit": P17_SOURCE,
        "integration_commit": P17_INTEGRATION,
        "closure_run_id": P17_RUN,
        "artifact_id": P17_ARTIFACT,
        "artifact_digest": P17_DIGEST,
        "phase": "signed",
        "merge_authoritative": True,
    }
    (inherited / "p17-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def bind_p04() -> dict:
    run = api(f"https://api.github.com/repos/{REPO}/actions/runs/{P04_RUN}")
    artifact = api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{P04_ARTIFACT}")
    if not (
        run.get("head_sha") == P04_SOURCE
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and artifact.get("digest") == P04_DIGEST
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == P04_RUN
    ):
        raise SystemExit("P04 inherited Docs-shell authority live metadata mismatch")
    inherited = P18 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    metadata = {
        "node": "P04",
        "reviewed_pre_sign_commit": P04_SOURCE,
        "workflow_run_id": P04_RUN,
        "artifact_id": P04_ARTIFACT,
        "artifact_digest": P04_DIGEST,
        "required_tests": "10/10",
    }
    (inherited / "p04-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def primary_review(text: str) -> str:
    lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", text, flags=re.MULTILINE)
    if len(lines) != 1 or lines[0] not in (PENDING, SIGNED):
        raise SystemExit(f"invalid P18 primary review status lines: {lines}")
    return lines[0]


def grab(text: str, pattern: str, label: str) -> str:
    match = re.search(pattern, text)
    if not match:
        raise SystemExit(f"signed P18 review missing {label}")
    return match.group(1)


def bind_presign_if_signed(review: str) -> dict | None:
    if primary_review(review) != SIGNED:
        return None
    source = grab(review, r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`", "reviewed pre-sign SHA")
    run_id = int(grab(review, r"Pre-sign T026 closure run: `([0-9]+)`", "pre-sign closure run"))
    artifact_id = int(grab(review, r"Pre-sign T026 closure artifact: `([0-9]+)`", "pre-sign closure artifact"))
    digest = grab(review, r"Pre-sign T026 closure digest: `(sha256:[0-9a-f]{64})`", "pre-sign closure digest")
    if "Evidence disposition: `P18-T001..P18-T025 PASS`" not in review or "P0/P1/DECISION REQUIRED: `0/0/0`" not in review:
        raise SystemExit("signed P18 review missing exact case/defect disposition")
    run = api(f"https://api.github.com/repos/{REPO}/actions/runs/{run_id}")
    artifact = api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{artifact_id}")
    if not (
        run.get("head_sha") == source
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and artifact.get("digest") == digest
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == run_id
        and artifact.get("workflow_run", {}).get("head_sha") == source
    ):
        raise SystemExit("P18 pre-sign closure live authority metadata mismatch")
    archive = archive_artifact(artifact_id, digest, Path("/tmp/p18-presign.zip"))
    temp = Path("/tmp/p18-presign")
    shutil.rmtree(temp, ignore_errors=True)
    temp.mkdir(parents=True)
    with zipfile.ZipFile(archive) as z:
        z.extractall(temp)
    closure_path = next(iter(temp.rglob("closure.json")), None)
    if closure_path is None:
        raise SystemExit("pre-sign P18 artifact missing closure.json")
    closure = json.loads(closure_path.read_text(encoding="utf-8"))
    if (
        closure.get("node") != "P18"
        or closure.get("implementation_commit") != source
        or closure.get("status") != "PASS"
        or closure.get("phase") != "pre-sign"
        or closure.get("merge_authoritative") is not False
    ):
        raise SystemExit("P18 pre-sign closure artifact content invalid")
    parent = subprocess.check_output(["git", "rev-parse", "HEAD^"], cwd=ROOT, text=True).strip()
    if parent != source:
        raise SystemExit(f"signed P18 revision must be a direct review-only child of pre-sign source: parent={parent} source={source}")
    changed = {
        line for line in subprocess.check_output(["git", "diff", "--name-only", f"{source}..{HEAD}"], cwd=ROOT, text=True).splitlines()
        if line
    }
    if changed != {"artifacts/v10/P18/review.md"}:
        raise SystemExit(f"signed P18 child is not review-only: {sorted(changed)}")
    metadata = {
        "source_commit": source,
        "closure_run_id": run_id,
        "artifact_id": artifact_id,
        "artifact_digest": digest,
        "workflow_status": run.get("status"),
        "workflow_conclusion": run.get("conclusion"),
        "phase": "pre-sign",
        "merge_authoritative": False,
    }
    inherited = P18 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    (inherited / "pre-sign-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def main() -> int:
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if actual != HEAD:
        raise SystemExit(f"checkout exact-head mismatch: {actual} != {HEAD}")
    if len(REQUIRED) != 10:
        raise SystemExit(f"P18 affected matrix contract expected 10 workflows, got {len(REQUIRED)}")

    contract_raw = subprocess.check_output(["python3", "scripts/p18/validate_contract.py"], cwd=ROOT, text=True)
    contract = json.loads(contract_raw)
    if (
        contract.get("status") != "PASS"
        or contract.get("errors") != []
        or contract.get("implementation_commit") != HEAD
        or contract.get("contract_authority") != CONTRACT_AUTHORITY
    ):
        raise SystemExit(f"P18 contract guard invalid at closure: {contract}")

    review = (P18 / "review.md").read_text(encoding="utf-8")
    status = primary_review(review)
    review_phase = "signed" if status == SIGNED else "pending"
    if contract.get("review_phase") != review_phase:
        raise SystemExit(f"review/contract phase mismatch {review_phase} != {contract.get('review_phase')}")

    rows = wait_matrix()
    t025_authority = download_t025(rows)
    p17_authority = bind_p17()
    p04_authority = bind_p04()
    presign_authority = bind_presign_if_signed(review)

    phase = "signed" if review_phase == "signed" else "pre-sign"
    defects = {"p0": 0, "p1": 0, "decision_required": 0}
    result = {
        "node": "P18",
        "case": "P18-T026",
        "status": "PASS",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": HEAD,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P18-T001..P18-T026",
        "phase": phase,
        "review_phase": review_phase,
        "review_only_signed_child": phase == "signed",
        "merge_authoritative": phase == "signed",
        "defects": defects,
        "evidence": {
            "t001_t024": "PASS on one exact head through T025 producer coherence",
            "t025": "PASS",
            "t026": "PASS",
            "input_case_count": 25,
            "final_case_count": 26,
        },
        "affected_matrix": {
            "passed": len(rows),
            "required": len(REQUIRED),
            "complete": len(rows) == len(REQUIRED),
            "scope": MATRIX_SCOPE,
        },
        "t025_authority": t025_authority,
        "p17_predecessor_authority": p17_authority,
        "p04_docs_shell_authority": p04_authority,
        "pre_sign_authority": presign_authority,
        "excluded_revision_specific_workflows": EXCLUDED_REVISION_SPECIFIC,
        "errors": [],
    }
    if phase == "pre-sign":
        result["signing_required"] = True
        result["signing_instruction"] = (
            "Create exactly one direct review-only child that changes only artifacts/v10/P18/review.md, "
            "records this pre-sign T026 run/artifact/digest, and reruns the complete exact-head matrix."
        )
    else:
        result["signing_required"] = False
        if presign_authority is None:
            raise SystemExit("signed phase missing live-bound pre-sign authority")

    (P18 / "closure").mkdir(parents=True, exist_ok=True)
    (P18 / "closure" / "P18-T026.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (P18 / "closure.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
