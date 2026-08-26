#!/usr/bin/env python3
"""CI orchestration for P16-T029 exact-head closure."""
from __future__ import annotations

import json, os, re, shutil, subprocess, time, urllib.parse, urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
P16=ROOT/"artifacts"/"v10"/"P16"
HEAD=os.environ["EXACT_HEAD"]
HEAD_REF=os.environ["HEAD_REF"]
REPO=os.environ["REPOSITORY"]
TOKEN=os.environ["GH_TOKEN"]
CURRENT_RUN=int(os.environ["GITHUB_RUN_ID"])
P15_SOURCE="6f39d87f1d94f71590fd79d4551cdd1cea652a76"; P15_RUN=32931945354; P15_ART=9593689993
P15_DIG="sha256:5a43c87ea26f86081523d371de260e100a20c5c05b3581f48223fb70e68cd233"
PENDING="Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED="Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

REQUIRED={
"P00 Bootstrap and G0 Traceability":"p00-bootstrap.yml","P01 Engineering Foundation":"p01-engineering.yml","P02 Brand Foundation":"p02-brand-foundation.yml","P03 Design System":"p03-design-system.yml","P04 Product Shells":"p04-product-shells.yml",
"P05 Links Domain Contract":"p05-links-domain-contract.yml","P05 Real Integration":"p05-integration.yml","P05 Workspace Browser":"p05-browser.yml",
"P06 Custom Domains":"p06-custom-domains.yml","P06 Real Integration":"p06-integration.yml","P06 Workspace Domains Browser":"p06-browser.yml",
"P07 Analytics Contract":"p07-analytics.yml","P07 Real Integration":"p07-integration.yml","P07 Workspace Analytics Browser":"p07-browser.yml",
"P08 QR Contract":"p08-qr.yml","P08 Real QR Integration":"p08-integration.yml","P08 Workspace QR Browser":"p08-browser.yml","P08 Evidence Coherence":"p08-evidence.yml",
"P09 Files Contract":"p09-files.yml","P09 Real Files and ClamAV Integration":"p09-integration.yml","P09 Files Health and Installer Preflight":"p09-health.yml","P09 Workspace Files Browser":"p09-browser.yml","P09 Evidence Coherence":"p09-evidence.yml",
"P10 Text Contract":"p10-text.yml","P10 Real Text Integration":"p10-integration.yml","P10 Workspace Text Browser":"p10-browser.yml","P10 Evidence Coherence":"p10-evidence.yml",
"P11 Bio Contract":"p11-bio.yml","P11 Real Bio Integration":"p11-integration.yml","P11 Workspace Bio Browser":"p11-browser.yml","P11 Evidence Coherence":"p11-evidence.yml",
"P12 Workspace Organization Contract":"p12-workspace-organization.yml","P12 Real Workspace Organization Integration":"p12-integration.yml","P12 Workspace Organization Browser":"p12-browser.yml","P12 Evidence Coherence":"p12-evidence.yml",
"P13 Billing Payments Entitlements Contract":"p13-billing-payments-entitlements.yml","P13 Real Billing Payments Entitlements Integration":"p13-integration.yml","P13 Billing Commerce Browser":"p13-browser.yml","P13 Evidence Coherence":"p13-evidence.yml",
"P14 Real Support Tickets and Mail Integration":"p14-integration.yml","P14 Workspace Support Browser":"p14-browser.yml","P14 Admin Tickets Mail Contact Browser":"p14-browser-023.yml",
"P15 Real Authentication Integration":"p15-real-auth-integration.yml","P15 Authentication Security Integration":"p15-auth-security-integration.yml","P15 Account OAuth Integration":"p15-account-oauth-integration.yml","P15 Handoff Mail Audit Integration":"p15-handoff-mail-audit-integration.yml","P15 Auth Route Browser Authority":"p15-browser.yml","P15 Workspace Account Settings Browser Authority":"p15-account-browser.yml","P15 Admin OAuth Browser Authority":"p15-admin-oauth-browser.yml",
"P16 Trust Destination Risk Abuse Contract":"p16-trust-destination-risk-abuse.yml","P16 Real Destination Risk Integration":"p16-destination-risk-integration.yml","P16 Security Notification Producer":"p16-notification-producer.yml","P16 Admin Risk API Integration":"p16-admin-risk-api.yml","P16 Trust Browser Authority":"p16-browser.yml","P16 Evidence Coherence":"p16-evidence.yml",
}
EXCLUDED={**{f"P{i:02d} Closure":"revision-specific predecessor closure is inherited through P15 signed authority and is not reinterpreted on a P16 HEAD" for i in range(5,14)},
"P14 Support Tickets and Mail Contract":"revision-specific predecessor closure is inherited through P15 signed authority and P14 functional workflows are rerun separately on the P16 HEAD",
"P15 Authentication OAuth Account Contract":f"revision-specific immediate predecessor closure is inherited from P15 signed source {P15_SOURCE}; P15 functional workflows are rerun separately on the P16 HEAD"}
HEADERS={"Accept":"application/vnd.github+json","Authorization":f"Bearer {TOKEN}","X-GitHub-Api-Version":"2022-11-28"}

def api(url:str, *, method="GET", body=None):
    data=None if body is None else json.dumps(body).encode()
    req=urllib.request.Request(url,data=data,method=method,headers=HEADERS)
    with urllib.request.urlopen(req,timeout=30) as r:
        raw=r.read(); return json.loads(raw) if raw else None

def runs():
    q=urllib.parse.urlencode({"head_sha":HEAD,"per_page":100}); return api(f"https://api.github.com/repos/{REPO}/actions/runs?{q}").get("workflow_runs",[])

def dispatch(file:str):
    api(f"https://api.github.com/repos/{REPO}/actions/workflows/{urllib.parse.quote(file,safe='')}/dispatches",method="POST",body={"ref":HEAD_REF})

def wait_matrix() -> dict:
    P16.mkdir(parents=True,exist_ok=True); path=P16/"regression-manifest.json"; dispatched=set(); deadline=time.time()+160*60
    while time.time()<deadline:
        latest={}
        for run in runs():
            name=run.get("name")
            if name in REQUIRED and (name not in latest or int(run.get("id",0))>int(latest[name].get("id",0))): latest[name]=run
        missing=[n for n in REQUIRED if n not in latest]
        pending=[n for n in REQUIRED if n in latest and latest[n].get("status")!="completed"]
        failed=[n for n in REQUIRED if n in latest and latest[n].get("status")=="completed" and latest[n].get("conclusion")!="success"]
        rows={n:{"run_id":int(latest[n]["id"]),"run_number":int(latest[n].get("run_number",0)),"head_sha":latest[n].get("head_sha"),"event":latest[n].get("event"),"status":latest[n].get("status"),"conclusion":latest[n].get("conclusion"),"html_url":latest[n].get("html_url")} for n in REQUIRED if n in latest}
        manifest={"generated_at":datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00","Z"),"implementation_commit":HEAD,"required_workflows":rows,"excluded_revision_specific_workflows":EXCLUDED,"dispatched_workflows":sorted(dispatched),"missing":missing,"pending":pending,"failed":failed}
        path.write_text(json.dumps(manifest,indent=2,sort_keys=True)+"\n",encoding="utf-8")
        if failed: raise SystemExit("required P16 exact-head workflows failed: "+", ".join(failed))
        if not missing and not pending:
            print(f"P00-P16 affected exact-head matrix green ({len(REQUIRED)}/{len(REQUIRED)}) for {HEAD}"); return rows
        for name in missing:
            if name not in dispatched:
                print(f"Dispatching missing exact-head workflow {name} via {REQUIRED[name]} at {HEAD_REF}"); dispatch(REQUIRED[name]); dispatched.add(name)
        print(f"Waiting P16 matrix missing={missing} pending={pending}",flush=True); time.sleep(10)
    raise SystemExit(f"timed out waiting for P00-P16 affected workflows on {HEAD}")

def run_gh(*args:str, stdout=None):
    env={**os.environ,"GH_TOKEN":TOKEN}; return subprocess.run(["gh",*args],cwd=ROOT,env=env,check=True,stdout=stdout)

def download_t028(rows:dict):
    run_id=int(rows["P16 Evidence Coherence"]["run_id"]); meta=api(f"https://api.github.com/repos/{REPO}/actions/runs/{run_id}/artifacts?per_page=100")
    name=f"gojet-v10-p16-evidence-{HEAD}"; matches=[a for a in meta.get("artifacts",[]) if a.get("name")==name and not a.get("expired")]
    if len(matches)!=1 or not str(matches[0].get("digest","")).startswith("sha256:"): raise SystemExit(f"expected one exact-head T028 artifact {name}")
    tmp=Path("/tmp/p16-t028"); shutil.rmtree(tmp,ignore_errors=True); tmp.mkdir(parents=True)
    run_gh("run","download",str(run_id),"--repo",REPO,"--name",name,"--dir",str(tmp))
    t028=json.loads((tmp/"results"/"P16-T028.json").read_text(encoding="utf-8"))
    if t028.get("implementation_commit")!=HEAD or t028.get("status")!="PASS": raise SystemExit("downloaded T028 is not exact-head PASS")
    for item in tmp.iterdir():
        target=P16/item.name
        if item.is_dir(): target.mkdir(parents=True,exist_ok=True); shutil.copytree(item,target,dirs_exist_ok=True)
        else: shutil.copy2(item,target)

def bind_p15():
    run=api(f"https://api.github.com/repos/{REPO}/actions/runs/{P15_RUN}"); art=api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{P15_ART}")
    if not (run.get("head_sha")==P15_SOURCE and run.get("status")=="completed" and run.get("conclusion")=="success" and int(art.get("id",0))==P15_ART and art.get("digest")==P15_DIG and art.get("expired") is False and int(art.get("workflow_run",{}).get("id",0))==P15_RUN and art.get("workflow_run",{}).get("head_sha")==P15_SOURCE): raise SystemExit("P15 inherited authority live metadata mismatch")
    inherited=P16/"inherited"; inherited.mkdir(parents=True,exist_ok=True); z=Path("/tmp/p15-authority.zip")
    with z.open("wb") as f: run_gh("api","-H","Accept: application/vnd.github+json","-H","X-GitHub-Api-Version: 2022-11-28",f"/repos/{REPO}/actions/artifacts/{P15_ART}/zip",stdout=f)
    archive=subprocess.check_output(["sha256sum",str(z)],text=True).split()[0]
    if archive!=P15_DIG.removeprefix("sha256:"): raise SystemExit("P15 signed artifact archive digest mismatch")
    tmp=Path("/tmp/p15-authority"); shutil.rmtree(tmp,ignore_errors=True); tmp.mkdir(parents=True); subprocess.run(["unzip","-q",str(z),"-d",str(tmp)],check=True)
    closure=json.loads((tmp/"closure.json").read_text(encoding="utf-8"))
    if closure.get("node")!="P15" or closure.get("status")!="PASS" or closure.get("phase")!="signed" or closure.get("merge_authoritative") is not True: raise SystemExit("P15 signed artifact content invalid")
    target=inherited/"P15"; shutil.rmtree(target,ignore_errors=True); shutil.copytree(tmp,target)
    metadata={"source_commit":P15_SOURCE,"closure_run_id":P15_RUN,"workflow_head_sha":run.get("head_sha"),"workflow_status":run.get("status"),"workflow_conclusion":run.get("conclusion"),"artifact_id":P15_ART,"artifact_digest":P15_DIG,"artifact_expired":art.get("expired"),"archive_sha256":archive}
    (inherited/"p15-authority.json").write_text(json.dumps(metadata,indent=2,sort_keys=True)+"\n",encoding="utf-8")

def primary_review(text:str)->str:
    lines=re.findall(r"^Status: \*\*[^\n]+\*\*$",text,flags=re.MULTILINE)
    if len(lines)!=1 or lines[0] not in (PENDING,SIGNED): raise SystemExit(f"invalid P16 primary review status lines: {lines}")
    return lines[0]

def bind_presign_if_signed():
    text=(P16/"review.md").read_text(encoding="utf-8"); path=P16/"inherited"/"pre-sign-authority.json"
    if primary_review(text)!=SIGNED: path.unlink(missing_ok=True); return
    def grab(pattern,label):
        m=re.search(pattern,text)
        if not m: raise SystemExit(f"signed review missing {label}")
        return m.group(1)
    source=grab(r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`","pre-sign SHA"); run_id=int(grab(r"Pre-sign T029 closure run: `([0-9]+)`","pre-sign run")); art_id=int(grab(r"Pre-sign T029 closure artifact: `([0-9]+)`","pre-sign artifact")); expected=grab(r"Pre-sign T029 closure digest: `(sha256:[0-9a-f]{64})`","pre-sign digest")
    run=api(f"https://api.github.com/repos/{REPO}/actions/runs/{run_id}"); art=api(f"https://api.github.com/repos/{REPO}/actions/artifacts/{art_id}")
    if not (run.get("head_sha")==source and run.get("status")=="completed" and run.get("conclusion")=="success" and art.get("digest")==expected and art.get("expired") is False and int(art.get("workflow_run",{}).get("id",0))==run_id and art.get("workflow_run",{}).get("head_sha")==source): raise SystemExit("pre-sign closure live authority metadata mismatch")
    z=Path("/tmp/p16-presign.zip")
    with z.open("wb") as f: run_gh("api","-H","Accept: application/vnd.github+json","-H","X-GitHub-Api-Version: 2022-11-28",f"/repos/{REPO}/actions/artifacts/{art_id}/zip",stdout=f)
    archive=subprocess.check_output(["sha256sum",str(z)],text=True).split()[0]
    if archive!=expected.removeprefix("sha256:"): raise SystemExit("pre-sign closure archive digest mismatch")
    tmp=Path("/tmp/p16-presign"); shutil.rmtree(tmp,ignore_errors=True); tmp.mkdir(parents=True); subprocess.run(["unzip","-q",str(z),"-d",str(tmp)],check=True)
    closure=json.loads((tmp/"closure.json").read_text(encoding="utf-8"))
    if closure.get("implementation_commit")!=source or closure.get("status")!="PASS" or closure.get("phase")!="pre-sign" or closure.get("merge_authoritative") is not False: raise SystemExit("pre-sign closure artifact content invalid")
    meta={"source_commit":source,"closure_run_id":run_id,"workflow_status":run.get("status"),"workflow_conclusion":run.get("conclusion"),"artifact_id":art_id,"artifact_digest":expected,"artifact_expired":art.get("expired"),"archive_sha256":archive,"pre_sign_phase":"pre-sign","pre_sign_merge_authoritative":False}
    path.write_text(json.dumps(meta,indent=2,sort_keys=True)+"\n",encoding="utf-8")

def main():
    actual=subprocess.check_output(["git","rev-parse","HEAD"],cwd=ROOT,text=True).strip()
    if actual!=HEAD: raise SystemExit(f"checkout exact-head mismatch: {actual} != {HEAD}")
    rows=wait_matrix(); download_t028(rows); bind_p15(); bind_presign_if_signed()
    env={**os.environ,"EXACT_HEAD":HEAD}; subprocess.run(["python3","scripts/p16/validate_closure.py"],cwd=ROOT,env=env,check=True)
    result=json.loads((P16/"results"/"P16-T029.json").read_text(encoding="utf-8"))
    if result.get("status")!="PASS" or result.get("implementation_commit")!=HEAD or result.get("defects")!={"p0":0,"p1":0,"decision_required":0}: raise SystemExit(f"P16-T029 closure invalid: {result}")
    phase=result.get("phase")
    if phase=="pre-sign" and result.get("merge_authoritative") is not False: raise SystemExit("pre-sign T029 must not be merge authoritative")
    if phase=="signed" and result.get("merge_authoritative") is not True: raise SystemExit("signed T029 must be merge authoritative")
    if phase not in ("pre-sign","signed"): raise SystemExit(f"invalid T029 phase {phase}")
    print(f"P16-T029 {phase} closure PASS on {HEAD}")

if __name__=="__main__": main()
