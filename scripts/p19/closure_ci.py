#!/usr/bin/env python3
"""P19-T032 accountable exact-head closure orchestration."""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import time
import urllib.parse
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[2]
P19 = ROOT / "artifacts" / "v10" / "P19"
GATES_ROOT = ROOT / "artifacts" / "v10" / "gates"
HEAD = os.environ["EXACT_HEAD"].strip()
HEAD_REF = os.environ["HEAD_REF"].strip()
REPO = os.environ["REPOSITORY"].strip()
TOKEN = os.environ["GH_TOKEN"].strip()

CONTRACT_AUTHORITY = "d1e6f2a4af2006ccd44bf0d363144845efd535e0"
P18_SOURCE = "e8746159b02c729a877e3dcbd9655d415a5cc269"
P18_INTEGRATION = "43e693b10c0118e32d7f14c61156e0b06c155111"
P18_RUN = 33260817755
P18_ARTIFACT = 9717210947
P18_DIGEST = "sha256:3e403765409b3ab273be1c35a9d88b565505c416a47364d9a6f0339cc130efe4"

PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

REQUIRED = {
    "P00 Bootstrap and G0 Traceability": "p00-bootstrap.yml",
    "P01 Engineering Foundation": "p01-engineering.yml",
    "P02 Brand Foundation": "p02-brand-foundation.yml",
    "P03 Design System": "p03-design-system.yml",
    "P04 Product Shells": "p04-product-shells.yml",
    "P06 Custom Domains": "p06-custom-domains.yml",
    "P09 Files Contract": "p09-files.yml",
    "P13 Billing Payments Entitlements Contract": "p13-billing-payments-entitlements.yml",
    "P14 Workspace Support Browser": "p14-browser.yml",
    "P14 Admin Tickets Mail Contact Browser": "p14-browser-023.yml",
    "P15 Auth Route Browser Authority": "p15-browser.yml",
    "P16 Trust Browser Authority": "p16-browser.yml",
    "P19 Website and Technical SEO Contract": "p19-website-technical-seo.yml",
    "P19 Website Core Integration": "p19-website-core.yml",
    "P19 Website Content Authority": "p19-website-content.yml",
    "P19 Website Crawl and Discovery": "p19-website-discovery.yml",
    "P19 Website Browser and Visual": "p19-website-browser.yml",
    "P19 Website Performance and Runtime": "p19-website-quality.yml",
    "P19 Evidence Coherence": "p19-website-evidence.yml",
}

EXCLUDED_REVISION_SPECIFIC = {
    "P16 Evidence Coherence": "P16 evidence is revision-specific; P19 live-binds the integrated P16 capability authority and reruns the applicable Trust browser surface instead of re-signing P16.",
    "P16 Closure": "P16 closure is revision-specific predecessor governance and is not P19 merge authority.",
    "P18 Closure": f"Immediate predecessor signed authority is live-bound from {P18_SOURCE}/{P18_RUN}/{P18_ARTIFACT}; P19 does not reinterpret or rerun P18 closure as current-head authority.",
    "P05-P17 historical Closure/Evidence": "Historical node closure/evidence remains bound through signed integration ancestry and P19-T031 public capability authority; revision-specific historical closure is not counted as P19 exact-head regression.",
}

MATRIX_SCOPE = (
    "P19 exact-head regression covers P00-P04 foundations; P06 Domains, P09 Files, P13 Billing/Pricing; "
    "P14 Support/Contact, P15 Auth route and P16 Trust browser surfaces touched or represented by the Website; "
    "plus all seven P19 Contract/Core/Content/Discovery/Browser/Quality/Evidence authorities. "
    "Revision-specific predecessor closure/evidence workflows are excluded and inherited authority is live-bound."
)

GATE_SPECS = {
    "G4": {
        "slug": "browser-responsive",
        "cases": ["P19-T023", "P19-T024", "P19-T026"],
        "roles": ["Frontend Lead", "QA"],
        "name": "Browser and Responsive",
    },
    "G5": {
        "slug": "accessibility",
        "cases": ["P19-T025"],
        "roles": ["Accessibility Reviewer"],
        "name": "Accessibility",
    },
    "G7": {
        "slug": "seo",
        "cases": ["P19-T001", "P19-T002", "P19-T003", "P19-T004", "P19-T005", "P19-T006", "P19-T014", "P19-T017", "P19-T018", "P19-T020", "P19-T021", "P19-T022"],
        "roles": ["SEO Owner", "Frontend Lead"],
        "name": "SEO and Indexation",
    },
    "G8": {
        "slug": "visual",
        "cases": ["P19-T019", "P19-T026"],
        "roles": ["Design Lead"],
        "name": "Visual Quality",
    },
    "G9": {
        "slug": "performance",
        "cases": ["P19-T027", "P19-T028"],
        "roles": ["Performance Owner"],
        "name": "Performance",
    },
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


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def sha256(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def exact_runs() -> list[dict]:
    query = urllib.parse.urlencode({"head_sha": HEAD, "per_page": 100})
    return api(f"https://api.github.com/repos/{REPO}/actions/runs?{query}").get("workflow_runs", [])


def dispatch(workflow: str) -> None:
    quoted = urllib.parse.quote(workflow, safe="")
    api(
        f"https://api.github.com/repos/{REPO}/actions/workflows/{quoted}/dispatches",
        method="POST",
        body={"ref": HEAD_REF},
    )


def wait_matrix() -> dict[str, dict]:
    P19.mkdir(parents=True, exist_ok=True)
    path = P19 / "regression-manifest.json"
    dispatched: set[str] = set()
    deadline = time.time() + 220 * 60
    while time.time() < deadline:
        latest: dict[str, dict] = {}
        for run in exact_runs():
            name = run.get("name")
            if name in REQUIRED and (name not in latest or int(run.get("id", 0)) > int(latest[name].get("id", 0))):
                latest[name] = run
        missing = [name for name in REQUIRED if name not in latest]
        pending = [name for name in REQUIRED if name in latest and latest[name].get("status") != "completed"]
        failed = [
            name for name in REQUIRED
            if name in latest and latest[name].get("status") == "completed" and latest[name].get("conclusion") != "success"
        ]
        rows = {
            name: {
                "workflow_file": REQUIRED[name],
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
            raise SystemExit("required P19 exact-head workflows failed: " + ", ".join(failed))
        if not missing and not pending and len(rows) == len(REQUIRED):
            print(f"P19 applicable exact-head matrix green ({len(REQUIRED)}/{len(REQUIRED)}) for {HEAD}")
            return rows
        for name in missing:
            if name not in dispatched:
                print(f"Dispatching missing exact-head workflow {name} via {REQUIRED[name]} on {HEAD_REF}", flush=True)
                dispatch(REQUIRED[name])
                dispatched.add(name)
        print(f"Waiting P19 matrix missing={missing} pending={pending}", flush=True)
        time.sleep(10)
    raise SystemExit(f"timed out waiting for P19 applicable workflows on {HEAD}")


def artifact_for_run(run_id: int, name: str) -> dict:
    artifacts = api(f"https://api.github.com/repos/{REPO}/actions/runs/{run_id}/artifacts?per_page=100").get("artifacts", [])
    matches = [item for item in artifacts if item.get("name") == name and not item.get("expired")]
    if len(matches) != 1:
        raise SystemExit(f"expected one exact-head artifact {name} on run {run_id}, got {len(matches)}")
    artifact = matches[0]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", str(artifact.get("digest", ""))) or int(artifact.get("size_in_bytes", 0)) <= 0:
        raise SystemExit(f"invalid artifact metadata for {name}")
    return artifact


def download_archive(artifact_id: int, expected_digest: str, destination: Path) -> Path:
    destination.parent.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "GH_TOKEN": TOKEN}
    with destination.open("wb") as handle:
        subprocess.run(
            [
                "gh", "api",
                "-H", "Accept: application/vnd.github+json",
                "-H", "X-GitHub-Api-Version: 2022-11-28",
                f"/repos/{REPO}/actions/artifacts/{artifact_id}/zip",
            ],
            env=env,
            check=True,
            stdout=handle,
        )
    digest = sha256(destination)
    if digest != expected_digest:
        raise SystemExit(f"artifact archive digest mismatch {artifact_id}: {digest} != {expected_digest}")
    return destination


def safe_extract(archive: Path, destination: Path) -> None:
    shutil.rmtree(destination, ignore_errors=True)
    destination.mkdir(parents=True)
    names: set[str] = set()
    with zipfile.ZipFile(archive) as z:
        for info in z.infolist():
            pp = PurePosixPath(info.filename)
            if pp.is_absolute() or ".." in pp.parts:
                raise SystemExit(f"unsafe artifact path: {info.filename}")
            if info.filename in names:
                raise SystemExit(f"duplicate artifact member: {info.filename}")
            names.add(info.filename)
            mode = (info.external_attr >> 16) & 0xFFFF
            if stat.S_ISLNK(mode):
                raise SystemExit(f"symlink artifact member prohibited: {info.filename}")
            target = destination.joinpath(*pp.parts)
            if info.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            with z.open(info) as source, target.open("wb") as output:
                shutil.copyfileobj(source, output)


def unique_match(root: Path, name: str) -> Path:
    matches = list(root.rglob(name))
    if len(matches) != 1:
        raise SystemExit(f"expected exactly one {name}, got {len(matches)}")
    return matches[0]


def case_identity(data: dict) -> object:
    return data.get("case") if data.get("case") is not None else data.get("case_id")


def download_t031(rows: dict[str, dict]) -> tuple[dict, dict[str, dict], dict]:
    run_id = int(rows["P19 Evidence Coherence"]["run_id"])
    name = f"gojet-v10-p19-evidence-{HEAD}"
    artifact = artifact_for_run(run_id, name)
    archive = download_archive(int(artifact["id"]), artifact["digest"], Path("/tmp/p19-t031.zip"))
    temp = Path("/tmp/p19-t031")
    safe_extract(archive, temp)

    t031_path = unique_match(temp, "P19-T031.json")
    t031 = json.loads(t031_path.read_text(encoding="utf-8"))
    obs = t031.get("observations") or {}
    if not (
        t031.get("node") == "P19"
        and t031.get("case") == "P19-T031"
        and t031.get("status") == "PASS"
        and t031.get("errors") == []
        and t031.get("implementation_commit") == HEAD
        and t031.get("contract_authority") == CONTRACT_AUTHORITY
        and obs.get("input_evidence_count") == 30
        and obs.get("same_exact_head") is True
        and obs.get("secret_safe") is True
        and obs.get("producer_count") == 6
        and obs.get("producer_artifact_count") == 6
        and obs.get("p18_signed_predecessor_live_bound") is True
        and obs.get("public_capability_authority_count") == 14
        and obs.get("inherited_authorities_bound") is True
        and obs.get("mixed_head_rejected") is True
        and obs.get("stale_head_rejected") is True
        and obs.get("malformed_evidence_rejected") is True
        and obs.get("unsafe_evidence_rejected") is True
        and obs.get("merge_authoritative") is False
    ):
        raise SystemExit(f"T031 coherence authority invalid: {t031}")

    coherence = P19 / "coherence"
    cases_dir = coherence / "cases"
    cases_dir.mkdir(parents=True, exist_ok=True)
    shutil.copy2(t031_path, coherence / "P19-T031.json")
    for filename in ("evidence-index.json", "producer-manifest.json", "inherited-authorities.json"):
        shutil.copy2(unique_match(temp, filename), coherence / filename)

    cases: dict[str, dict] = {}
    for n in range(1, 31):
        cid = f"P19-T{n:03d}"
        source = unique_match(temp, f"{cid}.json")
        data = json.loads(source.read_text(encoding="utf-8"))
        if case_identity(data) != cid or data.get("status") != "PASS" or data.get("errors") != [] or data.get("implementation_commit") != HEAD:
            raise SystemExit(f"invalid exact-head case in T031 artifact {cid}: {data}")
        shutil.copy2(source, cases_dir / f"{cid}.json")
        cases[cid] = data

    metadata = {
        "run_id": run_id,
        "artifact_id": int(artifact["id"]),
        "artifact_name": artifact["name"],
        "artifact_digest": artifact["digest"],
        "artifact_size_in_bytes": int(artifact["size_in_bytes"]),
    }
    return metadata, cases, t031


def bind_p18() -> dict:
    run = api(f"https://api.github.com/repos/{REPO}/actions/runs/{P18_RUN}")
    artifact = api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{P18_ARTIFACT}")
    if not (
        run.get("head_sha") == P18_SOURCE
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
        and artifact.get("digest") == P18_DIGEST
        and artifact.get("expired") is False
        and int(artifact.get("workflow_run", {}).get("id", 0)) == P18_RUN
        and artifact.get("workflow_run", {}).get("head_sha") == P18_SOURCE
    ):
        raise SystemExit("P18 signed predecessor live metadata mismatch")
    archive = download_archive(P18_ARTIFACT, P18_DIGEST, Path("/tmp/p18-signed-authority-p19.zip"))
    temp = Path("/tmp/p18-signed-authority-p19")
    safe_extract(archive, temp)
    closure_path = unique_match(temp, "closure.json")
    closure = json.loads(closure_path.read_text(encoding="utf-8"))
    if not (
        closure.get("node") == "P18"
        and closure.get("status") == "PASS"
        and closure.get("phase") == "signed"
        and closure.get("review_phase") == "signed"
        and closure.get("review_only_signed_child") is True
        and closure.get("merge_authoritative") is True
    ):
        raise SystemExit("P18 signed predecessor authority content invalid")
    metadata = {
        "node": "P18",
        "signed_source_commit": P18_SOURCE,
        "integration_commit": P18_INTEGRATION,
        "closure_run_id": P18_RUN,
        "artifact_id": P18_ARTIFACT,
        "artifact_digest": P18_DIGEST,
        "phase": "signed",
        "review_phase": "signed",
        "review_only_signed_child": True,
        "merge_authoritative": True,
    }
    inherited = P19 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    (inherited / "p18-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def write_gate_evidence(cases: dict[str, dict], t031_authority: dict) -> dict[str, dict]:
    decisions: dict[str, dict] = {}
    test_plan_blob = git("rev-parse", "HEAD:artifacts/v10/P19/test-plan.json")
    for gate, spec in GATE_SPECS.items():
        directory = GATES_ROOT / gate / spec["slug"]
        directory.mkdir(parents=True, exist_ok=True)
        ids = spec["cases"]
        evidence = []
        errors = []
        for cid in ids:
            data = cases.get(cid)
            if not data:
                errors.append(f"missing {cid}")
                continue
            if data.get("status") != "PASS" or data.get("errors") != [] or data.get("implementation_commit") != HEAD:
                errors.append(f"{cid} is not exact-head PASS")
            path = P19 / "coherence" / "cases" / f"{cid}.json"
            evidence.append({
                "case": cid,
                "path": path.relative_to(ROOT).as_posix(),
                "sha256": sha256(path),
                "status": data.get("status"),
                "implementation_commit": data.get("implementation_commit"),
            })
        environment = {
            "gate": gate,
            "node": "P19",
            "runner": "GitHub Actions ubuntu-24.04",
            "evidence_mode": "exact-head producer aggregation via P19-T031",
            "production_site_runtime": "STATIC_NGINX_ONLY",
            "implementation_commit": HEAD,
        }
        source = {
            "gate": gate,
            "node": "P19",
            "implementation_commit": HEAD,
            "contract_authority": CONTRACT_AUTHORITY,
            "test_plan_blob": test_plan_blob,
            "t031_authority": t031_authority,
        }
        result = {
            "gate": gate,
            "name": spec["name"],
            "node_contribution": "P19",
            "status": "PASS" if not errors else "FAIL",
            "cases": ids,
            "errors": errors,
        }
        decision = {
            "gate": gate,
            "name": spec["name"],
            "node_contribution": "P19",
            "status": result["status"],
            "implementation_commit": HEAD,
            "accountable_roles": spec["roles"],
            "hard_failures": errors,
            "conditional_pass": False,
            "release_wide_finalization": "P20/P22 where contract assigns later release-wide verification",
        }
        (directory / "environment.json").write_text(json.dumps(environment, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (directory / "source.json").write_text(json.dumps(source, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (directory / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (directory / "evidence-index.json").write_text(json.dumps({"gate": gate, "evidence": evidence}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (directory / "decision.json").write_text(json.dumps(decision, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (directory / "commands.log").write_text(
            "python3 scripts/p19/closure_ci.py\n"
            f"Bound exact-head P19 evidence: {', '.join(ids)}\n"
            "Producer commands and raw machine evidence are digest-bound by P19-T031 producer-manifest/evidence-index.\n",
            encoding="utf-8",
        )
        if errors:
            raise SystemExit(f"{gate} P19 contribution failed: {errors}")
        decisions[gate] = decision
    return decisions


def primary_review(text: str) -> str:
    lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", text, flags=re.MULTILINE)
    if len(lines) != 1 or lines[0] not in (PENDING, SIGNED):
        raise SystemExit(f"invalid P19 primary review status lines: {lines}")
    return lines[0]


def grab(text: str, pattern: str, label: str) -> str:
    match = re.search(pattern, text)
    if not match:
        raise SystemExit(f"signed P19 review missing {label}")
    return match.group(1)


def bind_presign_if_signed(review: str) -> dict | None:
    if primary_review(review) != SIGNED:
        return None
    source = grab(review, r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`", "reviewed pre-sign SHA")
    run_id = int(grab(review, r"Pre-sign T032 closure run: `([0-9]+)`", "pre-sign closure run"))
    artifact_id = int(grab(review, r"Pre-sign T032 closure artifact: `([0-9]+)`", "pre-sign closure artifact"))
    digest = grab(review, r"Pre-sign T032 closure digest: `(sha256:[0-9a-f]{64})`", "pre-sign closure digest")
    if "Evidence disposition: `P19-T001..P19-T031 PASS`" not in review:
        raise SystemExit("signed P19 review missing exact evidence disposition")
    if "P0/P1/DECISION REQUIRED: `0/0/0`" not in review:
        raise SystemExit("signed P19 review missing zero defect/decision ledger")
    required_roles = ["SEO Owner", "Frontend Lead", "Design Lead", "Accessibility Reviewer", "Performance Owner", "QA Lead"]
    for role in required_roles:
        pattern = rf"### {re.escape(role)} — APPROVED[\s\S]*?Decision: \*\*APPROVED\*\*"
        if re.search(pattern, review) is None:
            raise SystemExit(f"signed P19 review missing APPROVED decision for {role}")

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
        raise SystemExit("P19 pre-sign closure live authority metadata mismatch")
    archive = download_archive(artifact_id, digest, Path("/tmp/p19-presign.zip"))
    temp = Path("/tmp/p19-presign")
    safe_extract(archive, temp)
    closure = json.loads(unique_match(temp, "closure.json").read_text(encoding="utf-8"))
    if not (
        closure.get("node") == "P19"
        and closure.get("implementation_commit") == source
        and closure.get("status") == "PASS"
        and closure.get("phase") == "pre-sign"
        and closure.get("review_phase") == "pending"
        and closure.get("merge_authoritative") is False
        and closure.get("defects") == {"p0": 0, "p1": 0, "decision_required": 0}
        and closure.get("gates", {}).get("passed") == 5
        and closure.get("applicable_matrix", {}).get("complete") is True
    ):
        raise SystemExit("P19 pre-sign closure artifact content invalid")

    parent = git("rev-parse", "HEAD^")
    if parent != source:
        raise SystemExit(f"signed P19 revision must be a direct review-only child: parent={parent} source={source}")
    changed = {line for line in git("diff", "--name-only", f"{source}..{HEAD}").splitlines() if line}
    if changed != {"artifacts/v10/P19/review.md"}:
        raise SystemExit(f"signed P19 child is not review-only: {sorted(changed)}")

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
    inherited = P19 / "inherited"
    inherited.mkdir(parents=True, exist_ok=True)
    (inherited / "pre-sign-authority.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return metadata


def main() -> int:
    actual = git("rev-parse", "HEAD")
    if actual != HEAD:
        raise SystemExit(f"checkout exact-head mismatch: {actual} != {HEAD}")
    if len(REQUIRED) != 19:
        raise SystemExit(f"P19 applicable matrix contract expected 19 workflows, got {len(REQUIRED)}")
    if set(GATE_SPECS) != {"G4", "G5", "G7", "G8", "G9"}:
        raise SystemExit("P19 gate set drift")

    contract = json.loads(subprocess.check_output(["python3", "scripts/p19/validate_contract.py"], cwd=ROOT, text=True))
    if not (
        contract.get("status") == "PASS"
        and contract.get("errors") == []
        and contract.get("implementation_commit") == HEAD
        and contract.get("contract_authority") == CONTRACT_AUTHORITY
        and contract.get("case_range") == "P19-T001..P19-T032"
        and contract.get("frozen_contract_preserved") is True
        and contract.get("merge_authoritative") is False
    ):
        raise SystemExit(f"P19 contract guard invalid at closure: {contract}")

    review = (P19 / "review.md").read_text(encoding="utf-8")
    review_status = primary_review(review)
    review_phase = "signed" if review_status == SIGNED else "pending"
    if contract.get("review_phase") != review_phase:
        raise SystemExit(f"review/contract phase mismatch {review_phase} != {contract.get('review_phase')}")

    rows = wait_matrix()
    t031_authority, cases, t031 = download_t031(rows)
    p18_authority = bind_p18()
    gate_decisions = write_gate_evidence(cases, t031_authority)
    presign_authority = bind_presign_if_signed(review)

    phase = "signed" if review_phase == "signed" else "pre-sign"
    defects = {"p0": 0, "p1": 0, "decision_required": 0}
    result = {
        "node": "P19",
        "case": "P19-T032",
        "status": "PASS",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "implementation_commit": HEAD,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P19-T001..P19-T032",
        "phase": phase,
        "review_phase": review_phase,
        "review_only_signed_child": phase == "signed",
        "merge_authoritative": phase == "signed",
        "defects": defects,
        "evidence": {
            "t001_t030": "PASS on one exact head through T031 producer coherence",
            "t031": "PASS",
            "t032": "PASS",
            "input_case_count": 31,
            "final_case_count": 32,
            "t031_observations": t031.get("observations"),
        },
        "gates": {
            "required": 5,
            "passed": len(gate_decisions),
            "complete": len(gate_decisions) == 5 and all(item.get("status") == "PASS" for item in gate_decisions.values()),
            "decisions": gate_decisions,
        },
        "applicable_matrix": {
            "passed": len(rows),
            "required": len(REQUIRED),
            "complete": len(rows) == len(REQUIRED),
            "scope": MATRIX_SCOPE,
        },
        "t031_authority": t031_authority,
        "p18_predecessor_authority": p18_authority,
        "pre_sign_authority": presign_authority,
        "excluded_revision_specific_workflows": EXCLUDED_REVISION_SPECIFIC,
        "errors": [],
    }
    if phase == "pre-sign":
        result["signing_required"] = True
        result["signing_instruction"] = (
            "Create exactly one direct review-only child changing only artifacts/v10/P19/review.md; "
            "record this pre-sign T032 run/artifact/digest and P19-T001..P19-T031 PASS with defects 0/0/0; "
            "then independently rerun the complete signed exact-head matrix and T032."
        )
    else:
        result["signing_required"] = False
        if presign_authority is None:
            raise SystemExit("signed phase missing live-bound pre-sign authority")

    (P19 / "closure").mkdir(parents=True, exist_ok=True)
    (P19 / "closure" / "P19-T032.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (P19 / "closure.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
