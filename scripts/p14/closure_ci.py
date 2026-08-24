#!/usr/bin/env python3
"""CI orchestration for P14-T025 closure on the exact candidate head."""
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

ROOT = Path(__file__).resolve().parents[2]
P14 = ROOT / "artifacts" / "v10" / "P14"
HEAD = os.environ["EXACT_HEAD"]
HEAD_REF = os.environ["HEAD_REF"]
REPOSITORY = os.environ["REPOSITORY"]
TOKEN = os.environ["GH_TOKEN"]
CURRENT_RUN = int(os.environ["GITHUB_RUN_ID"])
LOCAL_CONTRACT_RESULT = os.environ.get("LOCAL_CONTRACT_RESULT", "")
if LOCAL_CONTRACT_RESULT != "success":
    raise SystemExit(f"local P14 contract/T024 prerequisite is not success: {LOCAL_CONTRACT_RESULT}")

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
}
LOCAL = "P14 Support Tickets and Mail Contract"
EXCLUDED = {
    "P05 Closure": "revision-specific predecessor closure is inherited through P13 signed authority and is not reinterpreted on a P14 HEAD",
    "P06 Closure": "revision-specific predecessor closure is inherited through P13 signed authority; P06 functional authority is separately live-bound from signed source 4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23",
    "P07 Closure": "revision-specific predecessor closure is inherited through P13 signed authority and is not reinterpreted on a P14 HEAD",
    "P08 Closure": "revision-specific predecessor closure is inherited through P13 signed authority and is not reinterpreted on a P14 HEAD",
    "P09 Closure": "revision-specific predecessor closure is inherited through P13 signed authority; P09 ClamAV authority is separately live-bound from signed source eafa369a9c150c22c2c14c9f21848a9544f4f96a",
    "P10 Closure": "revision-specific predecessor closure is inherited through P13 signed authority and is not reinterpreted on a P14 HEAD",
    "P11 Closure": "revision-specific predecessor closure is inherited through P13 signed authority and is not reinterpreted on a P14 HEAD",
    "P12 Closure": "revision-specific predecessor closure is inherited through P13 signed authority; P12 Workspace/notification authority is separately live-bound from signed source 9d49d5ebf0e697ae9cd6537c432c27a15edc60bd",
    "P13 Closure": "revision-specific immediate predecessor closure is inherited from signed source 24cdbdf848bf722e53e38ed15dce12e1d42eb9d2 and is not reinterpreted on a P14 HEAD",
}

AUTHORITIES = {
    "P13": {
        "source": "24cdbdf848bf722e53e38ed15dce12e1d42eb9d2",
        "run": 32711262325,
        "artifact": 9514396804,
        "digest": "sha256:494a7942272afac7588eab153c07daf5a1f557c10b58b0dbd915eeda8709e998",
        "download": True,
    },
    "P12": {
        "source": "9d49d5ebf0e697ae9cd6537c432c27a15edc60bd",
        "run": 32663159008,
        "artifact": 9499336765,
        "digest": "sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a",
        "download": False,
    },
    "P06": {
        "source": "4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23",
        "run": 32519298309,
        "artifact": 9460016077,
        "digest": "sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8",
        "download": False,
    },
    "P09": {
        "source": "eafa369a9c150c22c2c14c9f21848a9544f4f96a",
        "run": 32618657967,
        "artifact": 9487743843,
        "digest": "sha256:f12aeeb5503bf375314f1d13a2d9833180d6617322765cef2aae0d728cc278d7",
        "download": False,
    },
}

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


def wait_matrix() -> None:
    P14.mkdir(parents=True, exist_ok=True)
    manifest_path = P14 / "regression-manifest.json"
    dispatched: set[str] = set()
    deadline = time.time() + 70 * 60
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
            raise SystemExit("required P14 exact-head workflows failed: " + ", ".join(failed))
        if not missing and not pending:
            print(f"P00-P14 affected exact-head matrix green for {HEAD}")
            return

        for name in missing:
            if name in dispatched:
                continue
            print(f"Dispatching missing exact-head workflow {name} via {EXTERNAL[name]} at {HEAD_REF}")
            dispatch(EXTERNAL[name])
            dispatched.add(name)

        print(f"Waiting P14 matrix missing={missing} pending={pending}")
        time.sleep(10)
    raise SystemExit(f"timed out waiting for P00-P14 affected workflows on {HEAD}")


def run_gh(*args: str, stdout=None) -> subprocess.CompletedProcess:
    env = os.environ.copy()
    env["GH_TOKEN"] = TOKEN
    return subprocess.run(["gh", *args], cwd=ROOT, env=env, check=True, text=False, stdout=stdout)


def find_one(root: Path, name: str) -> Path:
    matches = list(root.rglob(name))
    if not matches:
        raise SystemExit(f"missing {name} under {root}")
    return matches[0]


def download_t024() -> None:
    tmp = Path("/tmp/p14-current-evidence")
    shutil.rmtree(tmp, ignore_errors=True)
    tmp.mkdir(parents=True)
    run_gh(
        "run", "download", str(CURRENT_RUN),
        "--repo", REPOSITORY,
        "--name", f"gojet-v10-p14-evidence-{HEAD}",
        "--dir", str(tmp),
    )
    for dirname in ("contract", "api", "rbac", "security", "entitlement", "mail", "notification", "audit", "browser", "captures", "runtime", "results"):
        source = tmp / "artifacts" / "v10" / "P14" / dirname
        if not source.exists():
            source = tmp / dirname
        if source.exists():
            target = P14 / dirname
            target.mkdir(parents=True, exist_ok=True)
            shutil.copytree(source, target, dirs_exist_ok=True)
    for name in ("evidence-index.json", "evidence-producer-manifest.json"):
        source = find_one(tmp, name)
        shutil.copy2(source, P14 / name)
    if not (P14 / "results" / "P14-T024.json").is_file():
        raise SystemExit("current-run T024 evidence missing after artifact download")


def bind_authorities() -> None:
    inherited = P14 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    for label, cfg in AUTHORITIES.items():
        run = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/runs/{cfg['run']}")
        artifact = request_json(f"https://api.github.com/repos/{REPOSITORY}/actions/artifacts/{cfg['artifact']}")
        if not (
            run.get("head_sha") == cfg["source"]
            and run.get("status") == "completed"
            and run.get("conclusion") == "success"
            and int(artifact.get("id", 0)) == cfg["artifact"]
            and artifact.get("digest") == cfg["digest"]
            and artifact.get("expired") is False
            and int(artifact.get("workflow_run", {}).get("id", 0)) == cfg["run"]
            and artifact.get("workflow_run", {}).get("head_sha") == cfg["source"]
        ):
            raise SystemExit(f"{label} inherited authority live metadata mismatch")
        data = {
            "source_commit": cfg["source"],
            "closure_run_id": cfg["run"],
            "workflow_status": run.get("status"),
            "workflow_conclusion": run.get("conclusion"),
            "workflow_head_sha": run.get("head_sha"),
            "artifact_id": cfg["artifact"],
            "artifact_name": artifact.get("name"),
            "artifact_digest": artifact.get("digest"),
            "artifact_expired": artifact.get("expired"),
            "archive_sha256": None,
        }
        (inherited / f"{label.lower()}-authority.json").write_text(
            json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )

        if cfg["download"]:
            zip_path = Path(f"/tmp/{label.lower()}-authority.zip")
            with zip_path.open("wb") as handle:
                run_gh(
                    "api",
                    "-H", "Accept: application/vnd.github+json",
                    "-H", "X-GitHub-Api-Version: 2022-11-28",
                    f"/repos/{REPOSITORY}/actions/artifacts/{cfg['artifact']}/zip",
                    stdout=handle,
                )
            archive_sha = subprocess.check_output(["sha256sum", str(zip_path)], text=True).split()[0]
            if archive_sha != cfg["digest"].removeprefix("sha256:"):
                raise SystemExit(f"{label} signed artifact archive digest mismatch")
            data["archive_sha256"] = archive_sha
            (inherited / f"{label.lower()}-authority.json").write_text(
                json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8"
            )
            target = inherited / label
            shutil.rmtree(target, ignore_errors=True)
            target.mkdir(parents=True)
            subprocess.run(["unzip", "-q", str(zip_path), "-d", str(target)], check=True)
            for name in ("closure.json", "review.md", "closure-evidence-index.json"):
                if not list(target.rglob(name)):
                    raise SystemExit(f"{label} signed artifact missing {name}")
            if label == "P13" and not list(target.rglob("P13-T027.json")):
                raise SystemExit("P13 signed artifact missing P13-T027.json")


def normalize_p13_layout() -> None:
    root = P14 / "inherited" / "P13"
    mappings = {
        "closure.json": root / "closure.json",
        "review.md": root / "review.md",
        "closure-evidence-index.json": root / "closure-evidence-index.json",
        "P13-T027.json": root / "results" / "P13-T027.json",
    }
    for name, target in mappings.items():
        if target.is_file():
            continue
        candidates = sorted(root.rglob(name))
        primary = [
            path for path in candidates
            if "/inherited/" not in ("/" + path.relative_to(root).as_posix())
        ]
        source = primary[0] if primary else (candidates[0] if candidates else None)
        if source is None:
            raise SystemExit(f"P13 signed artifact missing {name}")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)


def main() -> int:
    if subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip() != HEAD:
        raise SystemExit("closure exact-head checkout mismatch")
    wait_matrix()
    download_t024()
    bind_authorities()
    normalize_p13_layout()
    completed = subprocess.run(
        ["python3", "scripts/p14/validate.py", "--case", "P14-T025", "--closure"],
        cwd=ROOT,
        check=False,
    )
    return completed.returncode


if __name__ == "__main__":
    raise SystemExit(main())
