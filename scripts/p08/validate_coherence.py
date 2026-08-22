#!/usr/bin/env python3
"""GoJet V10 P08 exact-head QR evidence coherence validator for P08-T015."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P08 = ROOT / "artifacts" / "v10" / "P08"
RESULTS = P08 / "results"
SCANNER = P08 / "scanner"
PLAN = P08 / "test-plan.json"
INDEX = P08 / "evidence-index.json"
T015 = RESULTS / "P08-T015.json"

EXPECTED_CASES = tuple(f"P08-T{number:03d}" for number in range(1, 17))
INPUT_CASES = tuple(f"P08-T{number:03d}" for number in range(1, 15))
CANONICAL_VIEWPORTS = {
    "desktop": {"width": 1440, "height": 900},
    "tablet": {"width": 1024, "height": 768},
    "mobile": {"width": 390, "height": 844},
}


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def exact_head() -> str:
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(128 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def case_path(case_id: str) -> Path:
    if case_id == "P08-T003":
        return SCANNER / "P08-T003.json"
    return RESULTS / f"{case_id}.json"


def nested(value: Any, *keys: str) -> Any:
    current = value
    for key in keys:
        if not isinstance(current, dict):
            return None
        current = current.get(key)
    return current


def validate_test_plan(errors: list[str]) -> dict[str, Any]:
    require(PLAN.is_file(), f"missing test plan: {PLAN}", errors)
    if not PLAN.is_file():
        return {}
    try:
        plan = load_json(PLAN)
    except Exception as exc:
        errors.append(f"invalid test plan JSON: {exc}")
        return {}
    require(isinstance(plan, dict), "test plan root must be object", errors)
    if not isinstance(plan, dict):
        return {}
    ids = tuple(case.get("id") for case in plan.get("cases", []) if isinstance(case, dict))
    require(ids == EXPECTED_CASES, f"test-plan case IDs/order mismatch: {ids}", errors)
    require(plan.get("base_integration_commit") == "04941afc59db763e6c7db8a67721dea542c72a43", "P08 base integration SHA drift", errors)
    closure = plan.get("closure_contract")
    require(isinstance(closure, dict), "test-plan closure_contract missing", errors)
    if isinstance(closure, dict):
        require(closure.get("same_exact_head_required") is True, "closure must require same exact head", errors)
        require(closure.get("required_case_range") == "P08-T001..P08-T016", "closure case range mismatch", errors)
    return plan


def load_input_cases(head: str, errors: list[str]) -> tuple[dict[str, dict[str, Any]], list[dict[str, Any]]]:
    payloads: dict[str, dict[str, Any]] = {}
    entries: list[dict[str, Any]] = []
    for case_id in INPUT_CASES:
        path = case_path(case_id)
        require(path.is_file(), f"missing evidence: {path}", errors)
        if not path.is_file():
            continue
        try:
            data = load_json(path)
        except Exception as exc:
            errors.append(f"invalid JSON {path}: {exc}")
            continue
        require(isinstance(data, dict), f"{case_id} root must be object", errors)
        if not isinstance(data, dict):
            continue
        require(data.get("case_id") == case_id, f"{case_id} payload case_id={data.get('case_id')}", errors)
        require(data.get("status") == "PASS", f"{case_id} status={data.get('status')} expected=PASS", errors)
        require(data.get("implementation_commit") == head, f"{case_id} implementation_commit={data.get('implementation_commit')} expected={head}", errors)
        case_errors = data.get("errors")
        require(isinstance(case_errors, list) and not case_errors, f"{case_id} errors={case_errors}", errors)
        payloads[case_id] = data
        entries.append({
            "case_id": case_id,
            "path": str(path.relative_to(ROOT)),
            "sha256": sha256(path),
            "status": data.get("status"),
            "implementation_commit": data.get("implementation_commit"),
        })
    require(tuple(entry["case_id"] for entry in entries) == INPUT_CASES, "input evidence case order/set mismatch", errors)
    return payloads, entries


def validate_scanner_chain(head: str, payloads: dict[str, dict[str, Any]], errors: list[str]) -> dict[str, Any]:
    t001 = payloads.get("P08-T001", {})
    t003 = payloads.get("P08-T003", {})
    fixture = nested(t001, "details", "scanner_fixture")
    require(isinstance(fixture, dict), "P08-T001 scanner_fixture missing", errors)
    if not isinstance(fixture, dict):
        return {}

    require(fixture.get("implementation_commit") == head, f"T001 scanner fixture head={fixture.get('implementation_commit')} expected={head}", errors)
    public_url = fixture.get("public_url")
    require(isinstance(public_url, str) and public_url.startswith("https://"), f"T001 public_url invalid: {public_url}", errors)
    require(t003.get("expected_public_url") == public_url, f"T003 expected_public_url={t003.get('expected_public_url')} T001={public_url}", errors)
    require(nested(t003, "redirect_follow", "destination_matches") is True, "T003 decoded URL did not prove live redirect destination match", errors)
    require(nested(t003, "redirect_follow", "status") in (301, 302, 307, 308), f"T003 redirect status invalid: {nested(t003, 'redirect_follow', 'status')}", errors)

    physical: dict[str, Any] = {}
    for fmt in ("png", "svg"):
        path = SCANNER / f"P08-T003-source.{fmt}"
        require(path.is_file(), f"missing scanner source artifact: {path}", errors)
        if not path.is_file():
            continue
        disk_digest = sha256(path)
        fixture_digest = fixture.get(f"{fmt}_sha256")
        scanner_artifact = nested(t003, "artifacts", fmt)
        require(isinstance(scanner_artifact, dict), f"T003 {fmt} artifact record missing", errors)
        if not isinstance(scanner_artifact, dict):
            continue
        require(disk_digest == fixture_digest, f"{fmt} disk digest={disk_digest} T001 fixture={fixture_digest}", errors)
        require(scanner_artifact.get("sha256") == disk_digest, f"T003 {fmt} recorded digest={scanner_artifact.get('sha256')} disk={disk_digest}", errors)
        require(scanner_artifact.get("decoded") == public_url, f"T003 {fmt} decoded={scanner_artifact.get('decoded')} expected={public_url}", errors)
        physical[fmt] = {"path": str(path.relative_to(ROOT)), "sha256": disk_digest, "decoded": scanner_artifact.get("decoded")}

    decoder = str(t003.get("decoder", "")).lower()
    require("zbar" in decoder, f"T003 independent decoder record is not ZBar: {t003.get('decoder')}", errors)
    require("independent" in decoder, f"T003 decoder record does not explicitly state independence: {t003.get('decoder')}", errors)
    require("skip2/go-qrcode" in decoder, f"T003 decoder record does not identify the production encoder boundary: {t003.get('decoder')}", errors)
    return {"public_url": public_url, "qr_id": fixture.get("qr_id"), "source_link_id": fixture.get("source_link_id"), "artifacts": physical, "decoder": t003.get("decoder"), "live_redirect": t003.get("redirect_follow")}


def validate_determinism(payloads: dict[str, dict[str, Any]], errors: list[str]) -> dict[str, Any]:
    formats = nested(payloads.get("P08-T004", {}), "details", "formats")
    require(isinstance(formats, dict), "P08-T004 formats missing", errors)
    if not isinstance(formats, dict):
        return {}
    require(set(formats) == {"png", "svg"}, f"P08-T004 formats={sorted(formats)}", errors)
    for fmt in ("png", "svg"):
        record = formats.get(fmt)
        require(isinstance(record, dict), f"P08-T004 {fmt} record missing", errors)
        if isinstance(record, dict):
            digest = record.get("sha256")
            require(isinstance(digest, str) and len(digest) == 64, f"P08-T004 {fmt} digest invalid: {digest}", errors)
            require(isinstance(record.get("bytes"), int) and record.get("bytes", 0) > 0, f"P08-T004 {fmt} byte length invalid", errors)
    return formats


def validate_product_authority(payloads: dict[str, dict[str, Any]], errors: list[str]) -> dict[str, Any]:
    t001 = payloads.get("P08-T001", {})
    t005 = payloads.get("P08-T005", {})
    t006 = payloads.get("P08-T006", {})
    t007 = payloads.get("P08-T007", {})
    t008 = payloads.get("P08-T008", {})
    t009 = payloads.get("P08-T009", {})
    t010 = payloads.get("P08-T010", {})

    require(nested(t001, "details", "alternate_target_rejected") is True, "T001 did not prove alternate raw target rejection", errors)
    require(nested(t005, "details", "viewer_create") == 403, "T005 viewer create was not server-denied", errors)
    require(nested(t005, "details", "viewer_delete") == 403, "T005 viewer delete was not server-denied", errors)
    require(nested(t006, "details", "denied_status") == 429, "T006 quota denial was not 429", errors)
    require(nested(t006, "details", "active_rows") == 2 and nested(t006, "details", "counter") == 2, "T006 quota denial mutated authoritative count", errors)
    require(nested(t007, "details", "detail_state") == "source-link-review", "T007 review state missing", errors)
    matrix = nested(t008, "details", "risk_matrix")
    require(isinstance(matrix, dict), "T008 risk matrix missing", errors)
    if isinstance(matrix, dict):
        require(matrix.get("block") == 403, "T008 block did not fail closed", errors)
        require(matrix.get("missing") == 409, "T008 missing/pending did not fail closed", errors)
        require(matrix.get("malformed") == 409, "T008 malformed did not fail closed", errors)
        require(matrix.get("stale") == 409, "T008 stale did not fail closed", errors)
        require(matrix.get("custom_domain_denied") == 409, "T008 custom-domain authority downgrade did not deny distribution", errors)
        require(nested(matrix, "changed_fingerprint", "old_allow_still_present") is True, "T008 old fingerprint fixture missing", errors)
        require(nested(matrix, "changed_fingerprint", "new_allow_present") is False, "T008 changed fingerprint unexpectedly retained allow", errors)
    require(nested(t009, "details", "after_delete", "detail") == 410, "T009 deleted detail not 410", errors)
    require(nested(t009, "details", "after_delete", "preview") == 410, "T009 deleted preview not 410", errors)
    require(nested(t009, "details", "after_delete", "download") == 410, "T009 deleted download not 410", errors)
    require(nested(t010, "details", "dependency", "status") == 503, "T010 dependency failure not 503", errors)
    require(nested(t010, "details", "destination_leaked") is False, "T010 destination leakage flag is not false", errors)
    return {"tenant_rbac": "P08-T002/P08-T005", "quota": "P08-T006", "risk": "P08-T007/P08-T008", "delete": "P08-T009", "errors": "P08-T010"}


def validate_browser(payloads: dict[str, dict[str, Any]], errors: list[str]) -> dict[str, Any]:
    t011 = nested(payloads.get("P08-T011", {}), "details")
    t012 = nested(payloads.get("P08-T012", {}), "details")
    t013 = nested(payloads.get("P08-T013", {}), "details")
    t014 = nested(payloads.get("P08-T014", {}), "details")
    for case_id, details in (("P08-T011", t011), ("P08-T012", t012), ("P08-T013", t013), ("P08-T014", t014)):
        require(isinstance(details, dict), f"{case_id} details missing", errors)
    if not all(isinstance(item, dict) for item in (t011, t012, t013, t014)):
        return {}

    observed_list = t011.get("observed_states")
    require(isinstance(observed_list, dict), "T011 observed_states missing", errors)
    if isinstance(observed_list, dict):
        require(set(observed_list) == {"loading", "empty", "create_form", "create_confirmed", "risk_denied", "quota_reached", "error"}, f"T011 state set={sorted(observed_list)}", errors)
    require(t011.get("route_backed") is True, "T011 is not route-backed", errors)
    require(t011.get("request_interception") is False, "T011 used request interception", errors)
    require(nested(t011, "observed_states", "create_form", "raw_destination_input_absent") is True, "T011 create form exposed a raw URL input", errors)
    require(nested(t011, "observed_states", "create_confirmed", "detail_ready") is True, "T011 create did not wait for server-confirmed ready detail", errors)

    observed_detail = t012.get("observed_states")
    require(isinstance(observed_detail, dict), "T012 observed_states missing", errors)
    if isinstance(observed_detail, dict):
        required = {"loading", "ready", "download", "link_detail", "review", "block", "delete_action", "deleted", "error"}
        require(set(observed_detail) == required, f"T012 state/action set={sorted(observed_detail)}", errors)
    require(t012.get("real_preview_download_delete") is True, "T012 real preview/download/delete flag missing", errors)
    require(t012.get("link_detail_same_authority") is True, "T012 Link Detail did not prove same QR authority", errors)
    require(nested(t012, "observed_states", "link_detail", "placeholder_absent") is True, "T012 old P05 QR placeholder remains", errors)

    viewport_evidence = t013.get("canonical_viewports")
    require(isinstance(viewport_evidence, dict), "T013 canonical_viewports missing", errors)
    if isinstance(viewport_evidence, dict):
        require(set(viewport_evidence) == set(CANONICAL_VIEWPORTS), f"T013 viewport keys={sorted(viewport_evidence)}", errors)
        for name, expected in CANONICAL_VIEWPORTS.items():
            record = viewport_evidence.get(name)
            require(isinstance(record, dict), f"T013 {name} record missing", errors)
            if not isinstance(record, dict):
                continue
            require(record.get("viewport") == expected, f"T013 {name} viewport={record.get('viewport')} expected={expected}", errors)
            for stage in ("list_create", "detail_preview", "risk_denied", "post_delete"):
                layout = record.get(stage)
                require(isinstance(layout, dict), f"T013 {name} {stage} layout missing", errors)
                if isinstance(layout, dict):
                    require(layout.get("root_overflow_px") == 0, f"T013 {name} {stage} root overflow", errors)
                    require(layout.get("body_overflow_px") == 0, f"T013 {name} {stage} body overflow", errors)
                    require(layout.get("clipped_required_controls_or_text") == [], f"T013 {name} {stage} clipped content", errors)
                    require(layout.get("unnamed_visible_controls") == [], f"T013 {name} {stage} unnamed controls", errors)
    require(t013.get("root_body_overflow_zero") is True and t013.get("clipped_required_content") is False, "T013 summary layout flags invalid", errors)

    require(t014.get("accessible_names_roles_values") is True, "T014 accessible name/role/value flag missing", errors)
    require(t014.get("required_source") is True, "T014 required source semantics missing", errors)
    require(t014.get("status_and_alert_roles") is True, "T014 status/alert role proof missing", errors)
    require(t014.get("reduced_motion") is True, "T014 reduced-motion proof missing", errors)
    reflow = t014.get("reflow_320")
    require(isinstance(reflow, dict), "T014 320px reflow evidence missing", errors)
    if isinstance(reflow, dict):
        require(nested(reflow, "viewport", "width") == 320, f"T014 reflow width={nested(reflow, 'viewport', 'width')}", errors)
        require(reflow.get("root_overflow_px") == 0 and reflow.get("body_overflow_px") == 0, "T014 320px overflow", errors)
        require(reflow.get("clipped_required_controls_or_text") == [], "T014 320px clipped content", errors)
        require(reflow.get("unnamed_visible_controls") == [], "T014 320px unnamed controls", errors)
    non_color = t014.get("non_color_safety_meaning")
    require(isinstance(non_color, dict), "T014 non-color safety evidence missing", errors)
    if isinstance(non_color, dict):
        require(non_color.get("review_text") == "Source under review", "T014 review semantic text missing", errors)
        require(non_color.get("block_text") == "Source blocked", "T014 block semantic text missing", errors)
        require(non_color.get("review_role") == "status", "T014 review role mismatch", errors)
        require(non_color.get("block_role") == "alert", "T014 block role mismatch", errors)
    return {"list_states": sorted(observed_list) if isinstance(observed_list, dict) else [], "detail_states_actions": sorted(observed_detail) if isinstance(observed_detail, dict) else [], "viewports": viewport_evidence, "accessibility": {"keyboard": t014.get("keyboard"), "non_color": non_color, "reduced_motion": t014.get("reduced_motion"), "reflow_320": reflow}}


def write_outputs(head: str, plan: dict[str, Any], entries: list[dict[str, Any]], details: dict[str, Any], errors: list[str]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    status = "PASS" if not errors else "FAIL"
    payload = {
        "node": "P08",
        "case_id": "P08-T015",
        "name": "evidence-coherence",
        "status": status,
        "generated_at": now_iso(),
        "implementation_commit": head,
        "driver": "python3 scripts/p08/validate.py --case P08-T015",
        "errors": list(errors),
        "details": {
            "input_evidence_count": len(entries),
            "required_input_evidence_count": 14,
            "same_exact_head": len(entries) == 14 and all(entry.get("implementation_commit") == head for entry in entries),
            "test_plan_cases": len(plan.get("cases", [])) if isinstance(plan, dict) else 0,
            **details,
        },
    }
    T015.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    index = {
        "node": "P08",
        "generated_at": now_iso(),
        "implementation_commit": head,
        "status": status,
        "test_plan_sha256": sha256(PLAN) if PLAN.is_file() else None,
        "input_evidence": entries,
        "coherence_result": {
            "case_id": "P08-T015",
            "path": str(T015.relative_to(ROOT)),
            "sha256": sha256(T015),
            "status": status,
            "implementation_commit": head,
        },
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["P08-T015"])
    parser.parse_args()

    head = exact_head()
    errors: list[str] = []
    plan = validate_test_plan(errors)
    payloads, entries = load_input_cases(head, errors)
    scanner = validate_scanner_chain(head, payloads, errors)
    determinism = validate_determinism(payloads, errors)
    authority = validate_product_authority(payloads, errors)
    browser = validate_browser(payloads, errors)
    details = {"scanner_chain": scanner, "deterministic_downloads": determinism, "authority_chain": authority, "browser_chain": browser}
    write_outputs(head, plan, entries, details, errors)

    if errors:
        for error in errors:
            print(f"P08-T015: {error}")
        return 1
    print(f"P08-T015: PASS — 14/14 exact-head evidence inputs, real QR scanner chain and browser authority coherent for {head}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
