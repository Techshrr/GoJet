#!/usr/bin/env python3
"""CI orchestration for P15-T029 closure on the exact candidate head."""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
P15 = ROOT / "artifacts" / "v10" / "P15"
HEAD = os.environ["EXACT_HEAD"]
HEAD_REF = os.environ["HEAD_REF"]
REPOSITORY = os.environ["REPOSITORY"]
TOKEN = os.environ["GH_TOKEN"]
CURRENT_RUN = int(os.environ["GITHUB_RUN_ID"])
LOCAL_CONTRACT_RESULT = os.environ.get("LOCAL_CONTRACT_RESULT", "")
if LOCAL_CONTRACT_RESULT != "success":
    raise SystemExit(f"local P15 contract/T028 prerequisite is not success: {LOCAL_CONTRACT_RESULT}")

EXTERNAL = {
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
}
LOCAL = "P15 Authentication OAuth Account Contract"
EXCLUDED = {
    "P05 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P06 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P07 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P08 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P09 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P10 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P11 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P12 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P13 Closure": "revision-specific predecessor closure is inherited through P14 signed authority and is not reinterpreted on a P15 HEAD",
    "P14 Support Tickets and Mail Contract": "revision-specific immediate predecessor signed closure is inherited from P14 signed source f079c938dbe49d0f55b8b09995e72201cd0aab6e; P14 functional workflows are rerun separately on the P15 HEAD",
}

P14_SOURCE = "f079c938dbe49d0f55b8b09995e72201cd0aab6e"
P14_RUN = 32763705854
P14_ART = 9533837642
P14_DIG = "sha256:3f334718539e8fdd9cf5896fffdca9c00b8d0fc9a57b03d39795e97e6af853a8"
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": f"Bearer {TOKEN}",
    "X-GitHub-Api-Version": "2022-11-28",
}


def request_json(url: str, *, method: str = "GET", body: dict | None = None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(url, data=data, method=method, headers=HEADERS)
    with urllib.request.urlopen(request, timeout=30) as response:
        raw = response.read()
        return json.loads(raw) if raw else None


def fetch_runs() -> list[dict]:
    query = urllib.parse.urlencode({"head_sha": HEAD, "per_page": 100})
    return request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/runs?{query}").get("workflow_runs", [])


def dispatch(workflow_file: str) -> None:
    encoded = urllib.parse.quote(workflow_file, safe="")
    request_json(
        f"https://api.github.com/repos/{REPOSITORY}/actions/workflows/{encoded}/dispatches",
        method="POST",
        body={"ref": HEAD_REF},
    )


def primary_review_status(text: str) -> str:
    lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", text, flags=re.MULTILINE)
    if len(lines) != 1:
        raise SystemExit(f"P15 review must contain exactly one primary Status line, got {len(lines)}")
    status = lines[0]
    if status not in (PENDING, SIGNED):
        raise SystemExit(f"unsupported P15 primary review status: {status}")
    return status


def validate_review_phase_parser() -> None:
    sample = PENDING + "\n\n`" + SIGNED + "`\n"
    if primary_review_status(sample) != PENDING:
        raise SystemExit("P15 review phase parser accepted a quoted future signed marker as authority")


def wait_matrix() -> None:
    P15.mkdir(parents=True, exist_ok=True)
    manifest_path = P15 / "regression-manifest.json"
    dispatched: set[str] = set()
    deadline = time.time() + 75 * 60
    while time.time() < deadline:
        runs = fetch_runs()
        latest: dict[str, dict] = {}
        for run in runs:
            name = run.get("name")
            if name not in EXTERNAL:
                continue
            previous = latest.get(name)
            if previous is None or int(run.get("id", 0)) > int(previous.get("id", 0)):
                latest[name] = run

        missing = [name for name in EXTERNAL if name not in latest]
        pending = [name for name in EXTERNAL if name in latest and latest[name].get("status") != "completed"]
        failed = [
            name for name in EXTERNAL
            if name in latest and latest[name].get("status") == "completed" and latest[name].get("conclusion") != "success"
        ]
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
            for name in EXTERNAL if name in latest
        }
        rows[LOCAL] = {
            "run_id": CURRENT_RUN,
            "run_number": int(os.environ.get("GITHUB_RUN_NUMBER", "0")),
            "head_sha": HEAD,
            "event": os.environ.get("GITHUB_EVENT_NAME", "pull_request"),
            "status": "completed",
            "conclusion": "success",
            "source": "current-run-contract-job",
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
        manifest_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        if failed:
            raise SystemExit("required P15 exact-head workflows failed: " + ", ".join(failed))
        if not missing and not pending:
            print(f"P00-P15 affected exact-head matrix green for {HEAD}")
            return

        for name in missing:
            if name in dispatched:
                continue
            print(f"Dispatching missing exact-head workflow {name} via {EXTERNAL[name]} at {HEAD_REF}")
            dispatch(EXTERNAL[name])
            dispatched.add(name)

        print(f"Waiting P15 matrix missing={missing} pending={pending}", flush=True)
        time.sleep(10)
    raise SystemExit(f"timed out waiting for P00-P15 affected workflows on {HEAD}")


def run_gh(*args: str, stdout=None) -> subprocess.CompletedProcess:
    env = os.environ.copy()
    env["GH_TOKEN"] = TOKEN
    return subprocess.run(["gh", *args], cwd=ROOT, env=env, check=True, text=False, stdout=stdout)


def current_t028_artifact() -> dict:
    data = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/runs/{CURRENT_RUN}/artifacts?per_page=100")
    wanted = f"p15-t028-evidence-coherence-{HEAD}"
    matches = [item for item in data.get("artifacts", []) if item.get("name") == wanted and not item.get("expired")]
    if len(matches) != 1:
        raise SystemExit(f"expected one current-run T028 artifact {wanted}, got {len(matches)}")
    item = matches[0]
    if not isinstance(item.get("digest"), str) or not item["digest"].startswith("sha256:"):
        raise SystemExit("current-run T028 artifact digest missing")
    return item


def find_p15_root(root: Path) -> Path:
    matches = list(root.rglob("P15-T028.json"))
    for match in matches:
        if match.parent.name == "results" and (match.parent.parent / "evidence-index.json").is_file():
            return match.parent.parent
    raise SystemExit(f"cannot locate P15 T028 evidence root under {root}")


def download_t028() -> dict:
    artifact = current_t028_artifact()
    tmp = Path("/tmp/p15-current-evidence")
    shutil.rmtree(tmp, ignore_errors=True)
    tmp.mkdir(parents=True)
    run_gh(
        "run", "download", str(CURRENT_RUN),
        "--repo", REPOSITORY,
        "--name", artifact["name"],
        "--dir", str(tmp),
    )
    source_root = find_p15_root(tmp)
    for dirname in ("contract-guard", "api", "security", "oauth", "mail", "audit", "browser", "captures", "runtime", "results"):
        source = source_root / dirname
        if source.exists():
            target = P15 / dirname
            target.mkdir(parents=True, exist_ok=True)
            shutil.copytree(source, target, dirs_exist_ok=True)
    for name in ("evidence-index.json", "evidence-producer-manifest.json"):
        source = source_root / name
        if not source.is_file():
            raise SystemExit(f"T028 artifact missing {name}")
        shutil.copy2(source, P15 / name)
    t028 = json.loads((P15 / "results" / "P15-T028.json").read_text(encoding="utf-8"))
    if t028.get("implementation_commit") != HEAD or t028.get("status") != "PASS":
        raise SystemExit("downloaded current-run T028 evidence is not exact-head PASS")
    return artifact


def bind_p14() -> None:
    run = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/runs/{P14_RUN}")
    artifact = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/artifacts/{P14_ART}")
    if not (
        run.get("head_sha") == P14_SOURCE
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and int(artifact.get("id", 0)) == P14_ART
        and artifact.get("digest") == P14_DIG
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == P14_RUN
        and artifact.get("workflow_run", {}).get("head_sha") == P14_SOURCE
    ):
        raise SystemExit("P14 inherited authority live metadata mismatch")

    inherited = P15 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    zip_path = Path("/tmp/p14-authority.zip")
    with zip_path.open("wb") as handle:
        run_gh(
            "api",
            "-H", "Accept: application/vnd.github+json",
            "-H", "X-GitHub-Api-Version: 2022-11-28",
            f"/repos/{REPOSITORY}/actions/artifacts/{P14_ART}/zip",
            stdout=handle,
        )
    archive_sha = subprocess.check_output(["sha256sum", str(zip_path)], text=True).split()[0]
    if archive_sha != P14_DIG.removeprefix("sha256:"):
        raise SystemExit("P14 signed artifact archive digest mismatch")

    tmp = Path("/tmp/p14-authority")
    shutil.rmtree(tmp, ignore_errors=True)
    tmp.mkdir(parents=True)
    subprocess.run(["unzip", "-q", str(zip_path), "-d", str(tmp)], check=True)
    candidates = [p.parent for p in tmp.rglob("closure.json") if (p.parent / "results" / "P14-T025.json").is_file() and (p.parent / "review.md").is_file()]
    if len(candidates) != 1:
        raise SystemExit(f"cannot uniquely locate P14 signed authority root: {len(candidates)}")
    target = inherited / "P14"
    shutil.rmtree(target, ignore_errors=True)
    shutil.copytree(candidates[0], target)

    metadata = {
        "source_commit": P14_SOURCE,
        "closure_run_id": P14_RUN,
        "workflow_status": run.get("status"),
        "workflow_conclusion": run.get("conclusion"),
        "workflow_head_sha": run.get("head_sha"),
        "artifact_id": P14_ART,
        "artifact_name": artifact.get("name"),
        "artifact_digest": artifact.get("digest"),
        "artifact_expired": artifact.get("expired"),
        "archive_sha256": archive_sha,
    }
    (inherited / "p14-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def bind_presign_if_signed() -> None:
    review = (P15 / "review.md").read_text(encoding="utf-8")
    path = P15 / "inherited" / "pre-sign-authority.json"
    if primary_review_status(review) != SIGNED:
        path.unlink(missing_ok=True)
        return

    def grab(pattern: str, label: str) -> str:
        match = re.search(pattern, review)
        if not match:
            raise SystemExit(f"signed review missing {label}")
        return match.group(1)

    source = grab(r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`", "pre-sign SHA")
    run_id = int(grab(r"Pre-sign T029 closure run: `([0-9]+)`", "pre-sign run"))
    artifact_id = int(grab(r"Pre-sign T029 closure artifact: `([0-9]+)`", "pre-sign artifact"))
    expected_digest = grab(r"Pre-sign T029 closure digest: `(sha256:[0-9a-f]{64})`", "pre-sign digest")
    run = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/runs/{run_id}")
    artifact = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/artifacts/{artifact_id}")
    if not (
        run.get("head_sha") == source
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and int(artifact.get("id", 0)) == artifact_id
        and artifact.get("digest") == expected_digest
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == run_id
        and artifact.get("workflow_run", {}).get("head_sha") == source
    ):
        raise SystemExit("pre-sign closure live authority metadata mismatch")
    metadata = {
        "source_commit": source,
        "closure_run_id": run_id,
        "workflow_status": run.get("status"),
        "workflow_conclusion": run.get("conclusion"),
        "workflow_head_sha": run.get("head_sha"),
        "artifact_id": artifact_id,
        "artifact_name": artifact.get("name"),
        "artifact_digest": artifact.get("digest"),
        "artifact_expired": artifact.get("expired"),
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def validate_t029() -> None:
    env = {**os.environ, "EXACT_HEAD": HEAD}
    subprocess.run(["python3", "scripts/p15/validate_closure.py"], cwd=ROOT, env=env, check=True)
    result = json.loads((P15 / "results" / "P15-T029.json").read_text(encoding="utf-8"))
    if result.get("case_id") != "P15-T029" or result.get("status") != "PASS":
        raise SystemExit(f"P15-T029 did not PASS: {result}")
    if result.get("implementation_commit") != HEAD or result.get("defects") != {"p0": 0, "p1": 0, "decision_required": 0}:
        raise SystemExit(f"P15-T029 exact-head/defect mismatch: {result}")
    phase = result.get("phase")
    if phase == "pre-sign" and result.get("merge_authoritative") is not False:
        raise SystemExit("pre-sign P15-T029 must not be merge authoritative")
    if phase == "signed" and result.get("merge_authoritative") is not True:
        raise SystemExit("signed P15-T029 must be merge authoritative")
    if phase not in ("pre-sign", "signed"):
        raise SystemExit(f"invalid P15-T029 phase {phase}")
    print(f"P15-T029 {phase} closure PASS on {HEAD}")


def main() -> int:
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if actual != HEAD:
        raise SystemExit(f"checkout exact-head mismatch: {actual} != {HEAD}")
    validate_review_phase_parser()
    download_t028()
    wait_matrix()
    bind_p14()
    bind_presign_if_signed()
    validate_t029()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())