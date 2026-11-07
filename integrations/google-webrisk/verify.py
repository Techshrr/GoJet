#!/usr/bin/env python3
"""Live, non-mutating acceptance. Three Google lookups; never reads real key."""
import argparse
import hashlib
import http.client
import json
import os
import ssl
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

from adapter import GoogleLookup, VERSION


def call(endpoint, target, source=None, host_override=None):
    url = urlsplit(endpoint)
    conn = http.client.HTTPSConnection(host_override or url.hostname, url.port or 443,
                                      timeout=15, context=ssl.create_default_context(), source_address=source)
    try:
        conn.request("POST", url.path, json.dumps({"target": target}), {"Content-Type": "application/json"})
        response = conn.getresponse()
        raw = response.read(65537)
        if len(raw) > 65536:
            raise ValueError("response too large")
        if response.status == 403:
            return 403, {}
        return response.status, json.loads(raw)
    finally:
        conn.close()


def verify(endpoint, server_ip, local_service=True):
    checks = {}
    details = {}
    cases = [("clean_lookup", "https://example.com/", "allow", "google-webrisk-no-match"),
             ("malware_lookup", "http://testsafebrowsing.appspot.com/s/malware.html", "block", "google-webrisk-threat")]
    for name, target, expected, signal in cases:
        try:
            status, data = call(endpoint, target)
            checks[name] = (status == 200 and data.get("complete") is True and data.get("verdict") == expected
                            and data.get("signal_code") == signal)
            details[name] = {"http_status": status, "verdict": data.get("verdict"),
                             "signal_code": data.get("signal_code"), "evidence": data.get("evidence")}
        except Exception as error:
            checks[name] = False
            details[name] = {"error_type": type(error).__name__}
    try:
        status, data = call(endpoint, "http://127.0.0.1/")
        checks["invalid_target_refused"] = status == 400 and data.get("complete") is False
    except Exception:
        checks["invalid_target_refused"] = False
    # Invalid test credential, never the owner's key. Exercises Google's real
    # rejection and the same adapter error mapping without changing the daemon.
    status, data = GoogleLookup("invalid-test-key-not-a-credential").check("https://example.com/")
    checks["google_invalid_key_refused"] = (status == 503 and data.get("complete") is False
                                           and data["evidence"].get("upstream_status") in (400, 401, 403))
    details["invalid_key"] = data["evidence"]
    if local_service:
        try:
            status, _ = call(endpoint, "https://example.com/", source=("127.0.0.2", 0))
            checks["nginx_non_allowlisted_source_refused"] = status == 403
        except Exception:
            checks["nginx_non_allowlisted_source_refused"] = False
        try:
            call(endpoint, "https://example.com/", host_override=server_ip)
            checks["wrong_tls_name_refused"] = False
        except ssl.SSLCertVerificationError:
            checks["wrong_tls_name_refused"] = True
        except Exception:
            checks["wrong_tls_name_refused"] = False
        for state in ("is-active", "is-enabled"):
            checks["systemd_" + state] = subprocess.run(
                ["systemctl", "--quiet", state, "gojet-webrisk.service"], capture_output=True).returncode == 0
        pid = subprocess.run(["systemctl", "show", "gojet-webrisk.service", "-p", "MainPID", "--value"],
                             capture_output=True, text=True).stdout.strip()
        try:
            uid_line = next(line for line in Path("/proc/" + str(int(pid)) + "/status").read_text().splitlines() if line.startswith("Uid:"))
            checks["non_root_service"] = all(int(uid) != 0 for uid in uid_line.split()[1:])
        except Exception:
            checks["non_root_service"] = False
        probe = Path("/opt/gojet-integrations/google-webrisk") / VERSION / "gojet-webrisk-probe"
        try:
            result = subprocess.run(["runuser", "-u", "gojet", "--", str(probe), "--endpoint", endpoint],
                                    capture_output=True, text=True, timeout=70)
            consumer = json.loads(result.stdout)
            checks["original_go_client_and_policy"] = result.returncode == 0 and consumer.get("passed") is True
            details["go_consumer"] = consumer
        except Exception as error:
            checks["original_go_client_and_policy"] = False
            details["go_consumer"] = {"error_type": type(error).__name__}
    return {"schema": "gojet.google-webrisk.acceptance.v1", "checked_at": datetime.now(timezone.utc).isoformat(),
            "adapter_version": VERSION, "endpoint": endpoint, "passed": all(checks.values()),
            "checks": checks, "details": details, "business_installation_complete": False,
            "scope": "Google Lookup + adapter + TLS/ACL/systemd; not eight-service application acceptance"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--server-ip", required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    report = verify("https://" + args.host + ":9443/v1/check", args.server_ip)
    text = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
    if args.output:
        # New evidence file only, never silently replace a prior acceptance.
        fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as file:
            file.write(text)
    print(text)
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
