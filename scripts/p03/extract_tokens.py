#!/usr/bin/env python3
"""Synchronize approved Design System token tables into canonical tokens.json."""
from __future__ import annotations
import json, re, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SPEC = ROOT / "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md"
OUT = ROOT / "frontend/packages/tokens/src/tokens.json"
TOKEN_RE = re.compile(r"^[a-z][a-z0-9.-]*$")
META_HEADERS = {"use","prohibited","contrast","notes","description","purpose","semantic","example"}

def cells(line: str) -> list[str]:
    return [c.strip().strip('`') for c in line.strip().strip('|').split('|')]

def key(h: str) -> str:
    return re.sub(r"[^a-z0-9]+","_",h.strip().lower()).strip('_')

def scalar(v: str):
    v=v.strip().strip('`')
    if re.fullmatch(r"-?\d+",v): return int(v)
    if re.fullmatch(r"-?\d+\.\d+",v): return float(v)
    return v

def main() -> int:
    lines=SPEC.read_text(encoding="utf-8").splitlines(); heading=""; tokens={}; i=0
    while i < len(lines):
        line=lines[i]
        if line.startswith('#'):
            heading=line.lstrip('#').strip(); i+=1; continue
        if line.lstrip().startswith('|') and i+1 < len(lines) and re.match(r"^\s*\|?\s*:?-{3,}",lines[i+1]):
            headers=cells(line); hkeys=[key(h) for h in headers]; i+=2
            if not headers or hkeys[0] != "token":
                while i < len(lines) and lines[i].lstrip().startswith('|'): i+=1
                continue
            while i < len(lines) and lines[i].lstrip().startswith('|'):
                row=cells(lines[i]); i+=1
                if len(row) < len(headers): row += ['']*(len(headers)-len(row))
                name=row[0].strip()
                if not TOKEN_RE.fullmatch(name): continue
                rec={"section":heading}
                hv=dict(zip(hkeys,row))
                if "light_reference" in hv and "dark_reference" in hv:
                    rec["themes"]={"light":scalar(hv["light_reference"]),"dark":scalar(hv["dark_reference"])}
                elif "light" in hv and "dark" in hv and hv.get("light") and hv.get("dark"):
                    rec["themes"]={"light":scalar(hv["light"]),"dark":scalar(hv["dark"])}
                elif "exact_value" in hv:
                    rec["value"]=scalar(hv["exact_value"])
                elif "value" in hv:
                    rec["value"]=scalar(hv["value"])
                else:
                    props={}
                    for hk,val in zip(hkeys[1:],row[1:]):
                        if hk and hk not in META_HEADERS and val: props[hk]=scalar(val)
                    if props: rec["properties"]=props
                meta={}
                for hk,val in zip(hkeys[1:],row[1:]):
                    if hk in META_HEADERS and val: meta[hk]=val
                if meta: rec["meta"]=meta
                prior=tokens.get(name)
                if prior and prior != rec:
                    # Repeated tables are acceptable only when implementation-bearing fields agree.
                    def core(x): return {k:v for k,v in x.items() if k not in {"section","meta"}}
                    if core(prior)!=core(rec):
                        raise SystemExit(f"conflicting token definition for {name}: {core(prior)} != {core(rec)}")
                    continue
                tokens[name]=rec
            continue
        i+=1
    required={"color.blue.600","color.cyan.500","color.sky.400","asset.logo.website.height","asset.logo.product.height","asset.logo.safe-area","motion.duration.path","motion.duration.reduced","icon.size.inline","icon.stroke.default"}
    missing=sorted(required-set(tokens))
    if missing: raise SystemExit("required Design System tokens not parsed: "+", ".join(missing))
    if len(tokens) < 80: raise SystemExit(f"implausibly low token count: {len(tokens)}")
    payload={
      "$schema":"./tokens.schema.json",
      "schema_version":1,
      "authority":"GJ-V10-DS-GREENFIELD-2026-08-20",
      "generated_from":"specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md",
      "token_count":len(tokens),
      "tokens":dict(sorted(tokens.items())),
    }
    OUT.parent.mkdir(parents=True,exist_ok=True)
    OUT.write_text(json.dumps(payload,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
    print(f"wrote {len(tokens)} tokens to {OUT.relative_to(ROOT)}")
    return 0

if __name__=="__main__": sys.exit(main())
