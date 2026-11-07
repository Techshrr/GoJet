#!/usr/bin/env python3
"""Ephemeral GitHub Ubuntu runner only: real Nginx/systemd, no Google calls."""
import http.client
import json
import os
from pathlib import Path
import ssl
import subprocess
import time

import install


def request(context, source=None):
    conn = http.client.HTTPSConnection("localhost", 9443, context=context, timeout=5, source_address=source)
    try:
        conn.request("POST", "/v1/check", json.dumps({"target": "http://127.0.0.1/"}), {"Content-Type": "application/json"})
        response = conn.getresponse()
        return response.status, response.read()
    finally:
        conn.close()


def main():
    if os.geteuid() != 0 or os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("This fixture is restricted to an ephemeral CI runner")
    unit, config = install.UNIT, install.NGINX
    for path in (unit, config, install.ROOT, install.PRIVATE):
        if os.path.lexists(path):
            raise SystemExit("Refusing existing fixture path")
    for directory in (install.ROOT.parent.parent, install.ROOT.parent, install.ROOT):
        install.public_directory(directory)
    install.checked_private_dir()
    install.write_new(install.ROOT / "adapter.py", (install.HERE / "adapter.py").read_bytes())
    key = "fixture-key-not-a-real-credential"
    install.write_new(install.KEY, key.encode(), 0o600)
    install.write_new(unit, (install.HERE / "gojet-webrisk.service").read_bytes())
    cert = Path("/etc/letsencrypt/live/localhost")
    cert.mkdir(parents=True)
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                    "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost",
                    "-keyout", str(cert / "privkey.pem"), "-out", str(cert / "fullchain.pem")],
                   check=True, capture_output=True)
    install.write_new(config, install.nginx_config("localhost", "127.0.0.1"))
    try:
        install.run("systemd-analyze", "verify", str(unit))
        install.run("nginx", "-t")
        install.run("systemctl", "daemon-reload")
        install.run("systemctl", "enable", "--now", "gojet-webrisk.service")
        install.run("systemctl", "start", "nginx")
        install.run("systemctl", "reload", "nginx")
        context = ssl.create_default_context(cafile=str(cert / "fullchain.pem"))
        for _ in range(40):
            try:
                status, raw = request(context)
                if status == 400:
                    break
            except OSError:
                pass
            time.sleep(0.25)
        else:
            raise RuntimeError("adapter did not serve through Nginx")
        assert json.loads(raw)["complete"] is False
        assert request(context, source=("127.0.0.2", 0))[0] == 403
        install.run("systemctl", "is-active", "gojet-webrisk.service")
        install.run("systemctl", "is-enabled", "gojet-webrisk.service")
        pid = install.run("systemctl", "show", "gojet-webrisk.service", "-p", "MainPID", "--value")
        line = next(x for x in Path("/proc/" + pid + "/status").read_text().splitlines() if x.startswith("Uid:"))
        assert all(int(x) != 0 for x in line.split()[1:])
        assert key not in install.run("journalctl", "-u", "gojet-webrisk.service", "--no-pager")
        print(json.dumps({"native_fixture_passed": True, "nginx_tls": True, "non_allowlisted_source_denied": True,
                          "systemd_credential_loaded": True, "non_root_process": True, "live_google_verified": False}))
    finally:
        subprocess.run(["systemctl", "disable", "--now", "gojet-webrisk.service"], capture_output=True)
        config.unlink()
        subprocess.run(["nginx", "-t"], check=True)
        subprocess.run(["systemctl", "reload", "nginx"], check=True)


if __name__ == "__main__":
    main()
