#!/usr/bin/env python3
"""P17-T035 accountable exact-head closure orchestration."""
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
P17 = ROOT / "artifacts" / "v10" / "P17"
HEAD = os.environ["EXACT_HEAD"].strip()
HEAD_REF = os.environ["HEAD_REF"].strip()
REPO = os.environ["REPOSITORY"].strip()
TOKEN = os.environ["GH_TOKEN"].strip()
CURRENT_RUN = int(os.environ.get("GITHUB_RUN_ID", "0"))

CONTRACT_AUTHORITY = "30174f40df28678360f644b8fed79736906b0ea0"
P16_SOURCE = "c22d87102a8a691b5d1d1a31506def21112700e7"
P16_INTEGRATION = "62d682a25532eef3cc207a5e9964a62f6072ede7"
P16_RUN = 33010844881
P16_ARTIFACT = 9630819391
P16_DIGEST = "sha256:00dbba2180f88ecdb6b369cb97abfdcafd211789088837d39e02a2d331a75722"
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

REQUIRED = {
    "P00 Bootstrap and G0 Traceability": "p00-bootstrap.yml",
    "P01 Engineering Foundation": "p01-engineering.yml",
    "P02 Brand Foundation": "p02-brand-foundation.yml",
    "P03 Design System": "p03-design-system.yml",
    "P04 Product Shells": "p04-product-shells.yml",
    "P05 Links Domain Contract": "p05-links-domain-contract.yml",
    "P05 Real Integration": "p05-integration.yml",
    "P05 Workspace Browser": "p05-browser.yml",
    "P06 Custom Domains": "p06-custom-domains.yml",
    "P06 Real Integration": "p06-integration.yml",
    "P06 Workspace Domains Browser": "p06-browser.yml",
    "P07 Analytics Contract": "p07-analytics.yml",
    "P07 Real Integration": "p07-integration.yml",
    "P07 Workspace Analytics Browser": "p07-browser.yml",
    "P08 QR Contract": "p08-qr.yml",
    "P08 Real QR Integration": "p08-integration.yml",
    "P08 Workspace QR Browser": "p08-browser.yml",
    "P08 Evidence Coherence": "p08-evidence.yml",
    "P09 Files Contract": "p09-files.yml",
    "P09 Real Files and ClamAV Integration": "p09-integration.yml",
    "P09 Files Health and Installer Preflight": "p09-health.yml",
    "P09 Workspace Files Browser": "p09-browser.yml",
    "P09 Evidence Coherence": "p09-evidence.yml",
    "P10 Text Contract": "p10-text.yml",
    "P10 Real Text Integration": "p10-integration.yml",
    "P10 Workspace Text Browser": "p10-browser.yml",
    "P10 Evidence Coherence": "p10-evidence.yml",
    "P11 Bio Contract": "p11-bio.yml",
    "P11 Real Bio Integration": "p11-integration.yml",
    "P11 Workspace Bio Browser": "p11-browser.yml",
    "P11 Evidence Coherence": "p11-evidence.yml",
    "P12 Workspace Organization Contract": "p12-workspace-organization.yml",
    "P12 Real Workspace Organization Integration": "p12-integration.yml",
    "P12 Workspace Organization Browser": "p12-browser.yml",
    "P12 Evidence Coherence": "p12-evidence.yml",
    "P13 Billing Payments Entitlements Contract": "p13-billing-payments-entitlements.yml",
    "P13 Real Billing Payments Entitlements Integration": "p13-integration.yml",
    "P13 Billing Commerce Browser": "p13-browser.yml",
    "P13 Evidence Coherence": "p13-evidence.yml",
    "P14 Real Support Tickets and Mail Integration": "p14-integration.yml",
    "P14 Workspace Support Browser": "p14-browser.yml",
    "P14 Admin Tickets Mail Contact Browser": "p14-browser-023.yml",
    "P15 Real Authentication Integration": "p15-real-auth-integration.yml",
    "P15 Authentication Security Integration": "p15-auth-security-integration.yml",
    "P15 Account OAuth Integration": "p15-account-oauth-integration.yml",
    "P15 Handoff Mail Audit Integration": "p15-handoff-mail-audit-integration.yml",
    "P15 Auth Route Browser Authority": "p15-browser.yml",
    "P15 Workspace Account Settings Browser Authority": "p15-account-browser.yml",
    "P15 Admin OAuth Browser Authority": "p15-admin-oauth-browser.yml",
    "P16 Trust Destination Risk Abuse Contract": "p16-trust-destination-risk-abuse.yml",
    "P16 Real Destination Risk Integration": "p16-destination-risk-integration.yml",
    "P16 Security Notification Producer": "p16-notification-producer.yml",
    "P16 Admin Risk API Integration": "p16-admin-risk-api.yml",
    "P16 Trust Browser Authority": "p16-browser.yml",
    "P16 Evidence Coherence": "p16-evidence.yml",
    "P17 Admin Permissions Audit Contract": "p17-admin-permissions-audit.yml",
    "P17 Admin Access Foundation": "p17-admin-access-foundation.yml",
    "P17 Real Admin Access Integration": "p17-admin-access-integration.yml",
    "P17 Domain Entitlement Governance Integration": "p17-domain-entitlement-integration.yml",
    "P17 User Workspace Governance Integration": "p17-user-workspace-governance-integration.yml",
    "P17 Resource and Operations Governance Integration": "p17-resource-operations-integration.yml",
    "P17 Platform Governance Integration": "p17-platform-governance-integration.yml",
    "P17 API-key Governance Integration": "p17-api-key-governance-integration.yml",
    "P17 Webhook Governance Integration": "p17-webhook-governance-integration.yml",
    "P17 Browser Governance Authority": "p17-browser.yml",
    "P17 Evidence Coherence": "p17-evidence.yml",
}

EXCLUDED = {
    **{f"P{i:02d} Closure": "revision-specific predecessor closure is inherited and functional authority is rerun on the P17 exact head" for i in range(5, 14)},
    "P14 Support Tickets and Mail Contract": "revision-specific P14 closure authority is inherited; P14 functional workflows are rerun on the P17 exact head",
    "P15 Authentication OAuth Account Contract": "revision-specific P15 closure authority is inherited; P15 functional workflows are rerun on the P17 exact head",
    "P16 Closure": f"immediate predecessor signed closure is live-bound from P16 signed source {P16_SOURCE}; P16 functional workflows are rerun on the P17 exact head",
}
HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": f"Bearer {TOKEN}",
    "X-GitHub-Api-Version": "2022-11-28",
}


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
    api(f"https://api.github.com/repos/{REPO}/actions/workflows/{quoted}/dispatches", method="POST", body={"ref": HEAD_REF})


def wait_matrix() -> dict[str, dict]:
    P17.mkdir(parents=True, exist_ok=True)
    path = P17 / "regression-manifest.json"
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
            "required_workflows": rows,
            "excluded_revision_specific_workflows": EXCLUDED,
            "dispatched_workflows": sorted(dispatched),
            "missing": missing,
            "pending": pending,
            "failed": failed,
        }
        path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        if failed:
            raise SystemExit("required P17 exact-head workflows failed: " + ", ".join(failed))
        if not missing and not pending and len(rows) == len(REQUIRED):
            print(f"P00-P17 affected exact-head matrix green ({len(REQUIRED)}/{len(REQUIRED)}) for {HEAD}")
            return rows
        for name in missing:
            if name not in dispatched:
                print(f"Dispatching missing exact-head workflow {name} via {REQUIRED[name]} on {HEAD_REF}", flush=True)
                dispatch(REQUIRED[name])
                dispatched.add(name)
        print(f"Waiting P17 matrix missing={missing} pending={pending}", flush=True)
        time.sleep(10)
    raise SystemExit(f"timed out waiting for P00-P17 affected workflows on {HEAD}")


def archive_artifact(artifact_id: int, expected_digest: str, destination: Path) -> Path:
    destination.parent.mkdir(parents=True, exist_ok=True)
    request = urllib.request.Request(
        f"https://api.github.com/repos/{REPO}/actions/artifacts/{artifact_id}/zip",
        headers=HEADERS,
    )
    with urllib.request.urlopen(request, timeout=60) as response, destination.open("wb") as handle:
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


def download_t034(rows: dict[str, dict]) -> dict:
    run_id = int(rows["P17 Evidence Coherence"]["run_id"])
    name = f"gojet-v10-p17-evidence-{HEAD}"
    artifact = artifact_for_run(run_id, name)
    archive = archive_artifact(int(artifact["id"]), artifact["digest"], Path("/tmp/p17-t034.zip"))
    temp = Path("/tmp/p17-t034")
    shutil.rmtree(temp, ignore_errors=True)
    temp.mkdir(parents=True)
    with zipfile.ZipFile(archive) as z:
        z.extractall(temp)
    result_path = next(iter(temp.rglob("P17-T034.json")), None)
    if result_path is None:
        raise SystemExit("T034 artifact missing P17-T034.json")
    t034 = json.loads(result_path.read_text(encoding="utf-8"))
    if t034.get("node") != "P17" or t034.get("case") != "P17-T034" or t034.get("status") != "PASS" or t034.get("implementation_commit") != HEAD:
        raise SystemExit(f"downloaded T034 is not exact-head PASS: {t034}")
    observations = t034.get("observations") or {}
    if not (observations.get("input_evidence_count") == 33 and observations.get("same_exact_head") is True and observations.get("secret_safe") is True and observations.get("mixed_head_rejected") is True and observations.get("unsafe_evidence_rejected") is True):
        raise SystemExit(f"T034 coherence observations invalid: {observations}")
    P17.joinpath("results").mkdir(parents=True, exist_ok=True)
    shutil.copy2(result_path, P17 / "results" / "P17-T034.json")
    for filename in ("evidence-index.json", "evidence-producer-manifest.json"):
        source = next(iter(temp.rglob(filename)), None)
        if source is None:
            raise SystemExit(f"T034 artifact missing {filename}")
        shutil.copy2(source, P17 / filename)
    return {
        "run_id": run_id,
        "artifact_id": int(artifact["id"]),
        "artifact_name": artifact["name"],
        "artifact_digest": artifact["digest"],
        "artifact_size_in_bytes": int(artifact["size_in_bytes"]),
    }


def bind_p16() -> dict:
    run = api(f"https://api.github.com/repos/{REPO}/actions/runs/{P16_RUN}")
    artifact = api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{P16_ARTIFACT}")
    if not (
        run.get("head_sha") == P16_SOURCE
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and int(artifact.get("id", 0)) == P16_ARTIFACT
        and artifact.get("digest") == P16_DIGEST
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == P16_RUN
        and artifact.get("workflow_run", {}).get("head_sha") == P16_SOURCE
    ):
        raise SystemExit("P16 inherited signed authority live metadata mismatch")
    archive = archive_artifact(P16_ARTIFACT, P16_DIGEST, Path("/tmp/p16-signed-authority.zip"))
    temp = Path("/tmp/p16-signed-authority")
    shutil.rmtree(temp, ignore_errors=True)
    temp.mkdir(parents=True)
    with zipfile.ZipFile(archive) as z:
        z.extractall(temp)
    closure_path = next(iter(temp.rglob("closure.json")), None)
    if closure_path is None:
        raise SystemExit("P16 signed authority missing closure.json")
    closure = json.loads(closure_path.read_text(encoding="utf-8"))
    if closure.get("node") != "P16" or closure.get("status") != "PASS" or closure.get("phase") != "signed" or closure.get("merge_authoritative") is not True:
        raise SystemExit("P16 signed authority content invalid")
    inherited = P17 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    metadata = {
        "node": "P16",
        "signed_source_commit": P16_SOURCE,
        "integration_commit": P16_INTEGRATION,
        "closure_run_id": P16_RUN,
        "artifact_id": P16_ARTIFACT,
        "artifact_digest": P16_DIGEST,
        "workflow_status": run.get("status"),
        "workflow_conclusion": run.get("conclusion"),
        "phase": closure.get("phase"),
        "merge_authoritative": closure.get("merge_authoritative"),
    }
    (inherited / "p16-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def primary_review(text: str) -> str:
    lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", text, flags=re.MULTILINE)
    if len(lines) != 1 or lines[0] not in (PENDING, SIGNED):
        raise SystemExit(f"invalid P17 primary review status lines: {lines}")
    return lines[0]


def grab(text: str, pattern: str, label: str) -> str:
    match = re.search(pattern, text)
    if not match:
        raise SystemExit(f"signed P17 review missing {label}")
    return match.group(1)


def bind_presign_if_signed(review: str) -> dict | None:
    if primary_review(review) != SIGNED:
        return None
    source = grab(review, r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`", "reviewed pre-sign SHA")
    run_id = int(grab(review, r"Pre-sign T035 closure run: `([0-9]+)`", "pre-sign closure run"))
    artifact_id = int(grab(review, r"Pre-sign T035 closure artifact: `([0-9]+)`", "pre-sign closure artifact"))
    digest = grab(review, r"Pre-sign T035 closure digest: `(sha256:[0-9a-f]{64})`", "pre-sign closure digest")
    if "Evidence disposition: `P17-T001..P17-T035 PASS`" not in review or "P0/P1/DECISION REQUIRED: `0/0/0`" not in review:
        raise SystemExit("signed P17 review missing exact case/defect disposition")
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
        raise SystemExit("P17 pre-sign closure live authority metadata mismatch")
    archive = archive_artifact(artifact_id, digest, Path("/tmp/p17-presign.zip"))
    temp = Path("/tmp/p17-presign")
    shutil.rmtree(temp, ignore_errors=True)
    temp.mkdir(parents=True)
    with zipfile.ZipFile(archive) as z:
        z.extractall(temp)
    closure_path = next(iter(temp.rglob("closure.json")), None)
    if closure_path is None:
        raise SystemExit("pre-sign P17 artifact missing closure.json")
    closure = json.loads(closure_path.read_text(encoding="utf-8"))
    if closure.get("node") != "P17" or closure.get("implementation_commit") != source or closure.get("status") != "PASS" or closure.get("phase") != "pre-sign" or closure.get("merge_authoritative") is not False:
        raise SystemExit("P17 pre-sign closure artifact content invalid")
    parent = subprocess.check_output(["git", "rev-parse", "HEAD^"], cwd=ROOT, text=True).strip()
    if parent != source:
        raise SystemExit(f"signed P17 revision must be a direct review-only child of pre-sign source: parent={parent} source={source}")
    changed = {line for line in subprocess.check_output(["git", "diff", "--name-only", f"{source}..{HEAD}"], cwd=ROOT, text=True).splitlines() if line}
    if changed != {"artifacts/v10/P17/review.md"}:
        raise SystemExit(f"signed P17 child is not review-only: {sorted(changed)}")
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
    inherited = P17 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    (inherited / "pre-sign-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def main() -> int:
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if actual != HEAD:
        raise SystemExit(f"checkout exact-head mismatch: {actual} != {HEAD}")
    if len(REQUIRED) != 66:
        raise SystemExit(f"affected matrix contract expected 66 workflows, got {len(REQUIRED)}")

    contract_raw = subprocess.check_output(["python3", "scripts/p17/validate_contract.py"], cwd=ROOT, text=True)
    contract = json.loads(contract_raw)
    if contract.get("status") != "PASS" or contract.get("errors") != [] or contract.get("implementation_commit") != HEAD or contract.get("contract_authority") != CONTRACT_AUTHORITY:
        raise SystemExit(f"P17 contract guard invalid at closure: {contract}")
    review = (P17 / "review.md").read_text(encoding="utf-8")
    status = primary_review(review)
    review_phase = "signed" if status == SIGNED else "pending"
    if contract.get("review_phase") != review_phase:
        raise SystemExit(f"review/contract phase mismatch {review_phase} != {contract.get('review_phase')}")

    rows = wait_matrix()
    t034_authority = download_t034(rows)
    p16_authority = bind_p16()
    presign_authority = bind_presign_if_signed(review)

    phase = "signed" if review_phase == "signed" else "pre-sign"
    merge_authoritative = phase == "signed"
    defects = {"p0": 0, "p1": 0, "decision_required": 0}
    result = {
        "node": "P17",
        "case": "P17-T035",
        "status": "PASS",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": HEAD,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P17-T001..P17-T035",
        "phase": phase,
        "review_phase": review_phase,
        "review_only_signed_child": phase == "signed",
        "merge_authoritative": merge_authoritative,
        "defects": defects,
        "evidence": {
            "t001_t033": "PASS on one exact head through T034 producer coherence",
            "t034": "PASS",
            "t035": "PASS",
            "input_case_count": 34,
            "final_case_count": 35,
        },
        "affected_matrix": {
            "passed": len(rows),
            "required": len(REQUIRED),
            "complete": len(rows) == len(REQUIRED),
        },
        "t034_authority": t034_authority,
        "p16_predecessor_authority": p16_authority,
        "pre_sign_authority": presign_authority,
        "excluded_revision_specific_workflows": EXCLUDED,
        "errors": [],
    }
    if phase == "pre-sign":
        result["signing_required"] = True
        result["signing_instruction"] = "Review-only child must sign artifacts/v10/P17/review.md and rerun the complete exact-head matrix."
    else:
        result["signing_required"] = False
        if presign_authority is None:
            raise SystemExit("signed phase missing live-bound pre-sign authority")

    P17.joinpath("results").mkdir(parents=True, exist_ok=True)
    (P17 / "results" / "P17-T035.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (P17 / "closure.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
