#!/usr/bin/env python3
from integration_common import *

PREFLIGHT = Path(os.environ.get("GOJET_P09_FILEPREFLIGHT", "/tmp/gojet-p09/filepreflight"))
INSTALLER_INDEX = Path(os.environ.get("GOJET_P09_INSTALLER_INDEX", "installer/public/index.php"))
HEALTH_CASES = {"P09-T019", "P09-T020"}


def preflight(address: str, *, dial_timeout: str = "500ms", signature_age: str = "72h"):
    env = os.environ.copy()
    env.update({
        "GOJET_CLAMAV_NETWORK": "tcp",
        "GOJET_CLAMAV_ADDRESS": address,
        "GOJET_CLAMAV_DIAL_TIMEOUT": dial_timeout,
        "GOJET_CLAMAV_SCAN_TIMEOUT": "5s",
        "GOJET_CLAMAV_MAX_SIGNATURE_AGE": signature_age,
    })
    completed = subprocess.run(
        [str(PREFLIGHT)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=12
    )
    try:
        report = json.loads(completed.stdout) if completed.stdout.strip() else {}
    except json.JSONDecodeError as exc:
        raise Failure(f"filepreflight returned invalid JSON rc={completed.returncode}") from exc
    return completed.returncode, report, completed.stdout, completed.stderr


def installer_state(address: str):
    env = os.environ.copy()
    env.update({
        "GOJET_FILE_PREFLIGHT_BIN": str(PREFLIGHT),
        "GOJET_CLAMAV_NETWORK": "tcp",
        "GOJET_CLAMAV_ADDRESS": address,
        "GOJET_CLAMAV_DIAL_TIMEOUT": "500ms",
        "GOJET_CLAMAV_SCAN_TIMEOUT": "5s",
        "GOJET_CLAMAV_MAX_SIGNATURE_AGE": "72h",
        "REQUEST_URI": "/install/environment",
    })
    completed = subprocess.run(
        ["php", str(INSTALLER_INDEX)], env=env, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=12,
    )
    return completed.returncode, completed.stdout, completed.stderr


def assert_secret_safe(text: str) -> None:
    forbidden = [str(STORAGE_ROOT), REAL_CLAMD, os.environ.get("GOJET_MYSQL_DSN", ""), "GOJET_CLAMAV_ADDRESS"]
    expect(not any(value and value in text for value in forbidden), f"private dependency detail leaked: {text[:1000]}")


def case_t019():
    expect(PREFLIGHT.is_file(), f"native filepreflight missing: {PREFLIGHT}")
    expect(os.access(PREFLIGHT, os.X_OK), "native filepreflight is not executable")
    expect(INSTALLER_INDEX.is_file(), f"installer index missing: {INSTALLER_INDEX}")

    healthy_rc, healthy, healthy_out, healthy_err = preflight(REAL_CLAMD)
    expect(healthy_rc == 0, f"healthy preflight rc={healthy_rc} report={healthy} stderr={healthy_err[-500:]}")
    expect(healthy.get("ready") is True and healthy.get("status") == "healthy", healthy)
    expect(healthy.get("storage", {}).get("state") == "healthy" and healthy.get("storage", {}).get("writable") is True, healthy)
    expect(healthy.get("clamav", {}).get("state") == "healthy", healthy)
    expect(healthy.get("clamav", {}).get("engine_version") and healthy.get("clamav", {}).get("signature_version"), healthy)
    assert_secret_safe(healthy_out)

    down_address = f"127.0.0.1:{free_port()}"
    down_rc, down, down_out, _ = preflight(down_address)
    expect(down_rc == 2 and down.get("ready") is False and down.get("status") == "unavailable", (down_rc, down))
    assert_secret_safe(down_out)

    with fault_server("health-timeout", hold_seconds=2.0) as (address, _):
        timeout_rc, timeout_report, timeout_out, _ = preflight(address, dial_timeout="250ms")
    expect(timeout_rc == 2 and timeout_report.get("ready") is False and timeout_report.get("status") == "unavailable", (timeout_rc, timeout_report))
    assert_secret_safe(timeout_out)

    with fault_server("stale") as (address, _):
        stale_rc, stale, stale_out, _ = preflight(address)
    expect(stale_rc == 2 and stale.get("ready") is False and stale.get("status") == "stale", (stale_rc, stale))
    assert_secret_safe(stale_out)

    with fault_server("health-indeterminate") as (address, _):
        ind_rc, indeterminate, ind_out, _ = preflight(address)
    expect(ind_rc == 2 and indeterminate.get("ready") is False and indeterminate.get("status") == "indeterminate", (ind_rc, indeterminate))
    assert_secret_safe(ind_out)

    quarantine = STORAGE_ROOT / "quarantine"
    original_mode = stat.S_IMODE(quarantine.stat().st_mode)
    os.chmod(quarantine, 0o755)
    try:
        perm_rc, permission, perm_out, _ = preflight(REAL_CLAMD)
    finally:
        os.chmod(quarantine, original_mode)
    expect(perm_rc == 2 and permission.get("ready") is False and permission.get("status") == "permission_error", (perm_rc, permission))
    assert_secret_safe(perm_out)

    php_ok_rc, php_ok, php_ok_err = installer_state(REAL_CLAMD)
    expect(php_ok_rc == 0 and 'data-state="step-pass"' in php_ok, f"installer healthy state failed rc={php_ok_rc} stderr={php_ok_err[-500:]}")
    with fault_server("stale") as (address, _):
        php_fail_rc, php_fail, php_fail_err = installer_state(address)
    expect(php_fail_rc == 0 and 'data-state="hard-failure"' in php_fail, f"installer fail-closed state missing rc={php_fail_rc} stderr={php_fail_err[-500:]}")
    assert_secret_safe(php_ok + php_fail)

    return {
        "healthy_exit": healthy_rc,
        "engine_version": healthy["clamav"]["engine_version"],
        "signature_version": healthy["clamav"]["signature_version"],
        "daemon_down_exit": down_rc,
        "timeout_exit": timeout_rc,
        "stale_exit": stale_rc,
        "indeterminate_exit": ind_rc,
        "permission_error_exit": perm_rc,
        "installer_healthy_state": "step-pass",
        "installer_fault_state": "hard-failure",
        "preflight_secret_safe": True,
        "p22_release_closure_claimed": False,
    }


def case_t020():
    headers = {
        "X-GoJet-Test-Actor": "p09-admin",
        "X-GoJet-Test-Admin-Role": "admin",
        "X-Request-ID": f"p09-health-{uuid.uuid4().hex}",
    }
    status, response_headers, raw = request("GET", "/api/admin/platform/storage", headers=headers)
    expect(status == 200, f"admin health status {status}: {raw[:500]!r}")
    report = json.loads(raw)
    expect(report.get("ready") is True and report.get("status") == "healthy", report)
    expect(report.get("storage", {}).get("state") == "healthy" and report.get("storage", {}).get("writable") is True, report)
    expect(report.get("clamav", {}).get("state") == "healthy", report)
    expect(report.get("clamav", {}).get("engine_version") and report.get("clamav", {}).get("signature_version"), report)
    expect(response_headers.get("Cache-Control") == "no-store", response_headers)
    text = raw.decode("utf-8", "replace")
    assert_secret_safe(text)

    viewer_headers = dict(headers)
    viewer_headers["X-GoJet-Test-Admin-Role"] = "viewer"
    denied, _, denied_raw = request("GET", "/api/admin/platform/storage", headers=viewer_headers)
    expect(denied == 403, f"non-admin health status {denied}")
    assert_secret_safe(denied_raw.decode("utf-8", "replace"))

    missing_headers = {"X-GoJet-Test-Admin-Role": "admin"}
    missing, _, missing_raw = request("GET", "/api/admin/platform/storage", headers=missing_headers)
    expect(missing == 403, f"missing actor status {missing}")
    assert_secret_safe(missing_raw.decode("utf-8", "replace"))

    return {
        "authorized_status": status,
        "ready": report["ready"],
        "status": report["status"],
        "storage_state": report["storage"]["state"],
        "storage_writable": report["storage"]["writable"],
        "clamav_state": report["clamav"]["state"],
        "engine_version": report["clamav"]["engine_version"],
        "signature_version": report["clamav"]["signature_version"],
        "viewer_status": denied,
        "missing_actor_status": missing,
        "secret_safe": True,
        "p17_admin_completion_claimed": False,
        "p22_installer_completion_claimed": False,
    }


CASES = {"P09-T019": case_t019, "P09-T020": case_t020}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(HEALTH_CASES))
    args = parser.parse_args()
    errors: list[str] = []
    observations = {}
    try:
        observations = CASES[args.case]()
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    path = write_evidence(args.case, observations, errors)
    print(path)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"{args.case} PASS on {HEAD}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
