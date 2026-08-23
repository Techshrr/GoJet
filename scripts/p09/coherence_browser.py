from coherence_common import *

def validate_browser(cases: dict[str, dict[str, Any]], errors: list[str]) -> dict[str, Any]:
    t021, t022, t023, t024, t025 = (details(cases.get(f"P09-T{i:03d}", {})) for i in range(21, 26))
    req(t021.get("loading") is True and t021.get("empty") is True and t021.get("uploading") is True and t021.get("fake_success_before_server_confirmation") is False and t021.get("quota_status") == 429, f"T021 route-backed list states drift: {t021}", errors)
    req(set(t021.get("authoritative_states", [])) == set(SAFETY), f"T021 five-state set drift: {t021.get('authoritative_states')}", errors)
    req((t022.get("detail_route_state"), t022.get("publish_status"), t022.get("public_password_state"), t022.get("preauth_binary_status"), t022.get("authorized_binary_status"), t022.get("rescan_public_state"), t022.get("rescan_binary_status"), t022.get("blocked_public_state"), t022.get("blocked_binary_status")) == ("safe", 200, "password-required", 403, 200, "scan-pending", 403, "blocked", 403), f"T022 public authority drift: {t022}", errors)
    canonical = t023.get("canonical_viewports")
    req(isinstance(canonical, dict) and set(canonical) == set(CANONICAL_VIEWPORTS), f"T023 viewport set drift: {canonical}", errors)
    declared: set[Any] = set()
    if isinstance(canonical, dict):
        for name, expected in CANONICAL_VIEWPORTS.items():
            record = canonical.get(name, {})
            req(record.get("viewport") == expected, f"T023 {name} viewport drift: {record.get('viewport')}", errors)
            for surface in ("workspace", "public"):
                layout = record.get(surface, {})
                req(layout.get("root_overflow_px") == 0 and layout.get("body_overflow_px") == 0 and layout.get("clipped_required_controls_or_text") == [] and layout.get("overflowing_elements") == [], f"T023 {name} {surface} layout failure: {layout}", errors)
            declared.update([record.get("workspace_capture"), record.get("public_capture")])
    req(t023.get("root_body_overflow_zero") is True and t023.get("clipped_required_content") is False, "T023 summary drift", errors)
    req(t024.get("admin_route") == "/admin/platform/storage" and t024.get("admin_state") == "healthy" and t024.get("fault_installer") == {"status": 503, "state": "hard-failure"} and t024.get("p17_completion_claimed") is False and t024.get("p22_completion_claimed") is False and t024.get("private_dependency_detail_leaked") is False, f"T024 handoff drift: {t024}", errors)
    installer = t024.get("installer", {})
    req(set(installer) == {"/install/environment", "/install/services", "/install/health"} and all(value == {"status": 200, "state": "step-pass"} for value in installer.values()), f"T024 installer routes drift: {installer}", errors)
    req(t025.get("viewport") == {"width": 320, "height": 800} and t025.get("reduced_motion") is True and t025.get("color_only_safety_meaning") is False, f"T025 accessibility summary drift: {t025}", errors)
    req(isinstance(t025.get("keyboard_tabs_to_publish"), int) and 1 <= t025["keyboard_tabs_to_publish"] <= 60 and nested(t025, "focus", "active") is True, f"T025 keyboard/focus drift: {t025.get('focus')}", errors)
    states = t025.get("safety_states", {})
    req(set(states) == set(SAFETY), f"T025 safety state set drift: {states.keys() if isinstance(states, dict) else states}", errors)
    if isinstance(states, dict):
        for state, (icon, headline) in SAFETY.items():
            record = states.get(state, {})
            req(record.get("icon") == icon and record.get("headline") == headline and bool(record.get("reason")), f"T025 {state} non-color evidence drift: {record}", errors)
            layout = record.get("layout", {})
            req(layout.get("root_overflow_px") == 0 and layout.get("body_overflow_px") == 0 and layout.get("clipped_required_controls_or_text") == [] and layout.get("overflowing_elements") == [], f"T025 {state} 320px layout drift: {layout}", errors)
    list_layout = t025.get("list_layout", {})
    req(list_layout.get("root_overflow_px") == 0 and list_layout.get("body_overflow_px") == 0 and list_layout.get("clipped_required_controls_or_text") == [] and list_layout.get("overflowing_elements") == [], f"T025 320px list layout drift: {list_layout}", errors)
    return {"declared_t023_captures": declared}


def validate_captures(browser_info: dict[str, Any], errors: list[str]) -> list[dict[str, Any]]:
    existing = {path.name for path in CAPTURES.glob("*.png") if path.is_file()}
    req(browser_info.get("declared_t023_captures") == {name for name in EXPECTED_CAPTURES if name.startswith("P09-T023-")}, f"T023 declared capture manifest drift: {browser_info.get('declared_t023_captures')}", errors)
    req(EXPECTED_CAPTURES <= existing, f"missing canonical captures: {sorted(EXPECTED_CAPTURES - existing)}", errors)
    output: list[dict[str, Any]] = []
    for name in sorted(EXPECTED_CAPTURES):
        path = CAPTURES / name
        if path.is_file():
            req(path.stat().st_size > 0, f"empty capture {name}", errors)
            output.append({"path": str(path.relative_to(ROOT)), "bytes": path.stat().st_size, "sha256": digest(path)})
    return output

