#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import platform
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

ROOT = Path(__file__).resolve().parents[2]
P03 = ROOT / "artifacts/v10/P03"
RESULTS = P03 / "results"
COMPONENTS = P03 / "components"
G2 = ROOT / "artifacts/v10/gates/G2/design-system"
TOKENS = ROOT / "frontend/packages/tokens/src/tokens.json"
GEN = ROOT / "frontend/packages/tokens/generated"
UI = ROOT / "frontend/packages/ui/src"
CATALOG = ROOT / "frontend/packages/ui/evidence/component-catalog.json"
DS = ROOT / "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md"
AUTHORITY = "GJ-V10-DS-GREENFIELD-2026-08-20"

CASES: list[tuple[str, str]] = [
    ("P03-T001", "canonical-token-sync-and-reproducibility"),
    ("P03-T002", "token-authority-schema-and-normalization"),
    ("P03-T003", "raw-visual-value-lint"),
    ("P03-T004", "light-dark-and-theme-resolution"),
    ("P03-T005", "focus-and-keyboard-contract"),
    ("P03-T006", "wcag-contrast-report"),
    ("P03-T007", "non-color-state-semantics"),
    ("P03-T008", "density-and-responsive-foundation"),
    ("P03-T009", "motion-and-reduced-motion"),
    ("P03-T010", "component-capture-evidence"),
]


def now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def rel(path: Path) -> str:
    return str(path.relative_to(ROOT)).replace("\\", "/")


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(args: list[str], check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, cwd=ROOT, text=True, capture_output=True, check=check)


def load_no_duplicates(path: Path) -> Any:
    def hook(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        out: dict[str, Any] = {}
        for key, value in pairs:
            if key in out:
                raise ValueError(f"duplicate JSON key: {key}")
            out[key] = value
        return out
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=hook)


def record(case_id: str, ok: bool, errors: list[str], details: dict[str, Any]) -> bool:
    write_json(
        RESULTS / f"{case_id}.json",
        {
            "case_id": case_id,
            "name": dict(CASES)[case_id],
            "status": "PASS" if ok else "FAIL",
            "errors": errors,
            "details": details,
            "recorded_at": now(),
        },
    )
    return ok


def token_runtime() -> dict[str, Any]:
    return json.loads((GEN / "design-variables.json").read_text(encoding="utf-8"))["tokens"]


def runtime_value(name: str, theme: str) -> Any:
    runtime = token_runtime()
    themed = runtime[theme]
    if name in themed:
        return themed[name]
    if name in runtime["simple"]:
        return runtime["simple"][name]
    raise KeyError(name)


def t001() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    before = {rel(p): sha256(p) for p in [TOKENS, GEN / "tokens.css", GEN / "tokens.ts", GEN / "design-variables.json", GEN / "responsive.css"] if p.exists()}
    for command in ([sys.executable, "scripts/p03/extract_tokens.py"], [sys.executable, "scripts/p03/generate_tokens.py"]):
        result = run(list(command), check=False)
        if result.returncode:
            errors.append(f"command failed: {' '.join(command)}: {result.stderr.strip()}")
    tracked = [
        "frontend/packages/tokens/src/tokens.json",
        "frontend/packages/tokens/generated/tokens.css",
        "frontend/packages/tokens/generated/tokens.ts",
        "frontend/packages/tokens/generated/design-variables.json",
        "frontend/packages/tokens/generated/responsive.css",
    ]
    diff = run(["git", "diff", "--", *tracked], check=False).stdout
    if diff.strip():
        errors.append("canonical/generated token artifacts are not reproducible from the checked-in sources")
    after = {rel(p): sha256(p) for p in [TOKENS, GEN / "tokens.css", GEN / "tokens.ts", GEN / "design-variables.json", GEN / "responsive.css"] if p.exists()}
    if before and before != after:
        errors.append("token artifact hashes changed during reproducibility check")
    return not errors, errors, {"artifact_hashes": after, "git_diff_bytes": len(diff.encode())}


def t002() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    try:
        data = load_no_duplicates(TOKENS)
    except Exception as exc:
        return False, [str(exc)], {}
    tokens = data.get("tokens", {})
    if data.get("authority") != AUTHORITY:
        errors.append("tokens.json authority mismatch")
    if data.get("generated_from") != "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md":
        errors.append("tokens.json source path mismatch")
    if data.get("token_count") != len(tokens):
        errors.append("token_count does not equal number of token records")
    if len(tokens) < 200:
        errors.append(f"implausibly small canonical token inventory: {len(tokens)}")
    bad_names = [name for name in tokens if not re.fullmatch(r"[a-z][a-z0-9.-]*", name)]
    if bad_names:
        errors.append("non-canonical token names: " + ", ".join(bad_names[:20]))
    color_errors: list[str] = []
    for name, rec in tokens.items():
        values: list[Any] = []
        if "value" in rec:
            values.append(rec["value"])
        if "themes" in rec:
            values.extend(rec["themes"].values())
        if name.startswith("color."):
            for value in values:
                if isinstance(value, str) and value.startswith("#") and not re.fullmatch(r"#[0-9A-F]{6}", value):
                    color_errors.append(f"{name}={value}")
    if color_errors:
        errors.append("primitive color format is not canonical uppercase six-digit hex: " + ", ".join(color_errors[:20]))
    generated_ts = (GEN / "tokens.ts").read_text(encoding="utf-8") if (GEN / "tokens.ts").exists() else ""
    if f'export const TOKEN_AUTHORITY = "{AUTHORITY}" as const;' not in generated_ts:
        errors.append("generated TypeScript authority constant missing")
    if f"export const TOKEN_COUNT = {len(tokens)} as const;" not in generated_ts:
        errors.append("generated TypeScript token count missing")
    return not errors, errors, {"token_count": len(tokens), "authority": data.get("authority"), "bad_name_count": len(bad_names)}


def governed_files() -> list[Path]:
    out: list[Path] = []
    for root in [ROOT / "frontend/apps", ROOT / "frontend/packages/ui/src"]:
        if not root.exists():
            continue
        for path in root.rglob("*"):
            if path.is_file() and path.suffix in {".css", ".ts", ".tsx", ".js", ".jsx"}:
                out.append(path)
    return sorted(out)


def t003() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    findings: list[dict[str, str]] = []
    patterns = {
        "hex": re.compile(r"#[0-9A-Fa-f]{3,8}\b"),
        "rgb": re.compile(r"\brgba?\s*\("),
        "length-or-duration": re.compile(r"(?<![-\w])(?:\d+(?:\.\d+)?|\.\d+)(?:px|ms)\b"),
    }
    for path in governed_files():
        body = path.read_text(encoding="utf-8")
        for kind, pattern in patterns.items():
            for match in pattern.finditer(body):
                findings.append({"path": rel(path), "kind": kind, "value": match.group(0)})
        if path.suffix == ".css":
            for line in body.splitlines():
                stripped = line.strip()
                if stripped.startswith("z-index:") and "var(" not in stripped:
                    findings.append({"path": rel(path), "kind": "raw-z-index", "value": stripped})
                if stripped.startswith("border-radius:") and "var(" not in stripped:
                    findings.append({"path": rel(path), "kind": "raw-radius", "value": stripped})
                if stripped.startswith("box-shadow:") and "var(" not in stripped:
                    findings.append({"path": rel(path), "kind": "raw-shadow", "value": stripped})
    if findings:
        errors.append(f"unregistered raw visual values found in governed app/UI paths: {len(findings)}")
    write_json(G2 / "token-lint.json", {"authority": AUTHORITY, "generated_at": now(), "result": "pass" if not findings else "fail", "finding_count": len(findings), "findings": findings})
    return not errors, errors, {"scanned_files": len(governed_files()), "finding_count": len(findings), "sample": findings[:20]}


def t004() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    data = json.loads(TOKENS.read_text(encoding="utf-8"))
    themed = {name: rec for name, rec in data["tokens"].items() if "themes" in rec}
    incomplete = [name for name, rec in themed.items() if set(rec["themes"]) != {"light", "dark"}]
    if incomplete:
        errors.append("incomplete light/dark token mappings: " + ", ".join(incomplete[:20]))
    css = (GEN / "tokens.css").read_text(encoding="utf-8")
    if ':root[data-theme="dark"]' not in css:
        errors.append("generated dark-theme selector missing")
    theme = (UI / "theme.ts").read_text(encoding="utf-8")
    for needle in ["ThemePreference = 'light' | 'dark' | 'system'", "prefers-color-scheme: dark", "dataset.theme", "localStorage", "matchMedia"]:
        if needle not in theme:
            errors.append(f"theme runtime contract missing: {needle}")
    return not errors, errors, {"themed_token_count": len(themed), "incomplete_count": len(incomplete)}


def t005() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    component = (UI / "components.tsx").read_text(encoding="utf-8")
    styles = (UI / "styles.css").read_text(encoding="utf-8")
    required_keys = ["ArrowLeft", "ArrowRight", "ArrowDown", "ArrowUp", "Home", "End", "Enter", "Escape"]
    missing_keys = [key for key in required_keys if key not in component]
    if missing_keys:
        errors.append("keyboard contract keys missing from component implementation: " + ", ".join(missing_keys))
    for needle in [":focus-visible", "--gojet-focus-ring-width", "--gojet-focus-ring-offset", "--gojet-focus-backplate"]:
        if needle not in styles:
            errors.append(f"focus-visible contract missing: {needle}")
    positive_tabindex = [int(value) for value in re.findall(r"tabIndex=\{(\d+)\}", component) if int(value) > 0]
    if positive_tabindex:
        errors.append("positive tabindex is prohibited")
    semantic_needles = ["aria-describedby", "aria-invalid", "aria-selected", "aria-current", "aria-controls", "aria-activedescendant", "role=\"combobox\"", "role=\"tab\""]
    missing_semantics = [needle for needle in semantic_needles if needle not in component]
    if missing_semantics:
        errors.append("ARIA contract markers missing: " + ", ".join(missing_semantics))
    trace = {
        "authority": AUTHORITY,
        "generated_at": now(),
        "required_keys": required_keys,
        "missing_keys": missing_keys,
        "positive_tabindex": positive_tabindex,
        "focus_visible_css": ":focus-visible" in styles,
        "aria_markers": {needle: needle in component for needle in semantic_needles},
    }
    write_json(G2 / "keyboard-trace.json", trace)
    return not errors, errors, trace


def hex_rgb(value: str) -> tuple[float, float, float]:
    if not re.fullmatch(r"#[0-9A-Fa-f]{6}", value):
        raise ValueError(f"unsupported contrast color {value}")
    return tuple(int(value[i:i+2], 16) / 255 for i in (1, 3, 5))  # type: ignore[return-value]


def luminance(value: str) -> float:
    def channel(c: float) -> float:
        return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = hex_rgb(value)
    return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)


def contrast(a: str, b: str) -> float:
    l1, l2 = luminance(a), luminance(b)
    high, low = max(l1, l2), min(l1, l2)
    return (high + 0.05) / (low + 0.05)


def t006() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    pairs: list[tuple[str, str, float]] = [
        ("text.primary", "surface.default", 4.5),
        ("text.secondary", "surface.default", 4.5),
        ("text.muted", "surface.default", 4.5),
        ("text.link", "surface.default", 4.5),
        ("action.on-primary", "action.primary", 4.5),
        ("action.on-destructive", "action.destructive", 4.5),
        ("action.selected-fg", "action.selected-bg", 4.5),
        ("status.success.fg", "status.success.bg", 4.5),
        ("status.warning.fg", "status.warning.bg", 4.5),
        ("status.danger.fg", "status.danger.bg", 4.5),
        ("status.info.fg", "status.info.bg", 4.5),
        ("focus.ring", "surface.default", 3.0),
        ("border.default", "surface.default", 3.0),
    ]
    rows: list[dict[str, Any]] = []
    for theme in ["light", "dark"]:
        for fg_name, bg_name, minimum in pairs:
            try:
                fg = str(runtime_value(fg_name, theme)); bg = str(runtime_value(bg_name, theme)); ratio = contrast(fg, bg)
                ok = ratio + 1e-9 >= minimum
                rows.append({"theme": theme, "foreground": fg_name, "background": bg_name, "foreground_value": fg, "background_value": bg, "ratio": round(ratio, 3), "minimum": minimum, "result": "pass" if ok else "fail"})
                if not ok:
                    errors.append(f"contrast {theme} {fg_name}/{bg_name}={ratio:.3f} < {minimum}")
            except Exception as exc:
                rows.append({"theme": theme, "foreground": fg_name, "background": bg_name, "minimum": minimum, "result": "error", "error": str(exc)})
                errors.append(f"contrast pair could not be evaluated: {theme} {fg_name}/{bg_name}: {exc}")
    report = {"authority": AUTHORITY, "generated_at": now(), "result": "pass" if not errors else "fail", "pairs": rows}
    write_json(G2 / "contrast-report.json", report)
    return not errors, errors, {"evaluated_pairs": len(rows), "failures": len(errors)}


def t007() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    source = (UI / "components.tsx").read_text(encoding="utf-8")
    required_icons = ["CircleAlert", "CheckCircle2", "AlertTriangle", "Info"]
    missing_icons = [name for name in required_icons if name not in source]
    if missing_icons:
        errors.append("non-color state icons missing: " + ", ".join(missing_icons))
    for marker in ["data-status=\"invalid\"", "data-status=\"success\"", "role={variant === 'danger' ? 'alert' : 'status'}", "aria-live=\"polite\""]:
        if marker not in source:
            errors.append(f"state semantic marker missing: {marker}")
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    if catalog.get("authority") != AUTHORITY:
        errors.append("component catalog authority mismatch")
    categories = {item.get("category") for item in catalog.get("components", [])}
    expected = {"controls", "overlay", "navigation", "data", "feedback", "layout"}
    if not expected.issubset(categories):
        errors.append("component catalog does not cover all G2 foundation categories")
    return not errors, errors, {"icons": required_icons, "categories": sorted(str(x) for x in categories), "component_count": len(catalog.get("components", []))}


def t008() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    responsive = (GEN / "responsive.css").read_text(encoding="utf-8") if (GEN / "responsive.css").exists() else ""
    css = (GEN / "tokens.css").read_text(encoding="utf-8") if (GEN / "tokens.css").exists() else ""
    runtime = token_runtime()
    composite = runtime["composite"]
    expected_breakpoints = [str(composite["breakpoint.md"]["min_width"]), str(composite["breakpoint.lg"]["min_width"])]
    for value in expected_breakpoints:
        if f"@media (min-width: {value})" not in responsive:
            errors.append(f"generated responsive primitive missing breakpoint {value}")
    for density in ["compact", "default", "relaxed"]:
        if f'[data-density="{density}"]' not in css:
            errors.append(f"generated density selector missing: {density}")
    if ".gj-layout-grid" not in responsive:
        errors.append("generated responsive grid primitive missing")
    return not errors, errors, {"breakpoints": expected_breakpoints, "density_modes": ["compact", "default", "relaxed"]}


def t009() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    theme = (UI / "theme.ts").read_text(encoding="utf-8")
    styles = (UI / "styles.css").read_text(encoding="utf-8")
    spec = DS.read_text(encoding="utf-8")
    for needle in ["prefers-reduced-motion: reduce", "subscribeReducedMotion", "matchMedia(REDUCED_MOTION_QUERY)"]:
        haystack = styles if needle == "prefers-reduced-motion: reduce" else theme
        if needle not in haystack:
            errors.append(f"reduced-motion contract missing: {needle}")
    if "--gojet-motion-duration-reduced" not in styles:
        errors.append("reduced-motion CSS does not use canonical reduced duration token")
    if "`motion.duration.reduced` | 120ms" not in spec or "`motion.duration.path` | 6000ms" not in spec:
        errors.append("approved Design System motion values not found")
    forbidden = []
    for path in governed_files():
        if path.suffix != ".css":
            continue
        body = path.read_text(encoding="utf-8")
        for match in re.finditer(r"animation\s*:\s*([^;]+)", body):
            if "none" not in match.group(1):
                forbidden.append({"path": rel(path), "value": match.group(1).strip()})
    if forbidden:
        errors.append("nonessential continuous CSS animation exists in governed UI paths")
    write_json(G2 / "reduced-motion.json", {"authority": AUTHORITY, "generated_at": now(), "result": "pass" if not errors else "fail", "continuous_animation_findings": forbidden, "js_media_store": "subscribeReducedMotion" in theme})
    return not errors, errors, {"continuous_animation_findings": forbidden}


def parse_viewport(value: str) -> tuple[int, int]:
    match = re.fullmatch(r"(\d+)×(\d+)", value)
    if not match:
        raise ValueError(value)
    return int(match.group(1)), int(match.group(2))


def story_html(theme: str, locale: str) -> str:
    token_css = (GEN / "tokens.css").read_text(encoding="utf-8")
    responsive_css = (GEN / "responsive.css").read_text(encoding="utf-8")
    ui_css = (UI / "styles.css").read_text(encoding="utf-8")
    ui_css = re.sub(r'^@import\s+"[^"]+";\s*', '', ui_css)
    zh = locale == "zh-cn"
    title = "设计系统组件证据" if zh else "Design System component evidence"
    font_family = "--gojet-font-family-cjk" if zh else "--gojet-font-family-latin"
    save = "保存更改" if zh else "Save changes"
    remove = "删除域名" if zh else "Delete domain"
    label = "域名" if zh else "Domain"
    error = "请输入有效域名" if zh else "Enter a valid domain"
    success = "配置已保存" if zh else "Configuration saved"
    warning = "此资源正在等待安全复核" if zh else "This resource is awaiting safety review"
    empty = "暂无资源" if zh else "No resources yet"
    return f'''<!doctype html><html lang="{'zh-CN' if zh else 'en'}" data-theme="{theme}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>{token_css}\n{responsive_css}\n{ui_css}\nbody{{margin:var(--gojet-space-0);padding:var(--gojet-space-6);background:var(--gojet-surface-canvas);color:var(--gojet-text-primary);font-family:var({font_family});}} .evidence{{display:grid;gap:var(--gojet-space-6);max-inline-size:var(--gojet-content-website-section);margin-inline:auto}} .row{{display:flex;flex-wrap:wrap;gap:var(--gojet-space-3)}} .stack{{display:grid;gap:var(--gojet-space-3)}} .gj-sr-only{{position:absolute;inline-size:var(--gojet-border-width-default);block-size:var(--gojet-border-width-default);overflow:hidden;clip-path:inset(50%);white-space:nowrap}}</style></head><body><main class="evidence"><h1>{title}</h1><section class="stack"><h2>Controls</h2><div class="row"><button id="focus-target" class="gj-button" data-variant="primary">{save}</button><button class="gj-button" data-variant="secondary">Secondary</button><button class="gj-button" data-variant="destructive">{remove}</button><button class="gj-button" data-variant="primary" aria-busy="true" disabled>◌ <span>{save}</span><span class="gj-sr-only">in progress</span></button></div><div class="gj-field"><label class="gj-field__label" for="evidence-domain">{label}</label><input id="evidence-domain" class="gj-input" aria-invalid="true" aria-describedby="evidence-domain--error" value="example" readonly><div id="evidence-domain--error" class="gj-field__status" data-status="invalid" role="alert">⚠ <span>{error}</span></div></div></section><section class="stack"><h2>Feedback</h2><div class="gj-message" data-variant="success" role="status">✓ <span>{success}</span></div><div class="gj-message" data-variant="warning" role="status">⚠ <span>{warning}</span></div></section><section class="stack"><h2>Navigation & data</h2><div class="gj-tabs" role="tablist" aria-label="Evidence tabs"><button class="gj-tab" role="tab" aria-selected="true">Overview</button><button class="gj-tab" role="tab" aria-selected="false">Analytics</button></div><div class="gj-table-region" tabindex="0"><table class="gj-table"><caption>Resources</caption><thead><tr><th scope="col">Name</th><th scope="col">Status</th></tr></thead><tbody><tr><td>go.jet/demo</td><td>Active ✓</td></tr><tr><td>go.jet/review</td><td>Review ⚠</td></tr></tbody></table></div></section><section class="gj-empty"><div class="gj-empty__icon">◇</div><h2>{empty}</h2><p>{'创建第一个资源以开始。' if zh else 'Create the first resource to get started.'}</p><button class="gj-button" data-variant="primary">{'创建资源' if zh else 'Create resource'}</button><a class="gj-link" href="#docs">{'查看文档' if zh else 'View documentation'}</a></section><dialog id="evidence-dialog" class="gj-dialog"><div class="gj-dialog__body"><div class="gj-dialog__header"><div><h2>{'确认操作' if zh else 'Confirm action'}</h2><p>{'此操作会更改持久化状态。' if zh else 'This action changes persisted state.'}</p></div><button id="dialog-close" class="gj-dialog__close" aria-label="Close">×</button></div><div class="gj-message" data-variant="warning" role="status">⚠ <span>{warning}</span></div><div class="gj-dialog__actions"><button class="gj-button" data-variant="secondary">{'取消' if zh else 'Cancel'}</button><button class="gj-button" data-variant="destructive">{remove}</button></div></div></dialog></main><script>document.getElementById('evidence-dialog')?.showModal();document.getElementById('dialog-close')?.focus();</script></body></html>'''


def chrome_binary() -> str | None:
    for name in ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser"]:
        found = shutil.which(name)
        if found:
            return found
    return None


def t010() -> tuple[bool, list[str], dict[str, Any]]:
    errors: list[str] = []
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    if len(catalog.get("components", [])) < 20:
        errors.append("component catalog is incomplete")
    chrome = chrome_binary()
    if not chrome:
        errors.append("headless Chrome/Chromium is unavailable for canonical component captures")
        return False, errors, {"chrome": None}
    runtime = token_runtime()["composite"]
    viewports = {
        "desktop": parse_viewport(str(runtime["viewport.desktop"]["dimensions"])),
        "tablet": parse_viewport(str(runtime["viewport.tablet"]["dimensions"])),
        "mobile": parse_viewport(str(runtime["viewport.mobile"]["dimensions"])),
    }
    captures: list[dict[str, Any]] = []
    COMPONENTS.mkdir(parents=True, exist_ok=True)
    for theme in ["light", "dark"]:
        for locale in ["en", "zh-cn"]:
            html_path = COMPONENTS / f"story__{theme}__{locale}.html"
            html_path.write_text(story_html(theme, locale), encoding="utf-8")
            for viewport, (width, height) in viewports.items():
                stem = f"gjv10__workspace__p03-components__default__{theme}__{locale}__{viewport}"
                png = COMPONENTS / f"{stem}.png"
                command = [
                    chrome,
                    "--headless=new",
                    "--disable-gpu",
                    "--no-sandbox",
                    "--hide-scrollbars",
                    f"--window-size={width},{height}",
                    f"--screenshot={png}",
                    html_path.resolve().as_uri(),
                ]
                result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
                ok = result.returncode == 0 and png.exists() and png.stat().st_size > 1000
                if not ok:
                    errors.append(f"component capture failed: {stem}: {result.stderr.strip()}")
                manifest = {
                    "document_id": AUTHORITY,
                    "branch": os.environ.get("GITHUB_HEAD_REF") or os.environ.get("GITHUB_REF_NAME") or "detached",
                    "implementation_commit": run(["git", "rev-parse", "HEAD"]).stdout.strip(),
                    "build_id": "P03-G2",
                    "browser": subprocess.run([chrome, "--version"], text=True, capture_output=True).stdout.strip(),
                    "os": platform.platform(),
                    "viewport_token": f"viewport.{viewport}",
                    "theme": theme,
                    "locale": locale,
                    "case_id": "p03-components",
                    "state": "default",
                    "captured_at": now(),
                    "result": "pass" if ok else "fail",
                }
                write_json(COMPONENTS / f"{stem}__manifest.json", manifest)
                captures.append({"path": rel(png), "theme": theme, "locale": locale, "viewport": viewport, "bytes": png.stat().st_size if png.exists() else 0, "result": manifest["result"]})
    write_json(G2 / "component-captures.json", {"authority": AUTHORITY, "generated_at": now(), "result": "pass" if not errors else "fail", "captures": captures})
    return not errors, errors, {"chrome": chrome, "capture_count": len(captures), "captures": captures}


FUNCS: dict[str, Callable[[], tuple[bool, list[str], dict[str, Any]]]] = {
    "P03-T001": t001,
    "P03-T002": t002,
    "P03-T003": t003,
    "P03-T004": t004,
    "P03-T005": t005,
    "P03-T006": t006,
    "P03-T007": t007,
    "P03-T008": t008,
    "P03-T009": t009,
    "P03-T010": t010,
}


def emit_evidence() -> tuple[int, int]:
    commit = run(["git", "rev-parse", "HEAD"]).stdout.strip()
    branch = os.environ.get("GITHUB_HEAD_REF") or os.environ.get("GITHUB_REF_NAME") or run(["git", "branch", "--show-current"], check=False).stdout.strip() or "detached"
    write_json(P03 / "environment.json", {"generated_at": now(), "platform": platform.platform(), "python": platform.python_version(), "chrome": chrome_binary()})
    write_json(P03 / "source.json", {"repository": "Techshrr/GoJet", "branch": branch, "implementation_commit": commit, "specification_ids": ["GJ-V10-MP-GREENFIELD-2026-08-20", AUTHORITY, "GJ-V10-IA-GREENFIELD-2026-08-20"]})
    rows = [json.loads((RESULTS / f"{case_id}.json").read_text(encoding="utf-8")) for case_id, _ in CASES]
    passed = sum(row["status"] == "PASS" for row in rows)
    (P03 / "commands.log").write_text("\n".join(f"{row['case_id']}\t{row['status']}" for row in rows) + "\n", encoding="utf-8")
    candidates = [P03 / "environment.json", P03 / "source.json", P03 / "commands.log", P03 / "test-plan.json", P03 / "review.md", G2 / "token-lint.json", G2 / "contrast-report.json", G2 / "keyboard-trace.json", G2 / "reduced-motion.json", G2 / "component-captures.json"]
    candidates += [RESULTS / f"{case_id}.json" for case_id, _ in CASES]
    candidates += list(COMPONENTS.glob("*.png")) + list(COMPONENTS.glob("*__manifest.json"))
    files = [{"path": rel(path), "sha256": sha256(path)} for path in sorted(set(candidates)) if path.exists()]
    write_json(P03 / "evidence-index.json", {"schema_version": 1, "node": "P03", "gate": "G2", "implementation_commit": commit, "generated_at": now(), "results": {"passed": passed, "failed": len(rows) - passed, "total": len(rows)}, "files": files})
    return passed, len(rows)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", choices=list(FUNCS))
    args = parser.parse_args()
    RESULTS.mkdir(parents=True, exist_ok=True)
    G2.mkdir(parents=True, exist_ok=True)
    failed = 0
    selected = [args.case] if args.case else [case_id for case_id, _ in CASES]
    for case_id in selected:
        assert case_id is not None
        try:
            ok, errors, details = FUNCS[case_id]()
        except Exception as exc:
            ok, errors, details = False, [f"validator exception: {type(exc).__name__}: {exc}"], {}
        record(case_id, ok, errors, details)
        failed += 0 if ok else 1
        print(f"{case_id}: {'PASS' if ok else 'FAIL'}")
        for error in errors:
            print("  - " + error)
    if args.case:
        return 0 if failed == 0 else 1
    passed, total = emit_evidence()
    print(f"P03 summary: {passed}/{total} PASS")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
