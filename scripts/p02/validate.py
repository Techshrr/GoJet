#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json, os, platform, re, struct, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P02 = ROOT / "artifacts/v10/P02"
RESULTS = P02 / "results"
DS = ROOT / "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md"
ASSET_DIR = ROOT / "frontend/packages/tokens/brand/assets"
CONTRACT = ROOT / "frontend/packages/tokens/src/brand-contract.ts"
LICENSE_MD = ROOT / "frontend/packages/tokens/brand/BRAND-ASSET-LICENSES.md"
PROVENANCE = P02 / "license-records/asset-provenance.json"
JET = ROOT / "frontend/packages/tokens/brand/references/jet-path-reference.svg"
ASSETS = ["logo-full-light.svg","logo-full-dark.svg","logo-mark.svg","favicon.svg","favicon.ico","apple-touch-icon.png","og-brand.png"]
CASES = [(f"P02-T{i:03d}",name) for i,name in enumerate([
"brand-contract-and-required-assets","logo-safe-area-and-svg-integrity","brand-role-and-color-projection","asset-provenance-and-license-registry","jet-path-vocabulary-and-context-rules","reduced-motion-contract","single-authority-contract","raster-and-favicon-integrity"],1)]

def now(): return datetime.now(timezone.utc).isoformat().replace("+00:00","Z")
def sha256(p: Path): return hashlib.sha256(p.read_bytes()).hexdigest()
def load(p: Path): return json.loads(p.read_text(encoding="utf-8"))
def text(p: Path): return p.read_text(encoding="utf-8")
def write_json(p: Path,v:Any): p.parent.mkdir(parents=True,exist_ok=True); p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
def git(*args): return subprocess.run(["git",*args],cwd=ROOT,text=True,capture_output=True,check=True).stdout.strip()
def record(cid,ok,errors=None,details=None): write_json(RESULTS/f"{cid}.json",{"case_id":cid,"name":dict(CASES)[cid],"status":"PASS" if ok else "FAIL","errors":errors or [],"details":details or {},"recorded_at":now()}); return ok

def t001():
    errors=[]; required=[CONTRACT,LICENSE_MD,PROVENANCE,JET,ROOT/"frontend/packages/tokens/brand/BRAND-FOUNDATION.md"]+[ASSET_DIR/n for n in ASSETS]
    missing=[str(p.relative_to(ROOT)) for p in required if not p.exists()]
    if missing: errors.append("missing P02 brand files: "+", ".join(missing))
    body=text(CONTRACT) if CONTRACT.exists() else ""
    for n in ASSETS:
        if f"'{n}'" not in body: errors.append(f"brand contract missing asset identifier {n}")
    return not errors,errors,{"required_files":len(required),"assets":ASSETS}

def t002():
    errors=[]; ds=text(DS)
    for pattern,label in [(r"`asset\.logo\.website\.height`\s*\|\s*32px","website logo 32px"),(r"`asset\.logo\.product\.height`\s*\|\s*28px","product logo 28px"),(r"`asset\.logo\.safe-area`\s*\|\s*0\.5","logo safe area 0.5")]:
        if not re.search(pattern,ds,re.M): errors.append("Design System contract mismatch: "+label)
    for n in ("logo-full-light.svg","logo-full-dark.svg","logo-mark.svg","favicon.svg"):
        p=ASSET_DIR/n
        if not p.exists(): continue
        b=text(p)
        if "<svg" not in b or "viewBox=" not in b: errors.append(f"{n} is not a viewBox SVG")
        if 'data-authority="GJ-V10-DS-GREENFIELD-2026-08-20"' not in b: errors.append(f"{n} missing authority metadata")
        if re.search(r"<image\b|data:image/|https?://",b): errors.append(f"{n} contains embedded/external raster reference")
    return not errors,errors,{}

def t003():
    errors=[]; approved={"#2563EB","#06B6D4","#38BDF8","#0F172A","#F8FAFC","#F7F9FC","#FFFFFF","#475569","#CBD5E1","#334155"}; checked=[]
    for p in [ASSET_DIR/"logo-full-light.svg",ASSET_DIR/"logo-full-dark.svg",ASSET_DIR/"logo-mark.svg",ASSET_DIR/"favicon.svg",JET,P02/"brand-boards/brand-board.svg"]:
        if not p.exists(): continue
        checked.append(str(p.relative_to(ROOT))); vals={v.upper() for v in re.findall(r"#[0-9A-Fa-f]{6}",text(p))}; bad=sorted(vals-approved)
        if bad: errors.append(f"{p.relative_to(ROOT)} uses unapproved colors: {bad}")
    return not errors,errors,{"checked_files":checked}

def t004():
    errors=[]
    if not PROVENANCE.exists(): return False,["asset provenance record missing"],{}
    data=load(PROVENANCE); rows={Path(x["path"]).name:x for x in data.get("assets",[])}
    for n in ASSETS:
        if n not in rows: errors.append(f"provenance missing {n}")
        elif not rows[n].get("source") or not rows[n].get("license"): errors.append(f"incomplete provenance for {n}")
    if data.get("external_assets_bundled") is not False: errors.append("P02 unexpectedly declares external assets bundled")
    lb=text(LICENSE_MD) if LICENSE_MD.exists() else ""
    for n in ASSETS:
        if f"`{n}`" not in lb: errors.append(f"license registry missing {n}")
    for rule in ("Official Brand Kit","Official SVG","Simple Icons"):
        if rule not in lb: errors.append(f"external source order missing {rule}")
    return not errors,errors,{"records":len(rows)}

def t005():
    errors=[]; c=text(CONTRACT) if CONTRACT.exists() else ""; j=text(JET) if JET.exists() else ""
    for e in ("path","node","split","pulse"):
        if f"'{e}'" not in c: errors.append(f"contract missing Jet Path element {e}")
        if f'data-jet-element="{e}"' not in j: errors.append(f"reference missing Jet Path element {e}")
    for context in ("hero","product-transition","routing","a-b","analytics","empty-illustration","loading-illustration","input-border","button","table-cell","every-card"):
        if f"'{context}'" not in c: errors.append(f"Jet Path context missing {context}")
    return not errors,errors,{}

def t006():
    errors=[]; ds=text(DS); j=text(JET) if JET.exists() else ""
    if not re.search(r"`motion\.duration\.path`\s*\|\s*6000ms",ds): errors.append("Design System path duration mismatch")
    if not re.search(r"`motion\.duration\.reduced`\s*\|\s*120ms",ds): errors.append("Design System reduced duration mismatch")
    for needle in ("6000ms","prefers-reduced-motion: reduce","animation:none","120ms"):
        if needle not in j.replace(" ",""): errors.append(f"Jet Path reduced-motion reference missing {needle}")
    return not errors,errors,{}

def t007():
    errors=[]; body=text(CONTRACT) if CONTRACT.exists() else ""; forbidden=re.findall(r"#[0-9A-Fa-f]{6}|rgba?\(|\b\d+(?:\.\d+)?(?:px|ms)\b",body)
    if forbidden: errors.append("P02 projection duplicates exact visual values: "+", ".join(sorted(set(forbidden))))
    if "Exact values remain authoritative only" not in body: errors.append("single-authority declaration missing")
    if "TOKEN_IMPLEMENTATION_STAGE = 'P03'" not in text(ROOT/"frontend/packages/tokens/src/index.ts"): errors.append("P02 changed token implementation stage away from P03")
    return not errors,errors,{"exact_value_hits":forbidden}

def png_dims(p: Path):
    b=p.read_bytes()
    if b[:8] != b"\x89PNG\r\n\x1a\n" or b[12:16] != b"IHDR": raise ValueError("invalid PNG")
    return struct.unpack(">II",b[16:24])
def t008():
    errors=[]; dims={}
    for n,exp in {"apple-touch-icon.png":(180,180),"og-brand.png":(1200,630)}.items():
        try: got=png_dims(ASSET_DIR/n); dims[n]=got
        except Exception as e: errors.append(f"{n}: {e}"); continue
        if got!=exp: errors.append(f"{n} dimensions {got}, expected {exp}")
    ico=ASSET_DIR/"favicon.ico"
    if not ico.exists() or ico.read_bytes()[:4]!=b"\x00\x00\x01\x00": errors.append("favicon.ico invalid or missing")
    return not errors,errors,{"dimensions":{k:list(v) for k,v in dims.items()}}

FUNCS={"P02-T001":t001,"P02-T002":t002,"P02-T003":t003,"P02-T004":t004,"P02-T005":t005,"P02-T006":t006,"P02-T007":t007,"P02-T008":t008}
def emit():
    commit=git("rev-parse","HEAD"); branch=os.environ.get("GITHUB_HEAD_REF") or os.environ.get("GITHUB_REF_NAME") or git("branch","--show-current") or "detached"
    write_json(P02/"environment.json",{"generated_at":now(),"platform":platform.platform(),"python":platform.python_version()}); write_json(P02/"source.json",{"repository":"Techshrr/GoJet","branch":branch,"implementation_commit":commit,"specification_ids":["GJ-V10-MP-GREENFIELD-2026-08-20","GJ-V10-DS-GREENFIELD-2026-08-20","GJ-V10-IA-GREENFIELD-2026-08-20"]})
    rows=[load(RESULTS/f"{cid}.json") for cid,_ in CASES]; passed=sum(x["status"]=="PASS" for x in rows); (P02/"commands.log").write_text("\n".join(f"{x['case_id']}\t{x['status']}" for x in rows)+"\n",encoding="utf-8")
    candidates=[P02/"environment.json",P02/"source.json",P02/"commands.log",P02/"test-plan.json",P02/"review.md",P02/"brand-boards/brand-board.svg",PROVENANCE,P02/"reference-captures/jet-path-reference.svg"]+[RESULTS/f"{cid}.json" for cid,_ in CASES]
    files=[{"path":str(p.relative_to(ROOT)),"sha256":sha256(p)} for p in candidates if p.exists()]; write_json(P02/"evidence-index.json",{"schema_version":1,"node":"P02","implementation_commit":commit,"generated_at":now(),"results":{"passed":passed,"failed":len(rows)-passed,"total":len(rows)},"files":files}); return passed,len(rows)
def main():
    ap=argparse.ArgumentParser(); ap.add_argument("--case",choices=list(FUNCS)); a=ap.parse_args(); RESULTS.mkdir(parents=True,exist_ok=True)
    if a.case:
        ok,errs,det=FUNCS[a.case](); record(a.case,ok,errs,det); print(f"{a.case}: {'PASS' if ok else 'FAIL'}"); [print("  - "+e) for e in errs]; return 0 if ok else 1
    failed=0
    for cid,_ in CASES:
        ok,errs,det=FUNCS[cid](); record(cid,ok,errs,det); failed += 0 if ok else 1; print(f"{cid}: {'PASS' if ok else 'FAIL'}"); [print("  - "+e) for e in errs]
    passed,total=emit(); print(f"P02 summary: {passed}/{total} PASS"); return 0 if failed==0 else 1
if __name__=="__main__": sys.exit(main())
