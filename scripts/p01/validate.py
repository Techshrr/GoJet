#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json, os, platform, re, subprocess, sys, time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P01 = ROOT / "artifacts/v10/P01"; RESULTS = P01 / "results"; LOGS = P01 / "build-logs"
G1 = ROOT / "artifacts/v10/gates/G1/native-architecture"
SPEC_IDS = ["GJ-V10-MP-GREENFIELD-2026-08-20","GJ-V10-DS-GREENFIELD-2026-08-20","GJ-V10-IA-GREENFIELD-2026-08-20"]
APPS = {"@gojet/site","@gojet/docs","@gojet/workspace","@gojet/admin"}
PACKAGES = {"@gojet/api-client","@gojet/auth","@gojet/charts","@gojet/domain","@gojet/icons","@gojet/motion","@gojet/tokens","@gojet/ui","@gojet/utils"}
CASES = [("P01-T001","clean-frozen-install"),("P01-T002","strict-typecheck"),("P01-T003","independent-static-builds"),("P01-T004","package-boundaries-and-cycles"),("P01-T005","code-splitting-static-output"),("P01-T006","no-production-node-runtime"),("P01-T007","lockfile-and-dependency-inventory")]

def now(): return datetime.now(timezone.utc).isoformat().replace("+00:00","Z")
def digest(p: Path): return hashlib.sha256(p.read_bytes()).hexdigest()
def write_json(p: Path, v: Any): p.parent.mkdir(parents=True,exist_ok=True); p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
def load(p: Path): return json.loads(p.read_text(encoding="utf-8"))
def git(*a): return subprocess.run(["git",*a],cwd=ROOT,text=True,capture_output=True,check=True).stdout.strip()
def version(cmd):
    r=subprocess.run(cmd,cwd=ROOT,text=True,capture_output=True,check=False); return (r.stdout or r.stderr).strip().splitlines()[0]
def manifests():
    out={}
    for pattern in ("frontend/apps/*/package.json","frontend/packages/*/package.json"):
        for p in ROOT.glob(pattern):
            d=load(p); out[d["name"]]=(p,d)
    return out
def record(cid, ok, errors=None, details=None):
    write_json(RESULTS/f"{cid}.json",{"case_id":cid,"name":dict(CASES)[cid],"status":"PASS" if ok else "FAIL","errors":errors or [],"details":details or {},"recorded_at":now()})
def run(cid, cmd, log):
    t=time.monotonic(); r=subprocess.run(cmd,cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=False)
    LOGS.mkdir(parents=True,exist_ok=True); (LOGS/log).write_text(r.stdout or "",encoding="utf-8")
    err=[] if r.returncode==0 else [f"command exited {r.returncode}: {' '.join(cmd)}"]
    record(cid,r.returncode==0,err,{"command":cmd,"exit_code":r.returncode,"duration_seconds":round(time.monotonic()-t,3),"log":f"artifacts/v10/P01/build-logs/{log}"})
    return r.returncode==0

def boundary_case():
    ms=manifests(); errors=[]; expected=APPS|PACKAGES
    if expected-set(ms): errors.append("missing workspace manifests: "+", ".join(sorted(expected-set(ms))))
    if set(ms)-expected: errors.append("unexpected workspace manifests: "+", ".join(sorted(set(ms)-expected)))
    graph={n:set() for n in ms}
    for name,(path,data) in ms.items():
        declared=set()
        for key in ("dependencies","devDependencies","peerDependencies"):
            for dep in (data.get(key,{}) if isinstance(data.get(key,{}),dict) else {}):
                if dep in ms: declared.add(dep); graph[name].add(dep)
        if name in PACKAGES and declared&APPS: errors.append(f"shared package {name} depends on app(s): {sorted(declared&APPS)}")
        if name in APPS and (declared&APPS)-{name}: errors.append(f"app {name} depends on another app: {sorted((declared&APPS)-{name})}")
        src=path.parent/"src"
        if src.exists():
            for p in [*src.rglob("*.ts"),*src.rglob("*.tsx")]:
                text=p.read_text(encoding="utf-8")
                for dep in re.findall(r"(?:from\s+|import\s*)['\"](@gojet/[^'\"]+)['\"]",text):
                    if dep not in declared: errors.append(f"{p.relative_to(ROOT)} imports undeclared workspace dependency {dep}")
                    if name in PACKAGES and dep in APPS: errors.append(f"shared package {name} imports app {dep}")
    visiting=set(); visited=set(); stack=[]; cycles=[]
    def visit(n):
        if n in visiting: cycles.append(stack[stack.index(n):]+[n]); return
        if n in visited: return
        visiting.add(n); stack.append(n)
        for d in sorted(graph.get(n,())): visit(d)
        stack.pop(); visiting.remove(n); visited.add(n)
    for n in sorted(graph): visit(n)
    for c in cycles: errors.append("workspace cycle: "+" -> ".join(c))
    return not errors,errors,{"workspace_count":len(ms),"graph":{k:sorted(v) for k,v in sorted(graph.items())},"cycles":cycles}

def no_node_case():
    errors=[]; paths=[]
    for p in git("ls-files").splitlines():
        low=p.lower()
        if low.endswith("dockerfile") or "/dockerfile" in low or low.endswith(("compose.yml","compose.yaml")) or "/deploy/docker/" in f"/{low}": paths.append(p)
    if paths: errors.append("production container path(s) present: "+", ".join(paths))
    bad=[]
    all_ms={"root":(ROOT/"package.json",load(ROOT/"package.json")),**manifests()}
    for name,(path,data) in all_ms.items():
        for key,val in (data.get("scripts",{}) if isinstance(data.get("scripts",{}),dict) else {}).items():
            k=str(key).lower(); v=str(val).lower()
            if k in {"start","serve","serve:ssr","preview:prod"} or "pm2" in v or re.search(r"node\s+.*server",v) or "vite preview" in v or "astro preview" in v: bad.append(f"{path.relative_to(ROOT)}:{key}={val}")
    if bad: errors.append("production Node/SSR script(s) present: "+"; ".join(bad))
    return not errors,errors,{"forbidden_paths":paths,"forbidden_scripts":bad}

def dependency_case():
    errors=[]; lock=ROOT/"pnpm-lock.yaml"; lock_sha=None
    if not lock.exists(): errors.append("pnpm-lock.yaml missing")
    else:
        text=lock.read_text(encoding="utf-8"); lock_sha=digest(lock)
        if "lockfileVersion: '9.0'" not in text: errors.append("unexpected pnpm lockfile version")
        req=["19.2.8","1.170.29","5.101.4","8.2.1","7.2.2","0.41.7","7.0.2"]
        missing=[v for v in req if v not in text]
        if missing: errors.append("lockfile missing pinned versions: "+", ".join(missing))
    deps={}
    for n,(_,d) in sorted(manifests().items()):
        merged={}
        for k in ("dependencies","devDependencies"):
            if isinstance(d.get(k,{}),dict): merged.update({str(a):str(b) for a,b in d[k].items()})
        deps[n]=dict(sorted(merged.items()))
    return not errors,errors,{"lockfile_sha256":lock_sha,"workspace_dependencies":deps}
def bundle_report():
    report={"generated_at":now(),"apps":{}}
    for app in ("site","workspace","admin"):
        dist=ROOT/f"frontend/apps/{app}/dist"; mp=dist/".vite/manifest.json"; chunks=[]
        if mp.exists():
            for key,e in sorted(load(mp).items()):
                if isinstance(e,dict):
                    f=dist/str(e.get("file","")); chunks.append({"key":key,"file":e.get("file"),"bytes":f.stat().st_size if f.exists() else None,"isEntry":bool(e.get("isEntry")),"isDynamicEntry":bool(e.get("isDynamicEntry"))})
        report["apps"][app]={"manifest":str(mp.relative_to(ROOT)),"chunks":chunks}
    d=ROOT/"frontend/apps/docs/dist"; report["apps"]["docs"]={"html_files":sorted(str(p.relative_to(d)) for p in d.rglob("*.html")) if d.exists() else []}
    return report
def summary():
    items=[]
    for cid,_ in CASES:
        p=RESULTS/f"{cid}.json"; items.append(load(p) if p.exists() else {"case_id":cid,"status":"NOT_RUN","errors":["result missing"]})
    passed=sum(x.get("status")=="PASS" for x in items); return {"passed":passed,"failed":len(items)-passed,"total":len(items),"cases":items}
def evidence(commit,branch):
    env={"generated_at":now(),"os":platform.platform(),"python":platform.python_version(),"node":version(["node","--version"]),"pnpm":version(["pnpm","--version"])}; write_json(P01/"environment.json",env)
    write_json(P01/"source.json",{"repository":"Techshrr/GoJet","remote":"https://github.com/Techshrr/GoJet.git","branch":branch,"implementation_commit":commit,"specification_ids":SPEC_IDS,"toolchain":{"node":env["node"],"pnpm":env["pnpm"],"typescript":"7.0.2"}})
    ok,errs,det=dependency_case(); write_json(P01/"dependency-report.json",{"status":"PASS" if ok else "FAIL","errors":errs,**det}); write_json(P01/"bundle-report.json",bundle_report())
    s=summary(); write_json(G1/"p01-engineering.json",{"gate":"G1","scope":"P01 engineering foundation subset","implementation_commit":commit,"status":"PASS" if s["failed"]==0 else "FAIL","results":{k:s[k] for k in ("passed","failed","total")},"full_G1_release_gate_complete":False,"note":"G1 also has P21/P22 obligations; this closes only the P01 engineering subset."})
    lines=[]
    for cid,_ in CASES:
        x=load(RESULTS/f"{cid}.json"); lines.append(f"{cid}\t{x['status']}\t{json.dumps(x.get('details',{}).get('command','-'),ensure_ascii=False)}")
    (P01/"commands.log").write_text("\n".join(lines)+"\n",encoding="utf-8")
    candidates=[P01/"environment.json",P01/"source.json",P01/"commands.log",P01/"test-plan.json",P01/"review.md",P01/"bundle-report.json",P01/"dependency-report.json",G1/"p01-engineering.json",*[RESULTS/f"{cid}.json" for cid,_ in CASES]]
    files=[{"path":str(p.relative_to(ROOT)),"sha256":digest(p)} for p in candidates if p.exists()]
    write_json(P01/"evidence-index.json",{"schema_version":1,"node":"P01","implementation_commit":commit,"specification_ids":SPEC_IDS,"generated_at":now(),"results":{k:s[k] for k in ("passed","failed","total")},"files":files})
def main():
    a=argparse.ArgumentParser(); a.add_argument("--case",choices=[x for x,_ in CASES]); args=a.parse_args(); os.chdir(ROOT)
    for d in (P01,RESULTS,LOGS,G1): d.mkdir(parents=True,exist_ok=True)
    funcs={"P01-T004":boundary_case,"P01-T006":no_node_case,"P01-T007":dependency_case}
    if args.case:
        if args.case not in funcs: print(f"{args.case} is command-driven; run the full validator"); return 2
        ok,errs,det=funcs[args.case](); record(args.case,ok,errs,det); print(f"{args.case}: {'PASS' if ok else 'FAIL'}"); [print(f"  - {e}") for e in errs]; return 0 if ok else 1
    install=run("P01-T001",["pnpm","install","--frozen-lockfile"],"install.log")
    if install:
        run("P01-T002",["pnpm","run","typecheck"],"typecheck.log")
        errors=[]; details={"commands":[]}; start=time.monotonic()
        for app in ("@gojet/site","@gojet/workspace","@gojet/admin","@gojet/docs"):
            cmd=["pnpm","--filter",app,"build"]; r=subprocess.run(cmd,cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=False); ln=app.split("/")[-1]+"-build.log"; (LOGS/ln).write_text(r.stdout or "",encoding="utf-8"); details["commands"].append({"app":app,"command":cmd,"exit_code":r.returncode,"log":f"artifacts/v10/P01/build-logs/{ln}"}); errors += [] if r.returncode==0 else [f"{app} build exited {r.returncode}"]
        details["duration_seconds"]=round(time.monotonic()-start,3); record("P01-T003",not errors,errors,details)
    else: record("P01-T002",False,["precondition failed: frozen install"]); record("P01-T003",False,["precondition failed: frozen install"])
    ok,errs,det=boundary_case(); record("P01-T004",ok,errs,det)
    if load(RESULTS/"P01-T003.json")["status"]=="PASS": run("P01-T005",["node","scripts/p01/smoke.mjs"],"static-output-smoke.log")
    else: record("P01-T005",False,["precondition failed: independent builds"])
    ok,errs,det=no_node_case(); record("P01-T006",ok,errs,det); ok,errs,det=dependency_case(); record("P01-T007",ok,errs,det)
    commit=git("rev-parse","HEAD"); branch=os.environ.get("GITHUB_HEAD_REF") or os.environ.get("GITHUB_REF_NAME") or git("branch","--show-current") or "detached"; evidence(commit,branch); s=summary()
    for x in s["cases"]:
        print(f"{x['case_id']}: {x['status']}"); [print(f"  - {e}") for e in x.get("errors",[])]
    print(f"P01 summary: {s['passed']}/{s['total']} PASS"); return 0 if s["failed"]==0 else 1
if __name__=="__main__": sys.exit(main())
