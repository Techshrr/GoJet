#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P09 = ROOT / "artifacts" / "v10" / "P09"
RESULTS = P09 / "results"
CLAMAV = P09 / "clamav"
BROWSER = P09 / "browser"
CAPTURES = P09 / "captures"
CONTRACT = P09 / "contract" / "contract.json"
PRODUCERS = P09 / "evidence-producer-manifest.json"
PLAN = P09 / "test-plan.json"
INDEX = P09 / "evidence-index.json"
T026 = RESULTS / "P09-T026.json"
INPUT_CASES = tuple(f"P09-T{i:03d}" for i in range(1, 26))
EXPECTED_CASES = tuple(f"P09-T{i:03d}" for i in range(1, 28))
REQUIRED_PRODUCERS = (
    "P09 Files Contract",
    "P09 Real Files and ClamAV Integration",
    "P09 Files Health and Installer Preflight",
    "P09 Workspace Files Browser",
)
CANONICAL_VIEWPORTS = {
    "desktop": {"width": 1440, "height": 900},
    "tablet": {"width": 1024, "height": 768},
    "mobile": {"width": 390, "height": 844},
}
SAFETY = {
    "quarantined": ("PackageLock", "File quarantined"),
    "scanning": ("LoaderCircle", "Security scan in progress"),
    "safe": ("ShieldCheck", "File is safe to publish"),
    "blocked": ("ShieldX", "File blocked"),
    "scan_error": ("TriangleAlert", "Scan unavailable; file remains private"),
}
EXPECTED_CAPTURES = {
    "P09-T023-desktop-workspace.png", "P09-T023-desktop-public.png",
    "P09-T023-tablet-workspace.png", "P09-T023-tablet-public.png",
    "P09-T023-mobile-workspace.png", "P09-T023-mobile-public.png",
    "P09-T024-admin-storage.png", "P09-T024-installer-hard-failure.png",
    "P09-T025-320-reduced-motion.png",
}


def head() -> str:
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def load(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(131072), b""):
            h.update(chunk)
    return h.hexdigest()


def req(ok: bool, message: str, errors: list[str]) -> None:
    if not ok:
        errors.append(message)


def nested(value: Any, *keys: str) -> Any:
    for key in keys:
        if not isinstance(value, dict):
            return None
        value = value.get(key)
    return value


def case_path(case_id: str) -> Path:
    number = int(case_id[-3:])
    if 5 <= number <= 10:
        return CLAMAV / f"{case_id}.json"
    if 21 <= number <= 25:
        return BROWSER / f"{case_id}.json"
    return RESULTS / f"{case_id}.json"


def details(data: dict[str, Any]) -> dict[str, Any]:
    value = data.get("observations") if "observations" in data else data.get("details")
    return value if isinstance(value, dict) else {}

