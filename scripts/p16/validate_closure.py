#!/usr/bin/env python3
"""P16-T029 exact-head pre-sign/final signed accountable closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P16 = ROOT / "artifacts" / "v10" / "P16"
RESULTS = P16 / "results"
PLAN = P16 / "test-plan.json"
REG = P16 / "regression-manifest.json"
REVIEW = P16 / "review.md"
CLOSURE = P16 / "closure.json"
INDEX = P16 / "closure-evidence-index.json"
T029 = RESULTS / "P16-T029.json"
P15A = P16 / "inherited" / "p15-authority.json"
P15 = P16 / "inherited" / "P15"
P15C = P15 / "closure.json"
P15T = P15 / "results" / "P15-T029.json"
P15R = P15 / "review.md"
P15I = P15 / "closure-evidence-index.json"
PRESIGN = P16 / "inherited" / "pre-sign-authority.json"

CASE_DIRS = {
    1:"api",2:"api",3:"security",4:"security",5:"risk",6:"security",7:"risk",8:"risk",
    9:"security",10:"security",11:"security",12:"audit",13:"security",14:"security",
    15:"domain",16:"domain",17:"security",18:"security",19:"abuse",20:"abuse",21:"abuse",
    22:"audit",23:"notifications",24:"api",25:"api",26:"browser",27:"browser",28:"results",
}
CASES = tuple(f"P16-T{i:03d}" for i in range(1, 30))
INPUT = CASES[:-1]
WF = (
    "P00 Bootstrap and G0 Traceability","P01 Engineering Foundation","P02 Brand Foundation","P03 Design System","P04 Product Shells",
    "P05 Links Domain Contract","P05 Real Integration","P05 Workspace Browser",
    "P06 Custom Domains","P06 Real Integration","P06 Workspace Domains Browser",
    "P07 Analytics Contract","P07 Real Integration","P07 Workspace Analytics Browser",
    "P08 QR Contract","P08 Real QR Integration","P08 Workspace QR Browser","P08 Evidence Coherence",
    "P09 Files Contract","P09 Real Files and ClamAV Integration","P09 Files Health and Installer Preflight","P09 Workspace Files Browser","P09 Evidence Coherence",
    "P10 Text Contract","P10 Real Text Integration","P10 Workspace Text Browser","P10 Evidence Coherence",
    "P11 Bio Contract","P11 Real Bio Integration","P11 Workspace Bio Browser","P11 Evidence Coherence",
    "P12 Workspace Organization Contract","P12 Real Workspace Organization Integration","P12 Workspace Organization Browser","P12 Evidence Coherence",
    "P13 Billing Payments Entitlements Contract","P13 Real Billing Payments Entitlements Integration","P13 Billing Commerce Browser","P13 Evidence Coherence",
    "P14 Real Support Tickets and Mail Integration","P14 Workspace Support Browser","P14 Admin Tickets Mail Contact Browser",
    "P15 Real Authentication Integration","P15 Authentication Security Integration","P15 Account OAuth Integration","P15 Handoff Mail Audit Integration",
    "P15 Auth Route Browser Authority","P15 Workspace Account Settings Browser Authority","P15 Admin OAuth Browser Authority",
    "P16 Trust Destination Risk Abuse Contract","P16 Real Destination Risk Integration","P16 Security Notification Producer",
    "P16 Admin Risk API Integration","P16 Trust Browser Authority","P16 Evidence Coherence",
)
EXCLUDED = tuple([f"P{i:02d} Closure" for i in range(5, 14)] + ["P14 Support Tickets and Mail Contract","P15 Authentication OAuth Account Contract"])
ZERO = {"p0":0,"p1":0,"decision_required":0}
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"
P15_SOURCE = "6f39d87f1d94f71590fd79d4551cdd1cea652a76"
P15_INTEGRATION = "dd70eacf02d4dd79fe82063f3d43610ab11885e8"
P15_RUN = 32931945354
P15_ART = 9593689993
P15_DIG = "sha256:5a43c87ea26f86081523d371de260e100a20c5c05b3581f48223fb70e68cd233"
CONTRACT = "43c5d4d7e1833c593ceacb48016abac6e3133893"


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")

def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()

def load(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))

def sha(path: Path) -> str:
    h=hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda:f.read(131072), b""): h.update(chunk)
    return h.hexdigest()

def req(ok: bool, msg: str, errors: list[str]) -> None:
    if not ok: errors.append(msg)

def case_path(cid: str) -> Path:
    return P16 / CASE_DIRS[int(cid[-3:])] / f"{cid}.json"

def primary_status(text: str, errors: list[str]) -> str | None:
    lines=re.findall(r"^Status: \*\*[^\n]+\*\*$", text, flags=re.MULTILINE)
    req(len(lines)==1, f"P16 review must contain exactly one primary Status line, got {len(lines)}", errors)
    if len(lines)!=1: return None
    req(lines[0] in (PENDING,SIGNED), f"unsupported P16 review status: {lines[0]}", errors)
    return lines[0]

def validate_plan(errors: list[str]) -> dict:
    req(PLAN.is_file(), "missing P16 test-plan.json", errors)
    if not PLAN.is_file(): return {}
    try: plan=load(PLAN)
    except Exception as exc: errors.append(f"invalid P16 test plan: {exc}"); return {}
    ids=tuple(row.get("id") for row in plan.get("cases",[]) if isinstance(row,dict))
    req(ids==CASES, "P16 test-plan case range/order drift", errors)
    closure=plan.get("closure",{})
    req(isinstance(closure,dict), "P16 closure contract missing", errors)
    if isinstance(closure,dict):
        req(closure.get("same_exact_head_required") is True, "P16 same-exact-head rule drift", errors)
        req(closure.get("required_case_range")=="P16-T001..P16-T029", "P16 closure case range drift", errors)
        req(closure.get("review_required") is True, "P16 review-required rule drift", errors)
        req(closure.get("defect_limits")==ZERO, "P16 defect limits drift", errors)
    pred=plan.get("predecessor_signed_authority",{})
    req(isinstance(pred,dict) and pred.get("node")=="P15" and pred.get("integration_commit")==P15_INTEGRATION
        and pred.get("signed_source_commit")==P15_SOURCE and pred.get("closure_run_id")==P15_RUN
        and pred.get("artifact_id")==P15_ART and pred.get("artifact_digest")==P15_DIG
        and pred.get("phase")=="signed" and pred.get("merge_authoritative") is True,
        "P15 frozen predecessor authority drift", errors)
    return plan

def validate_regression(head: str, errors: list[str]) -> dict:
    req(REG.is_file(), "missing P16 regression-manifest.json", errors)
    if not REG.is_file(): return {}
    try: reg=load(REG)
    except Exception as exc: errors.append(f"invalid P16 regression manifest: {exc}"); return {}
    req(reg.get("implementation_commit")==head, "P16 regression manifest head mismatch", errors)
    rows=reg.get("required_workflows",{})
    req(isinstance(rows,dict) and set(rows)==set(WF), "P16 regression workflow set mismatch", errors)
    req(reg.get("missing")==[] and reg.get("pending")==[] and reg.get("failed")==[], "P16 regression matrix not fully green", errors)
    if isinstance(rows,dict):
        for name in WF:
            row=rows.get(name,{})
            req(isinstance(row,dict) and row.get("head_sha")==head and row.get("status")=="completed"
                and row.get("conclusion")=="success" and isinstance(row.get("run_id"),int) and row.get("run_id",0)>0,
                f"{name} exact-head success record invalid", errors)
    excluded=reg.get("excluded_revision_specific_workflows",{})
    req(isinstance(excluded,dict) and set(excluded)==set(EXCLUDED), "P16 revision-specific exclusion set drift", errors)
    if isinstance(excluded,dict):
        for name in EXCLUDED:
            text=str(excluded.get(name,"")); req("revision-specific" in text.lower() and "inherited" in text.lower(), f"{name} exclusion rationale invalid", errors)
        req(P15_SOURCE in str(excluded.get("P15 Authentication OAuth Account Contract","")), "P15 contract exclusion not bound to signed source", errors)
    return reg

def validate_cases(head: str, errors: list[str]) -> list[dict]:
    out=[]
    for cid in INPUT:
        path=case_path(cid); req(path.is_file(), f"missing evidence {cid}", errors)
        if not path.is_file(): continue
        try: data=load(path)
        except Exception as exc: errors.append(f"invalid {cid}: {exc}"); continue
        ident=data.get("case_id",data.get("case")); ehead=data.get("implementation_commit",data.get("exact_head"))
        req(ident==cid, f"{cid} identity invalid", errors); req(data.get("status")=="PASS", f"{cid} not PASS", errors)
        if "errors" in data: req(data.get("errors")==[], f"{cid} has errors", errors)
        req(ehead==head, f"{cid} exact-head mismatch", errors)
        if cid=="P16-T028":
            obs=data.get("observations",{})
            phase=obs.get("review_phase") or ("pending" if obs.get("review_phase_pending") is True else None)
            req(obs.get("input_evidence_count")==27, "T028 input evidence count drift", errors)
            req(obs.get("same_exact_head") is True and obs.get("secret_safe") is True and obs.get("mixed_head_rejected") is True, "T028 coherence/security flags invalid", errors)
            req(obs.get("producer_count")==5, "T028 producer count drift", errors)
            req(int(obs.get("t026_capture_count",0))>=12 and int(obs.get("t027_capture_count",0))>=12, "T028 browser captures insufficient", errors)
            req(obs.get("merge_authoritative") is False, "T028 must never grant merge authority", errors)
            req(phase in ("pending","signed"), f"T028 review phase invalid: {phase}", errors)
        out.append({"case_id":cid,"path":str(path.relative_to(ROOT)),"sha256":sha(path),"status":data.get("status"),"implementation_commit":ehead})
    req(tuple(row["case_id"] for row in out)==INPUT, "P16 closure evidence set/order mismatch", errors)
    return out

def validate_p15(errors: list[str]) -> dict:
    for path in (P15A,P15C,P15T,P15R,P15I): req(path.is_file(), f"missing inherited P15 authority: {path}", errors)
    if not all(path.is_file() for path in (P15A,P15C,P15T,P15R,P15I)): return {}
    try: meta=load(P15A); closure=load(P15C); t029=load(P15T)
    except Exception as exc: errors.append(f"invalid inherited P15 authority: {exc}"); return {}
    req(meta.get("source_commit")==P15_SOURCE and meta.get("closure_run_id")==P15_RUN and meta.get("artifact_id")==P15_ART
        and meta.get("artifact_digest")==P15_DIG and meta.get("workflow_head_sha")==P15_SOURCE
        and meta.get("workflow_status")=="completed" and meta.get("workflow_conclusion")=="success"
        and meta.get("artifact_expired") is False and meta.get("archive_sha256")==P15_DIG.removeprefix("sha256:"),
        "P15 live authority metadata invalid", errors)
    req(closure.get("node")=="P15" and closure.get("case_id")=="P15-T029" and closure.get("implementation_commit")==P15_SOURCE,
        "P15 signed closure identity invalid", errors)
    req(closure.get("status")=="PASS" and closure.get("phase")=="signed" and closure.get("merge_authoritative") is True,
        "P15 predecessor not signed merge authority", errors)
    req(closure.get("defects")==ZERO and closure.get("required_regression_workflow_count")==50, "P15 predecessor ledger/matrix invalid", errors)
    req(t029.get("case_id")=="P15-T029" and t029.get("implementation_commit")==P15_SOURCE and t029.get("status")=="PASS"
        and t029.get("phase")=="signed" and t029.get("merge_authoritative") is True and t029.get("defects")==ZERO,
        "P15-T029 signed authority invalid", errors)
    req(SIGNED in P15R.read_text(encoding="utf-8"), "P15 signed review marker missing", errors)
    return meta

def parse_review(head: str, errors: list[str]) -> dict:
    req(REVIEW.is_file(), "missing P16 review.md", errors)
    if not REVIEW.is_file(): return {"phase":"invalid","merge_authoritative":False}
    text=REVIEW.read_text(encoding="utf-8"); status=primary_status(text,errors)
    if status==PENDING:
        return {"phase":"pre-sign","status":"PENDING","merge_authoritative":False,"review_sha256":sha(REVIEW)}
    if status!=SIGNED: return {"phase":"invalid","merge_authoritative":False,"review_sha256":sha(REVIEW)}
    def grab(label: str, pattern: str) -> str | None:
        m=re.search(pattern,text); req(m is not None, f"signed review missing {label}", errors); return m.group(1) if m else None
    pre=grab("pre-sign SHA",r"Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`")
    run=grab("pre-sign closure run",r"Pre-sign T029 closure run: `([0-9]+)`")
    art=grab("pre-sign closure artifact",r"Pre-sign T029 closure artifact: `([0-9]+)`")
    dig=grab("pre-sign closure digest",r"Pre-sign T029 closure digest: `(sha256:[0-9a-f]{64})`")
    reviewer=grab("reviewer",r"Accountable reviewer: `([^`]+)`")
    date=grab("review date",r"Review date: `(\d{4}-\d{2}-\d{2})`")
    req("Evidence disposition: `P16-T001..P16-T029 PASS`" in text, "signed review P16 evidence disposition missing", errors)
    req("P0/P1/DECISION REQUIRED: `0/0/0`" in text, "signed review zero-defect ledger missing", errors)
    req("Review-only signed child: `true`" in text, "signed review-only marker missing", errors)
    req("Signed revision requires complete same-revision affected matrix before merge: `true`" in text, "signed same-revision marker missing", errors)
    try:
        parent=git("rev-parse","HEAD^"); changed=tuple(x for x in git("diff","--name-only","HEAD^..HEAD").splitlines() if x)
        req(changed==("artifacts/v10/P16/review.md",), f"signed child is not review-only: {changed}", errors)
        req(pre==parent, "signed review pre-sign SHA is not direct parent", errors)
    except Exception as exc: errors.append(f"cannot validate review-only signed child: {exc}")
    req(PRESIGN.is_file(), "missing live pre-sign closure authority metadata", errors)
    if PRESIGN.is_file():
        try: authority=load(PRESIGN)
        except Exception as exc: errors.append(f"invalid pre-sign authority metadata: {exc}"); authority={}
        req(authority.get("source_commit")==pre and authority.get("closure_run_id")==int(run or 0) and authority.get("artifact_id")==int(art or 0)
            and authority.get("artifact_digest")==dig and authority.get("workflow_status")=="completed" and authority.get("workflow_conclusion")=="success"
            and authority.get("artifact_expired") is False and authority.get("pre_sign_phase")=="pre-sign"
            and authority.get("pre_sign_merge_authoritative") is False,
            "live pre-sign closure authority mismatch", errors)
    return {"phase":"signed","status":"SIGNED","merge_authoritative":True,"review_sha256":sha(REVIEW),
            "pre_sign_implementation_commit":pre,"pre_sign_closure_run":int(run or 0),"pre_sign_closure_artifact":int(art or 0),
            "pre_sign_closure_artifact_digest":dig,"reviewer":reviewer,"review_date":date,"review_only_signed_child":True}

def main() -> int:
    errors=[]; head=git("rev-parse","HEAD")
    validate_plan(errors); reg=validate_regression(head,errors); cases=validate_cases(head,errors); p15=validate_p15(errors); review=parse_review(head,errors)
    phase=review.get("phase","invalid"); merge=phase=="signed" and not errors
    defects=ZERO if not errors else {"p0":0,"p1":0,"decision_required":len(errors)}
    closure={"node":"P16","case_id":"P16-T029","status":"PASS" if not errors else "FAIL","generated_at":now(),
             "implementation_commit":head,"phase":phase,"merge_authoritative":merge,"input_evidence_count":len(cases),
             "required_regression_workflow_count":len(WF),"defects":defects,"review":{**review,"merge_authoritative":merge},
             "predecessor":{"node":"P15","integration_commit":P15_INTEGRATION,"signed_source_commit":P15_SOURCE,
                            "closure_run_id":P15_RUN,"artifact_id":P15_ART,"artifact_digest":P15_DIG},"errors":errors}
    CLOSURE.write_text(json.dumps(closure,indent=2,sort_keys=True)+"\n",encoding="utf-8")
    RESULTS.mkdir(parents=True,exist_ok=True)
    result={k:closure[k] for k in ("case_id","status","generated_at","implementation_commit","phase","merge_authoritative","input_evidence_count","required_regression_workflow_count","defects","errors")}
    result["contract_authority"]=CONTRACT
    T029.write_text(json.dumps(result,indent=2,sort_keys=True)+"\n",encoding="utf-8")
    index={"node":"P16","case":"P16-T029","generated_at":closure["generated_at"],"implementation_commit":head,
           "case_evidence":cases,"regression_manifest":{"path":str(REG.relative_to(ROOT)),"sha256":sha(REG) if REG.is_file() else None},
           "review":{"path":str(REVIEW.relative_to(ROOT)),"sha256":sha(REVIEW) if REVIEW.is_file() else None},
           "predecessor_authority":{"path":str(P15A.relative_to(ROOT)),"sha256":sha(P15A) if P15A.is_file() else None},
           "required_workflows":list(WF),"excluded_revision_specific_workflows":list(EXCLUDED)}
    INDEX.write_text(json.dumps(index,indent=2,sort_keys=True)+"\n",encoding="utf-8")
    print(json.dumps(result,indent=2,sort_keys=True))
    return 0 if not errors else 1

if __name__=="__main__": raise SystemExit(main())
