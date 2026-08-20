#!/usr/bin/env python3
"""Generate runtime Design System artifacts from canonical tokens.json."""
from __future__ import annotations
import json, re, sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SRC=ROOT/"frontend/packages/tokens/src/tokens.json"
GEN=ROOT/"frontend/packages/tokens/generated"

def css_name(name:str)->str: return "--gojet-"+name.replace('.','-')
def ref_name(value):
    if not isinstance(value,str): return None
    s=value.strip().strip('`')
    if s.startswith('{') and s.endswith('}'): s=s[1:-1]
    return s if re.fullmatch(r"[a-z][a-z0-9.-]*",s) else None

def main()->int:
    data=json.loads(SRC.read_text(encoding='utf-8')); tokens=data['tokens']; GEN.mkdir(parents=True,exist_ok=True)
    def resolve(value,theme='light',stack=()):
        if not isinstance(value,str): return value
        def sub(m): return str(resolve(m.group(1),theme,stack))
        if re.search(r"\{[a-z][a-z0-9.-]*\}",value): return re.sub(r"\{([a-z][a-z0-9.-]*)\}",sub,value)
        rn=ref_name(value)
        if rn and rn in tokens:
            if rn in stack: raise ValueError('token cycle: '+' -> '.join((*stack,rn)))
            rec=tokens[rn]
            if 'themes' in rec: return resolve(rec['themes'][theme],theme,(*stack,rn))
            if 'value' in rec: return resolve(rec['value'],theme,(*stack,rn))
        return value
    simple={}; light={}; dark={}; composite={}
    for name,rec in tokens.items():
        if 'themes' in rec:
            light[name]=resolve(rec['themes']['light'],'light',(name,)); dark[name]=resolve(rec['themes']['dark'],'dark',(name,))
        elif 'value' in rec:
            simple[name]=resolve(rec['value'],'light',(name,))
        elif 'properties' in rec:
            composite[name]={k:resolve(v,'light',(name,)) for k,v in rec['properties'].items()}

    lines=["/* GENERATED from src/tokens.json — do not edit. */",":root {"]
    for name,val in sorted(simple.items()): lines.append(f"  {css_name(name)}: {val};")
    for name,val in sorted(light.items()): lines.append(f"  {css_name(name)}: {val};")
    for name,props in sorted(composite.items()):
        for prop,val in sorted(props.items()): lines.append(f"  {css_name(name+'.'+prop.replace('_','.'))}: {val};")
    lines.extend([
        "  --gojet-density-row-current: var(--gojet-density-default-row-height);",
        "  --gojet-density-header-current: var(--gojet-density-default-header-height);",
        "}",
        ':root[data-theme="dark"] {'
    ])
    for name,val in sorted(dark.items()): lines.append(f"  {css_name(name)}: {val};")
    lines.extend([
        "}",
        '[data-density="compact"] {',
        "  --gojet-density-row-current: var(--gojet-density-compact-row-height);",
        "  --gojet-density-header-current: var(--gojet-density-compact-header-height);",
        "}",
        '[data-density="default"] {',
        "  --gojet-density-row-current: var(--gojet-density-default-row-height);",
        "  --gojet-density-header-current: var(--gojet-density-default-header-height);",
        "}",
        '[data-density="relaxed"] {',
        "  --gojet-density-row-current: var(--gojet-density-relaxed-row-height);",
        "  --gojet-density-header-current: var(--gojet-density-relaxed-header-height);",
        "}",
    ])
    (GEN/'tokens.css').write_text('\n'.join(lines)+'\n',encoding='utf-8')

    def composite_value(token:str, prop:str):
        try: return composite[token][prop]
        except KeyError as exc: raise ValueError(f"missing composite token {token}.{prop}") from exc

    mobile_cols=composite_value('grid.mobile','columns'); mobile_gap=composite_value('grid.mobile','gutter'); mobile_margin=composite_value('grid.mobile','outer_margin')
    tablet_cols=composite_value('grid.tablet','columns'); tablet_gap=composite_value('grid.tablet','gutter'); tablet_margin=composite_value('grid.tablet','outer_margin')
    desktop_cols=composite_value('grid.desktop','columns'); desktop_gap=composite_value('grid.desktop','gutter'); desktop_margin=composite_value('grid.desktop','outer_margin')
    md=composite_value('breakpoint.md','min_width'); lg=composite_value('breakpoint.lg','min_width')
    responsive=[
        "/* GENERATED responsive primitives from src/tokens.json — do not edit. */",
        ".gj-layout-grid {",
        f"  grid-template-columns: repeat({mobile_cols}, minmax(0, 1fr));",
        f"  gap: {mobile_gap};",
        f"  padding-inline: {mobile_margin};",
        "}",
        f"@media (min-width: {md}) {{",
        "  .gj-layout-grid {",
        f"    grid-template-columns: repeat({tablet_cols}, minmax(0, 1fr));",
        f"    gap: {tablet_gap};",
        f"    padding-inline: {tablet_margin};",
        "  }",
        "}",
        f"@media (min-width: {lg}) {{",
        "  .gj-layout-grid {",
        f"    grid-template-columns: repeat({desktop_cols}, minmax(0, 1fr));",
        f"    gap: {desktop_gap};",
        f"    padding-inline: {desktop_margin};",
        "  }",
        "}",
    ]
    (GEN/'responsive.css').write_text('\n'.join(responsive)+'\n',encoding='utf-8')

    runtime={"simple":simple,"light":light,"dark":dark,"composite":composite}
    ts=(
        "// GENERATED from src/tokens.json — do not edit.\n"
        f"export const TOKEN_AUTHORITY = {json.dumps(data['authority'])} as const;\n"
        f"export const TOKEN_COUNT = {len(tokens)} as const;\n"
        "export const TOKENS = "+json.dumps(runtime,ensure_ascii=False,indent=2)+" as const;\n"
        "export type GeneratedTokens = typeof TOKENS;\n"
    )
    (GEN/'tokens.ts').write_text(ts,encoding='utf-8')
    (GEN/'design-variables.json').write_text(json.dumps({"authority":data['authority'],"token_count":len(tokens),"tokens":runtime},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(f"generated {len(tokens)} canonical token records")
    return 0
if __name__=='__main__': sys.exit(main())
